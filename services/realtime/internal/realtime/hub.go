package realtime

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/couchbase/gocb/v2"

	"realtime/internal/db"
)

type BroadcastPayload struct {
	Topic   string      `json:"topic"`   // e.g. "db:myscope:mycollection"
	Event   string      `json:"event"`   // "INSERT", "UPDATE", "DELETE"
	Payload interface{} `json:"payload"` // JSON document
}

type Policy struct {
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	Collection string `json:"collection"`
	Action     string `json:"action"`
	Expression string `json:"expression"`
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
			// Parse scope and collection from topic (e.g. "db:myscope:mycollection")
			parts := strings.Split(message.Topic, ":")
			var policies []Policy
			if len(parts) >= 3 && parts[0] == "db" {
				scope := parts[1]
				collection := parts[2]
				policies, _ = fetchCollectionPolicies(scope, collection, "SELECT")
			}

			for client := range h.clients {
				if client.IsSubscribed(message.Topic) {
					// Check active RLS Policies
					if len(policies) > 0 && message.Payload != nil {
						// Convert payload interface to map[string]interface{} for dynamic evaluation
						docBytes, err := json.Marshal(message.Payload)
						if err != nil {
							continue
						}
						var doc map[string]interface{}
						if err := json.Unmarshal(docBytes, &doc); err != nil {
							continue
						}

						allowed := false
						for _, policy := range policies {
							if evaluatePolicyInMemory(doc, policy.Expression, client.UID, client.Role) {
								allowed = true
								break
							}
						}
						if !allowed {
							// Filter out message: Client does not have SELECT access to this mutated row
							continue
						}
					}

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

func fetchCollectionPolicies(scopeName, collectionName, action string) ([]Policy, error) {
	if db.Instance == nil || db.Instance.Cluster == nil {
		return nil, nil // Return empty if database connection is not active (e.g. during testing)
	}

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	queryStr := fmt.Sprintf("SELECT id, scope, collection, action, expression FROM `%s`.`auth`.`policies` WHERE scope = $1 AND collection = $2 AND action = $3", bucketName)
	rows, err := db.Instance.Cluster.Query(queryStr, &gocb.QueryOptions{
		PositionalParameters: []interface{}{scopeName, collectionName, action},
	})
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var policies []Policy
	for rows.Next() {
		var policy Policy
		if err := rows.Row(&policy); err == nil {
			policies = append(policies, policy)
		}
	}
	return policies, nil
}

func evaluatePolicyInMemory(doc map[string]interface{}, expression string, uid string, role string) bool {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return true
	}

	operators := []string{"==", "!=", "=", "<>", ">=", "<=", ">", "<"}
	var op string
	var left, right string

	for _, o := range operators {
		if strings.Contains(expression, o) {
			op = o
			parts := strings.SplitN(expression, o, 2)
			left = strings.TrimSpace(parts[0])
			right = strings.TrimSpace(parts[1])
			break
		}
	}

	if op == "" {
		field := strings.Trim(expression, "` ")
		if val, ok := doc[field].(bool); ok {
			return val
		}
		return false
	}

	left = strings.Trim(left, "` ")
	val, ok := doc[left]
	if !ok {
		return false
	}

	compareVal := right
	if right == "$uid" {
		compareVal = uid
	} else if right == "$role" {
		compareVal = role
	} else {
		compareVal = strings.Trim(right, `"'`)
	}

	valStr := fmt.Sprintf("%v", val)

	switch op {
	case "==", "=":
		return valStr == compareVal
	case "!=", "<>":
		return valStr != compareVal
	default:
		return false
	}
}
