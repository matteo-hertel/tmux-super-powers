package service

import (
	"testing"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/device"
)

func TestWaitingNotificationRequiresSemanticQuestionID(t *testing.T) {
	last := make(map[string]waitingNotification)
	now := time.Date(2026, 5, 14, 20, 0, 0, 0, time.UTC)
	key := "repo-task:1:run_123"

	if shouldSendWaitingNotification(last, key, "", "Agent needs your input", now) {
		t.Fatal("expected terminal-only waiting state to be suppressed")
	}

	if len(last) != 0 {
		t.Fatal("expected terminal-only waiting state to stay out of dedupe state")
	}

	if !shouldSendWaitingNotification(last, key, "q_123", "Full semantic question text", now.Add(10*time.Second)) {
		t.Fatal("expected semantic question to notify")
	}
	if shouldSendWaitingNotification(last, key, "q_123", "Full semantic question text", now.Add(20*time.Second)) {
		t.Fatal("expected repeated semantic question to be suppressed")
	}
	if !shouldSendWaitingNotification(last, key, "q_456", "Full semantic question text", now.Add(30*time.Second)) {
		t.Fatal("expected different semantic question id to notify even with the same prompt text")
	}
}

func TestWaitingNotificationIgnoresTerminalOnlyNavigationPrompts(t *testing.T) {
	last := make(map[string]waitingNotification)
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	key := "repo-task:1:run_123"

	first := "\x1b[38;5;153mPress Enter to select, press up and down to navigate\x1b[0m"
	second := "\x1b[38;5;154mPress Enter to select, press up and down to navigate\x1b[0m"

	if shouldSendWaitingNotification(last, key, "", first, now) {
		t.Fatal("expected terminal-only navigation prompt to be suppressed")
	}
	if shouldSendWaitingNotification(last, key, "", second, now.Add(500*time.Millisecond)) {
		t.Fatal("expected styled terminal-only navigation prompt to be suppressed")
	}
}

func TestAutomaticNotifierCategoryAllowedOnlyInputAndDone(t *testing.T) {
	for _, category := range []string{"waiting", "done"} {
		if !automaticNotifierCategoryAllowed(category) {
			t.Fatalf("expected automatic notification category %q to be allowed", category)
		}
	}
	for _, category := range []string{"", "error", "ci_fail", "notify"} {
		if automaticNotifierCategoryAllowed(category) {
			t.Fatalf("expected automatic notification category %q to be blocked", category)
		}
	}
}

func TestWaitingNotificationStateSurvivesTransientActiveStatus(t *testing.T) {
	n := NewNotifier(NewMonitor(500, nil, "", nil, NewBus()), device.NewStore(t.TempDir()+"/devices.json"), NewBus())
	n.lastWaiting["session:1:run_123"] = waitingNotification{
		Body:       "Agent needs your input",
		NotifiedAt: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
	}

	n.onStatusChanged(StatusChangedEvent{Session: "session", From: "waiting", To: "active"})

	if _, ok := n.lastWaiting["session:1:run_123"]; !ok {
		t.Fatal("expected transient waiting -> active status to keep waiting dedupe state")
	}
}

func TestShouldNotifyDoneIgnoresIdleToDone(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	run := AgentRun{
		ID:         "run_123",
		LastSeenAt: now.Add(-time.Minute),
		Status:     "stopped",
	}

	if shouldNotifyDone("idle", run, true, now) {
		t.Fatal("idle sessions should not produce done notifications")
	}
	for _, from := range []string{"active", "waiting", "error"} {
		if !shouldNotifyDone(from, run, true, now) {
			t.Fatalf("expected %s -> done to notify", from)
		}
	}
}

func TestShouldNotifyDoneRequiresRecentAgentRun(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

	if shouldNotifyDone("active", AgentRun{}, false, now) {
		t.Fatal("done notification should require an agent run")
	}
	stale := AgentRun{
		ID:         "run_old",
		LastSeenAt: now.Add(-doneNotificationRunWindow - time.Second),
		Status:     "stopped",
	}
	if shouldNotifyDone("active", stale, true, now) {
		t.Fatal("stale agent runs should not produce done notifications")
	}
	recent := AgentRun{
		ID:         "run_recent",
		LastSeenAt: now.Add(-doneNotificationRunWindow + time.Second),
		Status:     "stopped",
	}
	if !shouldNotifyDone("active", recent, true, now) {
		t.Fatal("recently observed agent run should produce done notification")
	}
}
