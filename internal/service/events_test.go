package service

import (
	"sync"
	"testing"
	"time"
)

func TestBusPublishSubscribe(t *testing.T) {
	bus := NewBus()
	var received []Event
	var mu sync.Mutex

	unsub := bus.Subscribe(func(e Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})
	defer unsub()

	bus.Publish(StatusChangedEvent{Session: "test", From: "active", To: "done"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	e, ok := received[0].(StatusChangedEvent)
	if !ok {
		t.Fatalf("expected StatusChangedEvent, got %T", received[0])
	}
	if e.Session != "test" || e.From != "active" || e.To != "done" {
		t.Errorf("unexpected event: %+v", e)
	}
}

func TestBusUnsubscribe(t *testing.T) {
	bus := NewBus()
	count := 0
	var mu sync.Mutex

	unsub := bus.Subscribe(func(e Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	bus.Publish(SessionCreatedEvent{Name: "a"})
	time.Sleep(50 * time.Millisecond)
	unsub()

	bus.Publish(SessionCreatedEvent{Name: "b"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 event after unsubscribe, got %d", count)
	}
}

func TestBusHandlerPanicRecovery(t *testing.T) {
	bus := NewBus()
	var received []Event
	var mu sync.Mutex

	// Panicking subscriber
	bus.Subscribe(func(e Event) {
		panic("test panic")
	})
	// Normal subscriber
	bus.Subscribe(func(e Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	bus.Publish(SessionCreatedEvent{Name: "test"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected normal subscriber to still receive event, got %d", len(received))
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := NewBus()
	var count1, count2 int
	var mu sync.Mutex

	bus.Subscribe(func(e Event) {
		mu.Lock()
		count1++
		mu.Unlock()
	})
	bus.Subscribe(func(e Event) {
		mu.Lock()
		count2++
		mu.Unlock()
	})

	bus.Publish(SessionCreatedEvent{Name: "x"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count1 != 1 || count2 != 1 {
		t.Errorf("expected both subscribers to receive, got %d and %d", count1, count2)
	}
}
