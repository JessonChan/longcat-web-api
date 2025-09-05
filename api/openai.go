package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"github.com/google/uuid"
)

// OpenAI compatible request structures
type ChatCompletionRequest struct {
	Model     string          `json:"model"`
	Messages  []OpenaiMessage `json:"messages"`
	Stream    bool            `json:"stream,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type OpenaiMessage struct {
	Role    string
	Content any // string or []ClaudeMessageContent
}

// OpenAI compatible response structures - ENHANCED
type ChatCompletionChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Delta        Delta  `json:"delta"`
	Index        int    `json:"index"`
	FinishReason string `json:"finish_reason,omitempty"` // OpenAI uses underscore
}

type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// For non-streaming responses
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// LongCat specific structures needed for OpenAI service
type LongCatResponse struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversationId"`
	MessageID      int             `json:"messageId"`
	ParentID       int             `json:"parentId"`
	Object         string          `json:"object"`
	Created        int64           `json:"created"`
	Model          string          `json:"model"`
	Choices        []LongCatChoice `json:"choices"`
	Content        string          `json:"content"`
	ReasonContent  string          `json:"reasonContent"`
	SearchEnabled  bool            `json:"searchEnabled"`
	ReasonEnabled  bool            `json:"reasonEnabled"`
	Title          *string         `json:"title"`
	ReasonStatus   string          `json:"reasonStatus"`
	SearchStatus   *string         `json:"searchStatus"`
	ContentStatus  string          `json:"contentStatus"`
	SearchResults  *string         `json:"searchResults"`
	TokenInfo      TokenInfo       `json:"tokenInfo"`
	PluginInfo     *string         `json:"pluginInfo"`
	LoadingStatus  bool            `json:"loadingStatus"`
	Sensitive      bool            `json:"sensitive"`
	LastOne        bool            `json:"lastOne"`
}

type LongCatChoice struct {
	Delta struct {
		Role             string  `json:"role"`
		Content          string  `json:"content"`
		ReasoningContent *string `json:"reasoningContent"`
		FunctionCall     *string `json:"functionCall"`
	} `json:"delta"`
	Index        int    `json:"index"`
	FinishReason string `json:"finishReason"`
}

type TokenInfo struct {
	PromptTokens     int  `json:"promptTokens"`
	CompletionTokens int  `json:"completionTokens"`
	TotalTokens      int  `json:"totalTokens"`
	HasTokens        bool `json:"hasTokens"`
}

// StreamProcessor - ENHANCED with proper OpenAI response formatting
type StreamProcessor struct {
	conversationID string
	messageID      int
	parentID       int
	responseID     string
	model          string
	accumulated    strings.Builder // Tracks what we've already sent
	lastContent    string          // Tracks the last full content from LongCat
	finishReason   string
	tokenInfo      TokenInfo
}

func NewStreamProcessor() *StreamProcessor {
	return &StreamProcessor{
		responseID:  uuid.New().String(),
		model:       "LongCat-Flash",
		accumulated: strings.Builder{},
		lastContent: "",
	}
}

func (p *StreamProcessor) ProcessStream(resp *http.Response, stream bool) (<-chan ChatCompletionChunk, <-chan error) {
	chunks := make(chan ChatCompletionChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			// fmt.Println("Received line:", line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
			if data == "[DONE]" {
				break
			}

			var longCatResp LongCatResponse
			if err := json.Unmarshal([]byte(data), &longCatResp); err != nil {
				errs <- fmt.Errorf("failed to unmarshal response: %w", err)
				return
			}

			// Update processor state
			p.conversationID = longCatResp.ConversationID
			p.messageID = longCatResp.MessageID
			p.parentID = longCatResp.ParentID
			if p.model == "" {
				p.model = longCatResp.Model
			}
			if longCatResp.TokenInfo.HasTokens {
				p.tokenInfo = longCatResp.TokenInfo
			}

			// Accumulate content
			// LongCat sends cumulative content (full content so far), not deltas
			// We need to track this to calculate deltas for streaming
			if longCatResp.Content != "" {
				p.lastContent = longCatResp.Content
			}

			// Determine finish reason
			finishReason := longCatResp.Choices[0].FinishReason
			if finishReason == "" && longCatResp.LastOne {
				finishReason = "stop"
			}
			if finishReason == "" && longCatResp.ContentStatus == "FINISHED" {
				finishReason = "stop"
			}
			if finishReason != "" {
				p.finishReason = finishReason
			}

			// Convert to OpenAI format with proper delta handling
			chunk := p.convertToOpenAIFormat(longCatResp, true)
			if chunk != nil && stream {
				chunks <- *chunk
			}

			// If this is the final chunk, ensure it's properly handled
			if longCatResp.LastOne || finishReason == "stop" {
				// For streaming, send a final chunk with finish reason if not already included
				if stream && chunk != nil && chunk.Choices[0].FinishReason == "" && finishReason != "" {
					finalChunk := ChatCompletionChunk{
						ID:      chunk.ID,
						Object:  chunk.Object,
						Created: chunk.Created,
						Model:   chunk.Model,
						Choices: []Choice{
							{
								Delta: Delta{
									Content: "",
								},
								Index:        0,
								FinishReason: finishReason,
							},
						},
					}
					chunks <- finalChunk
				}
				if !stream && chunk != nil {
					chunks <- *chunk
				}
				break
			}
		}

		if err := scanner.Err(); err != nil {
			errs <- fmt.Errorf("scanner error: %w", err)
		}
	}()

	return chunks, errs
}

// convertToOpenAIFormat - ENHANCED to properly format OpenAI responses
func (p *StreamProcessor) convertToOpenAIFormat(longCatResp LongCatResponse, stream bool) *ChatCompletionChunk {
	// For streaming, we need to handle deltas carefully
	if stream {
		// First chunk should include the role
		role := ""
		if longCatResp.Choices[0].Delta.Role != "" && longCatResp.ContentStatus == "PROCESSING" {
			role = "assistant"
		}

		// Calculate delta content
		content := ""
		if longCatResp.Choices[0].Delta.Content != "" {
			// If LongCat provides delta directly, use it
			content = longCatResp.Choices[0].Delta.Content
		} else if longCatResp.Content != "" {
			// Calculate the delta by comparing with what we've already sent
			accumulated := p.accumulated.String()
			if len(longCatResp.Content) > len(accumulated) {
				// New content is everything after what we've already sent
				content = longCatResp.Content[len(accumulated):]
			} else if longCatResp.Content != accumulated {
				// If content is different but not longer, send the difference
				// This handles cases where the final message might be shorter due to cleanup
				content = longCatResp.Content
			}
		}

		// Build OpenAI chunk
		chunk := &ChatCompletionChunk{
			ID:      p.responseID,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   p.model,
			Choices: []Choice{
				{
					Delta: Delta{
						Role:    role,
						Content: content,
					},
					Index:        0,
					FinishReason: p.finishReason,
				},
			},
		}

		// Update accumulated content with what we're sending
		if content != "" {
			p.accumulated.WriteString(content)
		}

		// Only return chunk if it has content or is the final chunk
		if content != "" || p.finishReason != "" || role != "" {
			return chunk
		}
		
		// Special case: if this is the final message and we haven't sent all content yet,
		// ensure we send the remaining content
		if longCatResp.LastOne || longCatResp.ContentStatus == "FINISHED" {
			accumulated := p.accumulated.String()
			if longCatResp.Content != "" && longCatResp.Content != accumulated {
				// Send the remaining content as the final chunk
				chunk.Choices[0].Delta.Content = longCatResp.Content
				return chunk
			}
		}
		
		return nil
	}

	// For non-streaming, we'll handle this in the handler
	return nil
}

// OpenAIService implements APIService for OpenAI compatibility
type OpenAIService struct {
	longCatClient *LongCatClient
}

func NewOpenAIService(client *LongCatClient) *OpenAIService {
	return &OpenAIService{
		longCatClient: client,
	}
}


func (s *OpenAIService) ProcessRequest(ctx context.Context, requestBody []byte, conversationID string) (*http.Response, error) {
	var req ChatCompletionRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return nil, fmt.Errorf("invalid OpenAI request: %w", err)
	}

	longCatReq, err := s.convertRequest(requestBody, conversationID)
	if err != nil {
		return nil, err
	}

	return s.longCatClient.SendRequest(ctx, longCatReq)
}

// convertRequest converts OpenAI request format to LongCat request format
func (s *OpenAIService) convertRequest(requestBody []byte, conversationID string) (LongCatRequest, error) {
	var openAIReq ChatCompletionRequest
	if err := json.Unmarshal(requestBody, &openAIReq); err != nil {
		return LongCatRequest{}, fmt.Errorf("invalid OpenAI request: %w", err)
	}

	var content string
	if len(openAIReq.Messages) > 0 {
		lastMsg := openAIReq.Messages[len(openAIReq.Messages)-1]
		if str, ok := lastMsg.Content.(string); ok {
			content = str
		}
		if ls, ok := lastMsg.Content.([]interface{}); ok {
			for _, l := range ls {
				if str, ok := l.(map[string]any); ok {
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

func (s *OpenAIService) ConvertResponse(resp *http.Response, stream bool) (<-chan interface{}, <-chan error) {
	chunks := make(chan interface{}, 10) // Buffered channel
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)
		defer resp.Body.Close()

		processor := NewStreamProcessor()
		rawChunks, rawErrs := processor.ProcessStream(resp, stream)

		for {
			select {
			case chunk, ok := <-rawChunks:
				if !ok {
					return
				}
				select {
				case chunks <- chunk:
				case <-time.After(5 * time.Second):
					errs <- fmt.Errorf("timeout sending chunk")
					return
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

func (s *OpenAIService) GetResponseContentType(stream bool) string {
	if stream {
		return "text/event-stream"
	}
	return "application/json"
}


func (s *OpenAIService) HandleNonStreamingResponse(w http.ResponseWriter, chunks <-chan interface{}, errs <-chan error) error {
	// Collect all chunks and build final response
	var fullContent strings.Builder
	var finishReason string
	responseID := uuid.New().String()
	model := "LongCat-Flash"
	tokenInfo := TokenInfo{}

	// Process all chunks
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				// Build final response
				response := ChatCompletionResponse{
					ID:      responseID,
					Object:  "chat.completion",
					Created: time.Now().Unix(),
					Model:   model,
					Choices: []Choice{{
						Delta: Delta{
							Role:    "assistant",
							Content: fullContent.String(),
						},
						Index:        0,
						FinishReason: finishReason,
					}},
					Usage: Usage{
						PromptTokens:     tokenInfo.PromptTokens,
						CompletionTokens: tokenInfo.CompletionTokens,
						TotalTokens:      tokenInfo.TotalTokens,
					},
				}

				w.Header().Set("Content-Type", "application/json")
				return json.NewEncoder(w).Encode(response)
			}

			if openAIChunk, ok := chunk.(ChatCompletionChunk); ok {
				if openAIChunk.Choices != nil && len(openAIChunk.Choices) > 0 {
					fullContent.WriteString(openAIChunk.Choices[0].Delta.Content)
					if openAIChunk.Choices[0].FinishReason != "" {
						finishReason = openAIChunk.Choices[0].FinishReason
					}
				}
				model = openAIChunk.Model
				responseID = openAIChunk.ID
			}

		case err := <-errs:
			if err != nil {
				return fmt.Errorf("error processing chunks: %w", err)
			}
		}
	}
}

func (s *OpenAIService) HandleStreamingResponse(w http.ResponseWriter, flusher http.Flusher, chunks <-chan interface{}, errs <-chan error) error {
	hasReceivedContent := false

	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				if !hasReceivedContent {
					// Send a default chunk if no content was received
					defaultChunk := ChatCompletionChunk{
						ID:      uuid.New().String(),
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   "LongCat-Flash",
						Choices: []Choice{{
							Delta: Delta{
								Role:    "assistant",
								Content: "I apologize, but I'm unable to process your request at the moment.",
							},
							Index:        0,
							FinishReason: "stop",
						}},
					}
					if data, err := json.Marshal(defaultChunk); err == nil {
						fmt.Fprintf(w, "data: %s\n\n", data)
						flusher.Flush()
					}
				}
				// Send final [DONE] marker
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return nil
			}

			hasReceivedContent = true
			if data, err := json.Marshal(chunk); err == nil {
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}

		case err := <-errs:
			if err != nil {
				return fmt.Errorf("error processing stream: %w", err)
			}
		}
	}
}