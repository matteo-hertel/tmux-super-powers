package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/agentlog"
)

const (
	AgentProviderClaude   = "claude"
	AgentProviderCodex    = "codex"
	AgentProviderFallback = "fallback"

	AgentConfidenceHigh   = "high"
	AgentConfidenceMedium = "medium"
	AgentConfidenceLow    = "low"
)

// AgentRun is the durable identity for one controllable agent pane.
type AgentRun struct {
	ID             string    `json:"id"`
	Provider       string    `json:"provider"`
	SessionName    string    `json:"sessionName"`
	TmuxSession    string    `json:"tmuxSession,omitempty"`
	PaneIndex      int       `json:"paneIndex"`
	PID            int       `json:"pid,omitempty"`
	CWD            string    `json:"cwd,omitempty"`
	LogPath        string    `json:"logPath,omitempty"`
	Status         string    `json:"status"`
	StartedAt      time.Time `json:"startedAt"`
	LastSeenAt     time.Time `json:"lastSeenAt"`
	Confidence     string    `json:"confidence"`
	AgentSessionID string    `json:"agentSessionId,omitempty"`
}

// ObservedAgentRun is a monitor observation used to create or refresh a run.
type ObservedAgentRun struct {
	Provider       string
	SessionName    string
	PaneIndex      int
	PID            int
	CWD            string
	LogPath        string
	Status         string
	Confidence     string
	AgentSessionID string
}

type agentRunFile struct {
	Runs []AgentRun `json:"runs"`
}

// AgentRunRegistry stores agent runs in memory and persists them as JSON.
type AgentRunRegistry struct {
	mu   sync.RWMutex
	path string
	runs map[string]AgentRun
}

func NewAgentRunRegistry(path string) (*AgentRunRegistry, error) {
	reg := &AgentRunRegistry{
		path: path,
		runs: make(map[string]AgentRun),
	}
	if path == "" {
		return reg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return nil, err
	}
	var stored agentRunFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	for _, run := range stored.Runs {
		if run.ID == "" {
			continue
		}
		if run.TmuxSession == "" {
			run.TmuxSession = run.SessionName
		}
		reg.runs[run.ID] = run
	}
	return reg, nil
}

func (r *AgentRunRegistry) UpsertObserved(obs ObservedAgentRun, now time.Time) (AgentRun, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	obs = normalizeObservedRun(obs)

	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.findMatchingLocked(obs)
	if id == "" {
		id = newAgentRunID()
		r.runs[id] = AgentRun{
			ID:          id,
			StartedAt:   now,
			SessionName: obs.SessionName,
			TmuxSession: obs.SessionName,
			PaneIndex:   obs.PaneIndex,
		}
	}

	run := r.runs[id]
	run.Provider = obs.Provider
	run.SessionName = obs.SessionName
	run.TmuxSession = obs.SessionName
	run.PaneIndex = obs.PaneIndex
	run.PID = obs.PID
	run.CWD = obs.CWD
	if obs.LogPath != "" || run.LogPath == "" {
		run.LogPath = obs.LogPath
	}
	run.Status = obs.Status
	run.LastSeenAt = now
	run.Confidence = obs.Confidence
	run.AgentSessionID = obs.AgentSessionID
	r.runs[id] = run

	return run, r.saveLocked()
}

func normalizeObservedRun(obs ObservedAgentRun) ObservedAgentRun {
	if obs.Provider == "" {
		obs.Provider = AgentProviderFallback
	}
	if obs.Status == "" {
		obs.Status = "active"
	}
	if obs.Confidence == "" {
		obs.Confidence = AgentConfidenceLow
	}
	return obs
}

func (r *AgentRunRegistry) findMatchingLocked(obs ObservedAgentRun) string {
	var best AgentRun
	for _, run := range r.runs {
		if run.SessionName != obs.SessionName || run.PaneIndex != obs.PaneIndex {
			continue
		}
		if obs.PID != 0 && run.PID != obs.PID {
			continue
		}
		if obs.PID == 0 && run.Provider != obs.Provider {
			continue
		}
		if best.ID == "" || run.LastSeenAt.After(best.LastSeenAt) {
			best = run
		}
	}
	return best.ID
}

func (r *AgentRunRegistry) MarkUnseenStopped(seen map[string]bool, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for id, run := range r.runs {
		if seen[id] {
			continue
		}
		if run.Status == "stopped" {
			continue
		}
		run.Status = "stopped"
		r.runs[id] = run
		changed = true
	}
	if !changed {
		return nil
	}
	return r.saveLocked()
}

func (r *AgentRunRegistry) Find(id string) (AgentRun, bool) {
	if r == nil {
		return AgentRun{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[id]
	return run, ok
}

func (r *AgentRunRegistry) FindByPane(sessionName string, paneIndex int) (AgentRun, bool) {
	if r == nil {
		return AgentRun{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best AgentRun
	for _, run := range r.runs {
		if run.SessionName != sessionName || run.PaneIndex != paneIndex {
			continue
		}
		if run.Status == "stopped" && best.ID != "" && best.Status != "stopped" {
			continue
		}
		if best.ID == "" || run.LastSeenAt.After(best.LastSeenAt) {
			best = run
		}
	}
	return best, best.ID != ""
}

func (r *AgentRunRegistry) List() []AgentRun {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	runs := make([]AgentRun, 0, len(r.runs))
	for _, run := range r.runs {
		runs = append(runs, run)
	}
	sortRuns(runs)
	return runs
}

func (r *AgentRunRegistry) ListBySession(sessionName string) []AgentRun {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var runs []AgentRun
	for _, run := range r.runs {
		if run.SessionName == sessionName {
			runs = append(runs, run)
		}
	}
	sortRuns(runs)
	return runs
}

func sortRuns(runs []AgentRun) {
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].LastSeenAt.Equal(runs[j].LastSeenAt) {
			if runs[i].SessionName == runs[j].SessionName {
				return runs[i].PaneIndex < runs[j].PaneIndex
			}
			return runs[i].SessionName < runs[j].SessionName
		}
		return runs[i].LastSeenAt.After(runs[j].LastSeenAt)
	})
}

func (r *AgentRunRegistry) saveLocked() error {
	if r.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return err
	}
	runs := make([]AgentRun, 0, len(r.runs))
	for _, run := range r.runs {
		runs = append(runs, run)
	}
	sortRuns(runs)
	data, err := json.MarshalIndent(agentRunFile{Runs: runs}, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func newAgentRunID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "run_" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("run_%d", time.Now().UnixNano())
}

func DetectAgentProvider(process string) string {
	process = strings.TrimSpace(filepath.Base(process))
	process = strings.ToLower(process)
	switch process {
	case "claude":
		return AgentProviderClaude
	case "codex":
		return AgentProviderCodex
	}
	if strings.HasPrefix(process, "codex-") {
		return AgentProviderCodex
	}
	if isClaudeVersion(process) {
		return AgentProviderClaude
	}
	return AgentProviderFallback
}

// ResolveAgentLogPath maps an observed provider/cwd/session ID to the best
// known log file. Empty path means pane capture should remain the fallback.
func ResolveAgentLogPath(provider, cwd, agentSessionID string) (string, string) {
	if cwd == "" {
		return "", AgentConfidenceLow
	}
	switch provider {
	case AgentProviderClaude:
		if agentSessionID != "" {
			if sess, ok := agentlog.FindJSONLByID(cwd, agentSessionID); ok {
				return sess.Path, AgentConfidenceHigh
			}
		}
		if path, err := agentlog.FindJSONL(cwd); err == nil {
			return path, AgentConfidenceLow
		}
	case AgentProviderCodex:
		if sess, ok := agentlog.FindCodexJSONL(cwd); ok {
			return sess.Path, AgentConfidenceLow
		}
	}
	return "", AgentConfidenceLow
}
