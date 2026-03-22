package service

import (
	"sync"
	"time"
)

// Monitor continuously polls tmux sessions and maintains their state.
type Monitor struct {
	mu            sync.RWMutex
	sessions      []Session
	refreshMs     int
	errorPatterns []string
	promptPattern string
	inputPatterns []string
	subscribers   []chan []Session // kept during migration
	subMu         sync.Mutex
	stopCh        chan struct{}
	bus           *Bus
}

func NewMonitor(refreshMs int, errorPatterns []string, promptPattern string, inputPatterns []string, bus *Bus) *Monitor {
	return &Monitor{
		refreshMs:     refreshMs,
		errorPatterns: errorPatterns,
		promptPattern: promptPattern,
		inputPatterns: inputPatterns,
		stopCh:        make(chan struct{}),
		bus:           bus,
	}
}

func (m *Monitor) Start() { go m.loop() }

func (m *Monitor) Stop() { close(m.stopCh) }

// Snapshot returns a copy of current session states.
func (m *Monitor) Snapshot() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]Session, len(m.sessions))
	copy(cp, m.sessions)
	return cp
}

// FindSession returns a session by name, or nil.
func (m *Monitor) FindSession(name string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.sessions {
		if m.sessions[i].Name == name {
			s := m.sessions[i]
			return &s
		}
	}
	return nil
}

// Subscribe returns a channel that receives session snapshots on every refresh.
func (m *Monitor) Subscribe() chan []Session {
	ch := make(chan []Session, 1)
	m.subMu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (m *Monitor) Unsubscribe(ch chan []Session) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for i, sub := range m.subscribers {
		if sub == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

func (m *Monitor) loop() {
	ticker := time.NewTicker(time.Duration(m.refreshMs) * time.Millisecond)
	defer ticker.Stop()
	m.poll()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

func (m *Monitor) poll() {
	names, err := ListSessions()
	if err != nil || len(names) == 0 {
		m.mu.Lock()
		m.sessions = nil
		m.mu.Unlock()
		m.notify()
		return
	}
	now := time.Now()
	m.mu.Lock()
	existing := make(map[string]*Session)
	for i := range m.sessions {
		existing[m.sessions[i].Name] = &m.sessions[i]
	}
	var updated []Session
	for _, name := range names {
		paneCount := GetPaneCount(name)
		var panes []Pane
		var primaryContent string
		for p := 0; p < paneCount; p++ {
			process := GetPaneProcess(name, p)
			pType := PaneTypeFromProcess(process)
			pane := Pane{Index: p, Type: pType, Process: process}
			if pType != "editor" {
				content := CapturePaneContent(name, p)
				pane.Content = content
				if primaryContent == "" {
					primaryContent = content
				}
			}
			// For agent panes, resolve the JSONL session ID (cached from prev cycle)
			if pType == "agent" {
				if prev, ok := existing[name]; ok {
					for _, pp := range prev.Panes {
						if pp.Index == p && pp.AgentSessionID != "" {
							pane.AgentSessionID = pp.AgentSessionID
							break
						}
					}
				}
				// Resolve if not cached (first discovery or process restarted)
				if pane.AgentSessionID == "" {
					pane.AgentSessionID = GetAgentSessionID(name, p)
				}
			}
			panes = append(panes, pane)
		}
		s := Session{Name: name, Panes: panes, LastChanged: now}
		if prev, ok := existing[name]; ok {
			s.LastChanged = prev.LastChanged
			s.PrevContent = prev.PrevContent
			s.Branch = prev.Branch
			s.IsWorktree = prev.IsWorktree
			s.IsGitRepo = prev.IsGitRepo
			s.GitPath = prev.GitPath
			s.WorktreePath = prev.WorktreePath
			s.Dir = prev.Dir
			s.Diff = prev.Diff
			s.PR = prev.PR
			if primaryContent != prev.PrevContent {
				s.LastChanged = now
			}
			s.PrevContent = primaryContent
			s.Status = InferStatus(prev.PrevContent, primaryContent, s.LastChanged, now, m.errorPatterns, m.promptPattern)
		} else {
			info := DetectSessionGitInfoFull(name)
			s.Dir = info.Cwd
			if info.GitPath != "" {
				s.IsGitRepo = true
				s.GitPath = info.GitPath
				s.Branch = info.Branch
				s.IsWorktree = info.IsWorktree
				s.WorktreePath = info.WorktreePath
			}
			s.PrevContent = primaryContent
			s.Status = "active"
		}
		for i := range s.Panes {
			if s.Panes[i].Type != "editor" {
				s.Panes[i].Status = s.Status
			}
		}
		// Detect per-pane waiting for input (only mark the specific pane that's waiting).
		if s.Status != "error" && s.Status != "done" {
			waitingPanes := DetectWaitingPanes(s.Panes, m.inputPatterns)
			if len(waitingPanes) > 0 {
				s.Status = "waiting"
				waitingIdx := make(map[int]string)
				for _, wp := range waitingPanes {
					waitingIdx[wp.Index] = wp.Prompt
				}
				for i := range s.Panes {
					if prompt, ok := waitingIdx[s.Panes[i].Index]; ok {
						s.Panes[i].Status = "waiting"
						s.Panes[i].Prompt = prompt
					}
				}
			}
		}
		updated = append(updated, s)
	}
	// Collect events to publish AFTER releasing the lock (prevents deadlock
	// since event handlers may call FindSession/Snapshot which need RLock).
	var events []Event
	prevNames := make(map[string]bool)
	for name := range existing {
		prevNames[name] = true
	}
	for _, s := range updated {
		if !prevNames[s.Name] {
			events = append(events, SessionCreatedEvent{Name: s.Name})
		}
		if prev, ok := existing[s.Name]; ok {
			if prev.Status != s.Status {
				events = append(events, StatusChangedEvent{Session: s.Name, From: prev.Status, To: s.Status})
			}
			if s.Status == "waiting" {
				for _, p := range s.Panes {
					if p.Status == "waiting" {
						events = append(events, AgentWaitingEvent{Session: s.Name, PaneIndex: p.Index, Prompt: p.Prompt})
					}
				}
			}
			// Detect agent crash: pane was agent, now shell
			for _, p := range s.Panes {
				for _, pp := range prev.Panes {
					if pp.Index == p.Index && pp.Type == "agent" && p.Type == "shell" {
						events = append(events, AgentCrashedEvent{Session: s.Name, PaneIndex: p.Index, PrevProcess: pp.Process})
					}
				}
			}
			// Detect agent stuck: agent pane unchanged for >5 minutes while session is "idle"
			if s.Status == "idle" {
				idleDuration := now.Sub(s.LastChanged)
				if idleDuration > 5*time.Minute {
					for _, p := range s.Panes {
						if p.Type == "agent" {
							events = append(events, AgentStuckEvent{Session: s.Name, PaneIndex: p.Index, IdleDuration: idleDuration})
						}
					}
				}
			}
		}
	}
	// Detect removed sessions
	currentNames := make(map[string]bool)
	for _, s := range updated {
		currentNames[s.Name] = true
	}
	for name := range existing {
		if !currentNames[name] {
			events = append(events, SessionRemovedEvent{Name: name})
		}
	}

	m.sessions = updated
	m.mu.Unlock()
	m.notify() // keep channel notify during migration

	// Publish events outside the lock
	for _, e := range events {
		m.bus.Publish(e)
	}
}

func (m *Monitor) notify() {
	snapshot := m.Snapshot()
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for _, ch := range m.subscribers {
		select {
		case ch <- snapshot:
		default:
		}
	}
}
