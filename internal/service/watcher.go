package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/config"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
)

// trackedSession holds the lifecycle state for a single spawned session.
type trackedSession struct {
	state        string // working, done, pr_polling, watching, fixing_ci, fixing_reviews, green, gave_up, merged, cleanup_done
	branch       string
	worktreePath string
	gitPath      string
	prNumber     int
	prURL        string
	ciRetries    int
	reviewCount  int
	pollErrors   int
	lastPoll     time.Time
}

// Watcher tracks spawned sessions and automates the post-PR lifecycle.
type Watcher struct {
	mu      sync.Mutex
	tracked map[string]*trackedSession
	bus     *Bus
	cfg     config.WatcherConfig
	monitor *Monitor
	stopCh  chan struct{}
	unsub   UnsubscribeFunc
}

// NewWatcher creates a new Watcher.
func NewWatcher(bus *Bus, cfg config.WatcherConfig) *Watcher {
	return &Watcher{
		tracked: make(map[string]*trackedSession),
		bus:     bus,
		cfg:     cfg,
		stopCh:  make(chan struct{}),
	}
}

// SetMonitor sets the monitor reference for session lookups.
func (w *Watcher) SetMonitor(m *Monitor) {
	w.monitor = m
}

// Start begins the watcher loop and subscribes to events.
func (w *Watcher) Start() {
	if !w.cfg.Enabled {
		return
	}
	w.loadState()
	w.unsub = w.bus.Subscribe(func(e Event) {
		w.HandleEvent(e)
	})
	go w.pollLoop()
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	if w.unsub != nil {
		w.unsub()
	}
	select {
	case <-w.stopCh:
		// already closed
	default:
		close(w.stopCh)
	}
	w.saveState()
}

// Track starts tracking a spawned session.
func (w *Watcher) Track(sessionName, branch, worktreePath, gitPath string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tracked[sessionName] = &trackedSession{
		state:        "working",
		branch:       branch,
		worktreePath: worktreePath,
		gitPath:      gitPath,
	}
	w.saveStateLocked()
}

// State returns the current state for a tracked session, or "" if not tracked.
func (w *Watcher) State(sessionName string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ts, ok := w.tracked[sessionName]; ok {
		return ts.state
	}
	return ""
}

func (w *Watcher) getTracked(name string) *trackedSession {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tracked[name]
}

// HandleEvent processes a single event.
func (w *Watcher) HandleEvent(e Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	switch ev := e.(type) {
	case StatusChangedEvent:
		ts, ok := w.tracked[ev.Session]
		if !ok {
			return
		}
		switch ts.state {
		case "working":
			if ev.To == "done" {
				ts.state = "done"
				w.saveStateLocked()
			}
		case "fixing_ci", "fixing_reviews":
			// Wait for agent to start working (From != "done") then finish (To == "done")
			if ev.From != "done" && ev.To == "done" {
				ts.state = "watching"
				w.saveStateLocked()
			}
		}

	case SessionRemovedEvent:
		delete(w.tracked, ev.Name)
		w.saveStateLocked()
	}
}

// pollLoop runs periodic checks for PR/CI/merge status.
func (w *Watcher) pollLoop() {
	interval := time.Duration(w.cfg.PollIntervalS) * time.Second
	if interval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.pollAll()
		}
	}
}

func (w *Watcher) pollAll() {
	w.mu.Lock()
	type pollItem struct {
		name string
		ts   *trackedSession
	}
	var items []pollItem
	for name, ts := range w.tracked {
		items = append(items, pollItem{name: name, ts: ts})
	}
	w.mu.Unlock()

	for _, item := range items {
		w.pollSession(item.name, item.ts)
	}
}

func (w *Watcher) pollSession(name string, ts *trackedSession) {
	w.mu.Lock()
	state := ts.state
	w.mu.Unlock()

	switch state {
	case "done", "pr_polling":
		w.pollForPR(name, ts)
	case "watching", "green":
		w.pollCIAndReviews(name, ts)
	case "gave_up":
		w.pollForMerge(name, ts)
	}
}

func (w *Watcher) pollForPR(name string, ts *trackedSession) {
	prNumber, prURL := FindPRForBranch(ts.branch)
	if prNumber > 0 {
		w.mu.Lock()
		ts.prNumber = prNumber
		ts.prURL = prURL
		ts.state = "watching"
		ts.pollErrors = 0
		w.saveStateLocked()
		w.mu.Unlock()
		w.bus.Publish(PRDetectedEvent{Session: name, PRNumber: prNumber, URL: prURL})
	} else {
		w.mu.Lock()
		if ts.state == "done" {
			ts.state = "pr_polling"
			w.saveStateLocked()
		}
		w.mu.Unlock()
	}
}

func (w *Watcher) pollCIAndReviews(name string, ts *trackedSession) {
	if ts.prNumber == 0 {
		return
	}

	// Check merge status
	if w.checkMerged(ts.prNumber) {
		w.handleMerged(name, ts)
		return
	}

	// Check CI
	ciStatus := GetCIStatus(ts.prNumber)
	w.mu.Lock()
	prevCI := ""
	if ts.state == "green" {
		prevCI = "pass"
	}

	if ciStatus == "fail" && ts.state != "fixing_ci" {
		if ts.ciRetries >= w.cfg.MaxCIRetries {
			ts.state = "gave_up"
			w.saveStateLocked()
			w.mu.Unlock()
			w.bus.Publish(CIStatusChangedEvent{Session: name, PRNumber: ts.prNumber, From: prevCI, To: "fail"})
			return
		}
		ts.state = "fixing_ci"
		ts.ciRetries++
		w.saveStateLocked()
		w.mu.Unlock()
		w.bus.Publish(CIStatusChangedEvent{Session: name, PRNumber: ts.prNumber, From: prevCI, To: "fail"})
		w.sendFixCI(name, ts)
		return
	}

	if ciStatus == "pass" {
		if ts.ciRetries > 0 {
			ts.ciRetries = 0
		}
		ts.state = "green"
		w.saveStateLocked()
		w.mu.Unlock()
		if prevCI != "pass" {
			w.bus.Publish(CIStatusChangedEvent{Session: name, PRNumber: ts.prNumber, From: prevCI, To: "pass"})
		}
		// Check for new reviews
		w.checkReviews(name, ts)
		return
	}
	w.mu.Unlock()
}

func (w *Watcher) checkReviews(name string, ts *trackedSession) {
	count := GetReviewCommentCount(ts.prNumber)
	w.mu.Lock()
	if count > ts.reviewCount {
		prevCount := ts.reviewCount
		ts.reviewCount = count
		ts.state = "fixing_reviews"
		w.saveStateLocked()
		w.mu.Unlock()
		w.bus.Publish(ReviewsChangedEvent{Session: name, PRNumber: ts.prNumber, Count: count, PrevCount: prevCount})
		w.sendFixReviews(name, ts)
		return
	}
	w.mu.Unlock()
}

func (w *Watcher) pollForMerge(name string, ts *trackedSession) {
	if ts.prNumber == 0 {
		return
	}
	if w.checkMerged(ts.prNumber) {
		w.handleMerged(name, ts)
	}
}

func (w *Watcher) checkMerged(prNumber int) bool {
	cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", prNumber), "--json", "state", "--jq", ".state")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "MERGED"
}

func (w *Watcher) handleMerged(name string, ts *trackedSession) {
	w.mu.Lock()
	ts.state = "merged"
	w.saveStateLocked()
	w.mu.Unlock()

	w.bus.Publish(PRMergedEvent{Session: name, PRNumber: ts.prNumber})

	if w.cfg.AutoCleanup {
		w.cleanup(name, ts)
	}
}

func (w *Watcher) cleanup(name string, ts *trackedSession) {
	tmuxpkg.KillSession(name)
	if err := exec.Command("git", "-C", ts.gitPath, "worktree", "remove", ts.worktreePath, "--force").Run(); err != nil {
		exec.Command("git", "-C", ts.gitPath, "worktree", "prune").Run()
	}
	exec.Command("git", "-C", ts.gitPath, "branch", "-D", ts.branch).Run()

	w.mu.Lock()
	ts.state = "cleanup_done"
	delete(w.tracked, name)
	w.saveStateLocked()
	w.mu.Unlock()

	w.bus.Publish(CleanupCompletedEvent{Session: name, WorktreePath: ts.worktreePath, Branch: ts.branch})
}

func (w *Watcher) sendFixCI(name string, ts *trackedSession) {
	logs, err := FetchFailingCILogs(ts.prNumber)
	if err != nil {
		log.Printf("watcher: failed to fetch CI logs for %s: %v", name, err)
		return
	}
	prompt := "The CI pipeline failed. Here are the failing logs:\n\n" + logs + "\n\nPlease fix the issues and push."
	if len(prompt) > 4000 {
		prompt = prompt[:4000] + "\n\n[truncated]"
	}
	agentPane := w.findAgentPane(name)
	if err := SendToPane(name, agentPane, prompt); err != nil {
		log.Printf("watcher: failed to send fix-ci to %s: %v", name, err)
	}
	w.bus.Publish(FixAttemptedEvent{Session: name, FixType: "ci", Attempt: ts.ciRetries, MaxAttempts: w.cfg.MaxCIRetries})
}

func (w *Watcher) sendFixReviews(name string, ts *trackedSession) {
	comments, err := FetchPRComments(ts.prNumber)
	if err != nil || len(comments) == 0 {
		log.Printf("watcher: no review comments for %s: %v", name, err)
		return
	}
	formatted := FormatPRComments(comments)
	prompt := "Please address these PR review comments:\n\n" + formatted
	agentPane := w.findAgentPane(name)
	if err := SendToPane(name, agentPane, prompt); err != nil {
		log.Printf("watcher: failed to send fix-reviews to %s: %v", name, err)
	}
	w.bus.Publish(FixAttemptedEvent{Session: name, FixType: "reviews", Attempt: 1, MaxAttempts: 1})
}

func (w *Watcher) findAgentPane(name string) int {
	if w.monitor == nil {
		return 1
	}
	s := w.monitor.FindSession(name)
	if s == nil {
		return 1
	}
	for _, p := range s.Panes {
		if p.Type == "agent" {
			return p.Index
		}
	}
	return 1
}

// --- State persistence ---

type watcherPersist struct {
	Sessions map[string]persistedSession `json:"sessions"`
}

type persistedSession struct {
	State        string `json:"state"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktreePath"`
	GitPath      string `json:"gitPath"`
	PRNumber     int    `json:"prNumber,omitempty"`
	CIRetries    int    `json:"ciRetries,omitempty"`
	ReviewCount  int    `json:"reviewCount,omitempty"`
}

func (w *Watcher) saveStateLocked() {
	p := watcherPersist{Sessions: make(map[string]persistedSession)}
	for name, ts := range w.tracked {
		p.Sessions[name] = persistedSession{
			State:        ts.state,
			Branch:       ts.branch,
			WorktreePath: ts.worktreePath,
			GitPath:      ts.gitPath,
			PRNumber:     ts.prNumber,
			CIRetries:    ts.ciRetries,
			ReviewCount:  ts.reviewCount,
		}
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(config.TspDir(), "watcher-state.json")
	os.WriteFile(path, data, 0600)
}

func (w *Watcher) saveState() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saveStateLocked()
}

func (w *Watcher) loadState() {
	path := filepath.Join(config.TspDir(), "watcher-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var p watcherPersist
	if err := json.Unmarshal(data, &p); err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for name, ps := range p.Sessions {
		ts := &trackedSession{
			state:        ps.State,
			branch:       ps.Branch,
			worktreePath: ps.WorktreePath,
			gitPath:      ps.GitPath,
			prNumber:     ps.PRNumber,
			ciRetries:    ps.CIRetries,
			reviewCount:  ps.ReviewCount,
		}
		// Recovery: in-flight fixing states restart as watching
		if ts.state == "fixing_ci" || ts.state == "fixing_reviews" {
			ts.state = "watching"
		}
		w.tracked[name] = ts
	}
}
