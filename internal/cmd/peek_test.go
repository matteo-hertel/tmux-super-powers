package cmd

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPeekModel_QuitOnQ(t *testing.T) {
	m := peekModel{
		sessions: []string{"session1", "session2"},
		cursor:   0,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command, got nil")
	}
}

func TestPeekModel_QuitOnEsc(t *testing.T) {
	m := peekModel{
		sessions: []string{"session1"},
		cursor:   0,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command, got nil")
	}
}

func TestPeekModel_NavigateDown(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2", "s3"},
		cursor:   0,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyDown}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.cursor != 1 {
		t.Errorf("cursor = %d, want 1", pm.cursor)
	}
}

func TestPeekModel_NavigateUp(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2", "s3"},
		cursor:   2,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyUp}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.cursor != 1 {
		t.Errorf("cursor = %d, want 1", pm.cursor)
	}
}

func TestPeekModel_NavigateDownWrap(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2"},
		cursor:   1,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyDown}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (wrapped)", pm.cursor)
	}
}

func TestPeekModel_NavigateUpWrap(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2"},
		cursor:   0,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyUp}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (wrapped)", pm.cursor)
	}
}

func TestPeekModel_EnterSelectsSession(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2"},
		cursor:   1,
		width:    120,
		height:   40,
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.selected != "s2" {
		t.Errorf("selected = %q, want \"s2\"", pm.selected)
	}
}

func TestPeekModel_TabCyclesPane(t *testing.T) {
	m := peekModel{
		sessions:    []string{"s1"},
		cursor:      0,
		previewPane: 0,
		width:       120,
		height:      40,
	}

	msg := tea.KeyMsg{Type: tea.KeyTab}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.previewPane != 1 {
		t.Errorf("previewPane = %d, want 1", pm.previewPane)
	}
}

func TestPeekModel_WindowSizeMsg(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1"},
	}

	msg := tea.WindowSizeMsg{Width: 200, Height: 50}
	newModel, _ := m.Update(msg)
	pm := newModel.(peekModel)

	if pm.width != 200 || pm.height != 50 {
		t.Errorf("size = (%d, %d), want (200, 50)", pm.width, pm.height)
	}
}

func TestPeekModel_ViewRenders(t *testing.T) {
	m := peekModel{
		sessions: []string{"s1", "s2"},
		cursor:   0,
		preview:  "some content",
		width:    120,
		height:   40,
	}

	view := m.View()
	if view == "" {
		t.Error("View() returned empty string")
	}
}
