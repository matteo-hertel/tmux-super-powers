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
	subscribers   []chan []Session
	subMu         sync.Mutex
	stopCh        chan struct{}
}

func NewMonitor(refreshMs int, errorPatterns []string, promptPattern string) *Monitor {
	return &Monitor{
		refreshMs:     refreshMs,
		errorPatterns: errorPatterns,
		promptPattern: promptPattern,
		stopCh:        make(chan struct{}),
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
			s.Diff = prev.Diff
			s.PR = prev.PR
			if primaryContent != prev.PrevContent {
				s.LastChanged = now
			}
			s.PrevContent = primaryContent
			s.Status = InferStatus(prev.PrevContent, primaryContent, s.LastChanged, now, m.errorPatterns, m.promptPattern)
		} else {
			gitPath, branch := DetectSessionGitInfo(name)
			if gitPath != "" {
				s.IsGitRepo = true
				s.GitPath = gitPath
				s.Branch = branch
			}
			s.PrevContent = primaryContent
			s.Status = "active"
		}
		for i := range s.Panes {
			if s.Panes[i].Type != "editor" {
				s.Panes[i].Status = s.Status
			}
		}
		updated = append(updated, s)
	}
	m.sessions = updated
	m.mu.Unlock()
	m.notify()
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
