package ws

import "sync"

// Hub keeps track of every websocket session connected to THIS process and
// fans out messages to all of them. It has no idea where messages come
// from (REST call, cron job, Redis...) — that's the publisher/subscriber's
// job.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}

	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
	}
}

// Run must be started once, in its own goroutine, before the hub is used.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					// Slow/stuck client: drop it rather than block every
					// other client on the hub.
					close(c.send)
					delete(h.clients, c)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast implements broker.Broadcaster. Called by the Redis subscriber
// for every message it receives — the Hub just relays it to local clients.
func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
}
