package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/matteo-hertel/tmux-super-powers/config"
	"github.com/matteo-hertel/tmux-super-powers/internal/pathutil"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
	tmuxpkg "github.com/matteo-hertel/tmux-super-powers/internal/tmux"
	"github.com/spf13/cobra"
)

var dashCmd = &cobra.Command{
	Use:   "dash",
	Short: "Open the agent manager",
	Long: `Manage local Claude Code, Codex, and other terminal agents.

The dashboard takes an on-demand snapshot instead of polling agent output or CI.
Jump to any tmux session, see the agents running in it, open configured
projects, spawn agents, stop a process, or remove a managed worktree from one
place.`,
	Run: func(cmd *cobra.Command, args []string) {
		if !tmuxpkg.IsInsideTmux() {
			fmt.Fprintln(os.Stderr, "Error: dash must be run inside a tmux session")
			os.Exit(1)
		}

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		// Finish worktree removals an earlier tsp started but did not live to
		// complete. Each one unlinks in a detached process.
		service.SweepWorktreeTrash(pathutil.ExpandPath(cfg.Spawn.WorktreeBase))
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error determining current directory: %v\n", err)
			os.Exit(1)
		}
		registry, err := service.NewAgentRunRegistry(filepath.Join(config.TspDir(), "agent-runs.json"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading agent registry: %v\n", err)
			os.Exit(1)
		}
		agents, err := discoverAgents(registry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error discovering agents: %v\n", err)
			os.Exit(1)
		}

		model := newAgentDashboardModel(agents, cfg, registry, cwd)
		program := tea.NewProgram(model)
		finalModel, err := program.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if final, ok := finalModel.(agentDashboardModel); ok && final.attachSession != "" {
			if err := tmuxpkg.AttachOrSwitch(final.attachSession); err != nil {
				fmt.Fprintf(os.Stderr, "Error opening tmux session: %v\n", err)
				return
			}
			if final.attachPane.ID != "" || final.attachPane.Index >= 0 {
				if err := tmuxpkg.SelectPane(final.attachSession, final.attachPane); err != nil {
					fmt.Fprintf(os.Stderr, "Error selecting tmux pane: %v\n", err)
				}
			}
		}
		if final, ok := finalModel.(agentDashboardModel); ok && final.openDirectory != "" {
			sessionName, _, openErr := ensureDirectorySession(final.openDirectory, cfg.Spawn.AgentCommand)
			if openErr != nil {
				fmt.Fprintf(os.Stderr, "Error opening directory: %v\n", openErr)
				return
			}
			if openErr := tmuxpkg.AttachOrSwitch(sessionName); openErr != nil {
				fmt.Fprintf(os.Stderr, "Error opening tmux session: %v\n", openErr)
			}
		}
	},
}

type agentEntry struct {
	isSession     bool
	run           service.AgentRun
	branch        string
	worktreePath  string
	gitPath       string
	output        string
	live          bool
	sessionExists bool
	isWorktree    bool
}

func (a agentEntry) title() string {
	if strings.TrimSpace(a.run.Task) != "" {
		return strings.TrimSpace(a.run.Task)
	}
	return a.run.SessionName
}

func (a agentEntry) status() string {
	switch {
	case a.live:
		return "running"
	case a.isSession && a.sessionExists:
		return "idle"
	case a.isSession:
		return "closed"
	case a.sessionExists:
		return "exited"
	default:
		return "missing"
	}
}

func (a agentEntry) provider() string {
	if a.isSession {
		return "session"
	}
	if a.run.Provider == "" || a.run.Provider == service.AgentProviderFallback {
		return "agent"
	}
	return a.run.Provider
}

func (a agentEntry) workspacePath() string {
	return firstNonEmpty(a.worktreePath, a.run.WorktreePath, a.run.CWD)
}

func (a agentEntry) key() string {
	if a.isSession {
		return sessionKey(a.run.SessionName)
	}
	return a.run.ID
}

func sessionKey(name string) string {
	return "session:" + name
}

// ownsWorkspace reports whether cleaning up this entry should also remove its
// directory and branch. Managed runs own the worktree they were spawned into;
// any other agent owns one when its session sits in a git worktree itself.
func (a agentEntry) ownsWorkspace() bool {
	if a.isSession || a.worktreePath == "" {
		return false
	}
	return a.run.Managed || a.isWorktree
}

func runPane(run service.AgentRun) tmuxpkg.Pane {
	return tmuxpkg.Pane{ID: run.PaneID, Index: run.PaneIndex}
}

func discoverAgents(registry *service.AgentRunRegistry) ([]agentEntry, error) {
	sessionNames, err := service.ListSessions()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sessionSet := make(map[string]bool, len(sessionNames))
	seenRuns := make(map[string]bool)
	var sessions, entries []agentEntry

	for _, sessionName := range sessionNames {
		sessionSet[sessionName] = true
		gitInfo := service.DetectSessionGitInfoFull(sessionName)
		panes := tmuxpkg.Panes(sessionName)
		session := agentEntry{
			isSession:     true,
			run:           service.AgentRun{SessionName: sessionName, CWD: gitInfo.Cwd},
			branch:        gitInfo.Branch,
			worktreePath:  gitInfo.WorktreePath,
			gitPath:       gitInfo.GitPath,
			sessionExists: true,
			isWorktree:    gitInfo.IsWorktree,
		}
		sessionAgents := len(entries)
		paneIDs := make([]string, 0, len(panes))
		for _, pane := range panes {
			paneIDs = append(paneIDs, pane.ID)
		}
		for _, pane := range panes {
			if service.IsPaneDead(sessionName, pane) {
				continue
			}
			process := service.GetPaneProcess(sessionName, pane)
			processInfo := service.DetectPaneAgentProcess(sessionName, pane, process)
			isAgent := service.PaneTypeFromProcess(process) == "agent" ||
				(processInfo.Provider != "" && processInfo.Provider != service.AgentProviderFallback) ||
				processInfo.Command == "aider"
			if !isAgent {
				continue
			}

			provider := processInfo.Provider
			if provider == "" || (provider == service.AgentProviderFallback && processInfo.Command == "aider") {
				provider = processInfo.Command
			}
			if provider == "" {
				provider = service.DetectAgentProvider(process)
			}
			cwd := service.GetAgentPaneCwd(sessionName, pane)
			run, upsertErr := registry.UpsertObserved(service.ObservedAgentRun{
				Provider:       provider,
				SessionName:    sessionName,
				PaneIndex:      pane.Index,
				PaneID:         pane.ID,
				PID:            processInfo.PID,
				CWD:            cwd,
				Status:         "running",
				SessionPaneIDs: paneIDs,
			}, now)
			if upsertErr != nil {
				return nil, upsertErr
			}
			seenRuns[run.ID] = true
			entries = append(entries, agentEntry{
				run:           run,
				branch:        firstNonEmpty(run.Branch, gitInfo.Branch),
				worktreePath:  firstNonEmpty(run.WorktreePath, gitInfo.WorktreePath),
				gitPath:       firstNonEmpty(run.GitPath, gitInfo.GitPath),
				output:        service.CapturePanePreview(sessionName, pane),
				live:          true,
				sessionExists: true,
				isWorktree:    gitInfo.IsWorktree,
			})
		}
		if len(panes) > 0 {
			for _, agent := range entries[sessionAgents:] {
				if agent.run.PaneID == panes[0].ID {
					session.output = agent.output
				}
			}
			if session.output == "" {
				session.output = service.CapturePanePreview(sessionName, panes[0])
			}
		}
		sessions = append(sessions, session)
	}

	if err := registry.MarkUnseenStopped(seenRuns, now); err != nil {
		return nil, err
	}

	// Keep tsp-managed agents visible after their process or session exits so
	// the operator can inspect and clean the workspace deliberately.
	for _, run := range registry.List() {
		if seenRuns[run.ID] || !run.Managed {
			continue
		}
		entry := agentEntry{
			run:           run,
			branch:        run.Branch,
			worktreePath:  run.WorktreePath,
			gitPath:       run.GitPath,
			sessionExists: sessionSet[run.SessionName],
		}
		if entry.sessionExists && tmuxpkg.PaneExists(run.SessionName, runPane(run)) {
			entry.output = service.CapturePanePreview(run.SessionName, runPane(run))
		}
		entries = append(entries, entry)
	}

	return groupBySession(sessions, entries), nil
}

// sortAgentEntries lists running agents first, then managed ones, then the
// newest.
func sortAgentEntries(entries []agentEntry) []agentEntry {
	ordered := append([]agentEntry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].live != ordered[j].live {
			return ordered[i].live
		}
		if ordered[i].run.Managed != ordered[j].run.Managed {
			return ordered[i].run.Managed
		}
		return ordered[i].run.StartedAt.After(ordered[j].run.StartedAt)
	})
	return ordered
}

func groupBySession(sessions, agents []agentEntry) []agentEntry {
	listed := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		listed[session.run.SessionName] = true
	}
	members := make(map[string][]agentEntry)
	for _, agent := range sortAgentEntries(agents) {
		name := agent.run.SessionName
		if !listed[name] {
			listed[name] = true
			sessions = append(sessions, agentEntry{
				isSession: true,
				run:       service.AgentRun{SessionName: name, CWD: agent.run.CWD},
				branch:    agent.branch,
			})
		}
		members[name] = append(members[name], agent)
	}
	rows := make([]agentEntry, 0, len(sessions)+len(agents))
	for _, session := range sessions {
		group := members[session.run.SessionName]
		for _, agent := range group {
			session.live = session.live || agent.live
		}
		if session.output == "" && len(group) > 0 {
			session.output = group[0].output
		}
		rows = append(rows, session)
		rows = append(rows, group...)
	}
	return rows
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type agentDashboardMode int

const (
	dashAgentsBrowse agentDashboardMode = iota
	dashAgentsOpenDirectory
	dashAgentsSpawn
	dashAgentsConfirmStop
	dashAgentsConfirmCleanup
	dashAgentsHelp
)

type agentDashboardModel struct {
	agents              []agentEntry
	cursor              int
	width               int
	height              int
	cfg                 *config.Config
	registry            *service.AgentRunRegistry
	cwd                 string
	mode                agentDashboardMode
	taskInput           textinput.Model
	pathInput           textinput.Model
	baseInput           textinput.Model
	directoryInput      textinput.Model
	filterInput         textinput.Model
	filterFocused       bool
	focusedInput        int
	spawnAgent          string
	busy                bool
	statusMessage       string
	attachSession       string
	attachPane          tmuxpkg.Pane
	directories         []string
	filteredDirectories []string
	directoryCursor     int
	directoryError      string
	openDirectory       string
	expanded            map[string]bool
}

type agentsRefreshedMsg struct {
	agents []agentEntry
	err    error
}

type agentActionDoneMsg struct {
	agents     []agentEntry
	message    string
	selectedID string
	err        error
}

func newAgentDashboardModel(agents []agentEntry, cfg *config.Config, registry *service.AgentRunRegistry, cwd string) agentDashboardModel {
	taskInput := textinput.New()
	taskInput.Placeholder = "What should the agent do?"
	taskInput.CharLimit = 1000
	taskInput.SetWidth(72)

	pathInput := textinput.New()
	pathInput.Placeholder = "/path/to/project"
	pathInput.CharLimit = 1000
	pathInput.SetWidth(72)
	baseInput := textinput.New()
	baseInput.Placeholder = "Current branch"
	baseInput.CharLimit = 200
	baseInput.SetWidth(36)
	directoryInput := textinput.New()
	directoryInput.Placeholder = "Filter configured projects"
	directoryInput.CharLimit = 500
	directoryInput.SetWidth(72)
	filterInput := textinput.New()
	filterInput.Placeholder = "Filter sessions"
	filterInput.Prompt = "/ "
	filterInput.CharLimit = 200
	filterInput.SetWidth(72)

	return agentDashboardModel{
		agents:         agents,
		cfg:            cfg,
		registry:       registry,
		cwd:            cwd,
		taskInput:      taskInput,
		pathInput:      pathInput,
		baseInput:      baseInput,
		directoryInput: directoryInput,
		filterInput:    filterInput,
		expanded:       make(map[string]bool),
	}
}

func (m agentDashboardModel) Init() tea.Cmd {
	return nil
}

// entryMatchesFilter reports whether one roster entry matches the query. The
// comparison is case-insensitive and matches any substring of the fields an
// operator would recognise the row by. An empty query matches every entry.
func entryMatchesFilter(entry agentEntry, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	fields := []string{
		entry.run.SessionName,
		entry.run.Task,
		entry.branch,
		entry.run.Branch,
		entry.run.CWD,
		entry.workspacePath(),
	}
	if !entry.isSession {
		fields = append(fields, entry.provider())
	}
	for _, field := range fields {
		if field != "" && strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// sessionMatchesFilter keeps a session visible when it matches the query itself
// or when any agent inside it does, so a search for a task still finds the
// collapsed session holding it.
func sessionMatchesFilter(session agentEntry, members []agentEntry, query string) bool {
	if entryMatchesFilter(session, query) {
		return true
	}
	for _, member := range members {
		if entryMatchesFilter(member, query) {
			return true
		}
	}
	return false
}

func (m agentDashboardModel) filterQuery() string {
	return strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
}

// filterVisible reports whether the inline filter bar occupies a row. It stays
// on screen while the input is focused and afterwards for as long as a query
// narrows the roster.
func (m agentDashboardModel) filterVisible() bool {
	return m.filterFocused || m.filterQuery() != ""
}

func (m agentDashboardModel) rows() []agentEntry {
	sessions := make(map[string]bool)
	members := make(map[string][]agentEntry)
	for _, entry := range m.agents {
		if entry.isSession {
			sessions[entry.run.SessionName] = true
			continue
		}
		members[entry.run.SessionName] = append(members[entry.run.SessionName], entry)
	}
	query := m.filterQuery()
	matched := make(map[string]bool, len(sessions))
	for _, entry := range m.agents {
		if entry.isSession {
			matched[entry.run.SessionName] = sessionMatchesFilter(entry, members[entry.run.SessionName], query)
		}
	}
	rows := make([]agentEntry, 0, len(m.agents))
	for _, entry := range m.agents {
		name := entry.run.SessionName
		switch {
		case entry.isSession:
			if matched[name] {
				rows = append(rows, entry)
			}
		case !sessions[name]:
			// An agent without a session row is the only member of its own
			// virtual session, so the session rule above reduces to matching
			// this entry alone.
			if entryMatchesFilter(entry, query) {
				rows = append(rows, entry)
			}
		case m.expanded[name] && matched[name]:
			rows = append(rows, entry)
		}
	}
	return rows
}

func (m agentDashboardModel) selected() (agentEntry, bool) {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return agentEntry{}, false
	}
	return rows[m.cursor], true
}

func (m agentDashboardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// The input scrolls against its own width while it is being typed into,
		// so it has to learn the terminal size here rather than at render time.
		m.filterInput.SetWidth(filterInputWidth(msg.Width))
		return m, nil

	case agentsRefreshedMsg:
		m.busy = false
		if msg.err != nil {
			m.statusMessage = "Refresh failed: " + msg.err.Error()
			return m, nil
		}
		selectedID := ""
		if selected, ok := m.selected(); ok {
			selectedID = selected.key()
		}
		m.agents = msg.agents
		m.restoreSelection(selectedID)
		sessions, _, _ := m.rosterCounts()
		m.statusMessage = fmt.Sprintf("Snapshot refreshed · %d sessions", sessions)
		return m, nil

	case agentActionDoneMsg:
		m.busy = false
		m.mode = dashAgentsBrowse
		if msg.err != nil {
			m.statusMessage = msg.message + ": " + msg.err.Error()
			return m, nil
		}
		m.agents = msg.agents
		if msg.selectedID != "" {
			m.restoreSelection(msg.selectedID)
		} else if rows := len(m.rows()); m.cursor >= rows {
			// An action can remove several rows at once, and a filter can hide
			// more of what is left, so step back to the last row rather than one.
			m.cursor = max(0, rows-1)
		}
		m.statusMessage = msg.message
		return m, nil

	case tea.KeyPressMsg:
		if m.busy {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.mode != dashAgentsBrowse {
			return m.updateModal(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m agentDashboardModel) updateBrowse(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.filterFocused {
		return m.updateFilter(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		// An active filter is the nearest thing to back out of, so it takes
		// priority over quitting the dashboard.
		if m.filterQuery() == "" {
			return m, tea.Quit
		}
		m.clearFilter()
	case "/":
		m.focusFilter()
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "right", "l":
		m.expandSelected()
	case "left", "h":
		m.collapseSelected()
	case "enter":
		if selected, ok := m.selected(); ok && !selected.sessionExists {
			m.statusMessage = "Session is closed · expand it to inspect or clean retained agents"
		} else if ok {
			m.attachSession = selected.run.SessionName
			m.attachPane = runPane(selected.run)
			if selected.isSession {
				m.attachPane = tmuxpkg.Pane{Index: -1}
			}
			return m, tea.Quit
		}
	case "n":
		m.openSpawn()
	case "o":
		m.openDirectoryPicker()
	case "s":
		if selected, ok := m.selected(); ok && !selected.isSession && selected.live {
			m.mode = dashAgentsConfirmStop
		} else {
			m.statusMessage = "Select a running agent to stop"
		}
	case "x":
		if _, ok := m.selected(); ok {
			m.mode = dashAgentsConfirmCleanup
		}
	case "r":
		m.busy = true
		m.statusMessage = "Refreshing session snapshot…"
		return m, refreshAgentsCmd(m.registry)
	case "?":
		m.mode = dashAgentsHelp
	}
	return m, nil
}

// updateFilter owns the keyboard while the inline filter bar is focused. The
// roster keeps rendering behind it, so up and down still move the selection.
func (m agentDashboardModel) updateFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.clearFilter()
		return m, nil
	case "enter":
		m.filterFocused = false
		m.filterInput.Blur()
		return m, nil
	case "up":
		m.moveCursor(-1)
		return m, nil
	case "down":
		m.moveCursor(1)
		return m, nil
	}
	selectedID := ""
	if selected, ok := m.selected(); ok {
		selectedID = selected.key()
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.keepFilteredSelection(selectedID)
	return m, cmd
}

func (m *agentDashboardModel) focusFilter() {
	m.filterFocused = true
	m.filterInput.Focus()
	m.filterInput.CursorEnd()
}

func (m *agentDashboardModel) clearFilter() {
	selectedID := ""
	if selected, ok := m.selected(); ok {
		selectedID = selected.key()
	}
	m.filterInput.SetValue("")
	m.filterInput.Blur()
	m.filterFocused = false
	m.keepFilteredSelection(selectedID)
}

// keepFilteredSelection follows the selected row to its new index after the
// query changed, and falls back to the top when the filter removed it.
func (m *agentDashboardModel) keepFilteredSelection(id string) {
	if id != "" {
		for index, row := range m.rows() {
			if row.key() == id {
				m.cursor = index
				return
			}
		}
	}
	m.cursor = 0
}

func (m agentDashboardModel) updateModal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case dashAgentsOpenDirectory:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.closeModal()
			return m, nil
		case "up":
			m.moveDirectoryCursor(-1)
			return m, nil
		case "down":
			m.moveDirectoryCursor(1)
			return m, nil
		case "home":
			m.directoryCursor = 0
			return m, nil
		case "end":
			m.directoryCursor = max(0, len(m.filteredDirectories)-1)
			return m, nil
		case "enter":
			if path, ok := m.selectedDirectory(); ok {
				m.openDirectory = path
				return m, tea.Quit
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.directoryInput, cmd = m.directoryInput.Update(msg)
		m.filterDirectories()
		return m, cmd

	case dashAgentsSpawn:
		switch msg.String() {
		case "esc":
			m.closeModal()
			return m, nil
		case "tab", "shift+tab":
			if msg.String() == "shift+tab" {
				m.focusedInput = (m.focusedInput + 3) % 4
			} else {
				m.focusedInput = (m.focusedInput + 1) % 4
			}
			m.syncInputFocus()
			return m, nil
		case "left", "right", "space":
			if m.focusedInput == 2 {
				m.toggleSpawnAgent()
				return m, nil
			}
		case "enter":
			task := strings.TrimSpace(m.taskInput.Value())
			path := strings.TrimSpace(m.pathInput.Value())
			if task == "" || path == "" {
				m.statusMessage = "Project path and task are required"
				return m, nil
			}
			m.busy = true
			m.statusMessage = "Creating worktree and starting agent…"
			return m, spawnManagedAgentCmd(
				m.cfg,
				m.registry,
				path,
				task,
				strings.TrimSpace(m.baseInput.Value()),
				m.spawnAgent,
				m.spawnAgentCommand(),
			)
		}
		return m.updateFocusedInput(msg)

	case dashAgentsConfirmStop:
		switch msg.String() {
		case "y", "enter":
			selected, ok := m.selected()
			if !ok {
				m.closeModal()
				return m, nil
			}
			m.busy = true
			m.statusMessage = "Stopping agent process…"
			return m, stopAgentCmd(m.registry, selected)
		case "n", "esc":
			m.closeModal()
		}

	case dashAgentsConfirmCleanup:
		switch msg.String() {
		case "y", "enter":
			selected, ok := m.selected()
			if !ok {
				m.closeModal()
				return m, nil
			}
			m.busy = true
			m.statusMessage = "Removing selected tmux resources…"
			return m, cleanupAgentCmd(m.registry, selected)
		case "n", "esc":
			m.closeModal()
		}

	case dashAgentsHelp:
		m.closeModal()
	}
	return m, nil
}

func (m agentDashboardModel) updateFocusedInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focusedInput {
	case 0:
		m.taskInput, cmd = m.taskInput.Update(msg)
	case 1:
		m.pathInput, cmd = m.pathInput.Update(msg)
	case 3:
		m.baseInput, cmd = m.baseInput.Update(msg)
	}
	return m, cmd
}

func (m *agentDashboardModel) openSpawn() {
	m.mode = dashAgentsSpawn
	m.focusedInput = 0
	m.taskInput.SetValue("")
	m.taskInput.Placeholder = "What should the agent do?"
	path := m.cwd
	if selected, ok := m.selected(); ok {
		path = firstNonEmpty(selected.gitPath, selected.run.CWD, path)
	}
	m.pathInput.SetValue(path)
	m.spawnAgent = config.AgentClaude
	if m.cfg != nil && providerFromCommand(m.cfg.Spawn.AgentCommand) == config.AgentCodex {
		m.spawnAgent = config.AgentCodex
	}
	m.baseInput.SetValue(currentBranchAt(path))
	m.syncInputFocus()
}

func (m *agentDashboardModel) openDirectoryPicker() {
	m.mode = dashAgentsOpenDirectory
	m.directoryCursor = 0
	m.directoryError = ""
	m.openDirectory = ""
	m.directoryInput.SetValue("")
	m.directoryInput.Focus()
	m.directories = nil
	m.filteredDirectories = nil
	if m.cfg == nil {
		m.directoryError = "Configuration is unavailable."
		return
	}
	directories, err := expandDirectories(m.cfg.Directories, m.cfg.IgnoreDirectories)
	if err != nil {
		m.directoryError = err.Error()
		return
	}
	m.directories = directories
	m.filteredDirectories = append([]string(nil), directories...)
}

func (m *agentDashboardModel) filterDirectories() {
	query := strings.ToLower(strings.TrimSpace(m.directoryInput.Value()))
	m.filteredDirectories = m.filteredDirectories[:0]
	for _, path := range m.directories {
		name := strings.ToLower(filepath.Base(path))
		if query == "" || strings.Contains(name, query) || strings.Contains(strings.ToLower(path), query) {
			m.filteredDirectories = append(m.filteredDirectories, path)
		}
	}
	m.directoryCursor = 0
}

func (m *agentDashboardModel) moveDirectoryCursor(delta int) {
	if len(m.filteredDirectories) == 0 {
		return
	}
	m.directoryCursor = (m.directoryCursor + delta + len(m.filteredDirectories)) % len(m.filteredDirectories)
}

func (m agentDashboardModel) selectedDirectory() (string, bool) {
	if m.directoryCursor < 0 || m.directoryCursor >= len(m.filteredDirectories) {
		return "", false
	}
	return m.filteredDirectories[m.directoryCursor], true
}

func (m *agentDashboardModel) syncInputFocus() {
	m.taskInput.Blur()
	m.pathInput.Blur()
	m.baseInput.Blur()
	if m.focusedInput == 0 {
		m.taskInput.Focus()
		return
	}
	if m.focusedInput == 1 {
		m.pathInput.Focus()
	} else if m.focusedInput == 3 {
		m.baseInput.Focus()
	}
}

func (m *agentDashboardModel) toggleSpawnAgent() {
	if m.spawnAgent == config.AgentCodex {
		m.spawnAgent = config.AgentClaude
	} else {
		m.spawnAgent = config.AgentCodex
	}
}

func (m agentDashboardModel) spawnAgentCommand() string {
	if m.cfg == nil {
		return ""
	}
	if providerFromCommand(m.cfg.Spawn.AgentCommand) == m.spawnAgent {
		return m.cfg.Spawn.AgentCommand
	}
	if m.spawnAgent == config.AgentCodex {
		return m.cfg.Spawn.CodexCommand
	}
	return m.cfg.Spawn.ClaudeCommand
}

func currentBranchAt(path string) string {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (m *agentDashboardModel) closeModal() {
	m.mode = dashAgentsBrowse
	m.taskInput.Blur()
	m.pathInput.Blur()
	m.baseInput.Blur()
	m.directoryInput.Blur()
	m.statusMessage = ""
}

func (m *agentDashboardModel) moveCursor(delta int) {
	rows := len(m.rows())
	if rows == 0 {
		return
	}
	m.cursor = (m.cursor + delta + rows) % rows
}

func (m *agentDashboardModel) restoreSelection(id string) {
	if id != "" {
		for _, entry := range m.agents {
			if !entry.isSession && entry.key() == id {
				m.expanded[entry.run.SessionName] = true
			}
		}
		for index, row := range m.rows() {
			if row.key() == id {
				m.cursor = index
				return
			}
		}
	}
	if rows := len(m.rows()); m.cursor >= rows {
		m.cursor = max(0, rows-1)
	}
}

func (m *agentDashboardModel) expandSelected() {
	selected, ok := m.selected()
	if !ok || !selected.isSession {
		return
	}
	name := selected.run.SessionName
	if agents, _ := m.sessionAgentCounts(name); agents == 0 {
		return
	}
	if m.expanded[name] {
		m.moveCursor(1)
		return
	}
	m.expanded[name] = true
}

func (m *agentDashboardModel) collapseSelected() {
	selected, ok := m.selected()
	if !ok {
		return
	}
	delete(m.expanded, selected.run.SessionName)
	m.restoreSelection(sessionKey(selected.run.SessionName))
}

func refreshAgentsCmd(registry *service.AgentRunRegistry) tea.Cmd {
	return func() tea.Msg {
		agents, err := discoverAgents(registry)
		return agentsRefreshedMsg{agents: agents, err: err}
	}
}

func spawnManagedAgentCmd(cfg *config.Config, registry *service.AgentRunRegistry, path, task, baseBranch, spawnAgent, agentCommand string) tea.Cmd {
	return func() tea.Msg {
		if cfg == nil {
			return agentActionDoneMsg{message: "Spawn failed", err: fmt.Errorf("configuration is unavailable")}
		}
		spawnConfig := *cfg
		spawnConfig.Spawn = cfg.Spawn
		spawnConfig.Spawn.AgentCommand = agentCommand
		results, err := service.SpawnAgents([]string{task}, baseBranch, false, &spawnConfig, path)
		selectedID := ""
		if err == nil {
			for _, result := range results {
				if result.Status != "ok" {
					err = fmt.Errorf("%s", result.Error)
					break
				}
				registered, registerErr := registry.RegisterManaged(result, spawnAgent, result.PaneIndex, time.Now().UTC())
				if registerErr != nil {
					err = registerErr
					break
				}
				selectedID = registered.ID
			}
		}
		agents, refreshErr := discoverAgents(registry)
		if err == nil {
			err = refreshErr
		}
		return agentActionDoneMsg{agents: agents, message: "Agent spawned", selectedID: selectedID, err: err}
	}
}

func providerFromCommand(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return service.AgentProviderFallback
	}
	provider := service.DetectAgentProvider(filepath.Base(fields[0]))
	if provider == service.AgentProviderFallback && filepath.Base(fields[0]) == "aider" {
		return "aider"
	}
	return provider
}

func stopAgentCmd(registry *service.AgentRunRegistry, agent agentEntry) tea.Cmd {
	return func() tea.Msg {
		err := service.InterruptPane(agent.run.SessionName, runPane(agent.run))
		agents, refreshErr := discoverAgents(registry)
		if err == nil {
			err = refreshErr
		}
		return agentActionDoneMsg{agents: agents, message: "Agent interrupted", err: err}
	}
}

func cleanupAgentCmd(registry *service.AgentRunRegistry, agent agentEntry) tea.Cmd {
	return func() tea.Msg {
		err := service.KillSession(
			agent.run.SessionName,
			agent.ownsWorkspace(),
			agent.worktreePath,
			agent.branch,
			agent.gitPath,
		)
		if err == nil {
			err = registry.Delete(agent.run.ID)
		}
		agents, refreshErr := discoverAgents(registry)
		if err == nil {
			err = refreshErr
		}
		message := "Session, worktree, and branch removed"
		if !agent.ownsWorkspace() {
			message = "Tmux session removed"
		}
		return agentActionDoneMsg{agents: agents, message: message, err: err}
	}
}

var (
	dashMint       = lipgloss.Color("#9FE8C3")
	dashAmber      = lipgloss.Color("#E5B566")
	dashInk        = lipgloss.Color("#E7EBE8")
	dashMuted      = lipgloss.Color("#8A948F")
	dashFaint      = lipgloss.Color("#59615D")
	dashCanvas     = lipgloss.Color("#111513")
	dashSelected   = lipgloss.Color("#1C2923")
	dashRule       = lipgloss.Color("#2A322E")
	dashDanger     = lipgloss.Color("#E38B84")
	dashTitleStyle = lipgloss.NewStyle().Foreground(dashInk).Bold(true)
	dashMetaStyle  = lipgloss.NewStyle().Foreground(dashMuted)
	dashKeyStyle   = lipgloss.NewStyle().Foreground(dashMint).Bold(true)
)

func (m agentDashboardModel) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}

func (m agentDashboardModel) render() string {
	if m.width == 0 || m.height == 0 {
		return "Opening agent manager…"
	}
	var view string
	switch m.mode {
	case dashAgentsOpenDirectory:
		view = m.renderDirectoryPicker()
	case dashAgentsSpawn:
		view = m.renderSpawn()
	case dashAgentsConfirmStop:
		view = m.renderConfirmation(false)
	case dashAgentsConfirmCleanup:
		view = m.renderConfirmation(true)
	case dashAgentsHelp:
		view = m.renderHelp()
	default:
		view = m.renderDashboard()
	}
	return constrainTerminalView(view, m.width, m.height)
}

func (m agentDashboardModel) renderDashboard() string {
	sessions, agents, running := m.rosterCounts()
	header := lipgloss.NewStyle().
		Background(dashCanvas).
		Foreground(dashInk).
		Padding(0, 1).
		Render(fmt.Sprintf("tsp / sessions   %d sessions · %d agents · %d running", sessions, agents, running))
	project := dashMetaStyle.Render("spawn target  " + m.cwd)
	// The body gets exactly what the one-row header, spawn target, optional
	// filter bar, and footer leave behind, so the footer survives every height.
	chrome := 3
	sections := []string{header, project}
	if m.filterVisible() {
		sections = append(sections, m.renderFilterBar())
		chrome++
	}
	bodyHeight := max(1, m.height-chrome)

	var body string
	if len(m.agents) == 0 {
		body = m.renderEmpty(bodyHeight)
	} else if m.width < 86 {
		listHeight := max(1, min(bodyHeight/2, max(5, len(m.rows())+1)))
		if detailHeight := bodyHeight - listHeight; detailHeight > 0 {
			body = lipgloss.JoinVertical(
				lipgloss.Left,
				m.renderRoster(m.width, listHeight, true),
				m.renderDetail(m.width, detailHeight),
			)
		} else {
			body = m.renderRoster(m.width, bodyHeight, true)
		}
	} else {
		rosterWidth := max(30, m.width*36/100)
		detailWidth := max(40, m.width-rosterWidth-1)
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.renderRoster(rosterWidth, bodyHeight, false),
			lipgloss.NewStyle().Foreground(dashRule).Render("│"),
			m.renderDetail(detailWidth, bodyHeight),
		)
	}

	// The footer picks its variant by measurement, not by the layout
	// breakpoint, because the key list is longer than the 86-column wide
	// layout it sits under.
	footerText := m.fitFooter(
		"o open   n new   enter attach   h/l fold   / filter   s stop   x clean   r refresh   ? help   q quit",
		"o open  n new  ↵ attach  h/l fold  / filter  s stop  x clean  r refresh  ? help  q quit",
	)
	if m.busy {
		footerText = "◌ " + m.statusMessage
	} else if m.filterFocused {
		footerText = m.fitFooter(
			dashKeyStyle.Render("enter")+dashMetaStyle.Render(" apply   ")+
				dashKeyStyle.Render("esc")+dashMetaStyle.Render(" clear   ")+
				dashKeyStyle.Render("↑ / ↓")+dashMetaStyle.Render(" choose"),
			dashKeyStyle.Render("↵")+dashMetaStyle.Render(" apply  ")+
				dashKeyStyle.Render("esc")+dashMetaStyle.Render(" clear  ")+
				dashKeyStyle.Render("↑↓")+dashMetaStyle.Render(" choose"),
		)
	} else if m.statusMessage != "" {
		footerText = m.statusMessage
	}
	footer := lipgloss.NewStyle().Foreground(dashMuted).Padding(0, 1).Render(footerText)
	return lipgloss.JoinVertical(lipgloss.Left, append(sections, body, footer)...)
}

// fitFooter keeps the spelled-out footer while it fits inside the terminal,
// including the row's one column of padding on each side, and falls back to the
// abbreviated one rather than letting the last keys be chopped off.
func (m agentDashboardModel) fitFooter(wide, narrow string) string {
	if lipgloss.Width(wide)+2 <= m.width {
		return wide
	}
	return narrow
}

// filterInputWidth reserves the columns the bar spends on the FILTER label, its
// two-column gap, and the "/ " prompt, so a query longer than the terminal
// scrolls inside the input instead of running off the right edge.
func filterInputWidth(width int) int {
	return max(12, width-10)
}

// renderFilterBar draws the inline filter as one row between the spawn target
// and the body. It is not a modal, so the roster stays readable behind it.
func (m agentDashboardModel) renderFilterBar() string {
	label := lipgloss.NewStyle().Foreground(dashFaint).Bold(true).Render("FILTER")
	value := m.filterInput.View()
	if !m.filterFocused {
		value = lipgloss.NewStyle().Foreground(dashInk).Render(m.filterInput.Prompt + m.filterInput.Value())
	}
	return ansi.Truncate(label+"  "+value, max(1, m.width), "")
}

func (m agentDashboardModel) rosterCounts() (sessions, agents, running int) {
	for _, entry := range m.agents {
		switch {
		case entry.isSession:
			if entry.sessionExists {
				sessions++
			}
		case entry.live:
			agents++
			running++
		default:
			agents++
		}
	}
	return sessions, agents, running
}

// sessionRowCounts compares the session rows a filter left visible with every
// session the snapshot knows about. Agent rows are not counted, and closed
// sessions are excluded on both sides so this shares the header's denominator.
func (m agentDashboardModel) sessionRowCounts(rows []agentEntry) (visible, total int) {
	for _, row := range rows {
		if row.isSession && row.sessionExists {
			visible++
		}
	}
	for _, entry := range m.agents {
		if entry.isSession && entry.sessionExists {
			total++
		}
	}
	return visible, total
}

func (m agentDashboardModel) sessionAgentCounts(name string) (agents, running int) {
	for _, entry := range m.agents {
		if entry.isSession || entry.run.SessionName != name {
			continue
		}
		agents++
		if entry.live {
			running++
		}
	}
	return agents, running
}

func (m agentDashboardModel) sessionFold(name string) string {
	if agents, _ := m.sessionAgentCounts(name); agents == 0 {
		return " "
	}
	if m.expanded[name] {
		return "▾"
	}
	return "▸"
}

func (m agentDashboardModel) sessionSummary(name string) string {
	agents, running := m.sessionAgentCounts(name)
	if agents == 0 {
		return "no agents"
	}
	noun := "agents"
	if agents == 1 {
		noun = "agent"
	}
	return fmt.Sprintf("%d %s · %d running", agents, noun, running)
}

func (m agentDashboardModel) renderEmpty(height int) string {
	lines := []string{
		"",
		dashTitleStyle.Render("No tmux sessions or agents"),
		dashMetaStyle.Render("Open a project or spawn an agent, then refresh the snapshot."),
		"",
		dashKeyStyle.Render("o") + dashMetaStyle.Render("  open project"),
		dashKeyStyle.Render("n") + dashMetaStyle.Render("  new agent"),
	}
	lines = clampRenderedLines(lines, height)
	return lipgloss.NewStyle().Width(max(1, m.width-2)).Height(height).PaddingLeft(2).Render(strings.Join(lines, "\n"))
}

func (m agentDashboardModel) renderRoster(width, height int, compact bool) string {
	contentWidth := max(12, width-3)
	rowsVisible := max(1, (height-1)/3)
	if compact {
		rowsVisible = max(1, height-1)
	}
	rows := m.rows()
	start := 0
	if m.cursor >= rowsVisible {
		start = m.cursor - rowsVisible + 1
	}
	end := min(len(rows), start+rowsVisible)
	heading := "SESSIONS"
	visible, total := m.sessionRowCounts(rows)
	scrolling := len(rows) > rowsVisible
	switch {
	case m.filterQuery() != "" && scrolling:
		heading = fmt.Sprintf("SESSIONS  %d–%d / %d of %d", start+1, end, len(rows), total)
	case m.filterQuery() != "":
		heading = fmt.Sprintf("SESSIONS  %d / %d", visible, total)
	case scrolling:
		heading = fmt.Sprintf("SESSIONS  %d–%d / %d", start+1, end, len(rows))
	}
	lines := []string{lipgloss.NewStyle().Foreground(dashFaint).Bold(true).Render(heading)}
	if len(rows) == 0 && m.filterQuery() != "" {
		lines = append(lines, dashMetaStyle.Render("No sessions match this filter."))
	}
	for index := start; index < end; index++ {
		entry := rows[index]
		indent := ""
		if !entry.isSession {
			indent = "  "
		}
		textWidth := max(8, contentWidth-len(indent)-3)
		statusColor := dashFaint
		statusGlyph := "○"
		if entry.live {
			statusColor = dashMint
			statusGlyph = "●"
		} else if entry.sessionExists {
			statusColor = dashAmber
			statusGlyph = "◌"
		}
		glyph := lipgloss.NewStyle().Foreground(statusColor).Render(statusGlyph)
		rowHead := m.sessionFold(entry.run.SessionName) + " " + ansi.Truncate(entry.run.SessionName, textWidth-2, "…")
		rowLabel := m.sessionSummary(entry.run.SessionName)
		headStyle, labelStyle := dashTitleStyle, dashMetaStyle
		if !entry.isSession {
			rowHead = strings.ToUpper(entry.provider())
			rowLabel = firstNonEmpty(strings.TrimSpace(entry.run.Task), fmt.Sprintf("pane %d", entry.run.PaneIndex))
			headStyle, labelStyle = lipgloss.NewStyle().Foreground(dashMuted).Bold(true), dashTitleStyle
		}
		row := fmt.Sprintf(
			"%s%s %s\n%s  %s\n%s  %s",
			indent,
			glyph,
			headStyle.Render(rowHead),
			indent,
			labelStyle.Render(ansi.Truncate(rowLabel, textWidth, "…")),
			indent,
			dashMetaStyle.Render(ansi.Truncate(firstNonEmpty(entry.branch, entry.run.CWD), textWidth, "…")),
		)
		if compact {
			labelWidth := max(8, contentWidth-len(indent)-lipgloss.Width(rowHead)-4)
			row = fmt.Sprintf(
				"%s%s %s  %s",
				indent,
				glyph,
				headStyle.Render(rowHead),
				labelStyle.Render(ansi.Truncate(rowLabel, labelWidth, "…")),
			)
		}
		style := lipgloss.NewStyle().Width(contentWidth).PaddingLeft(1)
		if index == m.cursor {
			style = style.Background(dashSelected)
		}
		lines = append(lines, style.Render(row))
	}
	lines = clampRenderedLines(lines, height)
	return lipgloss.NewStyle().Width(width).Height(height).PaddingLeft(1).Render(strings.Join(lines, "\n"))
}

func (m agentDashboardModel) renderDetail(width, height int) string {
	agent, ok := m.selected()
	if !ok {
		return ""
	}
	contentWidth := max(16, width-4)
	statusColor := dashMint
	if !agent.live {
		statusColor = dashAmber
	}
	title := ansi.Truncate(agent.title(), contentWidth, "…")
	lines := []string{
		dashTitleStyle.Render(title),
		lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(strings.ToUpper(agent.status())) +
			dashMetaStyle.Render("  "+strings.ToUpper(agent.provider())),
		"",
		dashMetaStyle.Render("session   ") + agent.run.SessionName,
		dashMetaStyle.Render("project   ") + firstNonEmpty(agent.run.CWD, "—"),
		dashMetaStyle.Render("branch    ") + firstNonEmpty(agent.branch, "—"),
	}
	workspaceMode := "session"
	if agent.ownsWorkspace() {
		workspaceMode = "owned"
	}
	workspace := dashMetaStyle.Render("workspace ") + workspaceMode
	controls := dashKeyStyle.Render("enter") + " attach   " +
		dashKeyStyle.Render("s") + " stop   " + dashKeyStyle.Render("x") + " clean"
	if !agent.sessionExists {
		controls = dashKeyStyle.Render("x") + " clean retained agent"
	}
	if agent.isSession {
		workspace = dashMetaStyle.Render("agents    ") + m.sessionSummary(agent.run.SessionName)
		if agent.sessionExists {
			controls = dashKeyStyle.Render("enter") + " attach   " + dashKeyStyle.Render("h / l") + " fold   " +
				dashKeyStyle.Render("x") + " remove session"
		} else {
			controls = dashKeyStyle.Render("h / l") + " fold   " +
				dashKeyStyle.Render("x") + " clean retained agents"
		}
	}
	lines = append(lines,
		workspace,
		"",
		lipgloss.NewStyle().Foreground(dashFaint).Bold(true).Render("CONTROLS"),
		controls,
	)
	// The snapshot spends only the rows the identity and controls left over,
	// and its heading goes with it when there is no room for a line under it.
	if remaining := height - len(lines); remaining >= 3 {
		lines = append(lines,
			"",
			lipgloss.NewStyle().Foreground(dashFaint).Bold(true).Render("OUTPUT SNAPSHOT")+
				dashMetaStyle.Render("  press r to refresh"),
		)
		outputLines := nonEmptyTail(agent.output, remaining-2)
		if len(outputLines) == 0 {
			outputLines = []string{"No output captured."}
		}
		for _, line := range outputLines {
			lines = append(lines, lipgloss.NewStyle().Foreground(dashMuted).Render(ansi.Truncate(line, contentWidth, "…")))
		}
	}
	lines = clampRenderedLines(lines, height)
	return lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1).Render(strings.Join(lines, "\n"))
}

// clampRenderedLines keeps a pane inside the height it was given. A lipgloss
// Height() pads a short block but never trims a tall one, so every pane trims
// itself here instead of pushing the footer off the terminal. Entries may span
// several rows, such as a wide roster row, and are dropped whole.
func clampRenderedLines(lines []string, height int) []string {
	if height <= 0 {
		return nil
	}
	used := 0
	for index, line := range lines {
		rows := strings.Count(line, "\n") + 1
		if used+rows > height {
			return lines[:index]
		}
		used += rows
	}
	return lines
}

func nonEmptyTail(text string, limit int) []string {
	raw := strings.Split(strings.TrimSpace(text), "\n")
	var lines []string
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, " "))
		}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func (m agentDashboardModel) renderDirectoryPicker() string {
	contentWidth := min(max(24, m.width-16), 76)
	compact := m.height < 18
	lines := []string{dashTitleStyle.Render("Open a project")}
	if !compact {
		lines = append(lines,
			dashMetaStyle.Render("Configured directories open in their existing session or a new 80/20 session."),
			"",
		)
	}
	m.directoryInput.SetWidth(max(12, contentWidth-2))
	lines = append(lines,
		lipgloss.NewStyle().Foreground(dashFaint).Render("FILTER"),
		m.directoryInput.View(),
		lipgloss.NewStyle().Foreground(dashFaint).Bold(true).Render("PROJECTS"),
	)
	footerRows := 2
	if compact {
		footerRows = 1
	}
	visibleRows := max(1, min(10, m.height-6-len(lines)-footerRows))
	start := 0
	if m.directoryCursor >= visibleRows {
		start = m.directoryCursor - visibleRows + 1
	}
	end := min(len(m.filteredDirectories), start+visibleRows)
	heading := fmt.Sprintf("PROJECTS  %d", len(m.filteredDirectories))
	if len(m.filteredDirectories) > visibleRows {
		heading = fmt.Sprintf("PROJECTS  %d–%d / %d", start+1, end, len(m.filteredDirectories))
	}
	lines[len(lines)-1] = lipgloss.NewStyle().Foreground(dashFaint).Bold(true).Render(heading)
	if m.directoryError != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(dashDanger).Render(m.directoryError))
	} else if len(m.filteredDirectories) == 0 {
		empty := "No configured directories."
		if strings.TrimSpace(m.directoryInput.Value()) != "" {
			empty = "No projects match this filter."
		}
		lines = append(lines, dashMetaStyle.Render(empty))
	} else {
		for index := start; index < end; index++ {
			path := m.filteredDirectories[index]
			marker := "  "
			if index == m.directoryCursor {
				marker = "› "
			}
			name := filepath.Base(path)
			name = ansi.Truncate(name, max(8, contentWidth/2), "…")
			pathWidth := max(4, contentWidth-lipgloss.Width(name)-4)
			row := marker + dashTitleStyle.Render(name) + "  " + dashMetaStyle.Render(ansi.Truncate(path, pathWidth, "…"))
			style := lipgloss.NewStyle().Width(contentWidth)
			if index == m.directoryCursor {
				style = style.Background(dashSelected)
			}
			lines = append(lines, style.Render(row))
		}
	}
	footer := dashKeyStyle.Render("↑ / ↓") + " choose   " + dashKeyStyle.Render("enter") + " open   " + dashKeyStyle.Render("esc") + " cancel"
	if contentWidth < 38 {
		footer = dashKeyStyle.Render("↑↓") + "  " + dashKeyStyle.Render("enter") + " open  " + dashKeyStyle.Render("esc") + " back"
	}
	if !compact {
		lines = append(lines, "")
	}
	lines = append(lines, footer)
	return m.renderModalFrame(lines)
}

func (m agentDashboardModel) renderSpawn() string {
	command := "agent"
	if m.spawnAgentCommand() != "" {
		command = m.spawnAgentCommand()
	}
	agentSelector := "‹ " + strings.ToUpper(m.spawnAgent) + " ›"
	if m.focusedInput == 2 {
		agentSelector = dashKeyStyle.Render(agentSelector)
	} else {
		agentSelector = dashTitleStyle.Render(agentSelector)
	}
	lines := []string{
		dashTitleStyle.Render("Spawn an agent"),
		dashMetaStyle.Render("Creates an isolated worktree when the target is a git repository."),
		"",
		lipgloss.NewStyle().Foreground(dashFaint).Render("TASK"),
		m.taskInput.View(),
		"",
		lipgloss.NewStyle().Foreground(dashFaint).Render("PROJECT PATH"),
		m.pathInput.View(),
		"",
		lipgloss.NewStyle().Foreground(dashFaint).Render("AGENT"),
		agentSelector,
		"",
		lipgloss.NewStyle().Foreground(dashFaint).Render("BASE BRANCH"),
		m.baseInput.View(),
		"",
		dashMetaStyle.Render("command  ") + command,
		"",
		dashKeyStyle.Render("tab") + " switch field   " + dashKeyStyle.Render("← / →") + " agent   " + dashKeyStyle.Render("enter") + " spawn   " + dashKeyStyle.Render("esc") + " cancel",
	}
	return m.renderModalFrame(lines)
}

func (m agentDashboardModel) renderConfirmation(cleanup bool) string {
	agent, _ := m.selected()
	title := "Stop this agent?"
	description := "Sends Ctrl-C to the agent pane and leaves its session and files in place."
	if cleanup {
		if agent.ownsWorkspace() {
			title = "Remove this workspace?"
			description = "Kills this run's tmux session, then deletes the worktree " +
				agent.worktreePath + " and branch " + firstNonEmpty(agent.branch, "—") + " from disk."
		} else {
			title = "Remove this tmux session?"
			description = "Kills this tmux session and every agent in it. Files on disk remain in place."
		}
	}
	lines := []string{
		lipgloss.NewStyle().Foreground(dashDanger).Bold(true).Render(title),
		dashTitleStyle.Render(agent.title()),
		dashMetaStyle.Render(description),
		"",
		dashKeyStyle.Render("y / enter") + " confirm   " + dashKeyStyle.Render("n / esc") + " cancel",
	}
	return m.renderModalFrame(lines)
}

func (m agentDashboardModel) renderHelp() string {
	lines := []string{
		dashTitleStyle.Render("Session and agent manager"),
		dashMetaStyle.Render("Jump to any tmux session and manage its agents and workspaces."),
		"",
		"  " + dashKeyStyle.Render("o") + "          Open a configured directory",
		"  " + dashKeyStyle.Render("n") + "          Spawn an agent in a project",
		"  " + dashKeyStyle.Render("enter") + "      Attach to the session or agent pane",
		"  " + dashKeyStyle.Render("h / l") + "      Collapse or expand a session's agents",
		"  " + dashKeyStyle.Render("/") + "          Filter sessions by name, task, branch, or path",
		"  " + dashKeyStyle.Render("s") + "          Interrupt the agent process",
		"  " + dashKeyStyle.Render("x") + "          Remove the selected session or workspace",
		"  " + dashKeyStyle.Render("r") + "          Refresh the agent/output snapshot",
		"  " + dashKeyStyle.Render("j / k") + "      Move through the roster",
		"  " + dashKeyStyle.Render("q") + "          Quit",
		"",
		dashMetaStyle.Render("Press any key to close."),
	}
	return m.renderModalFrame(lines)
}

func (m agentDashboardModel) renderModalFrame(lines []string) string {
	frameWidth := min(max(36, m.width-8), 84)
	return lipgloss.NewStyle().
		Width(frameWidth).
		Padding(1, 2).
		Border(lipgloss.NormalBorder()).
		BorderForeground(dashRule).
		Margin(1, 2).
		Render(strings.Join(lines, "\n"))
}

func constrainTerminalView(view string, width, height int) string {
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, max(1, width), "")
	}
	return strings.Join(lines, "\n")
}
