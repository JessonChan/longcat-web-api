package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Claude API compatible request structure
type ClaudeAPIRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []ClaudeMessage `json:"messages"`
	Stream    bool            `json:"stream,omitempty"`
	System    interface{}     `json:"system,omitempty"` // string or []ClaudeMessageContent
}

type ClaudeMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []ClaudeMessageContent
}

type ClaudeMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Claude API response structure
type ClaudeAPIResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []ClaudeResponseContent `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason,omitempty"`
	StopSequence *string                 `json:"stop_sequence,omitempty"`
	Usage        ClaudeUsage             `json:"usage"`
	Container    *ClaudeContainer        `json:"container,omitempty"`
}

type ClaudeResponseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ClaudeUsage struct {
	InputTokens              int                  `json:"input_tokens"`
	OutputTokens             int                  `json:"output_tokens"`
	CacheCreationInputTokens *int                 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int                 `json:"cache_read_input_tokens,omitempty"`
	ServiceTier              *string              `json:"service_tier,omitempty"`
	ServerToolUse            *ClaudeServerToolUse `json:"server_tool_use,omitempty"`
}

type ClaudeServerToolUse struct {
	WebSearchRequests int `json:"web_search_requests"`
}

type ClaudeContainer struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Claude streaming response
type ClaudeStreamChunk struct {
	Type         string              `json:"type"`
	Index        int                 `json:"index,omitempty"`
	Delta        *ClaudeStreamDelta  `json:"delta,omitempty"`
	Message      *ClaudeAPIResponse  `json:"message,omitempty"`
	Usage        *ClaudeUsage        `json:"usage,omitempty"`
	ContentBlock *ClaudeContentBlock `json:"content_block,omitempty"`
	MessageDelta *ClaudeMessageDelta `json:"message_delta,omitempty"`
}

type ClaudeStreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ClaudeMessageDelta struct {
	Type  string      `json:"type"`
	Delta ClaudeDelta `json:"delta"`
	Usage ClaudeUsage `json:"usage"`
}

type ClaudeDelta struct {
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

type ClaudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ClaudeService implements APIService for Claude compatibility
type ClaudeService struct {
	longCatClient *LongCatClient
}

func NewClaudeService(client *LongCatClient) *ClaudeService {
	return &ClaudeService{
		longCatClient: client,
	}
}

func (s *ClaudeService) ProcessRequest(ctx context.Context, requestBody []byte, conversationID string) (*http.Response, error) {
	var req ClaudeAPIRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return nil, fmt.Errorf("invalid Claude request: %w", err)
	}

	longCatReq, err := s.convertRequest(requestBody, conversationID)
	if err != nil {
		return nil, err
	}

	return s.longCatClient.SendRequest(ctx, longCatReq)
}

// convertRequest converts Claude request format to LongCat request format
func (s *ClaudeService) convertRequest(requestBody []byte, conversationID string) (LongCatRequest, error) {
	var claudeReq ClaudeAPIRequest
	if err := json.Unmarshal(requestBody, &claudeReq); err != nil {
		return LongCatRequest{}, fmt.Errorf("invalid Claude request: %w", err)
	}

	var content string
	if len(claudeReq.Messages) > 0 {
		lastMsg := claudeReq.Messages[len(claudeReq.Messages)-1]
		if str, ok := lastMsg.Content.(string); ok {
			content = str
		}
		if ls, ok := lastMsg.Content.([]interface{}); ok {
			for _, part := range ls {
				if str, ok := part.(map[string]any); ok {
					content += str["text"].(string)
				}
			}
		}
	}

	// Handle system message if present
	// Note: LongCat doesn't have a separate system field, so we prepend it to the content
	if claudeReq.System != nil {
		systemContent := s.extractSystemContent(claudeReq.System)
		if systemContent != "" {
			content = "System: " + systemContent + "\n\nUser: " + content
		}
	}

	return LongCatRequest{
		Content:        content,
		ConversationId: conversationID,
		ReasonEnabled:  0,
		SearchEnabled:  0,
		Regenerate:     0,
	}, nil
}

// extractSystemContent extracts content from the system field (string or []ClaudeMessageContent)
func (s *ClaudeService) extractSystemContent(system interface{}) string {
	switch v := system.(type) {
	case string:
		return v
	case []interface{}:
		var content strings.Builder
		for _, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemType, ok := itemMap["type"].(string); ok && itemType == "text" {
					if itemText, ok := itemMap["text"].(string); ok {
						content.WriteString(itemText)
					}
				}
			}
		}
		return content.String()
	default:
		return ""
	}
}

func (s *ClaudeService) ConvertResponse(resp *http.Response, stream bool) (<-chan interface{}, <-chan error) {
	chunks := make(chan interface{}, 10) // Buffered channel
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)
		defer resp.Body.Close()

		processor := NewStreamProcessor()
		openAIChunks, rawErrs := processor.ProcessStream(resp, stream)

		// Convert OpenAI chunks to Claude format
		for {
			select {
			case openAIChunk, ok := <-openAIChunks:
				if !ok {
					return
				}
				// Convert OpenAI chunk to Claude format
				if claudeChunk := s.convertOpenAIToClaudeChunk(openAIChunk, processor); claudeChunk != nil {
					select {
					case chunks <- claudeChunk:
					case <-time.After(5 * time.Second):
						errs <- fmt.Errorf("timeout sending chunk")
						return
					}
				}
			case err := <-rawErrs:
				select {
				case errs <- err:
				default:
				}
				return
			}
		}
	}()

	return chunks, errs
}

func (s *ClaudeService) convertOpenAIToClaudeChunk(openAIChunk ChatCompletionChunk, processor *StreamProcessor) interface{} {
	// Ensure we have valid choices
	if len(openAIChunk.Choices) == 0 {
		return nil
	}

	choice := openAIChunk.Choices[0]

	// Handle content delta
	if choice.Delta.Content != "" {
		return ClaudeStreamChunk{
			Type:  "content_block_delta",
			Index: 0,
			Delta: &ClaudeStreamDelta{
				Type: "text_delta",
				Text: choice.Delta.Content,
			},
		}
	}

	// Handle final message with proper Claude stop reason
	if choice.FinishReason != "" {
		stopReason := s.mapToClaudeStopReason(choice.FinishReason)

		// Create message delta with final usage and stop reason
		return ClaudeStreamChunk{
			Type: "message_delta",
			MessageDelta: &ClaudeMessageDelta{
				Type: "message_delta",
				Delta: ClaudeDelta{
					StopReason: &stopReason,
				},
				Usage: ClaudeUsage{
					InputTokens:  processor.tokenInfo.PromptTokens,
					OutputTokens: processor.tokenInfo.CompletionTokens,
				},
			},
		}
	}

	return nil
}

// mapToClaudeStopReason maps OpenAI finish reasons to Claude stop reasons
func (s *ClaudeService) mapToClaudeStopReason(openAIReason string) string {
	switch openAIReason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}

func (s *ClaudeService) GetResponseContentType(stream bool) string {
	if stream {
		return "text/event-stream"
	}
	return "application/json"
}

func (s *ClaudeService) HandleNonStreamingResponse(w http.ResponseWriter, chunks <-chan interface{}, errs <-chan error) error {
	var fullContent strings.Builder
	var finalStopReason string
	var inputTokens, outputTokens int
	messageID := uuid.New().String()

	// Process all chunks
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				// Build final response with proper Claude format
				response := &ClaudeAPIResponse{
					ID:   messageID,
					Type: "message",
					Role: "assistant",
					Content: []ClaudeResponseContent{{
						Type: "text",
						Text: fullContent.String(),
					}},
					Model:      "LongCat-Flash",
					StopReason: finalStopReason,
					Usage: ClaudeUsage{
						InputTokens:  inputTokens,
						OutputTokens: outputTokens,
					},
				}

				w.Header().Set("Content-Type", "application/json")
				return json.NewEncoder(w).Encode(response)
			}

			if openAIChunk, ok := chunk.(ChatCompletionChunk); ok {
				if openAIChunk.Choices != nil && len(openAIChunk.Choices) > 0 {
					fullContent.WriteString(openAIChunk.Choices[0].Delta.Content)
					if openAIChunk.Choices[0].FinishReason != "" {
						finalStopReason = s.mapToClaudeStopReason(openAIChunk.Choices[0].FinishReason)
					}
				}
				// Extract token info from processor if available
				// Note: This would need to be passed through the chunk or accessed differently
			}

		case err := <-errs:
			if err != nil {
				return fmt.Errorf("error processing chunks: %w", err)
			}
		}
	}
}

func (s *ClaudeService) HandleStreamingResponse(w http.ResponseWriter, flusher http.Flusher, chunks <-chan interface{}, errs <-chan error) error {
	messageID := uuid.New().String()
	sentMessageStart := false
	sentContentBlockStart := false
	sentMessageDelta := false
	hasReceivedContent := false
	var inputTokens, outputTokens int

	for {
		select {
		case chunk, ok := <-chunks:
			fmt.Printf("Received chunk: %+v :%v\n", chunk, ok)
			if !ok {
				if !hasReceivedContent {
					// Send complete default sequence if no content was received
					s.sendDefaultSequence(w, flusher, messageID)
					return nil
				}

				// Send final message_stop if not already sent
				if !sentMessageDelta {
					s.sendMessageDelta(w, flusher, messageID, "end_turn", inputTokens, outputTokens)
					sentMessageDelta = true
				}

				s.sendMessageStop(w, flusher)
				return nil
			}

			hasReceivedContent = true

			if claudeChunk, ok := chunk.(ClaudeStreamChunk); ok {
				switch claudeChunk.Type {
				case "content_block_delta":
					// Send message_start if not already sent
					if !sentMessageStart {
						s.sendMessageStart(w, flusher, messageID, 0, 0)
						sentMessageStart = true
					}

					// Send content_block_start if not already sent
					if !sentContentBlockStart {
						s.sendContentBlockStart(w, flusher)
						sentContentBlockStart = true
					}

					// Send the content delta
					if data, err := json.Marshal(claudeChunk); err == nil {
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", claudeChunk.Type, data)
						flusher.Flush()
					}

				case "message_delta":
					// Send message_start if not already sent
					if !sentMessageStart {
						s.sendMessageStart(w, flusher, messageID,
							claudeChunk.MessageDelta.Usage.InputTokens,
							claudeChunk.MessageDelta.Usage.OutputTokens)
						sentMessageStart = true
					}

					// Send content_block_start if not already sent
					if !sentContentBlockStart {
						s.sendContentBlockStart(w, flusher)
						sentContentBlockStart = true
					}

					// Send content_block_stop before message_delta
					s.sendContentBlockStop(w, flusher)

					// Send message_delta with final usage
					if data, err := json.Marshal(claudeChunk); err == nil {
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", claudeChunk.Type, data)
						flusher.Flush()
					}

					sentMessageDelta = true
					inputTokens = claudeChunk.MessageDelta.Usage.InputTokens
					outputTokens = claudeChunk.MessageDelta.Usage.OutputTokens
				}
			}

		case err := <-errs:
			if err != nil {
				s.sendErrorEvent(w, flusher, err)
				return err
			}
		}
	}
}

// Helper methods for Claude streaming events
func (s *ClaudeService) sendMessageStart(w http.ResponseWriter, flusher http.Flusher, messageID string, inputTokens, outputTokens int) {
	msgStart := ClaudeStreamChunk{
		Type: "message_start",
		Message: &ClaudeAPIResponse{
			ID:      messageID,
			Type:    "message",
			Role:    "assistant",
			Content: []ClaudeResponseContent{},
			Model:   "LongCat-Flash",
			Usage: ClaudeUsage{
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			},
		},
	}
	if data, err := json.Marshal(msgStart); err == nil {
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", data)
		flusher.Flush()
	}
}

func (s *ClaudeService) sendContentBlockStart(w http.ResponseWriter, flusher http.Flusher) {
	blockStart := ClaudeStreamChunk{
		Type:  "content_block_start",
		Index: 0,
		ContentBlock: &ClaudeContentBlock{
			Type: "text",
		},
	}
	if data, err := json.Marshal(blockStart); err == nil {
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", data)
		flusher.Flush()
	}
}

func (s *ClaudeService) sendContentBlockStop(w http.ResponseWriter, flusher http.Flusher) {
	blockStop := ClaudeStreamChunk{
		Type:  "content_block_stop",
		Index: 0,
	}
	if data, err := json.Marshal(blockStop); err == nil {
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", data)
		flusher.Flush()
	}
}

func (s *ClaudeService) sendMessageDelta(w http.ResponseWriter, flusher http.Flusher, messageID string, stopReason string, inputTokens, outputTokens int) {
	msgDelta := ClaudeStreamChunk{
		Type: "message_delta",
		MessageDelta: &ClaudeMessageDelta{
			Type: "message_delta",
			Delta: ClaudeDelta{
				StopReason: &stopReason,
			},
			Usage: ClaudeUsage{
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			},
		},
	}
	if data, err := json.Marshal(msgDelta); err == nil {
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", data)
		flusher.Flush()
	}
}

func (s *ClaudeService) sendMessageStop(w http.ResponseWriter, flusher http.Flusher) {
	stopEvent := ClaudeStreamChunk{
		Type: "message_stop",
	}
	if data, err := json.Marshal(stopEvent); err == nil {
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", data)
		flusher.Flush()
	}
}

func (s *ClaudeService) sendDefaultSequence(w http.ResponseWriter, flusher http.Flusher, messageID string) {
	// Send complete default sequence for empty response
	s.sendMessageStart(w, flusher, messageID, 0, 0)
	s.sendContentBlockStart(w, flusher)

	// Send default content
	contentDelta := ClaudeStreamChunk{
		Type:  "content_block_delta",
		Index: 0,
		Delta: &ClaudeStreamDelta{
			Type: "text_delta",
			Text: "I apologize, but I'm unable to process your request at the moment.",
		},
	}
	if data, err := json.Marshal(contentDelta); err == nil {
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", data)
		flusher.Flush()
	}

	s.sendContentBlockStop(w, flusher)
	s.sendMessageDelta(w, flusher, messageID, "end_turn", 0, 0)
	s.sendMessageStop(w, flusher)
}

func (s *ClaudeService) sendErrorEvent(w http.ResponseWriter, flusher http.Flusher, err error) {
	errorEvent := map[string]interface{}{
		"type":  "error",
		"error": err.Error(),
	}
	if data, jsonErr := json.Marshal(errorEvent); jsonErr == nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
		flusher.Flush()
	}
}
