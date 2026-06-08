package service

import "testing"

func TestNewMonitor(t *testing.T) {
	m := NewMonitor(500, []string{"FAIL"}, `\$\s*$`, nil, NewBus())
	if m == nil {
		t.Fatal("expected non-nil monitor")
	}
	if m.refreshMs != 500 {
		t.Errorf("expected refreshMs 500, got %d", m.refreshMs)
	}
}

func TestMonitorSnapshot(t *testing.T) {
	m := NewMonitor(500, []string{"FAIL"}, `\$\s*$`, nil, NewBus())
	sessions := m.Snapshot()
	if len(sessions) != 0 {
		t.Errorf("expected empty snapshot, got %d sessions", len(sessions))
	}
}

func TestMonitorFindSessionEmpty(t *testing.T) {
	m := NewMonitor(500, nil, "", nil, NewBus())
	s := m.FindSession("nonexistent")
	if s != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestMonitorSubscribeUnsubscribe(t *testing.T) {
	m := NewMonitor(500, nil, "", nil, NewBus())
	ch := m.Subscribe()
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
	m.Unsubscribe(ch)
	// Channel should be closed after unsubscribe
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestShouldEmitWaitingEventOnlyForNewWaitingState(t *testing.T) {
	prev := &Session{
		Panes: []Pane{
			{Index: 1, Status: "waiting", Prompt: "Choose one", AgentRunID: "run_1"},
		},
	}

	if shouldEmitWaitingEvent(prev, Pane{Index: 1, Status: "waiting", Prompt: "Choose one", AgentRunID: "run_1"}) {
		t.Fatal("expected repeated waiting pane to be suppressed")
	}
	if !shouldEmitWaitingEvent(prev, Pane{Index: 1, Status: "waiting", Prompt: "Choose two", AgentRunID: "run_1"}) {
		t.Fatal("expected changed prompt to emit")
	}
	if !shouldEmitWaitingEvent(prev, Pane{Index: 1, Status: "waiting", Prompt: "Choose one", AgentRunID: "run_2"}) {
		t.Fatal("expected changed run to emit")
	}
	if !shouldEmitWaitingEvent(prev, Pane{Index: 2, Status: "waiting", Prompt: "Choose one", AgentRunID: "run_1"}) {
		t.Fatal("expected newly waiting pane to emit")
	}
}
