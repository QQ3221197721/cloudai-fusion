// Package websocket provides a real-time event push system for CloudAI Fusion.
// Uses standard library HTTP upgrade (RFC 6455) to push events (logs, alerts,
// workload status changes) to connected browser/CLI clients.
package websocket

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// EventType classifies real-time events.
type EventType string

const (
	EventTypeAlert          EventType = "alert"
	EventTypeWorkloadStatus EventType = "workload_status"
	EventTypeClusterHealth  EventType = "cluster_health"
	EventTypeGPUMetrics     EventType = "gpu_metrics"
	EventTypeAuditLog       EventType = "audit_log"
	EventTypeScheduler      EventType = "scheduler"
	EventTypeSystem         EventType = "system"
)

// Event is a real-time event pushed to WebSocket clients.
type Event struct {
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// Client represents a connected WebSocket client.
type Client struct {
	id       string
	conn     net.Conn
	writer   *bufio.Writer
	hub      *Hub
	send     chan []byte
	topics   map[EventType]bool
	mu       sync.Mutex
	closed   bool
}

// Hub manages WebSocket clients and broadcasts events.
type Hub struct {
	clients    map[string]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan *Event
	mu         sync.RWMutex
	logger     *logrus.Logger
}

// NewHub creates a new WebSocket hub and starts the event loop.
func NewHub(logger *logrus.Logger) *Hub {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	h := &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		broadcast:  make(chan *Event, 256),
		logger:     logger,
	}
	return h
}

// Run starts the hub event loop. Should be called in a goroutine.
func (h *Hub) Run(ctx context.Context) {
	h.logger.Info("WebSocket hub started")
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for _, c := range h.clients {
				c.close()
			}
			h.clients = make(map[string]*Client)
			h.mu.Unlock()
			h.logger.Info("WebSocket hub stopped")
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			h.mu.Unlock()
			h.logger.WithField("client_id", client.id).Debug("WebSocket client connected")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				client.close()
			}
			h.mu.Unlock()
			h.logger.WithField("client_id", client.id).Debug("WebSocket client disconnected")

		case event := <-h.broadcast:
			data, err := json.Marshal(event)
			if err != nil {
				h.logger.WithError(err).Error("Failed to marshal WebSocket event")
				continue
			}
			// Wrap as WebSocket text frame
			frame := encodeTextFrame(data)

			h.mu.RLock()
			for id, client := range h.clients {
				// Check topic subscription
				if len(client.topics) > 0 {
					if _, subscribed := client.topics[event.Type]; !subscribed {
						continue
					}
				}
				select {
				case client.send <- frame:
				default:
					h.logger.WithField("client_id", id).Warn("Client send buffer full, dropping")
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Publish sends an event to all subscribed WebSocket clients.
func (h *Hub) Publish(event *Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	select {
	case h.broadcast <- event:
	default:
		h.logger.Warn("WebSocket broadcast buffer full, event dropped")
	}
}

// PublishAlert publishes an alert event.
func (h *Hub) PublishAlert(severity, message, clusterID string) {
	h.Publish(&Event{
		Type: EventTypeAlert,
		Data: map[string]interface{}{
			"severity":   severity,
			"message":    message,
			"cluster_id": clusterID,
		},
	})
}

// PublishWorkloadStatus publishes a workload status change event.
func (h *Hub) PublishWorkloadStatus(workloadID, fromStatus, toStatus string) {
	h.Publish(&Event{
		Type: EventTypeWorkloadStatus,
		Data: map[string]interface{}{
			"workload_id": workloadID,
			"from_status": fromStatus,
			"to_status":   toStatus,
		},
	})
}

// ClientCount returns the number of connected WebSocket clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// HandleWebSocket is a Gin handler that upgrades HTTP to WebSocket.
func (h *Hub) HandleWebSocket() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Verify WebSocket upgrade headers
		if c.GetHeader("Upgrade") != "websocket" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "expected WebSocket upgrade"})
			return
		}

		// Hijack the connection
		hijacker, ok := c.Writer.(http.Hijacker)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server does not support hijacking"})
			return
		}

		conn, bufrw, err := hijacker.Hijack()
		if err != nil {
			h.logger.WithError(err).Error("Failed to hijack connection")
			return
		}

		// Perform WebSocket handshake
		if err := performHandshake(c.Request, bufrw.Writer); err != nil {
			h.logger.WithError(err).Error("WebSocket handshake failed")
			conn.Close()
			return
		}

		clientID := c.GetString("request_id")
		if clientID == "" {
			clientID = fmt.Sprintf("ws-%d", time.Now().UnixNano())
		}

		client := &Client{
			id:     clientID,
			conn:   conn,
			writer: bufrw.Writer,
			hub:    h,
			send:   make(chan []byte, 64),
			topics: make(map[EventType]bool),
		}

		// Parse optional topic filter from query
		if topics := c.Query("topics"); topics != "" {
			for _, t := range splitTopics(topics) {
				client.topics[EventType(t)] = true
			}
		}

		h.register <- client

		// Start write pump
		go client.writePump()
		// Start read pump (reads pong/close frames)
		go client.readPump()
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.hub.unregister <- c
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			_, err := c.conn.Write(msg)
			c.mu.Unlock()
			if err != nil {
				return
			}
		case <-ticker.C:
			// Send ping frame
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			_, err := c.conn.Write([]byte{0x89, 0x00}) // ping frame
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
	}()
	buf := make([]byte, 512)
	for {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, err := c.conn.Read(buf)
		if err != nil {
			return
		}
		// In a full implementation, we'd parse frames here.
		// For now, any read error triggers disconnect.
	}
}

func (c *Client) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.send)
	c.conn.Close()
}

// ============================================================================
// WebSocket Framing Helpers (RFC 6455 minimal implementation)
// ============================================================================

func performHandshake(r *http.Request, w *bufio.Writer) error {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return fmt.Errorf("missing Sec-WebSocket-Key header")
	}

	accept := computeAcceptKey(key)

	w.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	w.WriteString("Upgrade: websocket\r\n")
	w.WriteString("Connection: Upgrade\r\n")
	w.WriteString("Sec-WebSocket-Accept: " + accept + "\r\n")
	w.WriteString("\r\n")
	return w.Flush()
}

func computeAcceptKey(key string) string {
	// RFC 6455 Section 4.2.2: concatenate key + GUID, then SHA-1, then base64
	concat := key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	hasher := sha1.New()
	hasher.Write([]byte(concat))
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil))
}

func encodeTextFrame(payload []byte) []byte {
	length := len(payload)
	var frame []byte

	if length < 126 {
		frame = make([]byte, 2+length)
		frame[0] = 0x81 // FIN + text opcode
		frame[1] = byte(length)
		copy(frame[2:], payload)
	} else if length < 65536 {
		frame = make([]byte, 4+length)
		frame[0] = 0x81
		frame[1] = 126
		frame[2] = byte(length >> 8)
		frame[3] = byte(length & 0xFF)
		copy(frame[4:], payload)
	} else {
		frame = make([]byte, 10+length)
		frame[0] = 0x81
		frame[1] = 127
		for i := 0; i < 8; i++ {
			frame[9-i] = byte(length >> (8 * i))
		}
		copy(frame[10:], payload)
	}
	return frame
}

func splitTopics(s string) []string {
	var result []string
	for _, t := range splitString(s, ",") {
		t = trimSpace(t)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}

// Minimal string helpers to avoid importing strings for small ops
func splitString(s, sep string) []string {
	var result []string
	for {
		i := indexOf(s, sep)
		if i < 0 {
			result = append(result, s)
			break
		}
		result = append(result, s[:i])
		s = s[i+len(sep):]
	}
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
