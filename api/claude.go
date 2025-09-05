package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"github.com/google/uuid"
)

// Claude API compatible request structure
type ClaudeAPIRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	Messages  []ClaudeMessage        `json:"messages"`
	Stream    bool                   `json:"stream,omitempty"`
	System    []ClaudeMessageContent `json:"system,omitempty"`
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
}

type ClaudeResponseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ClaudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Claude streaming response
type ClaudeStreamChunk struct {
	Type         string              `json:"type"`
	Index        int                 `json:"index,omitempty"`
	Delta        *ClaudeStreamDelta  `json:"delta,omitempty"`
	Message      *ClaudeAPIResponse  `json:"message,omitempty"`
	Usage        *ClaudeUsage        `json:"usage,omitempty"`
	ContentBlock *ClaudeContentBlock `json:"content_block,omitempty"`
}

type ClaudeStreamDelta struct {
	Type         string  `json:"type"`
	Text         string  `json:"text,omitempty"`
	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

type ClaudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
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

func (s *ClaudeService) NeedsSession(requestBody []byte) bool {
	var req ClaudeAPIRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return false
	}
	return len(req.Messages) == 1
}

func (s *ClaudeService) ProcessRequest(ctx context.Context, requestBody []byte, conversationID string) (*http.Response, error) {
	var req ClaudeAPIRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return nil, fmt.Errorf("invalid Claude request: %w", err)
	}

	longCatReq, err := s.ConvertRequest(requestBody, conversationID)
	if err != nil {
		return nil, err
	}

	return s.longCatClient.SendRequest(ctx, longCatReq)
}

func (s *ClaudeService) ConvertRequest(requestBody []byte, conversationID string) (LongCatRequest, error) {
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

	return LongCatRequest{
		Content:        content,
		ConversationId: conversationID,
		ReasonEnabled:  0,
		SearchEnabled:  0,
		Regenerate:     0,
	}, nil
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

	// Convert OpenAI chunk to Claude format
	if openAIChunk.Choices[0].Delta.Content != "" {
		return ClaudeStreamChunk{
			Type:  "content_block_delta",
			Index: 0,
			Delta: &ClaudeStreamDelta{
				Type: "text_delta",
				Text: openAIChunk.Choices[0].Delta.Content,
			},
		}
	}

	if openAIChunk.Choices[0].FinishReason != "" {
		return ClaudeStreamChunk{
			Type: "message_stop",
			Usage: &ClaudeUsage{
				InputTokens:  processor.tokenInfo.PromptTokens,
				OutputTokens: processor.tokenInfo.CompletionTokens,
			},
		}
	}

	return nil
}

func (s *ClaudeService) GetResponseContentType(stream bool) string {
	if stream {
		return "text/event-stream"
	}
	return "application/json"
}

func (s *ClaudeService) GetServiceType() APIServiceType {
	return ClaudeServiceType
}

func (s *ClaudeService) HandleNonStreamingResponse(w http.ResponseWriter, chunks <-chan interface{}, errs <-chan error) error {
	var finalResponse *ClaudeAPIResponse

	// Process all chunks
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				if finalResponse == nil {
					// Create default response if no chunks received
					finalResponse = &ClaudeAPIResponse{
						ID:   uuid.New().String(),
						Type: "message",
						Role: "assistant",
						Content: []ClaudeResponseContent{{
							Type: "text",
							Text: "I apologize, but I'm unable to process your request at the moment.",
						}},
						Model:      "LongCat-Flash",
						StopReason: "end_turn",
						Usage: ClaudeUsage{
							InputTokens:  0,
							OutputTokens: 0,
						},
					}
				}

				w.Header().Set("Content-Type", "application/json")
				return json.NewEncoder(w).Encode(finalResponse)
			}

			if claudeResp, ok := chunk.(*ClaudeAPIResponse); ok {
				finalResponse = claudeResp
			}

		case err := <-errs:
			if err != nil {
				return fmt.Errorf("error processing chunks: %w", err)
			}
		}
	}
}

func (s *ClaudeService) HandleStreamingResponse(w http.ResponseWriter, flusher http.Flusher, chunks <-chan interface{}, errs <-chan error) error {
	hasReceivedContent := false
	sentMessageStart := false
	sentContentBlockStart := false

	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				if !hasReceivedContent {
					// Send complete sequence if no content was received
					defaultEvents := []ClaudeStreamChunk{
						{
							Type: "message_start",
							Message: &ClaudeAPIResponse{
								ID:      uuid.New().String(),
								Type:    "message",
								Role:    "assistant",
								Content: []ClaudeResponseContent{},
								Model:   "LongCat-Flash",
								Usage:   ClaudeUsage{InputTokens: 0, OutputTokens: 0},
							},
						},
						{
							Type:  "content_block_start",
							Index: 0,
							ContentBlock: &ClaudeContentBlock{
								Type: "text",
								Text: "",
							},
						},
						{
							Type:  "content_block_delta",
							Index: 0,
							Delta: &ClaudeStreamDelta{
								Type: "text_delta",
								Text: "I apologize, but I'm unable to process your request at the moment.",
							},
						},
						{
							Type:  "content_block_stop",
							Index: 0,
						},
						{
							Type: "message_stop",
							Usage: &ClaudeUsage{
								InputTokens:  0,
								OutputTokens: 0,
							},
						},
					}

					for _, event := range defaultEvents {
						if data, err := json.Marshal(event); err == nil {
							fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
							flusher.Flush()
						}
					}
					return nil
				}

				// Send final message_stop if not already sent
				stopEvent := ClaudeStreamChunk{
					Type: "message_stop",
				}
				if data, err := json.Marshal(stopEvent); err == nil {
					fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", data)
					flusher.Flush()
				}
				return nil
			}

			hasReceivedContent = true

			// Handle different chunk types
			if claudeChunk, ok := chunk.(ClaudeStreamChunk); ok {
				// Send message_start if not already sent
				if !sentMessageStart && claudeChunk.Type == "content_block_delta" {
					msgStart := ClaudeStreamChunk{
						Type: "message_start",
						Message: &ClaudeAPIResponse{
							ID:      uuid.New().String(),
							Type:    "message",
							Role:    "assistant",
							Content: []ClaudeResponseContent{},
							Model:   "LongCat-Flash",
							Usage:   ClaudeUsage{},
						},
					}
					if data, err := json.Marshal(msgStart); err == nil {
						fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", data)
						flusher.Flush()
					}
					sentMessageStart = true
				}

				// Send content_block_start if not already sent
				if !sentContentBlockStart && claudeChunk.Type == "content_block_delta" {
					blockStart := ClaudeStreamChunk{
						Type:  "content_block_start",
						Index: 0,
						ContentBlock: &ClaudeContentBlock{
							Type: "text",
							Text: "",
						},
					}
					if data, err := json.Marshal(blockStart); err == nil {
						fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", data)
						flusher.Flush()
					}
					sentContentBlockStart = true
				}

				// Send the actual chunk
				if data, err := json.Marshal(claudeChunk); err == nil {
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", claudeChunk.Type, data)
					flusher.Flush()
				}
			}

		case err := <-errs:
			if err != nil {
				// Send error as SSE event
				errorEvent := map[string]interface{}{
					"type":  "error",
					"error": err.Error(),
				}
				if data, jsonErr := json.Marshal(errorEvent); jsonErr == nil {
					fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
					flusher.Flush()
				}
				return err
			}
		}
	}
}