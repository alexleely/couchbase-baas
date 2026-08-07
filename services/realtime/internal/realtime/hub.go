package realtime

import "sync"

type BroadcastPayload struct {
	Topic   string      `json:"topic"`   // e.g. "db:myscope:mycollection"
	Event   string      `json:"event"`   // "INSERT", "UPDATE", "DELETE"
	Payload interface{} `json:"payload"` // JSON document
}

type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from the CDC streams
	broadcast chan BroadcastPayload

	// Register requests from the clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	mu sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan BroadcastPayload),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				if client.IsSubscribed(message.Topic) {
					select {
					case client.send <- message:
					default:
						// If client buffer is blocked, disconnect client
						go func(c *Client) {
							h.unregister <- c
						}(client)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all active topic subscribers
func (h *Hub) Broadcast(msg BroadcastPayload) {
	h.broadcast <- msg
}

func (h *Hub) RegisterClient(c *Client) {
	h.register <- c
}

func (h *Hub) UnregisterClient(c *Client) {
	h.unregister <- c
}
