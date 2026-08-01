package redteam

import (
	"context"
	"sync"
	"time"
)

// ChatMemoryStore provides conversation history with TTL and session management
type ChatMemoryStore struct {
	mu       sync.RWMutex
	sessions map[string][]ConversationTurn
	ttl      time.Duration
	maxHistory int
}

// ConversationTurn represents a single message in the chat history
type ConversationTurn struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	Role        string    `json:"role"` // "user", "assistant", "system"
	Content     string    `json:"content"`
	Metadata    *TurnMeta `json:"metadata,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// TurnMeta contains additional information about a conversation turn
type TurnMeta struct {
	APICallsExecuted []string   `json:"api_calls,omitempty"`
	ToolsUsed        []string   `json:"tools_used,omitempty"`
	ConfidenceScore  float64    `json:"confidence_score,omitempty"`
	Intent           string     `json:"intent,omitempty"`
	ParsedIntentData json.MarshalJSON `json:"parsed_intent_data,omitempty"`
	ActionsTaken     []string   `json:"actions_taken,omitempty"`
	RiskLevel        RiskLevel  `json:"risk_level,omitempty"`
	NeedsApproval    bool       `json:"needs_approval,omitempty"`
}

// NewChatMemoryStore creates a new memory store with default settings
func NewChatMemoryStore(ttl time.Duration, maxHistory int) *ChatMemoryStore {
	if ttl == 0 {
		ttl = 24 * time.Hour // Default 24 hours
	}
	if maxHistory <= 0 {
		maxHistory = 100 // Default 100 turns per session
	}
	
	return &ChatMemoryStore{
		sessions:     make(map[string][]ConversationTurn),
		ttl:          ttl,
		maxHistory:   maxHistory,
	}
}

// StartSession creates a new conversation session
func (cms *ChatMemoryStore) StartSession(ctx context.Context, sessionID string) error {
	cms.mu.Lock()
	defer cms.mu.Unlock()
	
	if _, exists := cms.sessions[sessionID]; !exists {
		cms.sessions[sessionID] = make([]ConversationTurn, 0)
		
		// Add welcome message for new sessions
		welcomeMsg := ConversationTurn{
			ID:          generateUUID(),
			SessionID:   sessionID,
			Role:        "system",
			Content:     "Welcome to CloudAI Fusion Red Team Assistant. How can I help you today?",
			Timestamp:   time.Now().UTC(),
			Metadata:    &TurnMeta{ConfidenceScore: 1.0},
		}
		cms.sessions[sessionID] = append(cms.sessions[sessionID], welcomeMsg)
	}
	
	return nil
}

// Append adds a new turn to the conversation history
func (cms *ChatMemoryStore) Append(sessionID string, turn ConversationTurn) error {
	cms.mu.Lock()
	defer cms.mu.Unlock()
	
	if _, exists := cms.sessions[sessionID]; !exists {
		if err := cms.StartSession(context.Background(), sessionID); err != nil {
			return err
		}
	}
	
	// Assign unique ID if not set
	if turn.ID == "" {
		turn.ID = generateUUID()
	}
	
	turn.SessionID = sessionID
	turn.Timestamp = time.Now().UTC()
	
	cms.sessions[sessionID] = append(cms.sessions[sessionID], turn)
	
	// Prune old entries if exceeded limit
	cms.pruneOldTurns(sessionID)
	
	return nil
}

// GetHistory retrieves conversation history for a session
func (cms *ChatMemoryStore) GetHistory(sessionID string, limit int) ([]ConversationTurn, error) {
	if limit <= 0 {
		limit = cms.maxHistory
	}
	
	cms.mu.RLock()
	defer cms.mu.RUnlock()
	
	history, exists := cms.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	
	// Return last N turns
	start := len(history) - limit
	if start < 0 {
		start = 0
	}
	
	result := make([]ConversationTurn, len(history[start:]))
	copy(result, history[start:])
	
	return result, nil
}

// GetLastN returns only the most recent N turns from each role
func (cms *ChatMemoryStore) GetLastN(sessionID string, byRole map[string]int) (map[string][]ConversationTurn, error) {
	cms.mu.RLock()
	defer cms.mu.RUnlock()
	
	history, exists := cms.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	
	result := make(map[string][]ConversationTurn)
	for role, count := range byRole {
		var recent []ConversationTurn
		for i := len(history) - 1; i >= 0 && len(recent) < count; i-- {
			if history[i].Role == role {
				recent = append(recent, history[i])
			}
		}
		// Reverse to chronological order
		for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
			recent[i], recent[j] = recent[j], recent[i]
		}
		result[role] = recent
	}
	
	return result, nil
}

// ClearSession deletes all history for a session
func (cms *ChatMemoryStore) ClearSession(sessionID string) {
	cms.mu.Lock()
	defer cms.mu.Unlock()
	delete(cms.sessions, sessionID)
}

// pruneOldTurns removes old conversation turns to manage memory
func (cms *ChatMemoryStore) pruneOldTurns(sessionID string) {
	cutoff := time.Now().Add(-cms.ttl)
	
	var recentTurns []ConversationTurn
	for _, turn := range cms.sessions[sessionID] {
		if turn.Timestamp.After(cutoff) || len(recentTurns) < cms.maxHistory {
			recentTurns = append(recentTurns, turn)
		}
	}
	
	cms.sessions[sessionID] = recentTurns
	
	// Keep only latest maxHistory turns even if within TTL
	if len(recentTurns) > cms.maxHistory {
		cms.sessions[sessionID] = recentTurns[len(recentTurns)-cms.maxHistory:]
	}
}

// ListSessions returns all active session IDs
func (cms *ChatMemoryStore) ListSessions() []string {
	cms.mu.RLock()
	defer cms.mu.RUnlock()
	
	sessions := make([]string, 0, len(cms.sessions))
	for id := range cms.sessions {
		sessions = append(sessions, id)
	}
	
	return sessions
}

// SessionStats returns statistics about a session
func (cms *ChatMemoryStore) SessionStats(sessionID string) (*SessionStatistics, error) {
	cms.mu.RLock()
	defer cms.mu.RUnlock()
	
	history, exists := cms.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	
	stats := &SessionStatistics{
		SessionID:   sessionID,
		MessageCount: len(history),
		CreatedAt:   history[0].Timestamp,
		LastActive:  history[len(history)-1].Timestamp,
		ByRole:      make(map[string]int),
	}
	
	for _, turn := range history {
		stats.ByRole[turn.Role]++
	}
	
	return stats, nil
}

// SessionStatistics holds statistics about a conversation session
type SessionStatistics struct {
	SessionID    string            `json:"session_id"`
	MessageCount int               `json:"message_count"`
	CreatedAt    time.Time         `json:"created_at"`
	LastActive   time.Time         `json:"last_active"`
	ByRole       map[string]int    `json:"by_role"`
	AvgResponseTime time.Duration `json:"avg_response_time"`
}
