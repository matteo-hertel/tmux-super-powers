package service

import "testing"

func TestNewMonitor(t *testing.T) {
	m := NewMonitor(500, []string{"FAIL"}, `\$\s*$`)
	if m == nil {
		t.Fatal("expected non-nil monitor")
	}
	if m.refreshMs != 500 {
		t.Errorf("expected refreshMs 500, got %d", m.refreshMs)
	}
}

func TestMonitorSnapshot(t *testing.T) {
	m := NewMonitor(500, []string{"FAIL"}, `\$\s*$`)
	sessions := m.Snapshot()
	if len(sessions) != 0 {
		t.Errorf("expected empty snapshot, got %d sessions", len(sessions))
	}
}

func TestMonitorFindSessionEmpty(t *testing.T) {
	m := NewMonitor(500, nil, "")
	s := m.FindSession("nonexistent")
	if s != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestMonitorSubscribeUnsubscribe(t *testing.T) {
	m := NewMonitor(500, nil, "")
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
