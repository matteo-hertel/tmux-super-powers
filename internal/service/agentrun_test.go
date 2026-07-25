package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectAgentProviderFromProcess(t *testing.T) {
	tests := []struct {
		name     string
		process  string
		expected string
	}{
		{name: "claude binary", process: "claude", expected: AgentProviderClaude},
		{name: "claude semver process", process: "2.1.71", expected: AgentProviderClaude},
		{name: "codex binary", process: "codex", expected: AgentProviderCodex},
		{name: "codex platform binary", process: "codex-aarch64-a", expected: AgentProviderCodex},
		{name: "unknown agent", process: "aider", expected: AgentProviderFallback},
		{name: "shell", process: "zsh", expected: AgentProviderFallback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectAgentProvider(tt.process); got != tt.expected {
				t.Fatalf("DetectAgentProvider(%q) = %q, want %q", tt.process, got, tt.expected)
			}
		})
	}
}

func TestAgentRunRegistryUpsertPersistsAndReusesRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runs.json")
	reg, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}

	start := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	run, err := reg.UpsertObserved(ObservedAgentRun{
		Provider:    AgentProviderClaude,
		SessionName: "project-task",
		PaneIndex:   1,
		PID:         1234,
		CWD:         "/repo",
		Status:      "active",
	}, start)
	if err != nil {
		t.Fatalf("UpsertObserved first: %v", err)
	}
	if run.ID == "" {
		t.Fatal("expected generated run id")
	}
	if !run.StartedAt.Equal(start) || !run.LastSeenAt.Equal(start) {
		t.Fatalf("unexpected timestamps: started=%s lastSeen=%s", run.StartedAt, run.LastSeenAt)
	}

	later := start.Add(time.Minute)
	updated, err := reg.UpsertObserved(ObservedAgentRun{
		Provider:    AgentProviderClaude,
		SessionName: "project-task",
		PaneIndex:   1,
		PID:         1234,
		CWD:         "/repo",
		Status:      "waiting",
	}, later)
	if err != nil {
		t.Fatalf("UpsertObserved second: %v", err)
	}
	if updated.ID != run.ID {
		t.Fatalf("expected same run id, got %q want %q", updated.ID, run.ID)
	}
	if updated.Status != "waiting" {
		t.Fatalf("expected updated run status, got %q", updated.Status)
	}
	if !updated.StartedAt.Equal(start) || !updated.LastSeenAt.Equal(later) {
		t.Fatalf("unexpected updated timestamps: started=%s lastSeen=%s", updated.StartedAt, updated.LastSeenAt)
	}

	reloaded, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	got, ok := reloaded.Find(run.ID)
	if !ok {
		t.Fatalf("expected persisted run %q", run.ID)
	}
	if got.Status != "waiting" {
		t.Fatalf("persisted run not updated: %#v", got)
	}
}

func TestAgentRunRegistryMarksUnseenStopped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runs.json")
	reg, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	keep, err := reg.UpsertObserved(ObservedAgentRun{
		Provider:    AgentProviderCodex,
		SessionName: "one",
		PaneIndex:   1,
		PID:         10,
		Status:      "active",
	}, now)
	if err != nil {
		t.Fatalf("UpsertObserved keep: %v", err)
	}
	stop, err := reg.UpsertObserved(ObservedAgentRun{
		Provider:    AgentProviderClaude,
		SessionName: "two",
		PaneIndex:   1,
		PID:         11,
		Status:      "active",
	}, now)
	if err != nil {
		t.Fatalf("UpsertObserved stop: %v", err)
	}

	if err := reg.MarkUnseenStopped(map[string]bool{keep.ID: true}, now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkUnseenStopped: %v", err)
	}

	gotKeep, _ := reg.Find(keep.ID)
	gotStop, _ := reg.Find(stop.ID)
	if gotKeep.Status != "active" {
		t.Fatalf("seen run status = %q, want active", gotKeep.Status)
	}
	if gotStop.Status != "stopped" {
		t.Fatalf("unseen run status = %q, want stopped", gotStop.Status)
	}
	if !gotStop.LastSeenAt.Equal(now) {
		t.Fatalf("unseen run lastSeenAt = %s, want original observation time %s", gotStop.LastSeenAt, now)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected registry file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty registry file")
	}
}

func TestAgentRunRegistryRegistersManagedAgentAndReusesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runs.json")
	reg, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	managed, err := reg.RegisterManaged(SpawnResult{
		Task:         "replace the dashboard",
		Branch:       "spawn/replace-dashboard-bold-tide",
		Session:      "tsp-replace-dashboard-bold-tide",
		WorktreePath: "/work/tsp-replace-dashboard-bold-tide",
		GitPath:      "/code/tsp",
	}, AgentProviderCodex, 1, now)
	if err != nil {
		t.Fatalf("RegisterManaged: %v", err)
	}
	if !managed.Managed || managed.Task != "replace the dashboard" {
		t.Fatalf("managed metadata not recorded: %#v", managed)
	}

	observed, err := reg.UpsertObserved(ObservedAgentRun{
		Provider:    AgentProviderCodex,
		SessionName: managed.SessionName,
		PaneIndex:   1,
		PID:         987,
		CWD:         managed.WorktreePath,
		Status:      "running",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("UpsertObserved: %v", err)
	}
	if observed.ID != managed.ID {
		t.Fatalf("observation created a duplicate run: got %q want %q", observed.ID, managed.ID)
	}
	if observed.Task != managed.Task || !observed.Managed {
		t.Fatalf("observation lost managed metadata: %#v", observed)
	}

	if err := reg.Delete(managed.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := reg.Find(managed.ID); ok {
		t.Fatal("deleted managed run is still present")
	}
}

func TestAgentRunRegistryPersistsDelegationHierarchy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runs.json")
	reg, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)
	root, err := reg.RegisterManaged(SpawnResult{
		Task:         "build the agent manager",
		Branch:       "spawn/agent-manager",
		Session:      "tsp-agent-manager",
		WorktreePath: "/work/tsp-agent-manager",
		GitPath:      "/code/tsp",
	}, AgentProviderCodex, 1, now)
	if err != nil {
		t.Fatalf("RegisterManaged: %v", err)
	}
	child, err := reg.RegisterDelegated(SpawnResult{
		Task:         "make CI green",
		Branch:       root.Branch,
		Session:      "tsp-agent-manager-delegate-ci",
		WorktreePath: root.WorktreePath,
		GitPath:      root.GitPath,
	}, AgentProviderClaude, root.ID, 1, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RegisterDelegated: %v", err)
	}
	grandchild, err := reg.RegisterDelegated(SpawnResult{
		Task:         "re-run the failing integration test",
		Branch:       root.Branch,
		Session:      "tsp-agent-manager-delegate-integration",
		WorktreePath: root.WorktreePath,
		GitPath:      root.GitPath,
	}, AgentProviderClaude, child.ID, 1, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("RegisterDelegated grandchild: %v", err)
	}

	descendants := reg.Descendants(root.ID)
	if len(descendants) != 2 {
		t.Fatalf("Descendants() count = %d, want 2", len(descendants))
	}
	if descendants[0].ID != grandchild.ID || descendants[1].ID != child.ID {
		t.Fatalf("Descendants() = %#v, want children before parents", descendants)
	}

	reloaded, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	persisted, ok := reloaded.Find(child.ID)
	if !ok {
		t.Fatalf("delegated run %q was not persisted", child.ID)
	}
	if persisted.ParentRunID != root.ID || persisted.WorktreePath != root.WorktreePath {
		t.Fatalf("delegated metadata not preserved: %#v", persisted)
	}
}

func TestAgentRunRegistryRequiresParentForDelegation(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	_, err = reg.RegisterDelegated(SpawnResult{Session: "delegate"}, AgentProviderClaude, "", 1, time.Now())
	if err == nil {
		t.Fatal("RegisterDelegated accepted an empty parent run")
	}
	_, err = reg.RegisterDelegated(SpawnResult{Session: "delegate"}, AgentProviderClaude, "missing", 1, time.Now())
	if err == nil {
		t.Fatal("RegisterDelegated accepted an unknown parent run")
	}
}

func TestAgentRunRegistryDescendantsToleratesCycle(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	reg.runs["root"] = AgentRun{ID: "root", ParentRunID: "child"}
	reg.runs["child"] = AgentRun{ID: "child", ParentRunID: "root"}
	descendants := reg.Descendants("root")
	if len(descendants) != 1 || descendants[0].ID != "child" {
		t.Fatalf("Descendants() with cycle = %#v, want only child", descendants)
	}
}
