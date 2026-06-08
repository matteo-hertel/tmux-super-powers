package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/service"
)

func TestHandleSessionAgentRunsReturnsRegistryRuns(t *testing.T) {
	srv := newTestServer()
	reg, err := service.NewAgentRunRegistry(filepath.Join(t.TempDir(), "agent-runs.json"))
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	run, err := reg.UpsertObserved(service.ObservedAgentRun{
		Provider:    service.AgentProviderCodex,
		SessionName: "repo-task",
		PaneIndex:   1,
		PID:         42,
		CWD:         "/repo",
		Status:      "active",
		Confidence:  "high",
	}, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("UpsertObserved: %v", err)
	}
	srv.agentRuns = reg

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{name}/agent-runs", srv.handleSessionAgentRuns)

	req := httptest.NewRequest("GET", "/api/sessions/repo-task/agent-runs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Runs []service.AgentRun `json:"runs"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Runs) != 1 || body.Runs[0].ID != run.ID {
		t.Fatalf("unexpected runs: %#v", body.Runs)
	}
}

func TestHandleAgentRunLogReadsRegisteredRun(t *testing.T) {
	srv := newTestServer()
	logPath := filepath.Join(t.TempDir(), "codex.jsonl")
	data := `{"timestamp":"2026-05-14T10:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ready"}]}}` + "\n"
	if err := os.WriteFile(logPath, []byte(data), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	run, err := srv.agentRuns.UpsertObserved(service.ObservedAgentRun{
		Provider:    service.AgentProviderCodex,
		SessionName: "repo-task",
		PaneIndex:   1,
		LogPath:     logPath,
		Status:      "active",
		Confidence:  "high",
	}, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("UpsertObserved: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agent-runs/{runId}/log", srv.handleGetAgentRunLog)
	req := httptest.NewRequest("GET", "/api/agent-runs/"+run.ID+"/log", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		RunID    string `json:"runId"`
		Provider string `json:"provider"`
		Chunks   []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"chunks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RunID != run.ID || body.Provider != service.AgentProviderCodex {
		t.Fatalf("unexpected identity: %#v", body)
	}
	if len(body.Chunks) != 1 || body.Chunks[0].Text != "ready" {
		t.Fatalf("unexpected chunks: %#v", body.Chunks)
	}
}

func TestHandlePendingQuestionsReturnsExtractedQuestions(t *testing.T) {
	srv := newTestServer()
	logPath := filepath.Join(t.TempDir(), "claude.jsonl")
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"ask-1","name":"AskUserQuestion","input":{"questions":[{"question":"Continue?","options":[{"label":"Yes"},{"label":"No"}]}]}}]}}` + "\n"
	if err := os.WriteFile(logPath, []byte(line), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	_, err := srv.agentRuns.UpsertObserved(service.ObservedAgentRun{
		Provider:    service.AgentProviderClaude,
		SessionName: "repo-task",
		PaneIndex:   1,
		LogPath:     logPath,
		Status:      "waiting",
		Confidence:  "high",
	}, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("UpsertObserved: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/questions/pending", srv.handlePendingQuestions)
	req := httptest.NewRequest("GET", "/api/questions/pending", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Questions []service.PendingQuestion `json:"questions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Questions) != 1 || body.Questions[0].Prompt != "Continue?" {
		t.Fatalf("unexpected questions: %#v", body.Questions)
	}
}

func TestLegacyAgentLogUsesRunRegistryWhenAvailable(t *testing.T) {
	srv := newTestServer()
	logPath := filepath.Join(t.TempDir(), "codex.jsonl")
	data := `{"timestamp":"2026-05-14T10:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"from run"}]}}` + "\n"
	if err := os.WriteFile(logPath, []byte(data), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	run, err := srv.agentRuns.UpsertObserved(service.ObservedAgentRun{
		Provider:    service.AgentProviderCodex,
		SessionName: "repo-task",
		PaneIndex:   1,
		LogPath:     logPath,
		Status:      "active",
		Confidence:  "high",
	}, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("UpsertObserved: %v", err)
	}
	srv.monitor.SetSessionsForTest([]service.Session{{Name: "repo-task", Panes: []service.Pane{{Index: 1, Type: "agent", AgentRunID: run.ID}}}})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{name}/agent-log", srv.handleGetAgentLog)
	req := httptest.NewRequest("GET", "/api/sessions/repo-task/agent-log?runId="+run.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		RunID  string `json:"runId"`
		Chunks []struct {
			Text string `json:"text"`
		} `json:"chunks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RunID != run.ID || len(body.Chunks) != 1 || body.Chunks[0].Text != "from run" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestHandleAnswerQuestionTargetsRecordedRun(t *testing.T) {
	srv := newTestServer()
	reg, err := service.NewAgentRunRegistry(filepath.Join(t.TempDir(), "agent-runs.json"))
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	run, err := reg.UpsertObserved(service.ObservedAgentRun{
		Provider:    service.AgentProviderClaude,
		SessionName: "repo-task",
		PaneIndex:   2,
		PID:         42,
		Status:      "waiting",
		Confidence:  "high",
	}, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("UpsertObserved: %v", err)
	}
	questions := service.NewQuestionRegistry()
	question := questions.UpsertPending(service.PendingQuestion{
		RunID:       run.ID,
		SessionName: run.SessionName,
		PaneIndex:   run.PaneIndex,
		Prompt:      "Pick one",
		Options: []service.QuestionOption{
			{Label: "A"},
			{Label: "B"},
		},
		CreatedAt: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
	})
	srv.agentRuns = reg
	srv.questions = questions

	var sentSession string
	var sentPane int
	var sentText string
	srv.sendToPane = func(session string, pane int, text string) error {
		sentSession = session
		sentPane = pane
		sentText = text
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/questions/{questionId}/answer", srv.handleAnswerQuestion)
	body := bytes.NewBufferString(`{"selectedOptionIndexes":[1]}`)
	req := httptest.NewRequest("POST", "/api/questions/"+question.ID+"/answer", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if sentSession != "repo-task" || sentPane != 2 || sentText != "2" {
		t.Fatalf("sent to %s pane %d text %q, want repo-task pane 2 text 2", sentSession, sentPane, sentText)
	}
	got, _ := questions.Find(question.ID)
	if !got.Answered {
		t.Fatal("expected question marked answered")
	}
}
