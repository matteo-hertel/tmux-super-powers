package service

import (
	"log"
	"sync"
	"time"
)

// Event is the interface all events implement.
type Event interface {
	EventType() string
}

// UnsubscribeFunc removes a subscriber when called.
type UnsubscribeFunc func()

// Bus is a typed pub/sub event bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[int]func(Event)
	nextID      int
}

// NewBus creates a new event bus.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[int]func(Event)),
	}
}

// Subscribe registers a handler that receives all published events.
// Returns an UnsubscribeFunc to remove the handler.
func (b *Bus) Subscribe(handler func(Event)) UnsubscribeFunc {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subscribers[id] = handler
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}
}

// Publish sends an event to all subscribers.
// Each handler is called in its own goroutine. Panics are recovered.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	handlers := make([]func(Event), 0, len(b.subscribers))
	for _, h := range b.subscribers {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(handler func(Event)) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("event bus: handler panicked on %s: %v", e.EventType(), r)
				}
			}()
			handler(e)
		}(h)
	}
}

// --- Core lifecycle events ---

type SessionCreatedEvent struct{ Name string }

func (e SessionCreatedEvent) EventType() string { return "session.created" }

type SessionRemovedEvent struct{ Name string }

func (e SessionRemovedEvent) EventType() string { return "session.removed" }

type StatusChangedEvent struct {
	Session string
	From    string
	To      string
}

func (e StatusChangedEvent) EventType() string { return "status.changed" }

type PaneUpdatedEvent struct {
	Session   string
	PaneIndex int
	Content   string
}

func (e PaneUpdatedEvent) EventType() string { return "pane.updated" }

// --- Agent health events ---

type AgentStuckEvent struct {
	Session      string
	PaneIndex    int
	IdleDuration time.Duration
}

func (e AgentStuckEvent) EventType() string { return "agent.stuck" }

type AgentCrashedEvent struct {
	Session     string
	PaneIndex   int
	PrevProcess string
}

func (e AgentCrashedEvent) EventType() string { return "agent.crashed" }

type AgentWaitingEvent struct {
	Session   string
	PaneIndex int
	Prompt    string
}

func (e AgentWaitingEvent) EventType() string { return "agent.waiting" }

// --- PR/CI lifecycle events ---

type PRDetectedEvent struct {
	Session  string
	PRNumber int
	URL      string
}

func (e PRDetectedEvent) EventType() string { return "pr.detected" }

type CIStatusChangedEvent struct {
	Session  string
	PRNumber int
	From     string
	To       string
}

func (e CIStatusChangedEvent) EventType() string { return "ci.status.changed" }

type ReviewsChangedEvent struct {
	Session   string
	PRNumber  int
	Count     int
	PrevCount int
}

func (e ReviewsChangedEvent) EventType() string { return "reviews.changed" }

type PRMergedEvent struct {
	Session  string
	PRNumber int
}

func (e PRMergedEvent) EventType() string { return "pr.merged" }

// --- Action events ---

type FixAttemptedEvent struct {
	Session     string
	FixType     string // "ci" or "reviews"
	Attempt     int
	MaxAttempts int
}

func (e FixAttemptedEvent) EventType() string { return "fix.attempted" }

type CleanupCompletedEvent struct {
	Session      string
	WorktreePath string
	Branch       string
}

func (e CleanupCompletedEvent) EventType() string { return "cleanup.completed" }
