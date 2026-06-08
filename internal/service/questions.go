package service

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/matteo-hertel/tmux-super-powers/internal/agentlog"
)

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type PendingQuestion struct {
	ID              string           `json:"id"`
	RunID           string           `json:"runId"`
	SessionName     string           `json:"sessionName"`
	PaneIndex       int              `json:"paneIndex"`
	Prompt          string           `json:"prompt"`
	Options         []QuestionOption `json:"options,omitempty"`
	MultiSelect     bool             `json:"multiSelect"`
	FreeTextAllowed bool             `json:"freeTextAllowed"`
	Answered        bool             `json:"answered,omitempty"`
	CreatedAt       time.Time        `json:"createdAt"`
	AnsweredAt      *time.Time       `json:"answeredAt,omitempty"`
}

type AnswerQuestionRequest struct {
	SelectedOptionIndexes []int  `json:"selectedOptionIndexes,omitempty"`
	Text                  string `json:"text,omitempty"`
}

type QuestionRegistry struct {
	mu        sync.RWMutex
	questions map[string]PendingQuestion
	byKey     map[string]string
}

func NewQuestionRegistry() *QuestionRegistry {
	return &QuestionRegistry{
		questions: make(map[string]PendingQuestion),
		byKey:     make(map[string]string),
	}
}

func (r *QuestionRegistry) RefreshFromLog(run AgentRun, chunks []agentlog.Chunk, now time.Time) []PendingQuestion {
	if r == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var refreshed []PendingQuestion
	seq := 0
	for _, chunk := range chunks {
		for _, item := range chunk.Items {
			if item.Type != "ask_user" {
				continue
			}
			for _, q := range item.Questions {
				createdAt := questionCreatedAt(chunk.Timestamp, now, seq)
				seq++
				pending := PendingQuestion{
					RunID:           run.ID,
					SessionName:     run.SessionName,
					PaneIndex:       run.PaneIndex,
					Prompt:          q.Question,
					Options:         convertQuestionOptions(q.Options),
					MultiSelect:     q.MultiSelect,
					FreeTextAllowed: q.FreeTextAllowed,
					CreatedAt:       createdAt,
				}
				pending = r.UpsertPending(pending)
				if item.Result != "" {
					r.MarkAnswered(pending.ID, createdAt)
					continue
				}
				if !pending.Answered {
					refreshed = append(refreshed, pending)
				}
			}
		}
	}
	return refreshed
}

func convertQuestionOptions(options []agentlog.AskUserOption) []QuestionOption {
	out := make([]QuestionOption, 0, len(options))
	for _, o := range options {
		out = append(out, QuestionOption{Label: o.Label, Description: o.Description})
	}
	return out
}

func questionCreatedAt(timestamp string, fallback time.Time, seq int) time.Time {
	if timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			return parsed.Add(time.Duration(seq) * time.Nanosecond)
		}
	}
	return fallback.Add(time.Duration(seq) * time.Nanosecond)
}

func (r *QuestionRegistry) UpsertPending(q PendingQuestion) PendingQuestion {
	if r == nil {
		return q
	}
	key := questionKey(q)
	if q.ID == "" {
		q.ID = "q_" + hashString(key)
	}
	if q.CreatedAt.IsZero() {
		q.CreatedAt = time.Now().UTC()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID := r.byKey[key]; existingID != "" {
		existing := r.questions[existingID]
		q.ID = existing.ID
		q.CreatedAt = existing.CreatedAt
		q.Answered = existing.Answered
		q.AnsweredAt = existing.AnsweredAt
	}
	r.questions[q.ID] = q
	r.byKey[key] = q.ID
	return q
}

func (r *QuestionRegistry) Find(id string) (PendingQuestion, bool) {
	if r == nil {
		return PendingQuestion{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	q, ok := r.questions[id]
	return q, ok
}

func (r *QuestionRegistry) ListPending() []PendingQuestion {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var pending []PendingQuestion
	for _, q := range r.questions {
		if !q.Answered {
			pending = append(pending, q)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})
	return pending
}

func (r *QuestionRegistry) LatestForRun(runID string) (PendingQuestion, bool) {
	if r == nil {
		return PendingQuestion{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest PendingQuestion
	for _, q := range r.questions {
		if q.RunID != runID || q.Answered {
			continue
		}
		if latest.ID == "" || q.CreatedAt.After(latest.CreatedAt) {
			latest = q
			continue
		}
		if q.CreatedAt.Equal(latest.CreatedAt) && q.ID > latest.ID {
			latest = q
		}
	}
	return latest, latest.ID != ""
}

func (r *QuestionRegistry) MarkAnswered(id string, now time.Time) (PendingQuestion, bool) {
	if r == nil {
		return PendingQuestion{}, false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	q, ok := r.questions[id]
	if !ok {
		return PendingQuestion{}, false
	}
	q.Answered = true
	q.AnsweredAt = &now
	r.questions[id] = q
	return q, true
}

func BuildQuestionAnswer(q PendingQuestion, req AnswerQuestionRequest) (text string, freeText bool, optionCount int, err error) {
	optionCount = len(q.Options)
	if strings.TrimSpace(req.Text) != "" {
		if !q.FreeTextAllowed {
			return "", false, optionCount, errors.New("free text is not allowed for this question")
		}
		return req.Text, true, optionCount, nil
	}
	if len(req.SelectedOptionIndexes) == 0 {
		return "", false, optionCount, errors.New("answer requires selectedOptionIndexes or text")
	}
	if !q.MultiSelect && len(req.SelectedOptionIndexes) > 1 {
		return "", false, optionCount, errors.New("question does not allow multiple selections")
	}
	values := make([]string, 0, len(req.SelectedOptionIndexes))
	for _, idx := range req.SelectedOptionIndexes {
		if idx < 0 || idx >= len(q.Options) {
			return "", false, optionCount, fmt.Errorf("selected option index %d out of range", idx)
		}
		values = append(values, fmt.Sprintf("%d", idx+1))
	}
	return strings.Join(values, ","), false, optionCount, nil
}

func questionKey(q PendingQuestion) string {
	var b strings.Builder
	b.WriteString(q.RunID)
	b.WriteByte('|')
	b.WriteString(q.Prompt)
	for _, o := range q.Options {
		b.WriteByte('|')
		b.WriteString(o.Label)
		b.WriteByte(':')
		b.WriteString(o.Description)
	}
	return b.String()
}

func hashString(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:8])
}
