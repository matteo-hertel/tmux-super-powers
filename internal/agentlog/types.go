package agentlog

import "encoding/json"

// Entry represents a single JSONL line from a Claude Code session log.
type Entry struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Timestamp string          `json:"timestamp"`
	Message   *Message        `json:"message"`
	CWD       string          `json:"cwd"`
	Version   string          `json:"version"`
	IsMeta    bool            `json:"isMeta"`
	Raw       json.RawMessage `json:"-"`
}

// Message is the message field within a JSONL entry.
type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Model      string          `json:"model"`
	StopReason string          `json:"stopReason"`
}

// ContentBlock represents a block within an assistant message's content array.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	// Tool use fields
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// Tool result fields
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// AskUserOption is a single option in an AskUserQuestion question.
type AskUserOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AskUserQuestionData is a single question from the AskUserQuestion tool.
type AskUserQuestionData struct {
	Question string           `json:"question"`
	Options  []AskUserOption  `json:"options"`
}

// DisplayItem is a structured element for rendering in the mobile app.
type DisplayItem struct {
	Type          string                `json:"type"`                    // "thinking", "text", "tool_call", "ask_user"
	Text          string                `json:"text,omitempty"`
	Tool          string                `json:"tool,omitempty"`
	Summary       string                `json:"summary,omitempty"`
	Input         string                `json:"input,omitempty"`
	Result        string                `json:"result,omitempty"`
	DurationMs    int64                 `json:"durationMs,omitempty"`
	TokenEstimate int                   `json:"tokenEstimate,omitempty"`
	Questions     []AskUserQuestionData `json:"questions,omitempty"`
}

// Chunk is a display-ready group of messages.
type Chunk struct {
	Type      string        `json:"type"`
	Text      string        `json:"text,omitempty"`
	Items     []DisplayItem `json:"items,omitempty"`
	Model     string        `json:"model,omitempty"`
	Timestamp string        `json:"timestamp,omitempty"`
}

// AgentLogResponse is the API response.
type AgentLogResponse struct {
	Chunks     []Chunk `json:"chunks"`
	Ongoing    bool    `json:"ongoing"`
	ByteOffset int64   `json:"byteOffset"`
}
