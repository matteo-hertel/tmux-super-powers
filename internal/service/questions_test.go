package service

import (
	"testing"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/agentlog"
)

func TestQuestionRegistryRefreshesFromAdapterOutput(t *testing.T) {
	store := NewQuestionRegistry()
	run := AgentRun{
		ID:          "run_123",
		Provider:    AgentProviderClaude,
		SessionName: "repo-task",
		PaneIndex:   1,
	}
	chunks := []agentlog.Chunk{
		{
			Type: "assistant",
			Items: []agentlog.DisplayItem{
				{
					Type: "ask_user",
					Questions: []agentlog.AskUserQuestionData{
						{
							Question:        "Which path?",
							MultiSelect:     true,
							FreeTextAllowed: true,
							Options: []agentlog.AskUserOption{
								{Label: "A", Description: "alpha"},
								{Label: "B", Description: "beta"},
							},
						},
					},
				},
			},
		},
	}

	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	pending := store.RefreshFromLog(run, chunks, now)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending question, got %d", len(pending))
	}
	q := pending[0]
	if q.ID == "" || q.RunID != run.ID || q.SessionName != run.SessionName || q.PaneIndex != 1 {
		t.Fatalf("unexpected question identity: %#v", q)
	}
	if q.Prompt != "Which path?" || !q.MultiSelect || !q.FreeTextAllowed {
		t.Fatalf("unexpected question fields: %#v", q)
	}
	if len(q.Options) != 2 || q.Options[0].Label != "A" || q.Options[1].Description != "beta" {
		t.Fatalf("unexpected options: %#v", q.Options)
	}

	again := store.RefreshFromLog(run, chunks, now.Add(time.Minute))
	if len(again) != 1 || again[0].ID != q.ID {
		t.Fatalf("expected stable question id on refresh, got %#v", again)
	}
}

func TestQuestionRegistryMarksToolResultQuestionsAnswered(t *testing.T) {
	store := NewQuestionRegistry()
	run := AgentRun{
		ID:          "run_123",
		Provider:    AgentProviderClaude,
		SessionName: "repo-task",
		PaneIndex:   1,
	}
	chunks := []agentlog.Chunk{
		{
			Type:      "assistant",
			Timestamp: "2026-05-14T12:00:00Z",
			Items: []agentlog.DisplayItem{
				{
					Type:   "ask_user",
					Result: "1",
					Questions: []agentlog.AskUserQuestionData{
						{
							Question: "Already answered?",
							Options: []agentlog.AskUserOption{
								{Label: "Yes"},
								{Label: "No"},
							},
						},
					},
				},
			},
		},
	}

	pending := store.RefreshFromLog(run, chunks, time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC))
	if len(pending) != 0 {
		t.Fatalf("expected answered question to be excluded, got %#v", pending)
	}
	if listed := store.ListPending(); len(listed) != 0 {
		t.Fatalf("expected no listed pending questions, got %#v", listed)
	}
}

func TestQuestionRegistryLatestForRunUsesLogOrder(t *testing.T) {
	store := NewQuestionRegistry()
	run := AgentRun{
		ID:          "run_123",
		Provider:    AgentProviderClaude,
		SessionName: "repo-task",
		PaneIndex:   1,
	}
	chunks := []agentlog.Chunk{
		{
			Type: "assistant",
			Items: []agentlog.DisplayItem{
				{
					Type: "ask_user",
					Questions: []agentlog.AskUserQuestionData{
						{Question: "First?"},
					},
				},
				{
					Type: "ask_user",
					Questions: []agentlog.AskUserQuestionData{
						{Question: "Second?"},
					},
				},
			},
		},
	}

	store.RefreshFromLog(run, chunks, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	latest, ok := store.LatestForRun(run.ID)
	if !ok {
		t.Fatal("expected latest question")
	}
	if latest.Prompt != "Second?" {
		t.Fatalf("latest prompt = %q, want Second?", latest.Prompt)
	}
}

func TestBuildAnswerTextFromSelectedOptions(t *testing.T) {
	q := PendingQuestion{
		Options: []QuestionOption{
			{Label: "Use SQLite"},
			{Label: "Use JSON"},
		},
	}
	text, freeText, optionCount, err := BuildQuestionAnswer(q, AnswerQuestionRequest{SelectedOptionIndexes: []int{1}})
	if err != nil {
		t.Fatalf("BuildQuestionAnswer: %v", err)
	}
	if freeText {
		t.Fatal("selected option answer should not be free text")
	}
	if text != "2" {
		t.Fatalf("answer text = %q, want option number 2", text)
	}
	if optionCount != 2 {
		t.Fatalf("option count = %d, want 2", optionCount)
	}
}

func TestBuildAnswerTextFromMultiSelectOptions(t *testing.T) {
	q := PendingQuestion{
		MultiSelect: true,
		Options: []QuestionOption{
			{Label: "D-pad"},
			{Label: "Calibrate"},
			{Label: "RGB"},
		},
	}
	text, freeText, optionCount, err := BuildQuestionAnswer(q, AnswerQuestionRequest{SelectedOptionIndexes: []int{0, 2}})
	if err != nil {
		t.Fatalf("BuildQuestionAnswer: %v", err)
	}
	if freeText {
		t.Fatal("selected option answer should not be free text")
	}
	if text != "1,3" {
		t.Fatalf("answer text = %q, want selected options 1,3", text)
	}
	if optionCount != 3 {
		t.Fatalf("option count = %d, want 3", optionCount)
	}
}

func TestBuildAnswerTextFromFreeText(t *testing.T) {
	q := PendingQuestion{
		FreeTextAllowed: true,
		Options: []QuestionOption{
			{Label: "A"},
			{Label: "Other"},
		},
	}
	text, freeText, optionCount, err := BuildQuestionAnswer(q, AnswerQuestionRequest{Text: "custom answer"})
	if err != nil {
		t.Fatalf("BuildQuestionAnswer: %v", err)
	}
	if !freeText {
		t.Fatal("expected free text answer")
	}
	if text != "custom answer" {
		t.Fatalf("answer text = %q, want custom answer", text)
	}
	if optionCount != 2 {
		t.Fatalf("option count = %d, want 2", optionCount)
	}
}
