package service

import (
	"testing"

	"github.com/matteo-hertel/tmux-super-powers/config"
)

func TestWatcherStateTransitions(t *testing.T) {
	bus := NewBus()
	w := NewWatcher(bus, config.WatcherConfig{
		Enabled:       true,
		PollIntervalS: 1,
		MaxCIRetries:  3,
		AutoCleanup:   true,
	})

	// Simulate adding a tracked session
	w.Track("test-session", "feature/test", "/tmp/wt", "/tmp/repo")

	state := w.State("test-session")
	if state != "working" {
		t.Errorf("expected working, got %s", state)
	}

	// Simulate agent finishing
	w.HandleEvent(StatusChangedEvent{Session: "test-session", From: "active", To: "done"})
	state = w.State("test-session")
	if state != "done" {
		t.Errorf("expected done, got %s", state)
	}
}

func TestWatcherSessionRemoval(t *testing.T) {
	bus := NewBus()
	w := NewWatcher(bus, config.WatcherConfig{Enabled: true, PollIntervalS: 1, MaxCIRetries: 3})

	w.Track("test", "branch", "/path", "/repo")
	if w.State("test") != "working" {
		t.Fatal("expected working")
	}

	w.HandleEvent(SessionRemovedEvent{Name: "test"})
	if w.State("test") != "" {
		t.Errorf("expected empty state after removal, got %s", w.State("test"))
	}
}

func TestWatcherIgnoresNonWorktree(t *testing.T) {
	bus := NewBus()
	w := NewWatcher(bus, config.WatcherConfig{Enabled: true, PollIntervalS: 1, MaxCIRetries: 3})

	// Don't track — should be a no-op
	w.HandleEvent(StatusChangedEvent{Session: "untracked", From: "active", To: "done"})
	if w.State("untracked") != "" {
		t.Errorf("expected empty for untracked session")
	}
}

func TestWatcherFixingCIWaitsForDone(t *testing.T) {
	bus := NewBus()
	w := NewWatcher(bus, config.WatcherConfig{Enabled: true, PollIntervalS: 1, MaxCIRetries: 3})

	w.Track("s", "b", "/wt", "/repo")
	ts := w.getTracked("s")
	ts.state = "fixing_ci"
	ts.prNumber = 1

	// Stale "done" event (From == "done") should NOT transition
	w.HandleEvent(StatusChangedEvent{Session: "s", From: "done", To: "done"})
	if w.State("s") != "fixing_ci" {
		t.Errorf("expected fixing_ci after stale done, got %s", w.State("s"))
	}

	// Real "done" event (From != "done") should transition to watching
	w.HandleEvent(StatusChangedEvent{Session: "s", From: "active", To: "done"})
	if w.State("s") != "watching" {
		t.Errorf("expected watching after real done, got %s", w.State("s"))
	}
}
