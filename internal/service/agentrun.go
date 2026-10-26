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
	Provider     string    `json:"provider"`
	Task         string    `json:"task,omitempty"`
	SessionName  string    `json:"sessionName"`
	PaneIndex    int       `json:"paneIndex"`
	PaneID       string    `json:"paneId,omitempty"`
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
	Provider       string
	SessionName    string
	PaneIndex      int
	PaneID         string
	PID            int
	CWD            string
	Status         string
	SessionPaneIDs []string
}

type agentRunFile struct {
	Runs []AgentRun `json:"runs"`
}

// storedAgentRun reads one record from the registry file. Older versions could
// delegate child agents into a parent's worktree; those records carry a parent
// id and must never be mistaken for the run that owns the workspace.
type storedAgentRun struct {
	AgentRun
	LegacyParentRunID string `json:"parentRunId,omitempty"`
}

// readAgentRunFile decodes the registry and drops legacy delegated children.
// Their temporary panes are gone, and as root runs they would claim the
// parent's worktree on cleanup.
func readAgentRunFile(data []byte) ([]AgentRun, error) {
	var stored struct {
		Runs []storedAgentRun `json:"runs"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	runs := make([]AgentRun, 0, len(stored.Runs))
	for _, run := range stored.Runs {
		if run.LegacyParentRunID != "" {
			continue
		}
		runs = append(runs, run.AgentRun)
	}
	return runs, nil
}

// AgentRunRegistry stores agent runs in memory and persists them as JSON.
type AgentRunRegistry struct {
	mu      sync.RWMutex
	path    string
	runs    map[string]AgentRun
	removed map[string]bool
}

func NewAgentRunRegistry(path string) (*AgentRunRegistry, error) {
	reg := &AgentRunRegistry{
		path:    path,
		runs:    make(map[string]AgentRun),
		removed: make(map[string]bool),
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
	stored, err := readAgentRunFile(data)
	if err != nil {
		return nil, err
	}
	for _, run := range MergePaneDuplicates(stored) {
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
	r.syncFromDiskLocked()

	id := r.findMatchingLocked(obs)
	if matched, ok := r.runs[id]; !ok || !matched.Managed {
		if orphan := r.findOrphanedManagedLocked(obs); orphan != "" {
			if ok {
				r.foldRunLocked(id)
			}
			id = orphan
		}
	}
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
	if obs.Provider != AgentProviderFallback || !run.Managed {
		run.Provider = obs.Provider
	}
	run.SessionName = obs.SessionName
	run.PaneIndex = obs.PaneIndex
	if obs.PaneID != "" {
		run.PaneID = obs.PaneID
	}
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
		if run.SessionName != obs.SessionName {
			continue
		}
		if obs.PaneID != "" && run.PaneID == "" && run.Managed && run.Status == "stopped" {
			continue
		}
		samePane := obs.PaneID != "" && run.PaneID == obs.PaneID
		if obs.PaneID != "" && run.PaneID != "" {
			if !samePane {
				continue
			}
		} else if run.PaneIndex != obs.PaneIndex {
			continue
		}
		// A managed agent still running its setup chain looks like a plain
		// shell. Only an unrelated pane may be refused on that basis; an exact
		// pane id is the run's own identity.
		if obs.Provider == AgentProviderFallback && run.Managed && !samePane {
			continue
		}
		if !samePane && obs.PID != 0 && run.PID != 0 && run.PID != obs.PID {
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

func (r *AgentRunRegistry) findOrphanedManagedLocked(obs ObservedAgentRun) string {
	if obs.PaneID == "" || len(obs.SessionPaneIDs) == 0 {
		return ""
	}
	live := make(map[string]bool, len(obs.SessionPaneIDs))
	for _, paneID := range obs.SessionPaneIDs {
		live[paneID] = true
	}
	var best AgentRun
	for _, run := range r.runs {
		if run.SessionName != obs.SessionName || !run.Managed || run.PaneID == "" || live[run.PaneID] {
			continue
		}
		if best.ID == "" || run.StartedAt.After(best.StartedAt) {
			best = run
		}
	}
	return best.ID
}

func (r *AgentRunRegistry) foldRunLocked(from string) {
	delete(r.runs, from)
	r.removed[from] = true
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
	r.syncFromDiskLocked()

	// A dashboard refresh can observe the new pane before this call lands, so
	// adopt any run already holding it instead of minting a second identity.
	var id string
	for candidateID, run := range r.runs {
		if run.SessionName != result.Session {
			continue
		}
		if result.PaneID != "" && run.PaneID != "" {
			if run.PaneID != result.PaneID {
				continue
			}
		} else if run.PaneIndex != paneIndex || !run.Managed {
			continue
		}
		id = candidateID
		break
	}
	if id == "" {
		id = newAgentRunID()
	}
	run := r.runs[id]
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	run.ID = id
	run.Provider = provider
	run.Task = result.Task
	run.SessionName = result.Session
	run.PaneIndex = paneIndex
	run.PaneID = result.PaneID
	run.PID = 0
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
	r.removed[id] = true
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
		if !run.Managed {
			delete(r.runs, id)
			r.removed[id] = true
			changed = true
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

// syncFromDiskLocked folds in runs another tsp process wrote since this one
// loaded the file. Without it a long-lived dashboard overwrites every record
// created by a concurrent `tsp spawn`.
func (r *AgentRunRegistry) syncFromDiskLocked() {
	if r.path == "" {
		return
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	stored, err := readAgentRunFile(data)
	if err != nil {
		return
	}
	for _, run := range stored {
		if run.ID == "" || r.removed[run.ID] {
			continue
		}
		if current, ok := r.runs[run.ID]; ok && !run.LastSeenAt.After(current.LastSeenAt) {
			continue
		}
		r.runs[run.ID] = run
	}
}

func (r *AgentRunRegistry) saveLocked() error {
	if r.path == "" {
		return nil
	}
	r.syncFromDiskLocked()
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

// MergePaneDuplicates folds records that describe the same tmux pane into one
// run. Earlier versions minted a second identity when a managed agent was first
// observed as a plain shell, which left the managed record orphaned and listed
// the pane twice in the dashboard.
func MergePaneDuplicates(runs []AgentRun) []AgentRun {
	grouped := make(map[string][]AgentRun)
	var order []string
	merged := make([]AgentRun, 0, len(runs))
	for _, run := range runs {
		if run.ID == "" {
			continue
		}
		if run.PaneID == "" {
			merged = append(merged, run)
			continue
		}
		key := run.SessionName + "\x00" + run.PaneID
		if _, seen := grouped[key]; !seen {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], run)
	}

	for _, key := range order {
		group := grouped[key]
		if len(group) == 1 {
			merged = append(merged, group[0])
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].Managed != group[j].Managed {
				return group[i].Managed
			}
			if !group[i].StartedAt.Equal(group[j].StartedAt) {
				return group[i].StartedAt.Before(group[j].StartedAt)
			}
			return group[i].ID < group[j].ID
		})
		primary := group[0]
		for _, run := range group[1:] {
			primary = absorbRun(primary, run)
		}
		merged = append(merged, primary)
	}
	sortRuns(merged)
	return merged
}

// absorbRun keeps primary's durable identity and takes the freshest process
// observation from the duplicate.
func absorbRun(primary, other AgentRun) AgentRun {
	if other.LastSeenAt.After(primary.LastSeenAt) {
		primary.Status = other.Status
		primary.LastSeenAt = other.LastSeenAt
		primary.PaneIndex = other.PaneIndex
		primary.PID = other.PID
	}
	if !other.StartedAt.IsZero() && other.StartedAt.Before(primary.StartedAt) {
		primary.StartedAt = other.StartedAt
	}
	if primary.PID == 0 {
		primary.PID = other.PID
	}
	if primary.Provider == "" || primary.Provider == AgentProviderFallback {
		if other.Provider != "" && other.Provider != AgentProviderFallback {
			primary.Provider = other.Provider
		}
	}
	primary.Managed = primary.Managed || other.Managed
	primary.Task = firstNonEmptyField(primary.Task, other.Task)
	primary.CWD = firstNonEmptyField(primary.CWD, other.CWD)
	primary.Branch = firstNonEmptyField(primary.Branch, other.Branch)
	primary.WorktreePath = firstNonEmptyField(primary.WorktreePath, other.WorktreePath)
	primary.GitPath = firstNonEmptyField(primary.GitPath, other.GitPath)
	return primary
}

func firstNonEmptyField(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
