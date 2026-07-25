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
)

const (
	AgentProviderClaude   = "claude"
	AgentProviderCodex    = "codex"
	AgentProviderFallback = "fallback"
)

// AgentRun is the durable identity for one controllable agent pane.
type AgentRun struct {
	ID           string    `json:"id"`
	ParentRunID  string    `json:"parentRunId,omitempty"`
	Provider     string    `json:"provider"`
	Task         string    `json:"task,omitempty"`
	SessionName  string    `json:"sessionName"`
	PaneIndex    int       `json:"paneIndex"`
	PID          int       `json:"pid,omitempty"`
	CWD          string    `json:"cwd,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	WorktreePath string    `json:"worktreePath,omitempty"`
	GitPath      string    `json:"gitPath,omitempty"`
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"startedAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
	Managed      bool      `json:"managed,omitempty"`
}

// ObservedAgentRun is an on-demand process observation used to create or
// refresh a run.
type ObservedAgentRun struct {
	Provider    string
	SessionName string
	PaneIndex   int
	PID         int
	CWD         string
	Status      string
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
			PaneIndex:   obs.PaneIndex,
		}
	}

	run := r.runs[id]
	run.Provider = obs.Provider
	run.SessionName = obs.SessionName
	run.PaneIndex = obs.PaneIndex
	run.PID = obs.PID
	run.CWD = obs.CWD
	run.Status = obs.Status
	run.LastSeenAt = now
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
	return obs
}

func (r *AgentRunRegistry) findMatchingLocked(obs ObservedAgentRun) string {
	var best AgentRun
	for _, run := range r.runs {
		if run.SessionName != obs.SessionName || run.PaneIndex != obs.PaneIndex {
			continue
		}
		if obs.PID != 0 && run.PID != 0 && run.PID != obs.PID {
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

// RegisterManaged records an agent created by tsp before its process becomes
// observable. A later UpsertObserved call reuses this identity and adds process
// details without losing the task or workspace metadata.
func (r *AgentRunRegistry) RegisterManaged(result SpawnResult, provider string, paneIndex int, now time.Time) (AgentRun, error) {
	if r == nil {
		return AgentRun{}, fmt.Errorf("agent run registry is nil")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if provider == "" {
		provider = AgentProviderFallback
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var id string
	for candidateID, run := range r.runs {
		if run.SessionName == result.Session && run.PaneIndex == paneIndex && run.Managed {
			id = candidateID
			break
		}
	}
	if id == "" {
		id = newAgentRunID()
	}
	run := r.runs[id]
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	run.ID = id
	run.ParentRunID = ""
	run.Provider = provider
	run.Task = result.Task
	run.SessionName = result.Session
	run.PaneIndex = paneIndex
	run.CWD = result.WorktreePath
	run.Branch = result.Branch
	run.WorktreePath = result.WorktreePath
	run.GitPath = result.GitPath
	run.Status = "starting"
	run.LastSeenAt = now
	run.Managed = true
	r.runs[id] = run

	return run, r.saveLocked()
}

// RegisterDelegated records an agent that continues work in an existing
// managed workspace. Delegated runs share their parent's files and branch; they
// never own or remove that workspace themselves.
func (r *AgentRunRegistry) RegisterDelegated(result SpawnResult, provider, parentRunID string, paneIndex int, now time.Time) (AgentRun, error) {
	if r == nil {
		return AgentRun{}, fmt.Errorf("agent run registry is nil")
	}
	if parentRunID == "" {
		return AgentRun{}, fmt.Errorf("parent agent run is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if provider == "" {
		provider = AgentProviderFallback
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[parentRunID]; !ok {
		return AgentRun{}, fmt.Errorf("parent agent run %q was not found", parentRunID)
	}

	id := newAgentRunID()
	run := AgentRun{
		ID:           id,
		ParentRunID:  parentRunID,
		Provider:     provider,
		Task:         result.Task,
		SessionName:  result.Session,
		PaneIndex:    paneIndex,
		CWD:          result.WorktreePath,
		Branch:       result.Branch,
		WorktreePath: result.WorktreePath,
		GitPath:      result.GitPath,
		Status:       "starting",
		StartedAt:    now,
		LastSeenAt:   now,
		Managed:      true,
	}
	r.runs[id] = run

	return run, r.saveLocked()
}

// Descendants returns every run delegated directly or indirectly from id,
// children before parents. This order lets callers stop child sessions safely
// before removing an owning workspace.
func (r *AgentRunRegistry) Descendants(id string) []AgentRun {
	if r == nil || id == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	children := make(map[string][]AgentRun)
	for _, run := range r.runs {
		if run.ParentRunID != "" {
			children[run.ParentRunID] = append(children[run.ParentRunID], run)
		}
	}
	var descendants []AgentRun
	seen := map[string]bool{id: true}
	var walk func(string)
	walk = func(parentID string) {
		for _, child := range children[parentID] {
			if seen[child.ID] {
				continue
			}
			seen[child.ID] = true
			walk(child.ID)
			descendants = append(descendants, child)
		}
	}
	walk(id)
	return descendants
}

// Delete forgets a managed agent after its session/worktree has been removed.
func (r *AgentRunRegistry) Delete(id string) error {
	if r == nil || id == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[id]; !ok {
		return nil
	}
	delete(r.runs, id)
	return r.saveLocked()
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
