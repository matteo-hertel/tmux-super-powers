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
	managedStop := reg.runs[stop.ID]
	managedStop.Managed = true
	reg.runs[stop.ID] = managedStop

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

func TestAgentRunRegistryPrunesUnseenObservedSessions(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	run, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderFallback, SessionName: "plain-shell", PaneIndex: 0, PID: 12, Status: "idle",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MarkUnseenStopped(nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find(run.ID); ok {
		t.Fatal("unseen observed session remained in the registry")
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

func TestAgentRunRegistryDoesNotReplaceManagedAgentWithIdleShell(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	managed, err := reg.RegisterManaged(SpawnResult{Session: "project", PaneID: "%1"}, AgentProviderClaude, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	idle, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderFallback, SessionName: "project", PaneID: "%2", PaneIndex: 0, PID: 55, Status: "idle",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if idle.ID == managed.ID {
		t.Fatal("idle shell replaced the managed agent run")
	}
	got, ok := reg.Find(managed.ID)
	if !ok || got.Provider != AgentProviderClaude || got.Status != "starting" {
		t.Fatalf("managed run was changed: %#v", got)
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

func TestAgentRunRegistryTracksDelegatedPanesInParentSession(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	root, err := reg.RegisterManaged(SpawnResult{
		Task: "root", Session: "shared-session", WorktreePath: "/work/shared",
	}, AgentProviderCodex, 1, time.Now())
	if err != nil {
		t.Fatalf("RegisterManaged: %v", err)
	}
	child, err := reg.RegisterDelegated(SpawnResult{
		Task: "child", Session: root.SessionName, WorktreePath: root.WorktreePath, OutputPath: "/tmp/delegate.log", PaneID: "%42",
	}, AgentProviderClaude, root.ID, 2, time.Now())
	if err != nil {
		t.Fatalf("RegisterDelegated: %v", err)
	}
	if child.SessionName != root.SessionName || child.PaneIndex != 2 || child.PaneID != "%42" {
		t.Fatalf("delegated target = %s:%d, want %s:2", child.SessionName, child.PaneIndex, root.SessionName)
	}
	if child.OutputPath != "/tmp/delegate.log" {
		t.Fatalf("delegated output path = %q", child.OutputPath)
	}
}

func TestAgentRunRegistryTracksDelegatedPaneWhenIndexMoves(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	root, err := reg.RegisterManaged(SpawnResult{Session: "shared", WorktreePath: "/work/shared"}, AgentProviderCodex, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	child, err := reg.RegisterDelegated(SpawnResult{Session: "shared", PaneID: "%42"}, AgentProviderClaude, root.ID, 1, now)
	if err != nil {
		t.Fatal(err)
	}

	observed, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "shared", PaneID: "%42", PaneIndex: 2, PID: 123, Status: "running",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if observed.ID != child.ID {
		t.Fatalf("shifted pane created run %q, want %q", observed.ID, child.ID)
	}
	if observed.PaneIndex != 2 {
		t.Fatalf("shifted pane index = %d, want 2", observed.PaneIndex)
	}
}

func TestAgentRunRegistryDoesNotMatchStoppedLegacyRunByPaneIndex(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	reg.runs["legacy"] = AgentRun{
		ID: "legacy", ParentRunID: "root", Provider: AgentProviderClaude, SessionName: "shared",
		PaneIndex: 1, Status: "stopped", Managed: true,
	}

	observed, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "shared", PaneID: "%55", PaneIndex: 1, PID: 987, Status: "running",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if observed.ID == "legacy" {
		t.Fatal("new pane reused a stopped legacy run")
	}
	legacy, ok := reg.Find("legacy")
	if !ok || legacy.Status != "stopped" || legacy.PaneID != "" {
		t.Fatalf("legacy run changed: %#v", legacy)
	}
}

func TestAgentRunRegistryDeleteRemovesStoredOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(delegatedOutputDir(), 0700); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(delegatedOutputDir(), "delegate-test.log")
	if err := os.WriteFile(outputPath, []byte("finished"), 0600); err != nil {
		t.Fatal(err)
	}
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	reg.runs["child"] = AgentRun{ID: "child", OutputPath: outputPath}

	if err := reg.Delete("child"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("stored output still exists: %v", err)
	}
}

func TestAgentRunRegistryDeleteRefusesOutputOutsideManagedDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	outputPath := filepath.Join(t.TempDir(), "delegate-test.log")
	if err := os.WriteFile(outputPath, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	reg.runs["child"] = AgentRun{ID: "child", OutputPath: outputPath}

	if err := reg.Delete("child"); err == nil {
		t.Fatal("Delete() removed a run with an unmanaged output path")
	}
	if _, ok := reg.Find("child"); !ok {
		t.Fatal("run was removed after output validation failed")
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("unmanaged output was removed: %v", err)
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
