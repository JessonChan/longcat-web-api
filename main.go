package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/Jessonchan/longcat-web-api/config"
)

// OpenAI compatible request structures
type ChatCompletionRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream,omitempty"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

// Claude API compatible request structure
type ClaudeAPIRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []ClaudeMessage `json:"messages"`
	Stream    bool            `json:"stream,omitempty"`
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

// Session creation structures
type SessionCreateRequest struct {
	Model   string `json:"model"`
	AgentID string `json:"agentId"`
}

type SessionCreateResponse struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    SessionCreateData `json:"data"`
}

type SessionCreateData struct {
	ConversationID   string `json:"conversationId"`
	Model            string `json:"model"`
	Agent            string `json:"agent"`
	Title            string `json:"title"`
	TitleType        string `json:"titleType"`
	CurrentMessageID int    `json:"currentMessageId"`
	Label            string `json:"label"`
	CreateAt         int64  `json:"createAt"`
	UpdateAt         int64  `json:"updateAt"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ClaudeMessage struct {
	Role    string                 `json:"role"`
	Content []ClaudeMessageContent `json:"content"`
}
type ClaudeMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
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

// LongCat specific structures - ENHANCED
type LongCatRequest struct {
	Content        string    `json:"content"`
	Messages       []Message `json:"messages"`
	ReasonEnabled  int       `json:"reasonEnabled"`
	SearchEnabled  int       `json:"searchEnabled"`
	Regenerate     int       `json:"regenerate"`
	ConversationId string    `json:"conversationId,omitempty"`
}

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

// APIServiceType represents the type of API service
type APIServiceType string

const (
	OpenAIServiceType APIServiceType = "openai"
	ClaudeServiceType APIServiceType = "claude"
)

// APIService interface for different API compatibility layers
type APIService interface {
	ProcessRequest(ctx context.Context, requestBody []byte) (*http.Response, error)
	ConvertResponse(resp *http.Response, stream bool) (<-chan interface{}, <-chan error)
	GetResponseContentType(stream bool) string
	NeedsSession(requestBody []byte) bool
	GetServiceType() APIServiceType
	HandleNonStreamingResponse(w http.ResponseWriter, chunks <-chan interface{}, errs <-chan error) error
	HandleStreamingResponse(w http.ResponseWriter, flusher http.Flusher, chunks <-chan interface{}, errs <-chan error) error
}

// LongCatClient handles unified HTTP requests to LongCat server
type LongCatClient struct {
	client       *http.Client
	longCatURL   string
	sessionURL   string
	headers      map[string]string
	conversation struct {
		ID string
		mu sync.RWMutex
	}
}

func NewLongCatClient() *LongCatClient {
	return &LongCatClient{
		client: &http.Client{
			Timeout: time.Duration(config.AppConfig.Timeout) * time.Second,
		},
		longCatURL: config.AppConfig.LongCatAPIURL,
		sessionURL: config.AppConfig.LongCatSessionURL,
		headers: map[string]string{
			"accept":             "text/event-stream,application/json",
			"accept-language":    "en,zh-Hans-CN;q=0.9,zh-CN;q=0.8,zh;q=0.7,en-GB;q=0.6,en-US;q=0.5,zh-TW;q=0.4",
			"content-type":       "application/json",
			"m-appkey":           "fe_com.sankuai.friday.fe.longcat",
			"m-traceid":          fmt.Sprintf("%d", time.Now().UnixNano()),
			"origin":             "https://longcat.chat",
			"sec-ch-ua":          `"Not(A:Brand";v="99", "Microsoft Edge";v="133", "Chromium";v="133"`,
			"sec-ch-ua-mobile":   "?0",
			"sec-ch-ua-platform": `"macOS"`,
			"sec-fetch-dest":     "empty",
			"sec-fetch-mode":     "cors",
			"sec-fetch-site":     "same-origin",
			"user-agent":         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36 Edg/133.0.0.0",
			"x-client-language":  "en",
			"x-requested-with":   "XMLHttpRequest",
		},
	}
}

func (c *LongCatClient) SetConversationID(id string) {
	c.conversation.mu.Lock()
	defer c.conversation.mu.Unlock()
	c.conversation.ID = id
}

func (c *LongCatClient) GetConversationID() string {
	c.conversation.mu.RLock()
	defer c.conversation.mu.RUnlock()
	return c.conversation.ID
}

// CreateSession creates a new conversation session
func (c *LongCatClient) CreateSession(ctx context.Context) (string, error) {
	sessionReq := SessionCreateRequest{
		Model:   "",
		AgentID: "",
	}

	resp, err := c.sendRequest(ctx, c.sessionURL, sessionReq)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	var sessionResp SessionCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return "", fmt.Errorf("failed to decode session response: %w", err)
	}

	if sessionResp.Code != 0 {
		return "", fmt.Errorf("session creation failed: %s", sessionResp.Message)
	}

	return sessionResp.Data.ConversationID, nil
}

// SendRequest sends a unified request to LongCat server
func (c *LongCatClient) SendRequest(ctx context.Context, longCatReq LongCatRequest) (*http.Response, error) {
	return c.sendRequest(ctx, c.longCatURL, longCatReq)
}

func (c *LongCatClient) sendRequest(ctx context.Context, reqUrl string, longCatReq any) (*http.Response, error) {
	body, err := json.Marshal(longCatReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	fmt.Println("LongCat request body:", string(body))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqUrl, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("referer", "https://longcat.chat/t")
	httpReq.Header.Set("referrer-policy", "strict-origin-when-cross-origin")

	cookies := []*http.Cookie{
		{Name: "_lxsdk_cuid", Value: config.AppConfig.Cookies.LxsdkCuid},
		{Name: "passport_token_key", Value: config.AppConfig.Cookies.PassportToken},
		{Name: "_lxsdk_s", Value: config.AppConfig.Cookies.LxsdkS},
	}

	for _, cookie := range cookies {
		httpReq.AddCookie(cookie)
	}

	httpReq.Header.Set("Connection", "keep-alive")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	return resp, nil
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

func (s *OpenAIService) NeedsSession(requestBody []byte) bool {
	var req ChatCompletionRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return false
	}
	return len(req.Messages) == 1
}

func (s *OpenAIService) ProcessRequest(ctx context.Context, requestBody []byte) (*http.Response, error) {
	var req ChatCompletionRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return nil, fmt.Errorf("invalid OpenAI request: %w", err)
	}

	longCatReq := s.convertRequest(req)
	return s.longCatClient.SendRequest(ctx, longCatReq)
}

func (s *OpenAIService) convertRequest(openAIReq ChatCompletionRequest) LongCatRequest {
	var content string
	if len(openAIReq.Messages) > 0 {
		lastMsg := openAIReq.Messages[len(openAIReq.Messages)-1]
		content = lastMsg.Content
	}

	conversationID := s.longCatClient.GetConversationID()

	return LongCatRequest{
		Content:        content,
		ConversationId: conversationID,
		ReasonEnabled:  0,
		SearchEnabled:  0,
		Regenerate:     0,
	}
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

func (s *OpenAIService) GetServiceType() APIServiceType {
	return OpenAIServiceType
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

func (s *ClaudeService) ProcessRequest(ctx context.Context, requestBody []byte) (*http.Response, error) {
	var req ClaudeAPIRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		return nil, fmt.Errorf("invalid Claude request: %w", err)
	}

	longCatReq := s.convertRequest(req)
	return s.longCatClient.SendRequest(ctx, longCatReq)
}

func (s *ClaudeService) convertRequest(claudeReq ClaudeAPIRequest) LongCatRequest {
	var content string
	if len(claudeReq.Messages) > 0 {
		lastMsg := claudeReq.Messages[len(claudeReq.Messages)-1]
		if len(lastMsg.Content) > 0 {
			for _, part := range lastMsg.Content {
				content += part.Text
			}
		}
	}

	messages := []Message{}
	for _, msg := range claudeReq.Messages {
		if len(msg.Content) > 0 {
			messages = append(messages, Message{
				Role:    msg.Role,
				Content: msg.Content[0].Text,
			})
		}
	}

	conversationID := s.longCatClient.GetConversationID()

	return LongCatRequest{
		Content:        content,
		ConversationId: conversationID,
		Messages:       messages,
		ReasonEnabled:  0,
		SearchEnabled:  0,
		Regenerate:     0,
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

// StreamProcessor - ENHANCED with proper OpenAI response formatting
type StreamProcessor struct {
	conversationID string
	messageID      int
	parentID       int
	responseID     string
	model          string
	accumulated    strings.Builder // Accumulate content for proper formatting
	finishReason   string
	tokenInfo      TokenInfo
}

func NewStreamProcessor() *StreamProcessor {
	return &StreamProcessor{
		responseID:  uuid.New().String(),
		model:       "LongCat-Flash",
		accumulated: strings.Builder{},
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
			if longCatResp.Content != "" {
				p.accumulated.Reset()
				p.accumulated.WriteString(longCatResp.Content)
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

			// If this is the final chunk, we're done
			if longCatResp.LastOne || finishReason == "stop" {
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

		// Only include content if it's new (not already accumulated)
		content := ""
		if longCatResp.Choices[0].Delta.Content != "" {
			content = longCatResp.Choices[0].Delta.Content
		} else if longCatResp.Content != "" {
			// Calculate the delta by subtracting accumulated content
			accumulated := p.accumulated.String()
			if strings.HasPrefix(longCatResp.Content, accumulated) {
				content = longCatResp.Content[len(accumulated):]
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

		// Update accumulated content
		if content != "" {
			p.accumulated.WriteString(content)
		}

		// Only return chunk if it has content or is the final chunk
		if content != "" || p.finishReason != "" {
			return chunk
		}
		return nil
	}

	// For non-streaming, we'll handle this in the handler
	return nil
}

// convertToClaudeFormat converts LongCat response to Claude API format
func (p *StreamProcessor) convertToClaudeFormat(longCatResp LongCatResponse, stream bool) interface{} {
	if stream {
		// For streaming, return Claude stream chunks
		if longCatResp.ContentStatus == "PROCESSING" && longCatResp.Content != "" {
			return ClaudeStreamChunk{
				Type:  "content_block_delta",
				Index: 0,
				Delta: &ClaudeStreamDelta{
					Type: "text_delta",
					Text: longCatResp.Content,
				},
			}
		}

		if longCatResp.LastOne {
			return ClaudeStreamChunk{
				Type: "message_stop",
				Usage: &ClaudeUsage{
					InputTokens:  p.tokenInfo.PromptTokens,
					OutputTokens: p.tokenInfo.CompletionTokens,
				},
			}
		}
	} else {
		// For non-streaming, return complete Claude response
		content := []ClaudeResponseContent{
			{
				Type: "text",
				Text: p.accumulated.String(),
			},
		}

		stopReason := "end_turn"
		if p.finishReason == "stop" {
			stopReason = "end_turn"
		}

		return &ClaudeAPIResponse{
			ID:         p.responseID,
			Type:       "message",
			Role:       "assistant",
			Content:    content,
			Model:      p.model,
			StopReason: stopReason,
			Usage: ClaudeUsage{
				InputTokens:  p.tokenInfo.PromptTokens,
				OutputTokens: p.tokenInfo.CompletionTokens,
			},
		}
	}

	return nil
}

// UnifiedHandler handles both OpenAI and Claude API requests using the interface
type UnifiedHandler struct {
	longCatClient *LongCatClient
	openAIService APIService
	claudeService APIService
}

func NewUnifiedHandler() *UnifiedHandler {
	longCatClient := NewLongCatClient()
	return &UnifiedHandler{
		longCatClient: longCatClient,
		openAIService: NewOpenAIService(longCatClient),
		claudeService: NewClaudeService(longCatClient),
	}
}

func (h *UnifiedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	// Handle CORS preflight requests
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/v1/messages" {
		fmt.Println(r.URL.Path, "not found")
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bs, errBs := io.ReadAll(r.Body)
	if errBs != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", errBs), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bs))
	fmt.Println("Request Body:", string(bs), r.URL.Path)

	// Select appropriate service based on endpoint
	var service APIService
	switch r.URL.Path {
	case "/v1/chat/completions":
		service = h.openAIService
	case "/v1/messages":
		service = h.claudeService
	}

	// Check if session creation is needed
	if service.NeedsSession(bs) {
		conversationID, err := h.longCatClient.CreateSession(r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
			return
		}
		h.longCatClient.SetConversationID(conversationID)
		fmt.Printf("Created new session with ID: %s\n", conversationID)
	}

	// Determine if streaming is requested
	streaming := h.isStreamingRequest(bs, r.URL.Path)

	if !streaming {
		h.handleNonStreaming(w, r, service, bs)
		return
	}

	h.handleStreaming(w, r, service, bs)
}

func (h *UnifiedHandler) isStreamingRequest(requestBody []byte, path string) bool {
	switch path {
	case "/v1/chat/completions":
		var req ChatCompletionRequest
		if err := json.Unmarshal(requestBody, &req); err == nil {
			return req.Stream
		}
	case "/v1/messages":
		var req ClaudeAPIRequest
		if err := json.Unmarshal(requestBody, &req); err == nil {
			return req.Stream
		}
	}
	return false
}

func (h *UnifiedHandler) handleNonStreaming(w http.ResponseWriter, r *http.Request, service APIService, requestBody []byte) {
	resp, err := service.ProcessRequest(r.Context(), requestBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to make request: %v", err), http.StatusInternalServerError)
		return
	}

	chunks, errs := service.ConvertResponse(resp, false)

	// Use the service's own handler method instead of type assertion
	if err := service.HandleNonStreamingResponse(w, chunks, errs); err != nil {
		http.Error(w, fmt.Sprintf("Failed to handle response: %v", err), http.StatusInternalServerError)
		return
	}
}

func (h *UnifiedHandler) handleStreaming(w http.ResponseWriter, r *http.Request, service APIService, requestBody []byte) {
	// Set SSE headers with CORS support
	w.Header().Set("Content-Type", service.GetResponseContentType(true))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version")
	w.Header().Set("Access-Control-Expose-Headers", "*")

	resp, err := service.ProcessRequest(r.Context(), requestBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to make request: %v", err), http.StatusInternalServerError)
		return
	}

	chunks, errs := service.ConvertResponse(resp, true)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Use the service's own handler method instead of type assertion
	if err := service.HandleStreamingResponse(w, flusher, chunks, errs); err != nil {
		fmt.Printf("Streaming error: %v\n", err)
		// Error is already handled by the service implementation
		return
	}
}

func main() {
	handler := NewUnifiedHandler()

	serverAddr := config.AppConfig.GetServerAddress()
	fmt.Printf("Starting OpenAI and Claude compatible server on %s\n", serverAddr)
	fmt.Println("Endpoints:")
	fmt.Println("  POST /v1/chat/completions (OpenAI compatible)")
	fmt.Println("  POST /v1/messages (Claude compatible)")

	if err := http.ListenAndServe(serverAddr, handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
