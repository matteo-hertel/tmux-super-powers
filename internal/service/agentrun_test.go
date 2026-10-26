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
	got, ok := reloaded.find(run.ID)
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

	gotKeep, _ := reg.find(keep.ID)
	gotStop, _ := reg.find(stop.ID)
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
	if _, ok := reg.find(run.ID); ok {
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
	if _, ok := reg.find(managed.ID); ok {
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
	got, ok := reg.find(managed.ID)
	if !ok || got.Provider != AgentProviderClaude || got.Status != "starting" {
		t.Fatalf("managed run was changed: %#v", got)
	}
}

func TestAgentRunRegistryTracksManagedPaneWhenIndexMoves(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	managed, err := reg.RegisterManaged(SpawnResult{Session: "shared", PaneID: "%42", WorktreePath: "/work/shared"}, AgentProviderClaude, 1, now)
	if err != nil {
		t.Fatal(err)
	}

	observed, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "shared", PaneID: "%42", PaneIndex: 2, PID: 123, Status: "running",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if observed.ID != managed.ID {
		t.Fatalf("shifted pane created run %q, want %q", observed.ID, managed.ID)
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
		ID: "legacy", Provider: AgentProviderClaude, SessionName: "shared",
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
	legacy, ok := reg.find("legacy")
	if !ok || legacy.Status != "stopped" || legacy.PaneID != "" {
		t.Fatalf("legacy run changed: %#v", legacy)
	}
}

func TestAgentRunRegistryKeepsManagedRunWhileSetupPaneLooksLikeShell(t *testing.T) {
	reg, err := NewAgentRunRegistry(filepath.Join(t.TempDir(), "agent-runs.json"))
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	now := time.Date(2026, 9, 2, 13, 9, 4, 0, time.UTC)
	managed, err := reg.RegisterManaged(SpawnResult{
		Task:         "TIGER-87",
		Session:      "backend-tiger-87",
		PaneID:       "%66",
		Branch:       "spawn/tiger-87",
		WorktreePath: "/tmp/backend-tiger-87",
		GitPath:      "/tmp/backend",
	}, AgentProviderClaude, 0, now)
	if err != nil {
		t.Fatalf("RegisterManaged: %v", err)
	}

	setup, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderFallback, SessionName: "backend-tiger-87",
		PaneID: "%66", PaneIndex: 0, PID: 62996, Status: "idle",
	}, now.Add(24*time.Millisecond))
	if err != nil {
		t.Fatalf("UpsertObserved: %v", err)
	}
	if setup.ID != managed.ID {
		t.Fatalf("setup pane forked a second run: got %q want %q", setup.ID, managed.ID)
	}
	if setup.Provider != AgentProviderClaude || setup.Task != "TIGER-87" {
		t.Fatalf("setup pane downgraded the managed run: %#v", setup)
	}

	started, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "backend-tiger-87",
		PaneID: "%66", PaneIndex: 0, PID: 62996, Status: "running",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("UpsertObserved: %v", err)
	}
	if started.ID != managed.ID || !started.Managed || started.WorktreePath == "" {
		t.Fatalf("running agent lost its managed identity: %#v", started)
	}
	if runs := reg.List(); len(runs) != 1 {
		t.Fatalf("expected 1 run for one pane, got %d: %#v", len(runs), runs)
	}
}

func TestAgentRunRegistryAdoptsPaneObservedBeforeRegistration(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 13, 9, 4, 0, time.UTC)
	observed, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "backend-tiger-87",
		PaneID: "%66", PaneIndex: 0, PID: 62996, Status: "running",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := reg.RegisterManaged(SpawnResult{
		Task: "TIGER-87", Session: "backend-tiger-87", PaneID: "%66",
		WorktreePath: "/tmp/backend-tiger-87",
	}, AgentProviderClaude, 0, now.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if managed.ID != observed.ID {
		t.Fatalf("registration forked a second run: got %q want %q", managed.ID, observed.ID)
	}
	if runs := reg.List(); len(runs) != 1 {
		t.Fatalf("expected 1 run for one pane, got %d", len(runs))
	}
}

func TestAgentRunRegistryKeepsRunWhenAgentRestartsInSamePane(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	first, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "project", PaneID: "%3", PID: 100, Status: "running",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "project", PaneID: "%3", PID: 200, Status: "running",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if restarted.ID != first.ID || restarted.PID != 200 {
		t.Fatalf("restarted agent = %#v, want run %q with pid 200", restarted, first.ID)
	}
	if runs := reg.List(); len(runs) != 1 {
		t.Fatalf("expected 1 run for one pane, got %d", len(runs))
	}
}

func TestAgentRunRegistryReattachesManagedRunWhosePaneClosed(t *testing.T) {
	reg, err := NewAgentRunRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	managed, err := reg.RegisterManaged(SpawnResult{
		Task: "sparta", Session: "backend-sparta", PaneID: "%9", WorktreePath: "/tmp/backend-sparta",
	}, AgentProviderClaude, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "backend-sparta", PaneID: "%10", PaneIndex: 1,
		PID: 300, Status: "running", SessionPaneIDs: []string{"%9", "%10"},
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == managed.ID {
		t.Fatal("managed run moved to another pane while its own pane was open")
	}
	reattached, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "backend-sparta", PaneID: "%10", PaneIndex: 0,
		PID: 300, Status: "running", SessionPaneIDs: []string{"%10", "%11"},
	}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reattached.ID != managed.ID || reattached.PaneID != "%10" || reattached.Task != "sparta" {
		t.Fatalf("reattached run = %#v, want managed run %q on pane %s", reattached, managed.ID, "%10")
	}
	if _, ok := reg.find(second.ID); ok {
		t.Fatal("duplicate observed run was kept")
	}
}

func TestMergePaneDuplicatesHealsOrphanedManagedRun(t *testing.T) {
	spawned := time.Date(2026, 9, 2, 13, 9, 4, 0, time.UTC)
	seen := time.Date(2026, 9, 2, 15, 39, 49, 0, time.UTC)
	merged := MergePaneDuplicates([]AgentRun{
		{
			ID: "run_observed", Provider: AgentProviderClaude, SessionName: "backend-tiger-87",
			PaneID: "%66", PaneIndex: 0, PID: 62996, Status: "running",
			StartedAt: spawned.Add(24 * time.Millisecond), LastSeenAt: seen,
		},
		{
			ID: "run_managed", Provider: AgentProviderClaude, Task: "TIGER-87",
			SessionName: "backend-tiger-87", PaneID: "%66", PaneIndex: 0,
			Branch: "spawn/tiger-87", WorktreePath: "/tmp/backend-tiger-87", GitPath: "/tmp/backend",
			Status: "stopped", StartedAt: spawned, LastSeenAt: spawned, Managed: true,
		},
	})

	if len(merged) != 1 {
		t.Fatalf("expected 1 run after healing, got %d: %#v", len(merged), merged)
	}
	pane := merged[0]
	if pane.ID != "run_managed" || !pane.Managed {
		t.Fatalf("healed run lost its managed identity: %#v", pane)
	}
	if pane.Task != "TIGER-87" || pane.WorktreePath != "/tmp/backend-tiger-87" || pane.Branch != "spawn/tiger-87" {
		t.Fatalf("healed run lost its workspace metadata: %#v", pane)
	}
	if pane.PID != 62996 || pane.Status != "running" || !pane.LastSeenAt.Equal(seen) {
		t.Fatalf("healed run lost the live observation: %#v", pane)
	}
	if !pane.StartedAt.Equal(spawned) {
		t.Fatalf("healed run lost its spawn time: %#v", pane)
	}
}

func TestAgentRunRegistryDropsLegacyDelegatedRunsOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runs.json")
	stored := `{"runs":[
		{"id":"run_root","provider":"codex","task":"root","sessionName":"s","paneId":"%1","paneIndex":0,"worktreePath":"/tmp/wt","status":"running","startedAt":"2026-09-02T13:09:04Z","lastSeenAt":"2026-09-02T13:09:04Z","managed":true},
		{"id":"run_child","parentRunId":"run_root","provider":"claude","task":"child","sessionName":"s","paneId":"%2","paneIndex":1,"worktreePath":"/tmp/wt","outputPath":"/tmp/delegate-1.log","status":"stopped","startedAt":"2026-09-02T13:10:04Z","lastSeenAt":"2026-09-02T13:10:04Z","managed":true}
	]}`
	if err := os.WriteFile(path, []byte(stored), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.find("run_child"); ok {
		t.Fatal("legacy delegated run was loaded as a root run that owns the parent worktree")
	}
	if _, ok := reg.find("run_root"); !ok {
		t.Fatal("root run was dropped with its delegated child")
	}
}

func TestAgentRunRegistryHealsDuplicatesOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runs.json")
	stored := `{"runs":[
		{"id":"run_observed","provider":"claude","sessionName":"s","paneId":"%1","paneIndex":0,"pid":42,"status":"running","startedAt":"2026-09-02T13:09:04.793387Z","lastSeenAt":"2026-09-02T15:39:49Z"},
		{"id":"run_managed","provider":"claude","task":"T","sessionName":"s","paneId":"%1","paneIndex":0,"worktreePath":"/tmp/wt","status":"stopped","startedAt":"2026-09-02T13:09:04.769407Z","lastSeenAt":"2026-09-02T13:09:04.769407Z","managed":true}
	]}`
	if err := os.WriteFile(path, []byte(stored), 0644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatalf("NewAgentRunRegistry: %v", err)
	}
	runs := reg.List()
	if len(runs) != 1 {
		t.Fatalf("expected 1 healed run, got %d: %#v", len(runs), runs)
	}
	if runs[0].ID != "run_managed" || runs[0].PID != 42 || runs[0].Task != "T" {
		t.Fatalf("healed run is wrong: %#v", runs[0])
	}
}

func TestAgentRunRegistryKeepsRunsWrittenByAnotherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runs.json")
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)

	dash, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dash.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "dash", PaneID: "%1", PID: 1, Status: "running",
	}, now); err != nil {
		t.Fatal(err)
	}

	spawn, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := spawn.RegisterManaged(SpawnResult{
		Task: "T", Session: "spawned", PaneID: "%2", WorktreePath: "/tmp/wt",
	}, AgentProviderClaude, 0, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// The dashboard's next refresh must not overwrite the spawned run.
	if _, err := dash.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "dash", PaneID: "%1", PID: 1, Status: "running",
	}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.find(other.ID); !ok {
		t.Fatalf("concurrent spawn was dropped: %#v", reloaded.List())
	}
	if len(reloaded.List()) != 2 {
		t.Fatalf("expected both runs, got %#v", reloaded.List())
	}
}

func TestAgentRunRegistryDoesNotResurrectDeletedRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-runs.json")
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	reg, err := NewAgentRunRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	run, err := reg.RegisterManaged(SpawnResult{
		Task: "T", Session: "s", PaneID: "%1", WorktreePath: "/tmp/wt",
	}, AgentProviderClaude, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Delete(run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.UpsertObserved(ObservedAgentRun{
		Provider: AgentProviderClaude, SessionName: "other", PaneID: "%9", PID: 3, Status: "running",
	}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.find(run.ID); ok {
		t.Fatal("deleted run came back from disk")
	}
}

func (r *AgentRunRegistry) find(id string) (AgentRun, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[id]
	return run, ok
}
