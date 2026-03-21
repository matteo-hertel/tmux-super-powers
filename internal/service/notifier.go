package service

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

// Notifier watches session state changes via the event bus and sends push notifications.
type Notifier struct {
	monitor     *Monitor
	deviceStore *device.Store
	push        *PushClient
	bus         *Bus

	mu sync.Mutex
	// lastNotified tracks the last status we sent a push notification for,
	// per session. We only send a new notification when the status changes to
	// something DIFFERENT from what we last notified. This prevents spam when
	// status flickers (e.g. done → active → done due to terminal content jitter).
	lastNotified   map[string]string // session name → last notified status
	lastCINotified map[string]string // session name → last notified CI status

	unsub  UnsubscribeFunc
	stopCh chan struct{}
}

// NewNotifier creates a notifier that watches the given monitor via the event bus.
func NewNotifier(monitor *Monitor, deviceStore *device.Store, bus *Bus) *Notifier {
	return &Notifier{
		monitor:        monitor,
		deviceStore:    deviceStore,
		push:           NewPushClient(),
		bus:            bus,
		lastNotified:   make(map[string]string),
		lastCINotified: make(map[string]string),
		stopCh:         make(chan struct{}),
	}
}

// Start begins watching for events.
func (n *Notifier) Start() {
	n.unsub = n.bus.Subscribe(func(e Event) {
		n.handleEvent(e)
	})
}

// Stop stops the notifier.
func (n *Notifier) Stop() {
	if n.unsub != nil {
		n.unsub()
	}
	select {
	case <-n.stopCh:
	default:
		close(n.stopCh)
	}
}

func (n *Notifier) handleEvent(e Event) {
	switch ev := e.(type) {
	case StatusChangedEvent:
		n.onStatusChanged(ev)
	case AgentWaitingEvent:
		n.onAgentWaiting(ev)
	case CIStatusChangedEvent:
		n.onCIStatusChanged(ev)
	case SessionRemovedEvent:
		n.mu.Lock()
		delete(n.lastNotified, ev.Name)
		delete(n.lastCINotified, ev.Name)
		n.mu.Unlock()
	}
}

func (n *Notifier) onStatusChanged(ev StatusChangedEvent) {
	tokens := n.deviceStore.PushTokens()
	if len(tokens) == 0 {
		return
	}

	// Skip if user is attached to this session
	if sessionHasAttachedClient(ev.Session) {
		return
	}

	var msg *PushMessage

	switch ev.To {
	case "done":
		if ev.From != "active" && ev.From != "idle" && ev.From != "waiting" && ev.From != "error" {
			return
		}
		s := n.monitor.FindSession(ev.Session)
		body := "Session completed"
		if s != nil && s.Diff != nil {
			body = fmt.Sprintf("%d files changed, +%d/-%d", s.Diff.Files, s.Diff.Insertions, s.Diff.Deletions)
		}
		msg = &PushMessage{
			Title:      fmt.Sprintf("Agent finished: %s", ev.Session),
			Body:       body,
			Sound:      "default",
			CategoryID: "done",
			Data: map[string]string{
				"type":        "status_change",
				"sessionName": ev.Session,
				"status":      "done",
			},
		}
	}

	if msg == nil {
		return
	}

	n.mu.Lock()
	if n.lastNotified[ev.Session] == ev.To {
		n.mu.Unlock()
		return
	}
	n.lastNotified[ev.Session] = ev.To
	n.mu.Unlock()

	n.sendToAll(tokens, msg)
}

func (n *Notifier) onAgentWaiting(ev AgentWaitingEvent) {
	tokens := n.deviceStore.PushTokens()
	if len(tokens) == 0 {
		return
	}
	if sessionHasAttachedClient(ev.Session) {
		return
	}

	body := "Agent needs your input"
	if ev.Prompt != "" {
		body = ev.Prompt
		if len(body) > 150 {
			body = body[:150]
		}
	}

	n.mu.Lock()
	if n.lastNotified[ev.Session] == "waiting" {
		n.mu.Unlock()
		return
	}
	n.lastNotified[ev.Session] = "waiting"
	n.mu.Unlock()

	msg := &PushMessage{
		Title:      fmt.Sprintf("Input needed: %s", ev.Session),
		Body:       body,
		Sound:      "default",
		Priority:   "high",
		CategoryID: "waiting",
		Data: map[string]string{
			"type":        "status_change",
			"sessionName": ev.Session,
			"status":      "waiting",
		},
	}
	n.sendToAll(tokens, msg)
}

func (n *Notifier) onCIStatusChanged(ev CIStatusChangedEvent) {
	tokens := n.deviceStore.PushTokens()
	if len(tokens) == 0 {
		return
	}

	if ev.To == "fail" {
		n.mu.Lock()
		if n.lastCINotified[ev.Session] == "fail" {
			n.mu.Unlock()
			return
		}
		n.lastCINotified[ev.Session] = "fail"
		n.mu.Unlock()

		msg := &PushMessage{
			To:         "",
			Title:      fmt.Sprintf("CI failing: %s", ev.Session),
			Body:       fmt.Sprintf("PR #%d checks failing", ev.PRNumber),
			Sound:      "default",
			Priority:   "high",
			CategoryID: "error",
			Data: map[string]string{
				"type":        "ci_fail",
				"sessionName": ev.Session,
			},
		}
		n.sendToAll(tokens, msg)
	}

	// When CI recovers, clear so a future failure can re-notify.
	if ev.From == "fail" && ev.To != "fail" {
		n.mu.Lock()
		delete(n.lastCINotified, ev.Session)
		n.mu.Unlock()
	}
}

func (n *Notifier) sendToAll(tokens []string, msg *PushMessage) {
	var messages []PushMessage
	for _, token := range tokens {
		m := *msg
		m.To = token
		messages = append(messages, m)
	}
	go func() {
		if err := n.push.Send(messages); err != nil {
			log.Printf("push notification error: %v", err)
		}
	}()
}

// sessionHasAttachedClient returns true if any tmux client is attached to the session.
func sessionHasAttachedClient(sessionName string) bool {
	out, err := exec.Command("tmux", "list-clients", "-t", sessionName, "-F", "#{client_name}").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}
