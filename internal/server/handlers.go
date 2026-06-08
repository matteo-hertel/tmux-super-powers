package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matteo-hertel/tmux-super-powers/internal/agentlog"
	"github.com/matteo-hertel/tmux-super-powers/internal/device"
	"github.com/matteo-hertel/tmux-super-powers/internal/service"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	tmuxOK := service.TmuxRunning()
	ghOK := service.GhAvailable()
	status := http.StatusOK
	if !tmuxOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]interface{}{
		"tmux": tmuxOK,
		"gh":   ghOK,
		"time": time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if !service.TmuxRunning() {
		writeError(w, http.StatusServiceUnavailable, "tmux is not running")
		return
	}
	sessions := s.monitor.Snapshot()
	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	// Enrich with PR data on demand
	if session.IsGitRepo && session.Branch != "" {
		service.EnrichWithPRData(session)
	}
	// Load diff on demand
	if session.IsGitRepo && session.Diff == nil {
		files, ins, del, _ := service.GetDiffStat(session.GitPath)
		if files > 0 {
			session.Diff = &service.DiffStat{Files: files, Insertions: ins, Deletions: del}
		}
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Dir      string `json:"dir"`
		LeftCmd  string `json:"leftCmd"`
		RightCmd string `json:"rightCmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" || req.Dir == "" {
		writeError(w, http.StatusBadRequest, "name and dir are required")
		return
	}
	if err := service.CreateSession(req.Name, req.Dir, req.LeftCmd, req.RightCmd); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": req.Name})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var req struct {
		CleanupWorktree bool `json:"cleanupWorktree"`
	}
	json.NewDecoder(r.Body).Decode(&req) // optional body

	err := service.KillSession(name, req.CleanupWorktree && session.IsWorktree, session.WorktreePath, session.Branch, session.GitPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleSendToPane(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var req struct {
		Pane                  int    `json:"pane"`
		Text                  string `json:"text"`
		FreeText              bool   `json:"freeText,omitempty"`
		OptionCount           int    `json:"optionCount,omitempty"`
		RunID                 string `json:"runId,omitempty"`
		QuestionID            string `json:"questionId,omitempty"`
		SelectedOptionIndexes []int  `json:"selectedOptionIndexes,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.QuestionID != "" {
		s.answerQuestionWithRequest(w, req.QuestionID, service.AnswerQuestionRequest{
			SelectedOptionIndexes: req.SelectedOptionIndexes,
			Text:                  req.Text,
		})
		return
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if req.RunID != "" {
		if s.agentRuns == nil {
			writeError(w, http.StatusNotFound, "agent run not found")
			return
		}
		run, ok := s.agentRuns.Find(req.RunID)
		if !ok {
			writeError(w, http.StatusNotFound, "agent run not found")
			return
		}
		name = run.SessionName
		req.Pane = run.PaneIndex
	}
	var err error
	if req.FreeText {
		err = s.answerFreeText(name, req.Pane, req.OptionCount, req.Text)
	} else {
		err = s.sendToPane(name, req.Pane, req.Text)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tasks     []string `json:"tasks"`
		Base      string   `json:"base"`
		Dir       string   `json:"dir"`
		NoInstall bool     `json:"noInstall"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Tasks) == 0 {
		writeError(w, http.StatusBadRequest, "tasks array is required")
		return
	}
	results, err := service.SpawnAgents(req.Tasks, req.Base, req.NoInstall, s.cfg, req.Dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Auto-track spawned sessions for lifecycle automation
	if s.watcher != nil {
		for i := range results {
			if results[i].Status == "ok" {
				s.watcher.Track(results[i].Session, results[i].Branch, results[i].WorktreePath, results[i].GitPath)
			}
		}
	}
	for i := range results {
		if results[i].Status == "ok" {
			if runID := s.registerSpawnedAgentRun(results[i].Session); runID != "" {
				results[i].AgentRunID = runID
			}
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"results": results})
}

func (s *Server) handleGetPR(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	if !session.IsGitRepo || session.Branch == "" {
		writeError(w, http.StatusBadRequest, "session is not a git repo or has no branch")
		return
	}
	service.EnrichWithPRData(session)
	if session.PR == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"pr": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"pr": session.PR})
}

func (s *Server) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	if !session.IsGitRepo {
		writeError(w, http.StatusBadRequest, "session is not a git repo")
		return
	}
	url, err := service.CreatePR(session.GitPath, session.Branch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": url})
}

func (s *Server) handleFixCI(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	service.EnrichWithPRData(session)
	if session.PR == nil || session.PR.Number == 0 {
		writeError(w, http.StatusBadRequest, "no PR found -- create one first")
		return
	}
	logs, err := service.FetchFailingCILogs(session.PR.Number)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	prompt := "The CI pipeline failed. Here are the failing logs:\n\n" + logs + "\n\nPlease fix the issues and push."
	if len(prompt) > 4000 {
		prompt = prompt[:4000] + "\n\n[truncated]"
	}
	// Send to agent pane (pane 1 by default for worktree sessions)
	agentPane := 1
	for _, p := range session.Panes {
		if p.Type == "agent" {
			agentPane = p.Index
			break
		}
	}
	if err := service.SendToPane(name, agentPane, prompt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "fix-ci prompt sent"})
}

func (s *Server) handleFixReviews(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	service.EnrichWithPRData(session)
	if session.PR == nil || session.PR.Number == 0 {
		writeError(w, http.StatusBadRequest, "no PR found -- create one first")
		return
	}
	comments, err := service.FetchPRComments(session.PR.Number)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(comments) == 0 {
		writeError(w, http.StatusBadRequest, "no review comments found")
		return
	}
	formatted := service.FormatPRComments(comments)
	prompt := "Please address these PR review comments:\n\n" + formatted

	agentPane := 1
	for _, p := range session.Panes {
		if p.Type == "agent" {
			agentPane = p.Index
			break
		}
	}
	if err := service.SendToPane(name, agentPane, prompt); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "review comments sent"})
}

func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !service.GhAvailable() {
		writeError(w, http.StatusNotImplemented, "gh CLI not installed")
		return
	}
	service.EnrichWithPRData(session)
	if session.PR == nil || session.PR.Number == 0 {
		writeError(w, http.StatusBadRequest, "no PR found -- create one first")
		return
	}
	if err := service.MergePR(session.PR.Number, session.GitPath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "merged"})
}

func (s *Server) handlePairInitiate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Name == "" {
		req.Name = "unnamed device"
	}
	code, err := s.pairing.Initiate(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Return an externally reachable address for the QR code.
	// Prefer Tailscale IP over the bind address (which may be 127.0.0.1).
	externalAddr := s.bindAddr
	if tsIP := DetectTailscaleIP(); tsIP != "" {
		externalAddr = tsIP
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"code":    code,
		"address": fmt.Sprintf("http://%s:%d", externalAddr, s.port),
	})
}

func (s *Server) handlePairComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	deviceName, err := s.pairing.Complete(req.Code)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	// Prefer the name from CLI (tsp device pair --name) over the app's generic name.
	name := deviceName
	if name == "" {
		name = req.Name
	}
	if name == "" {
		name = "unnamed device"
	}
	token := device.GenerateToken()
	id := device.GenerateDeviceID()
	d := device.Device{
		ID:       id,
		Name:     name,
		Token:    token,
		PairedAt: time.Now().UTC(),
	}
	if err := s.deviceStore.Add(d); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save device")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"token":     token,
		"device_id": id,
	})
}

func (s *Server) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "code query param required")
		return
	}
	claimed, name := s.pairing.Status(code)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"claimed":     claimed,
		"device_name": name,
	})
}

func (s *Server) handleRegisterPushToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PushToken string `json:"push_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.PushToken == "" {
		writeError(w, http.StatusBadRequest, "push_token is required")
		return
	}

	token := r.Header.Get("Authorization")
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	} else {
		token = r.URL.Query().Get("token")
	}

	if err := s.deviceStore.UpdatePushToken(token, req.PushToken); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save push token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
}

func (s *Server) handleTestPush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		Category string `json:"category"` // "waiting", "done", "error"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Title = "Test notification"
		req.Body = "Push notifications are working!"
		req.Category = "done"
	}
	if req.Title == "" {
		req.Title = "Test notification"
	}
	if req.Body == "" {
		req.Body = "Push notifications are working!"
	}
	if req.Category == "" {
		req.Category = "done"
	}

	tokens := s.deviceStore.PushTokens()
	if len(tokens) == 0 {
		writeError(w, http.StatusBadRequest, "no push tokens registered — open the app first")
		return
	}

	push := service.NewPushClient()
	var messages []service.PushMessage
	for _, token := range tokens {
		messages = append(messages, service.PushMessage{
			To:         token,
			Title:      req.Title,
			Body:       req.Body,
			Sound:      "default",
			Priority:   "high",
			CategoryID: req.Category,
			Data: map[string]string{
				"type": "test",
			},
		})
	}

	if err := push.Send(messages); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("push failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "sent",
		"recipients": len(tokens),
	})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := s.monitor.Subscribe()
	defer s.monitor.Unsubscribe(ch)

	// Send initial snapshot
	if data, err := service.MarshalSessions(s.monitor.Snapshot()); err == nil {
		conn.WriteMessage(websocket.TextMessage, data)
	}

	// Read pump (for ping/pong and close detection)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case sessions, ok := <-ch:
			if !ok {
				return
			}
			data, err := service.MarshalSessions(sessions)
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Type string `json:"type"` // "project" or "sandbox"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Type != "project" && req.Type != "sandbox" {
		writeError(w, http.StatusBadRequest, "type must be 'project' or 'sandbox'")
		return
	}

	// Resolve base path from config
	var basePath string
	if req.Type == "sandbox" {
		basePath = s.cfg.Sandbox.Path
	} else {
		basePath = s.cfg.Projects.Path
	}
	if basePath == "" {
		homeDir, _ := os.UserHomeDir()
		if req.Type == "sandbox" {
			basePath = filepath.Join(homeDir, "sandbox")
		} else {
			basePath = filepath.Join(homeDir, "projects")
		}
	}
	// Expand ~ prefix
	if strings.HasPrefix(basePath, "~/") {
		homeDir, _ := os.UserHomeDir()
		basePath = filepath.Join(homeDir, basePath[2:])
	}

	dirPath := filepath.Join(basePath, req.Name)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create directory: %v", err))
		return
	}

	// Sanitize session name
	sessionName := fmt.Sprintf("%s-%s", req.Type, req.Name)
	sessionName = strings.ReplaceAll(sessionName, ".", "-")
	sessionName = strings.ReplaceAll(sessionName, ":", "-")

	if err := service.CreateSession(sessionName, dirPath, s.cfg.Editor, ""); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"status":  "created",
		"session": sessionName,
		"path":    dirPath,
	})
}

func (s *Server) handleSessionAgentRuns(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	if s.agentRuns == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"runs": []service.AgentRun{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": s.agentRuns.ListBySession(name)})
}

func (s *Server) handleGetAgentRunLog(w http.ResponseWriter, r *http.Request) {
	if s.agentRuns == nil {
		writeError(w, http.StatusNotFound, "agent run not found")
		return
	}
	runID := r.PathValue("runId")
	run, ok := s.agentRuns.Find(runID)
	if !ok {
		writeError(w, http.StatusNotFound, "agent run not found")
		return
	}
	offset := parseOffset(r)
	resp, err := s.readAgentRunLog(run, offset)
	if err != nil {
		if fallback, ok := s.fallbackPaneLog(run, offset); ok {
			writeJSON(w, http.StatusOK, fallback)
			return
		}
		writeError(w, http.StatusNotFound, "no agent log found")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePendingQuestions(w http.ResponseWriter, r *http.Request) {
	s.refreshQuestionsFromRuns()
	if s.questions == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"questions": []service.PendingQuestion{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"questions": s.questions.ListPending()})
}

func (s *Server) handleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	s.answerQuestionWithRequest(w, r.PathValue("questionId"), decodeAnswerQuestionRequest(r))
}

func (s *Server) handleGetAgentLog(w http.ResponseWriter, r *http.Request) {
	name := ParseSessionName(r)
	session := s.monitor.FindSession(name)
	if session == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if run, ok := s.resolveRunForLegacyAgentLog(name, r); ok && run.LogPath != "" {
		offset := parseOffset(r)
		resp, err := s.readAgentRunLog(run, offset)
		if err == nil {
			resp.Sessions = legacyAgentSessionsForRun(run)
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	// Determine session directory for JSONL discovery.
	dir := session.Dir
	if session.WorktreePath != "" {
		dir = session.WorktreePath
	}

	// Resolve the agent session ID from the pane.
	// ?pane=N selects a specific agent pane; otherwise use the first agent pane.
	// The pane's AgentSessionID is pre-resolved by the monitor via lsof.
	agentSessionID := ""
	requestedPane := -1
	if paneStr := r.URL.Query().Get("pane"); paneStr != "" {
		if v, err := strconv.Atoi(paneStr); err == nil {
			requestedPane = v
		}
	}
	for _, p := range session.Panes {
		if p.Type == "agent" {
			if requestedPane >= 0 && p.Index != requestedPane {
				continue
			}
			if paneCwd := service.GetAgentPaneCwd(session.Name, p.Index); paneCwd != "" {
				dir = paneCwd
			}
			agentSessionID = p.AgentSessionID
			break
		}
	}
	if dir == "" {
		writeError(w, http.StatusNotFound, "session directory unknown")
		return
	}

	// List available agent sessions, filtered to this tmux session's panes.
	allSessions, _ := agentlog.FindAllJSONL(dir)

	// Collect agent session IDs belonging to this tmux session's panes
	paneSessionIDs := make(map[string]bool)
	for _, p := range session.Panes {
		if p.Type == "agent" && p.AgentSessionID != "" {
			paneSessionIDs[p.AgentSessionID] = true
		}
	}
	// If we have pane session IDs, filter the list to only those
	if len(paneSessionIDs) > 0 {
		var filtered []agentlog.AgentSession
		for _, as := range allSessions {
			if paneSessionIDs[as.ID] {
				filtered = append(filtered, as)
			}
		}
		// Only use filtered list if it's non-empty (fallback to all if no matches)
		if len(filtered) > 0 {
			allSessions = filtered
		}
	}

	// Pick JSONL file: ?session=<id>, or resolved from process, or most recent
	var jsonlPath string
	if sessionID := r.URL.Query().Get("session"); sessionID != "" {
		for _, as := range allSessions {
			if as.ID == sessionID {
				jsonlPath = as.Path
				break
			}
		}
		if jsonlPath == "" {
			writeError(w, http.StatusNotFound, "agent session not found")
			return
		}
	} else if agentSessionID != "" {
		// Use the session ID resolved from the running claude process
		for _, as := range allSessions {
			if as.ID == agentSessionID {
				jsonlPath = as.Path
				break
			}
		}
		if jsonlPath == "" {
			// Session ID found but JSONL doesn't exist yet — fall back
			var err error
			jsonlPath, err = agentlog.FindJSONL(dir)
			if err != nil {
				writeError(w, http.StatusNotFound, "no agent log found")
				return
			}
		}
	} else {
		var err error
		jsonlPath, err = agentlog.FindJSONL(dir)
		if err != nil {
			writeError(w, http.StatusNotFound, "no agent log found")
			return
		}
	}

	// Parse offset param
	var offset int64
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, _ = strconv.ParseInt(offsetStr, 10, 64)
	}

	// Read and parse
	entries, newOffset, err := agentlog.ReadEntriesFrom(jsonlPath, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	chunks := agentlog.Parse(entries)
	ongoing := agentlog.IsOngoing(jsonlPath)

	writeJSON(w, http.StatusOK, agentlog.AgentLogResponse{
		Chunks:     chunks,
		Ongoing:    ongoing,
		ByteOffset: newOffset,
		Sessions:   allSessions,
	})
}

func parseOffset(r *http.Request) int64 {
	var offset int64
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, _ = strconv.ParseInt(offsetStr, 10, 64)
	}
	return offset
}

func (s *Server) readAgentRunLog(run service.AgentRun, offset int64) (agentlog.AgentLogResponse, error) {
	resp, err := agentlog.ReadRunLog(agentlog.RunLogRef{
		ID:             run.ID,
		Provider:       run.Provider,
		CWD:            run.CWD,
		LogPath:        run.LogPath,
		AgentSessionID: run.AgentSessionID,
	}, offset)
	if err != nil {
		return agentlog.AgentLogResponse{}, err
	}
	if s.questions != nil {
		s.questions.RefreshFromLog(run, resp.Chunks, time.Now().UTC())
	}
	return resp, nil
}

func (s *Server) fallbackPaneLog(run service.AgentRun, offset int64) (agentlog.AgentLogResponse, bool) {
	if offset > 0 {
		return agentlog.AgentLogResponse{}, false
	}
	session := s.monitor.FindSession(run.SessionName)
	if session == nil {
		return agentlog.AgentLogResponse{}, false
	}
	for _, pane := range session.Panes {
		if pane.Index == run.PaneIndex && pane.Content != "" {
			return agentlog.AgentLogResponse{
				RunID:      run.ID,
				Provider:   run.Provider,
				Chunks:     []agentlog.Chunk{{Type: "assistant", Text: pane.Content}},
				Ongoing:    run.Status == "active" || run.Status == "waiting",
				ByteOffset: int64(len(pane.Content)),
			}, true
		}
	}
	return agentlog.AgentLogResponse{}, false
}

func (s *Server) refreshQuestionsFromRuns() {
	if s.agentRuns == nil || s.questions == nil {
		return
	}
	for _, run := range s.agentRuns.List() {
		if run.Status == "stopped" || run.LogPath == "" {
			continue
		}
		resp, err := s.readAgentRunLog(run, 0)
		if err != nil {
			continue
		}
		s.questions.RefreshFromLog(run, resp.Chunks, time.Now().UTC())
	}
}

func decodeAnswerQuestionRequest(r *http.Request) service.AnswerQuestionRequest {
	var req service.AnswerQuestionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	return req
}

func (s *Server) answerQuestionWithRequest(w http.ResponseWriter, questionID string, req service.AnswerQuestionRequest) {
	if s.questions == nil || s.agentRuns == nil {
		writeError(w, http.StatusNotFound, "question not found")
		return
	}
	q, ok := s.questions.Find(questionID)
	if !ok {
		writeError(w, http.StatusNotFound, "question not found")
		return
	}
	run, ok := s.agentRuns.Find(q.RunID)
	if !ok {
		writeError(w, http.StatusNotFound, "agent run not found")
		return
	}
	text, freeText, optionCount, err := service.BuildQuestionAnswer(q, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if freeText {
		err = s.answerFreeText(run.SessionName, run.PaneIndex, optionCount, text)
	} else {
		err = s.sendToPane(run.SessionName, run.PaneIndex, text)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.questions.MarkAnswered(questionID, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]string{"status": "answered"})
}

func (s *Server) resolveRunForLegacyAgentLog(name string, r *http.Request) (service.AgentRun, bool) {
	if s.agentRuns == nil {
		return service.AgentRun{}, false
	}
	if runID := r.URL.Query().Get("runId"); runID != "" {
		return s.agentRuns.Find(runID)
	}
	requestedPane := -1
	if paneStr := r.URL.Query().Get("pane"); paneStr != "" {
		if v, err := strconv.Atoi(paneStr); err == nil {
			requestedPane = v
		}
	}
	if requestedPane >= 0 {
		return s.agentRuns.FindByPane(name, requestedPane)
	}
	for _, run := range s.agentRuns.ListBySession(name) {
		if run.Status != "stopped" {
			return run, true
		}
	}
	runs := s.agentRuns.ListBySession(name)
	if len(runs) == 0 {
		return service.AgentRun{}, false
	}
	return runs[0], true
}

func legacyAgentSessionsForRun(run service.AgentRun) []agentlog.AgentSession {
	if run.Provider != service.AgentProviderClaude || run.CWD == "" {
		return nil
	}
	sessions, _ := agentlog.FindAllJSONL(run.CWD)
	return sessions
}

func (s *Server) registerSpawnedAgentRun(sessionName string) string {
	if s.agentRuns == nil {
		return ""
	}
	const agentPane = 1
	process := service.GetPaneProcess(sessionName, agentPane)
	info := service.DetectPaneAgentProcess(sessionName, agentPane, process)
	provider := info.Provider
	if provider == "" {
		provider = service.DetectAgentProvider(process)
	}
	if provider == service.AgentProviderFallback && s.cfg != nil && strings.Contains(s.cfg.Spawn.AgentCommand, "codex") {
		provider = service.AgentProviderCodex
	}
	cwd := service.GetAgentPaneCwd(sessionName, agentPane)
	agentSessionID := ""
	if provider == service.AgentProviderClaude {
		agentSessionID = service.GetAgentSessionID(sessionName, agentPane)
	}
	logPath, confidence := service.ResolveAgentLogPath(provider, cwd, agentSessionID)
	run, err := s.agentRuns.UpsertObserved(service.ObservedAgentRun{
		Provider:       provider,
		SessionName:    sessionName,
		PaneIndex:      agentPane,
		PID:            info.PID,
		CWD:            cwd,
		LogPath:        logPath,
		Status:         "active",
		Confidence:     confidence,
		AgentSessionID: agentSessionID,
	}, time.Now().UTC())
	if err != nil {
		return ""
	}
	return run.ID
}
