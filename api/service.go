package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"github.com/Jessonchan/longcat-web-api/config"
	"github.com/Jessonchan/longcat-web-api/types"
)

// APIServiceType represents the type of API service
type APIServiceType string

const (
	OpenAIServiceType APIServiceType = "openai"
	ClaudeServiceType APIServiceType = "claude"
)

// LongCatRequest represents a request to the LongCat API
type LongCatRequest struct {
	Content        string          `json:"content"`
	Messages       []types.Message `json:"messages"`
	ReasonEnabled  int             `json:"reasonEnabled"`
	SearchEnabled  int             `json:"searchEnabled"`
	Regenerate     int             `json:"regenerate"`
	ConversationId string          `json:"conversationId,omitempty"`
}

// LongCatClient handles unified HTTP requests to LongCat server
type LongCatClient struct {
	client     *http.Client
	longCatURL string
	sessionURL string
	headers    map[string]string
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

// CreateSession creates a new conversation session
func (c *LongCatClient) CreateSession(ctx context.Context) (string, error) {
	sessionReq := struct {
		Model   string `json:"model"`
		AgentID string `json:"agentId"`
	}{
		Model:   "",
		AgentID: "",
	}

	resp, err := c.sendRequest(ctx, c.sessionURL, sessionReq)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	var sessionResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			ConversationID string `json:"conversationId"`
		} `json:"data"`
	}
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

// APIService interface for different API compatibility layers
type APIService interface {
	ProcessRequest(ctx context.Context, requestBody []byte, conversationID string) (*http.Response, error)
	ConvertResponse(resp *http.Response, stream bool) (<-chan interface{}, <-chan error)
	GetResponseContentType(stream bool) string
	NeedsSession(requestBody []byte) bool
	GetServiceType() APIServiceType
	HandleNonStreamingResponse(w http.ResponseWriter, chunks <-chan interface{}, errs <-chan error) error
	HandleStreamingResponse(w http.ResponseWriter, flusher http.Flusher, chunks <-chan interface{}, errs <-chan error) error
	ConvertRequest(requestBody []byte, conversationID string) (LongCatRequest, error)
}