package realtime

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

type ClientMsg struct {
	Event   string      `json:"event"`   // "subscribe", "unsubscribe", "auth"
	Topic   string      `json:"topic"`   // "db:myscope:mycollection"
	Payload interface{} `json:"payload"` // Custom payload e.g. token
}

type Client struct {
	Hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan BroadcastPayload

	// List of active topics subscribed to
	subscriptions map[string]bool

	// Authenticated User ID
	UID string

	// Authenticated User Role
	Role string

	mu sync.RWMutex
}

func NewClient(hub *Hub, conn *websocket.Conn, uid string, role string) *Client {
	return &Client{
		Hub:           hub,
		conn:          conn,
		send:          make(chan BroadcastPayload, 256),
		subscriptions: make(map[string]bool),
		UID:           uid,
		Role:          role,
	}
}

func (c *Client) IsSubscribed(topic string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subscriptions[topic]
}

func (c *Client) Subscribe(topic string) {
	c.mu.Lock()
	c.subscriptions[topic] = true
	c.mu.Unlock()
	log.Printf("Client %s subscribed to topic: %s", c.UID, topic)
}

func (c *Client) Unsubscribe(topic string) {
	c.mu.Lock()
	delete(c.subscriptions, topic)
	c.mu.Unlock()
	log.Printf("Client %s unsubscribed from topic: %s", c.UID, topic)
}

func (c *Client) ReadPump() {
	defer func() {
		c.Hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket connection closed unexpectedly: %v", err)
			}
			break
		}

		var msg ClientMsg
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Invalid message payload from client: %v", err)
			continue
		}

		switch msg.Event {
		case "subscribe":
			if msg.Topic != "" {
				c.Subscribe(msg.Topic)
			}
		case "unsubscribe":
			if msg.Topic != "" {
				c.Unsubscribe(msg.Topic)
			}
		case "auth":
			// Handle dynamic auth if needed
			log.Printf("Client %s requested re-auth", c.UID)
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The Hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			json.NewEncoder(w).Encode(msg)

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
