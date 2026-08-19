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
	"hash/fnv"
	"net"
	"net/http"
	"runtime"
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
	id     string
	conn   net.Conn
	writer *bufio.Writer
	hub    *Hub
	send   chan []byte
	topics map[EventType]bool
	mu     sync.Mutex
	closed bool
}

// hubShard is a single shard holding a subset of clients behind its own lock.
// Sharding client storage across multiple shards keyed by a fast, deterministic
// FNV-1a hash of the client ID reduces lock contention under high concurrency:
// registrations, disconnects and broadcasts for clients on different shards no
// longer serialize on a single mutex.
type hubShard struct {
	mu      sync.RWMutex
	clients map[*Client]bool
}

// defaultShardCount picks a shard count based on available CPUs, with a floor of
// 4 so small machines still get meaningful parallelism.
func defaultShardCount() int {
	n := runtime.NumCPU()
	if n < 4 {
		n = 4
	}
	return n
}

// shardIndex maps a client ID to a shard index using FNV-1a. The modulo is done
// on the unsigned hash so the result is always a valid non-negative index on
// both 32-bit and 64-bit platforms.
func shardIndex(clientID string, numShards int) int {
	hh := fnv.New32a()
	_, _ = hh.Write([]byte(clientID))
	return int(hh.Sum32() % uint32(numShards))
}

// Hub manages WebSocket clients and broadcasts events. Client storage is sharded
// internally (see hubShard) to keep lock contention low at high connection
// counts while preserving the original public API.
type Hub struct {
	clients    []*hubShard
	numShards  int
	register   chan *Client
	unregister chan *Client
	broadcast  chan *Event
	logger     *logrus.Logger
}

// NewHub creates a new WebSocket hub and starts the event loop.
func NewHub(logger *logrus.Logger) *Hub {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	numShards := defaultShardCount()
	shards := make([]*hubShard, numShards)
	for i := range shards {
		shards[i] = &hubShard{clients: make(map[*Client]bool)}
	}
	h := &Hub{
		clients:    shards,
		numShards:  numShards,
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		broadcast:  make(chan *Event, 256),
		logger:     logger,
	}
	return h
}

// shardFor returns the shard that owns the given client ID.
func (h *Hub) shardFor(clientID string) *hubShard {
	return h.clients[shardIndex(clientID, h.numShards)]
}

// Run starts the hub event loop. Should be called in a goroutine.
func (h *Hub) Run(ctx context.Context) {
	h.logger.Info("WebSocket hub started")
	for {
		select {
		case <-ctx.Done():
			// Close all clients across shards on shutdown
			for _, shard := range h.clients {
				shard.mu.Lock()
				for client := range shard.clients {
					client.close()
				}
				shard.clients = make(map[*Client]bool)
				shard.mu.Unlock()
			}
			h.logger.Info("WebSocket hub stopped")
			return

		case client := <-h.register:
			shard := h.shardFor(client.id)
			shard.mu.Lock()
			shard.clients[client] = true
			shard.mu.Unlock()
			h.logger.WithField("client_id", client.id).Debug("WebSocket client connected")

		case client := <-h.unregister:
			shard := h.shardFor(client.id)
			shard.mu.Lock()
			if _, ok := shard.clients[client]; ok {
				delete(shard.clients, client)
				client.close()
			}
			shard.mu.Unlock()
			h.logger.WithField("client_id", client.id).Debug("WebSocket client disconnected")

		case event := <-h.broadcast:
			// Marshal once; all clients share the same frame payload
			data, err := json.Marshal(event)
			if err != nil {
				h.logger.WithError(err).Error("Failed to marshal WebSocket event")
				continue
			}
			frame := encodeTextFrame(data)
			shardCount := h.numShards
			var wg sync.WaitGroup
			wg.Add(shardCount)
			for i := range h.clients {
				go func(shard *hubShard) {
					defer wg.Done()
					shard.mu.RLock()
					for client := range shard.clients {
						// Check topic subscription when non-empty
						if len(client.topics) > 0 {
							if _, sub := client.topics[event.Type]; !sub {
								continue
							}
						}
						select {
						case client.send <- frame:
						default:
							// Send buffer full: drop for this slow consumer
							h.logger.WithField("client_id", client.id).Warn("Client send buffer full, dropping message")
						}
					}
					shard.mu.RUnlock()
				}(h.clients[i])
			}
			wg.Wait()
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

// ClientCount returns the total number of connected clients across all shards.
// Each shard is read under its own lock, so this operation has O(numShards)
// locking overhead rather than O(1) on a single global mutex.
func (h *Hub) ClientCount() int {
	total := 0
	for _, shard := range h.clients {
		shard.mu.RLock()
		total += len(shard.clients)
		shard.mu.RUnlock()
	}
	return total
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
			_ = conn.Close()
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
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
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
	_ = c.conn.Close()
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

	_, _ = w.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	_, _ = w.WriteString("Upgrade: websocket\r\n")
	_, _ = w.WriteString("Connection: Upgrade\r\n")
	_, _ = w.WriteString("Sec-WebSocket-Accept: " + accept + "\r\n")
	_, _ = w.WriteString("\r\n")
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

// HubEvent is a broadcast message payload used by ShardedHub.Run().
// The data field holds pre-encoded WebSocket frame bytes so Broadcast can
// forward directly to each client's send channel.
type HubEvent struct {
	Data []byte
}

// ShardedHub distributes clients across multiple shards to reduce lock contention
// at high concurrency. It provides both direct methods (Register/Unregister/Broadcast)
// and an optional Run() loop that consumes channels asynchronously.
type ShardedHub struct {
	shards    []*hubShard
	numShards int

	// Channels for client lifecycle
	register   chan *Client
	unregister chan *Client
	broadcast  chan *HubEvent

	ctx    context.Context
	cancel context.CancelFunc
}

// NewShardedHub creates a sharded hub backed by runtime.NumCPU shards (minimum 4).
func NewShardedHub(ctx context.Context) *ShardedHub {
	numShards := defaultShardCount()
	if numShards < 4 {
		numShards = 4
	}
	shards := make([]*hubShard, numShards)
	for i := range shards {
		shards[i] = &hubShard{clients: make(map[*Client]bool)}
	}
	sctx, cancel := context.WithCancel(ctx)
	return &ShardedHub{
		shards:     shards,
		numShards:  numShards,
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
		broadcast:  make(chan *HubEvent, 64),
		ctx:        sctx,
		cancel:     cancel,
	}
}

// getShard returns the shard for the given client ID using FNV-1a hashing.
func (sh *ShardedHub) getShard(clientID string) *hubShard {
	return sh.shards[shardIndex(clientID, sh.numShards)]
}

// Register adds a client to the appropriate shard (no locking of the hub itself).
func (sh *ShardedHub) Register(client *Client) {
	shard := sh.getShard(client.id)
	shard.mu.Lock()
	shard.clients[client] = true
	shard.mu.Unlock()
}

// Unregister removes a client from its shard.
func (sh *ShardedHub) Unregister(client *Client) {
	shard := sh.getShard(client.id)
	shard.mu.Lock()
	delete(shard.clients, client)
	shard.mu.Unlock()
}

// Broadcast forwards event.Data to all subscribed clients across all shards in parallel.
func (sh *ShardedHub) Broadcast(event *HubEvent) {
	var wg sync.WaitGroup
	wg.Add(sh.numShards)
	for _, shard := range sh.shards {
		go func(s *hubShard) {
			defer wg.Done()
			s.mu.RLock()
			defer s.mu.RUnlock()
			for client := range s.clients {
				select {
				case client.send <- event.Data:
				default:
					// Client buffer full; skip or mark for removal
				}
			}
		}(shard)
	}
	wg.Wait()
}

// ClientCount returns total connected clients across all shards.
func (sh *ShardedHub) ClientCount() int {
	total := 0
	for _, shard := range sh.shards {
		shard.mu.RLock()
		total += len(shard.clients)
		shard.mu.RUnlock()
	}
	return total
}

// Run starts the background loop that drains register/unregister/broadcast channels.
// Clients registered via the channels are automatically managed on their assigned shard.
// This mode is useful when you want an asynchronous, event-driven hub lifecycle.
func (sh *ShardedHub) Run() {
	defer sh.cancel()
	for {
		select {
		case client := <-sh.register:
			sh.Register(client)
		case client := <-sh.unregister:
			sh.Unregister(client)
		case event := <-sh.broadcast:
			sh.Broadcast(event)
		case <-sh.ctx.Done():
			// Cleanup: close all clients (simplified; real impl would do topic filtering too)
			for _, shard := range sh.shards {
				shard.mu.Lock()
				for client := range shard.clients {
					client.close()
				}
				shard.clients = make(map[*Client]bool)
				shard.mu.Unlock()
			}
			return
		}
	}
}

// Stop gracefully shuts down the ShardedHub by cancelling its context.
func (sh *ShardedHub) Stop() {
	sh.cancel()
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
