package conversation

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Jessonchan/longcat-web-api/types"
)

// ConversationEntry stores conversation metadata
type ConversationEntry struct {
	ConversationID string
	Messages       []types.Message // Store actual messages for comparison
	LastAccessed   time.Time
	CreatedAt      time.Time
}

// ConversationManager handles mapping with robust matching
type ConversationManager struct {
	mu            sync.RWMutex
	conversations map[string]*ConversationEntry   // fingerprint -> entry
	messageIndex  map[string][]*ConversationEntry // message content hash -> list of conversations containing it
	maxAge        time.Duration
}

func NewConversationManager() *ConversationManager {
	cm := &ConversationManager{
		conversations: make(map[string]*ConversationEntry),
		messageIndex:  make(map[string][]*ConversationEntry),
		maxAge:        24 * time.Hour, // Conversations expire after 24 hours
	}

	// Start cleanup goroutine
	go cm.cleanupExpired()

	return cm
}

// hashMessage creates a hash for a single message
func (cm *ConversationManager) hashMessage(msg types.Message) string {
	content := fmt.Sprintf("%s:%s", msg.Role, msg.Content)
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", hash)
}

// GenerateFingerprint creates a unique identifier from message sequence
func (cm *ConversationManager) GenerateFingerprint(messages []types.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var parts []string
	for _, msg := range messages {
		parts = append(parts, cm.hashMessage(msg))
	}

	// Create composite hash of all message hashes
	composite := strings.Join(parts, "-")
	finalHash := sha256.Sum256([]byte(composite))
	return fmt.Sprintf("%x", finalHash)
}

// FindConversation implements robust matching logic
func (cm *ConversationManager) FindConversation(messages []types.Message) (string, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(messages) == 0 {
		return "", false
	}

	fingerprint := cm.GenerateFingerprint(messages)

	// 1. Try exact match first
	if entry, exists := cm.conversations[fingerprint]; exists {
		entry.LastAccessed = time.Now()
		return entry.ConversationID, true
	}

	// 2. Try sliding window match for partial history
	// This handles the case where new messages continue an existing conversation
	bestMatch := cm.findBestContinuation(messages)
	if bestMatch != nil {
		bestMatch.LastAccessed = time.Now()
		return bestMatch.ConversationID, true
	}

	// 3. Check if this is a subset of an existing conversation
	// This handles the case where we're querying with partial history
	subset := cm.findSupersetConversation(messages)
	if subset != nil {
		subset.LastAccessed = time.Now()
		return subset.ConversationID, true
	}

	return "", false
}

// findBestContinuation finds conversations that could be continued by the new messages
func (cm *ConversationManager) findBestContinuation(newMessages []types.Message) *ConversationEntry {
	// Look for conversations where the tail matches the head of new messages
	// Example: existing [1,2,3], new [2,3,4] -> continues the conversation

	var bestMatch *ConversationEntry
	maxOverlap := 0

	for _, entry := range cm.conversations {
		overlap := cm.calculateOverlap(entry.Messages, newMessages)
		if overlap > maxOverlap && overlap >= len(entry.Messages)/2 { // At least 50% overlap
			maxOverlap = overlap
			bestMatch = entry
		}
	}

	return bestMatch
}

// calculateOverlap finds the overlap between tail of existing and head of new messages
func (cm *ConversationManager) calculateOverlap(existing, newMessages []types.Message) int {
	maxOverlap := 0

	// Check all possible overlaps
	for i := 1; i <= len(existing) && i <= len(newMessages); i++ {
		// Check if last i messages of existing match first i messages of new
		match := true
		for j := 0; j < i; j++ {
			existingIdx := len(existing) - i + j
			if !cm.messagesEqual(existing[existingIdx], newMessages[j]) {
				match = false
				break
			}
		}
		if match {
			maxOverlap = i
		}
	}

	return maxOverlap
}

// findSupersetConversation finds conversations that contain the entire message sequence
func (cm *ConversationManager) findSupersetConversation(messages []types.Message) *ConversationEntry {
	if len(messages) == 0 {
		return nil
	}

	// Use index to find conversations containing the first message
	firstMsgHash := cm.hashMessage(messages[0])
	candidates := cm.messageIndex[firstMsgHash]

	for _, entry := range candidates {
		if cm.containsSequence(entry.Messages, messages) {
			return entry
		}
	}

	return nil
}

// containsSequence checks if haystack contains needle as a subsequence
func (cm *ConversationManager) containsSequence(haystack, needle []types.Message) bool {
	if len(needle) > len(haystack) {
		return false
	}

	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if !cm.messagesEqual(haystack[i+j], needle[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}

	return false
}

// messagesEqual compares two messages
func (cm *ConversationManager) messagesEqual(a, b types.Message) bool {
	return a.Role == b.Role && a.Content == b.Content
}

// SetConversation stores a new conversation
func (cm *ConversationManager) SetConversation(messages []types.Message, conversationID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	fingerprint := cm.GenerateFingerprint(messages)
	entry := &ConversationEntry{
		ConversationID: conversationID,
		Messages:       messages,
		LastAccessed:   time.Now(),
		CreatedAt:      time.Now(),
	}

	cm.conversations[fingerprint] = entry

	// Update message index for efficient lookup
	for _, msg := range messages {
		msgHash := cm.hashMessage(msg)
		cm.messageIndex[msgHash] = append(cm.messageIndex[msgHash], entry)
	}
}

// UpdateConversation extends an existing conversation with new messages
func (cm *ConversationManager) UpdateConversation(conversationID string, newMessages []types.Message) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Find the existing conversation
	var existingEntry *ConversationEntry
	for _, entry := range cm.conversations {
		if entry.ConversationID == conversationID {
			existingEntry = entry
			break
		}
	}

	if existingEntry == nil {
		return
	}

	// Create new extended message history
	extendedMessages := append(existingEntry.Messages, newMessages...)

	// Remove old fingerprint
	oldFingerprint := cm.GenerateFingerprint(existingEntry.Messages)
	delete(cm.conversations, oldFingerprint)

	// Add with new fingerprint
	newFingerprint := cm.GenerateFingerprint(extendedMessages)
	existingEntry.Messages = extendedMessages
	existingEntry.LastAccessed = time.Now()
	cm.conversations[newFingerprint] = existingEntry

	// Update message index
	for _, msg := range newMessages {
		msgHash := cm.hashMessage(msg)
		cm.messageIndex[msgHash] = append(cm.messageIndex[msgHash], existingEntry)
	}
}

// cleanupExpired removes old conversations
func (cm *ConversationManager) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		cm.mu.Lock()
		now := time.Now()

		// Find expired entries
		var toDelete []string
		for fingerprint, entry := range cm.conversations {
			if now.Sub(entry.LastAccessed) > cm.maxAge {
				toDelete = append(toDelete, fingerprint)
			}
		}

		// Delete expired entries
		for _, fingerprint := range toDelete {
			entry := cm.conversations[fingerprint]
			delete(cm.conversations, fingerprint)

			// Clean up message index
			for _, msg := range entry.Messages {
				msgHash := cm.hashMessage(msg)
				entries := cm.messageIndex[msgHash]

				// Remove this entry from the list
				var filtered []*ConversationEntry
				for _, e := range entries {
					if e.ConversationID != entry.ConversationID {
						filtered = append(filtered, e)
					}
				}

				if len(filtered) == 0 {
					delete(cm.messageIndex, msgHash)
				} else {
					cm.messageIndex[msgHash] = filtered
				}
			}
		}

		cm.mu.Unlock()
	}
}

// GetStats returns statistics about the conversation manager
func (cm *ConversationManager) GetStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return map[string]interface{}{
		"total_conversations": len(cm.conversations),
		"indexed_messages":    len(cm.messageIndex),
		"max_age_hours":       cm.maxAge.Hours(),
	}
}
