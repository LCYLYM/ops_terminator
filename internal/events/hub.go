package events

import (
	"sync"

	"osagentmvp/internal/models"
)

type Hub struct {
	mu          sync.Mutex
	nextID      int
	subscribers map[string]map[int]chan models.Event
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[int]chan models.Event),
	}
}

func (h *Hub) Subscribe(runID string) (<-chan models.Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	id := h.nextID
	ch := make(chan models.Event, 128)
	if h.subscribers[runID] == nil {
		h.subscribers[runID] = make(map[int]chan models.Event)
	}
	h.subscribers[runID][id] = ch

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if runSubs, ok := h.subscribers[runID]; ok {
			if existing, ok := runSubs[id]; ok {
				close(existing)
				delete(runSubs, id)
			}
			if len(runSubs) == 0 {
				delete(h.subscribers, runID)
			}
		}
	}
}

func (h *Hub) SubscribeAll() (<-chan models.Event, func()) {
	return h.Subscribe("*")
}

func (h *Hub) Emit(event models.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, ch := range h.subscribers[event.RunID] {
		select {
		case ch <- event:
		default:
		}
	}
	for _, ch := range h.subscribers["*"] {
		select {
		case ch <- event:
		default:
		}
	}
}
