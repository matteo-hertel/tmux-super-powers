package cmd

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestDashViewKeepsSelectedSessionInsideTerminalHeight(t *testing.T) {
	m := dashModel{
		width:  90,
		height: 14,
		cursor: 24,
	}
	for i := 0; i < 30; i++ {
		m.sessions = append(m.sessions, dashSession{
			name:        fmt.Sprintf("session-%02d", i),
			status:      "active",
			lastChanged: time.Now(),
		})
	}

	view := m.View()
	lines := strings.Split(view, "\n")
	selectedLine := -1
	for i, line := range lines {
		if strings.Contains(line, "session-24") {
			selectedLine = i
			break
		}
	}

	if selectedLine == -1 {
		t.Fatal("selected session was not rendered")
	}
	if selectedLine >= m.height {
		t.Fatalf("selected session rendered below viewport at line %d for terminal height %d", selectedLine, m.height)
	}
	if len(lines) > m.height {
		t.Fatalf("view rendered %d lines for terminal height %d", len(lines), m.height)
	}
}

func TestDashViewBoundsWidePaneContentToTerminalWidth(t *testing.T) {
	m := dashModel{
		width:  80,
		height: 18,
		sessions: []dashSession{{
			name:        "wide-session",
			status:      "active",
			lastChanged: time.Now(),
			paneContent: strings.Repeat("x", 200),
		}},
	}

	for i, line := range strings.Split(m.View(), "\n") {
		if width := lipgloss.Width(line); width > m.width {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, width, m.width, line)
		}
	}
}

func TestDashConfirmDiscardStartsAsyncCleanup(t *testing.T) {
	m := dashModel{
		mode: dashConfirmDiscard,
		sessions: []dashSession{{
			name:         "repo-feature",
			status:       "active",
			lastChanged:  time.Now(),
			isWorktree:   true,
			worktreePath: t.TempDir(),
			gitPath:      t.TempDir(),
			branch:       "feature",
		}},
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got := next.(dashModel)

	if cmd == nil {
		t.Fatal("confirming worktree discard returned nil command; cleanup should run asynchronously")
	}
	if len(got.sessions) != 1 {
		t.Fatalf("session was removed before cleanup completed; got %d sessions", len(got.sessions))
	}
	if got.mode != dashBrowse {
		t.Fatalf("mode = %v, want dashBrowse while cleanup runs", got.mode)
	}
	if !strings.Contains(got.statusMsg, "Cleaning up repo-feature") {
		t.Fatalf("statusMsg = %q, want cleanup progress message", got.statusMsg)
	}
}
