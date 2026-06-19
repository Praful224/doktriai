package core

import (
	"sync"

	"github.com/praful224/doktriai/doktriai-packages"
)

type EventBus struct {
	mu   sync.RWMutex
	subs map[chan packages.Event]bool
}

func NewEventBus(capacity int) *EventBus {
	return &EventBus{
		subs: make(map[chan packages.Event]bool),
	}
}

func (eb *EventBus) Subscribe() chan packages.Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	ch := make(chan packages.Event, 20)
	eb.subs[ch] = true
	return ch
}

func (eb *EventBus) Unsubscribe(ch chan packages.Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	delete(eb.subs, ch)
	close(ch)
}

func (eb *EventBus) Publish(event packages.Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for ch := range eb.subs {
		select {
		case ch <- event:
		default:
			// Non-blocking write: skip full subscriber queues
		}
	}
}
