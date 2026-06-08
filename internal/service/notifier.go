package service

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/agentlog"
	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

// Notifier watches session state changes via the event bus and sends push notifications.
type Notifier struct {
	monitor     *Monitor
	deviceStore *device.Store
	push        *PushClient
	bus         *Bus
	agentRuns   *AgentRunRegistry
	questions   *QuestionRegistry

	mu sync.Mutex
	// lastNotified tracks the last status we sent a push notification for,
	// per session. We only send a new notification when the status changes to
	// something DIFFERENT from what we last notified. This prevents spam when
	// status flickers (e.g. done → active → done due to terminal content jitter).
	lastNotified map[string]string // session name → last notified status
	lastWaiting  map[string]waitingNotification

	unsub  UnsubscribeFunc
	stopCh chan struct{}
}

type waitingNotification struct {
	QuestionID string
	Body       string
	NotifiedAt time.Time
}

func (n *Notifier) SetAgentContext(agentRuns *AgentRunRegistry, questions *QuestionRegistry) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.agentRuns = agentRuns
	n.questions = questions
}

// NewNotifier creates a notifier that watches the given monitor via the event bus.
func NewNotifier(monitor *Monitor, deviceStore *device.Store, bus *Bus) *Notifier {
	return &Notifier{
		monitor:      monitor,
		deviceStore:  deviceStore,
		push:         NewPushClient(),
		bus:          bus,
		lastNotified: make(map[string]string),
		lastWaiting:  make(map[string]waitingNotification),
		stopCh:       make(chan struct{}),
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
	case SessionRemovedEvent:
		n.mu.Lock()
		delete(n.lastNotified, ev.Name)
		n.mu.Unlock()
		n.clearWaitingNotifications(ev.Name)
	}
}

func (n *Notifier) onStatusChanged(ev StatusChangedEvent) {
	if shouldClearWaitingNotifications(ev.From, ev.To) {
		n.clearWaitingNotifications(ev.Session)
	}

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
		run, hasRun := n.latestRunForSession(ev.Session)
		if !shouldNotifyDone(ev.From, run, hasRun, time.Now().UTC()) {
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
		if hasRun {
			msg.Data["runId"] = run.ID
			msg.Data["paneIndex"] = strconv.Itoa(run.PaneIndex)
			msg.Data["provider"] = run.Provider
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

const doneNotificationRunWindow = 5 * time.Minute

func shouldNotifyDone(from string, run AgentRun, hasRun bool, now time.Time) bool {
	if from != "active" && from != "waiting" && from != "error" {
		return false
	}
	if !hasRun || run.ID == "" || run.LastSeenAt.IsZero() {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.Sub(run.LastSeenAt) <= doneNotificationRunWindow
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
	if prompt := cleanWaitingPrompt(ev.Prompt); prompt != "" {
		body = prompt
		if len(body) > 150 {
			body = body[:150]
		}
	}

	runID := ev.RunID
	provider := ""
	if runID == "" {
		if run, ok := n.runForPane(ev.Session, ev.PaneIndex); ok {
			runID = run.ID
			provider = run.Provider
		}
	} else if run, ok := n.runByID(runID); ok {
		provider = run.Provider
	}
	questionID := ev.QuestionID
	if questionID == "" && runID != "" && n.questions != nil {
		n.refreshQuestionsForRun(runID)
		if q, ok := n.questions.LatestForRun(runID); ok {
			questionID = q.ID
			if prompt := cleanWaitingPrompt(q.Prompt); prompt != "" {
				body = prompt
				if len(body) > 150 {
					body = body[:150]
				}
			}
		}
	}
	if questionID == "" {
		return
	}

	notifyKey := waitingNotificationKey(ev.Session, ev.PaneIndex, runID)
	n.mu.Lock()
	if !shouldSendWaitingNotification(n.lastWaiting, notifyKey, questionID, body, time.Now().UTC()) {
		n.mu.Unlock()
		return
	}
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
			"paneIndex":   strconv.Itoa(ev.PaneIndex),
			"status":      "waiting",
		},
	}
	if runID != "" {
		msg.Data["runId"] = runID
	}
	if provider != "" {
		msg.Data["provider"] = provider
	}
	if questionID != "" {
		msg.Data["questionId"] = questionID
	}
	n.sendToAll(tokens, msg)
}

func shouldClearWaitingNotifications(from, to string) bool {
	if from != "waiting" || to == "waiting" {
		return false
	}
	return to == "done" || to == "error"
}

func waitingNotificationKey(session string, paneIndex int, runID string) string {
	return fmt.Sprintf("%s:%d:%s", session, paneIndex, runID)
}

func shouldSendWaitingNotification(last map[string]waitingNotification, key, questionID, body string, now time.Time) bool {
	if questionID == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if prev, ok := last[key]; ok {
		if prev.QuestionID == questionID {
			return false
		}
	}
	last[key] = waitingNotification{QuestionID: questionID, Body: body, NotifiedAt: now}
	return true
}

func (n *Notifier) clearWaitingNotifications(session string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	prefix := session + ":"
	for key := range n.lastWaiting {
		if strings.HasPrefix(key, prefix) {
			delete(n.lastWaiting, key)
		}
	}
}

func (n *Notifier) runForPane(session string, paneIndex int) (AgentRun, bool) {
	n.mu.Lock()
	reg := n.agentRuns
	n.mu.Unlock()
	if reg == nil {
		return AgentRun{}, false
	}
	return reg.FindByPane(session, paneIndex)
}

func (n *Notifier) runByID(runID string) (AgentRun, bool) {
	n.mu.Lock()
	reg := n.agentRuns
	n.mu.Unlock()
	if reg == nil {
		return AgentRun{}, false
	}
	return reg.Find(runID)
}

func (n *Notifier) refreshQuestionsForRun(runID string) {
	if n.questions == nil {
		return
	}
	run, ok := n.runByID(runID)
	if !ok || run.LogPath == "" {
		return
	}
	resp, err := agentlog.ReadRunLog(agentlog.RunLogRef{
		ID:             run.ID,
		Provider:       run.Provider,
		CWD:            run.CWD,
		LogPath:        run.LogPath,
		AgentSessionID: run.AgentSessionID,
	}, 0)
	if err != nil {
		return
	}
	n.questions.RefreshFromLog(run, resp.Chunks, time.Now().UTC())
}

func (n *Notifier) latestRunForSession(session string) (AgentRun, bool) {
	n.mu.Lock()
	reg := n.agentRuns
	n.mu.Unlock()
	if reg == nil {
		return AgentRun{}, false
	}
	for _, run := range reg.ListBySession(session) {
		if run.Status != "stopped" {
			return run, true
		}
	}
	runs := reg.ListBySession(session)
	if len(runs) == 0 {
		return AgentRun{}, false
	}
	return runs[0], true
}

func (n *Notifier) sendToAll(tokens []string, msg *PushMessage) {
	if msg == nil || !automaticNotifierCategoryAllowed(msg.CategoryID) {
		return
	}
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

func automaticNotifierCategoryAllowed(category string) bool {
	return category == "waiting" || category == "done"
}

// sessionHasAttachedClient returns true if any tmux client is attached to the session.
func sessionHasAttachedClient(sessionName string) bool {
	out, err := exec.Command("tmux", "list-clients", "-t", sessionName, "-F", "#{client_name}").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}
