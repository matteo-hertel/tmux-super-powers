package service

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

// Notifier watches session state changes and sends push notifications.
type Notifier struct {
	monitor     *Monitor
	deviceStore *device.Store
	push        *PushClient

	mu       sync.Mutex
	prev     map[string]sessionSnapshot
	debounce map[string]time.Time

	stopCh chan struct{}
}

type sessionSnapshot struct {
	Status   string
	CIStatus string
}

// NewNotifier creates a notifier that watches the given monitor.
func NewNotifier(monitor *Monitor, deviceStore *device.Store) *Notifier {
	return &Notifier{
		monitor:     monitor,
		deviceStore: deviceStore,
		push:        NewPushClient(),
		prev:        make(map[string]sessionSnapshot),
		debounce:    make(map[string]time.Time),
		stopCh:      make(chan struct{}),
	}
}

// Start begins watching for session state changes.
func (n *Notifier) Start() {
	go n.loop()
}

// Stop stops the notifier.
func (n *Notifier) Stop() {
	close(n.stopCh)
}

func (n *Notifier) loop() {
	ch := n.monitor.Subscribe()
	defer n.monitor.Unsubscribe(ch)

	for {
		select {
		case sessions, ok := <-ch:
			if !ok {
				return
			}
			n.check(sessions)
		case <-n.stopCh:
			return
		}
	}
}

func (n *Notifier) check(sessions []Session) {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()
	tokens := n.deviceStore.PushTokens()
	if len(tokens) == 0 {
		for _, s := range sessions {
			ci := ""
			if s.PR != nil {
				ci = s.PR.CIStatus
			}
			n.prev[s.Name] = sessionSnapshot{Status: s.Status, CIStatus: ci}
		}
		return
	}

	var messages []PushMessage

	for _, s := range sessions {
		ci := ""
		if s.PR != nil {
			ci = s.PR.CIStatus
		}
		snap := sessionSnapshot{Status: s.Status, CIStatus: ci}

		prev, existed := n.prev[s.Name]
		n.prev[s.Name] = snap

		if !existed {
			continue
		}

		if prev.Status != s.Status {
			msg := n.statusMessage(s, prev.Status)
			if msg != nil {
				key := fmt.Sprintf("%s:%s", s.Name, s.Status)
				if last, ok := n.debounce[key]; ok && now.Sub(last) < 30*time.Second {
					continue
				}
				n.debounce[key] = now
				for _, token := range tokens {
					m := *msg
					m.To = token
					messages = append(messages, m)
				}
			}
		}

		if prev.CIStatus != "fail" && ci == "fail" {
			key := fmt.Sprintf("%s:ci-fail", s.Name)
			if last, ok := n.debounce[key]; ok && now.Sub(last) < 30*time.Second {
				continue
			}
			n.debounce[key] = now
			pr := s.PR
			for _, token := range tokens {
				messages = append(messages, PushMessage{
					To:         token,
					Title:      fmt.Sprintf("CI failing: %s", s.Name),
					Body:       fmt.Sprintf("PR #%d checks failing", pr.Number),
					Sound:      "default",
					Priority:   "high",
					CategoryID: "error",
					Data: map[string]string{
						"type":        "ci_fail",
						"sessionName": s.Name,
					},
				})
			}
		}
	}

	// Clean up entries for removed sessions.
	current := make(map[string]bool)
	for _, s := range sessions {
		current[s.Name] = true
	}
	for name := range n.prev {
		if !current[name] {
			delete(n.prev, name)
		}
	}
	for key, t := range n.debounce {
		if now.Sub(t) > 5*time.Minute {
			delete(n.debounce, key)
		}
	}

	if len(messages) > 0 {
		go func() {
			if err := n.push.Send(messages); err != nil {
				log.Printf("push notification error: %v", err)
			}
		}()
	}
}

func (n *Notifier) statusMessage(s Session, prevStatus string) *PushMessage {
	switch s.Status {
	case "done":
		if prevStatus != "active" && prevStatus != "idle" && prevStatus != "waiting" {
			return nil
		}
		body := "Session completed"
		if s.Diff != nil {
			body = fmt.Sprintf("%d files changed, +%d/-%d", s.Diff.Files, s.Diff.Insertions, s.Diff.Deletions)
		}
		return &PushMessage{
			Title:      fmt.Sprintf("Agent finished: %s", s.Name),
			Body:       body,
			Sound:      "default",
			CategoryID: "done",
			Data: map[string]string{
				"type":        "status_change",
				"sessionName": s.Name,
				"status":      "done",
			},
		}

	case "error":
		body := "Agent encountered an error"
		for _, pane := range s.Panes {
			if pane.Type == "agent" && pane.Content != "" {
				lines := strings.Split(strings.TrimRight(pane.Content, "\n"), "\n")
				if len(lines) > 0 {
					body = lines[len(lines)-1]
					if len(body) > 100 {
						body = body[:100]
					}
				}
				break
			}
		}
		return &PushMessage{
			Title:      fmt.Sprintf("Agent error: %s", s.Name),
			Body:       body,
			Sound:      "default",
			Priority:   "high",
			CategoryID: "error",
			Data: map[string]string{
				"type":        "status_change",
				"sessionName": s.Name,
				"status":      "error",
			},
		}

	case "waiting":
		body := "Agent needs your input"
		for _, pane := range s.Panes {
			if pane.Prompt != "" {
				body = pane.Prompt
				if len(body) > 150 {
					body = body[:150]
				}
				break
			}
		}
		return &PushMessage{
			Title:      fmt.Sprintf("Input needed: %s", s.Name),
			Body:       body,
			Sound:      "default",
			Priority:   "high",
			CategoryID: "waiting",
			Data: map[string]string{
				"type":        "status_change",
				"sessionName": s.Name,
				"status":      "waiting",
			},
		}

	default:
		return nil
	}
}
