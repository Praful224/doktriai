package core

import "sync"

type EventBus struct {
	mu          sync.Mutex
	buffer      int
	nextID      int64
	subscribers map[chan Event]struct{}
}

func NewEventBus(buffer int) *EventBus {
	return &EventBus{
		buffer:      buffer,
		subscribers: map[chan Event]struct{}{},
	}
}

func (b *EventBus) Subscribe() chan Event {
	ch := make(chan Event, b.buffer)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *EventBus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	close(ch)
	b.mu.Unlock()
}

func (b *EventBus) Publish(event Event) {
	b.mu.Lock()
	if event.ID == 0 {
		b.nextID++
		event.ID = b.nextID
	}
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	b.mu.Unlock()
}
