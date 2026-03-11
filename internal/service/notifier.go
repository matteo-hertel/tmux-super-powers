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
	// lastNotified tracks the last status we sent a push notification for,
	// per session. We only send a new notification when the status changes to
	// something DIFFERENT from what we last notified. This prevents spam when
	// status flickers (e.g. done → active → done due to terminal content jitter).
	// Cleared only when the session is removed.
	lastNotified   map[string]string // session name → last notified status
	lastCINotified map[string]string // session name → last notified CI status

	stopCh chan struct{}
}

type sessionSnapshot struct {
	Status   string
	CIStatus string
}

// NewNotifier creates a notifier that watches the given monitor.
func NewNotifier(monitor *Monitor, deviceStore *device.Store) *Notifier {
	return &Notifier{
		monitor:        monitor,
		deviceStore:    deviceStore,
		push:           NewPushClient(),
		prev:           make(map[string]sessionSnapshot),
		lastNotified:   make(map[string]string),
		lastCINotified: make(map[string]string),
		stopCh:         make(chan struct{}),
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
			// Skip notification if user is attached to this tmux session.
			if sessionHasAttachedClient(s.Name) {
				continue
			}
			msg := n.statusMessage(s, prev.Status)
			if msg != nil {
				// Only notify if this is a DIFFERENT status from what we last
				// notified. This prevents spam from status flickering
				// (done → active → done would not re-notify).
				if n.lastNotified[s.Name] == s.Status {
					continue
				}
				n.lastNotified[s.Name] = s.Status
				for _, token := range tokens {
					m := *msg
					m.To = token
					messages = append(messages, m)
				}
			}
		}

		if prev.CIStatus != "fail" && ci == "fail" {
			if n.lastCINotified[s.Name] == "fail" {
				continue
			}
			n.lastCINotified[s.Name] = "fail"
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

		// When CI recovers, clear so a future failure can re-notify.
		if prev.CIStatus == "fail" && ci != "fail" {
			delete(n.lastCINotified, s.Name)
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
			delete(n.lastNotified, name)
			delete(n.lastCINotified, name)
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
