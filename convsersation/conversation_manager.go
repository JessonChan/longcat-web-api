package conversation

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JessonChan/longcat-web-api/types"
)

// ConversationEntry stores conversation metadata
type ConversationEntry struct {
	ConversationID string
	Messages       []types.Message // Store actual messages for comparison
	LastOriginal   []types.Message // Store last assistant response for disambiguation
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

// FindConversation implements len-2 prefix matching logic
func (cm *ConversationManager) FindConversation(messages []types.Message) (string, bool) {
	// only one message, no need to match
	if len(messages) < 2 {
		return "", false
	}
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

	// 2. Try len-2 prefix matching for new request format
	if len(messages) >= 2 {
		prefix := messages[:len(messages)-2]
		newMessages := messages[len(messages)-2:]

		// Find conversations with matching prefix
		matchingConversations := cm.findConversationsWithPrefix(prefix)

		if len(matchingConversations) == 1 {
			// Single match, use it
			matchingConversations[0].LastAccessed = time.Now()
			return matchingConversations[0].ConversationID, true
		} else if len(matchingConversations) > 1 {
			// Multiple matches, use LastOriginal to disambiguate
			bestMatch := cm.disambiguateByLastOriginal(matchingConversations, newMessages)
			if bestMatch != nil {
				bestMatch.LastAccessed = time.Now()
				return bestMatch.ConversationID, true
			}
		}
	}

	return "", false
}

// findConversationsWithPrefix finds all conversations that have the exact prefix
func (cm *ConversationManager) findConversationsWithPrefix(prefix []types.Message) []*ConversationEntry {
	var matches []*ConversationEntry

	for _, entry := range cm.conversations {
		if cm.hasExactPrefix(entry.Messages, prefix) {
			matches = append(matches, entry)
		}
	}

	return matches
}

// hasExactPrefix checks if the conversation messages start with the exact prefix
func (cm *ConversationManager) hasExactPrefix(messages, prefix []types.Message) bool {
	if len(messages) < len(prefix) {
		return false
	}

	for i := range prefix {
		if !cm.messagesEqual(messages[i], prefix[i]) {
			return false
		}
	}

	return true
}

// disambiguateByLastOriginal finds the best match using LastOriginal comparison
func (cm *ConversationManager) disambiguateByLastOriginal(conversations []*ConversationEntry, newMessages []types.Message) *ConversationEntry {
	if len(newMessages) == 0 {
		// No new messages to comparet
		return nil
	}

	// The first message in newMessages should be the assistant response
	assistantMsg := newMessages[0]

	// Try to find exact match with LastOriginal

	entries := make([]*ConversationEntry, 0)
	for _, entry := range conversations {
		if len(entry.LastOriginal) > 0 && cm.messagesEqual(entry.LastOriginal[0], assistantMsg) {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil
	}
	// find the latest entry
	lastedEntry := entries[0]
	for _, entry := range conversations {
		if entry.LastAccessed.After(lastedEntry.LastAccessed) {
			lastedEntry = entry
		}
	}
	return lastedEntry
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

	// Only append messages that don't already exist in the conversation
	uniqueMessages := cm.filterDuplicateMessages(existingEntry.Messages, newMessages)
	if len(uniqueMessages) == 0 {
		// No new messages to add, just update access time
		existingEntry.LastAccessed = time.Now()
		return
	}

	// Create new extended message history
	extendedMessages := append(existingEntry.Messages, uniqueMessages...)

	// Remove old fingerprint
	oldFingerprint := cm.GenerateFingerprint(existingEntry.Messages)
	delete(cm.conversations, oldFingerprint)

	// Add with new fingerprint
	newFingerprint := cm.GenerateFingerprint(extendedMessages)
	existingEntry.Messages = extendedMessages
	existingEntry.LastAccessed = time.Now()
	cm.conversations[newFingerprint] = existingEntry

	// Update message index
	for _, msg := range uniqueMessages {
		msgHash := cm.hashMessage(msg)
		cm.messageIndex[msgHash] = append(cm.messageIndex[msgHash], existingEntry)
	}
}

// filterDuplicateMessages returns only messages that don't already exist in the conversation
func (cm *ConversationManager) filterDuplicateMessages(existing, new []types.Message) []types.Message {
	var unique []types.Message

	for _, newMsg := range new {
		found := false
		for _, existingMsg := range existing {
			if cm.messagesEqual(newMsg, existingMsg) {
				found = true
				break
			}
		}
		if !found {
			unique = append(unique, newMsg)
		}
	}

	return unique
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

// UpdateLastOriginal updates the LastOriginal field for a conversation
func (cm *ConversationManager) UpdateLastOriginal(conversationID string, assistantMessages []types.Message) {
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

	// Update LastOriginal with the assistant response
	existingEntry.LastOriginal = assistantMessages
	existingEntry.LastAccessed = time.Now()
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
