package service

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

// Notifier watches session state changes and sends push notifications.
type Notifier struct {
	monitor     *Monitor
	deviceStore *device.Store
	push        *PushClient

	mu   sync.Mutex
	prev map[string]sessionSnapshot
	// sent tracks which notification has already been delivered for a session.
	// Key: "sessionName:status" (e.g. "my-task:done").
	// An entry is added when the notification fires and removed only when the
	// session moves to a *different* status, preventing repeated notifications
	// while the status flickers back and forth.
	sent map[string]bool

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
		sent:        make(map[string]bool),
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
			// Status genuinely changed — clear the sent flag for the OLD status
			// so that if it returns to that status later (legitimately) we can
			// notify again.
			oldKey := fmt.Sprintf("%s:%s", s.Name, prev.Status)
			delete(n.sent, oldKey)

			// Skip notification if user is attached to this tmux session.
			if sessionHasAttachedClient(s.Name) {
				continue
			}
			msg := n.statusMessage(s, prev.Status)
			if msg != nil {
				key := fmt.Sprintf("%s:%s", s.Name, s.Status)
				if n.sent[key] {
					// Already notified for this session+status; skip until
					// the session moves away and comes back.
					continue
				}
				n.sent[key] = true
				for _, token := range tokens {
					m := *msg
					m.To = token
					messages = append(messages, m)
				}
			}
		}

		if prev.CIStatus != "fail" && ci == "fail" {
			key := fmt.Sprintf("%s:ci-fail", s.Name)
			if n.sent[key] {
				continue
			}
			n.sent[key] = true
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

		// When CI recovers, clear the ci-fail sent flag so a future failure
		// can trigger a new notification.
		if prev.CIStatus == "fail" && ci != "fail" {
			delete(n.sent, fmt.Sprintf("%s:ci-fail", s.Name))
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
	// Clean sent entries for sessions that no longer exist.
	for key := range n.sent {
		// Keys are "sessionName:status" — extract session name.
		parts := strings.SplitN(key, ":", 2)
		if len(parts) > 0 && !current[parts[0]] {
			delete(n.sent, key)
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
		if prevStatus != "active" && prevStatus != "idle" && prevStatus != "waiting" && prevStatus != "error" {
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

// sessionHasAttachedClient returns true if any tmux client is attached to the session.
func sessionHasAttachedClient(sessionName string) bool {
	out, err := exec.Command("tmux", "list-clients", "-t", sessionName, "-F", "#{client_name}").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}
