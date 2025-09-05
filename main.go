package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/Jessonchan/longcat-web-api/api"
	"github.com/Jessonchan/longcat-web-api/config"
	conversation "github.com/Jessonchan/longcat-web-api/convsersation"
	"github.com/Jessonchan/longcat-web-api/types"
)



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








// UnifiedHandler handles both OpenAI and Claude API requests using the interface
type UnifiedHandler struct {
	longCatClient       *api.LongCatClient
	openAIService       api.APIService
	claudeService       api.APIService
	conversationManager *conversation.ConversationManager
}

func NewUnifiedHandler() *UnifiedHandler {
	longCatClient := api.NewLongCatClient()
	return &UnifiedHandler{
		longCatClient:       longCatClient,
		openAIService:       api.NewOpenAIService(longCatClient),
		claudeService:       api.NewClaudeService(longCatClient),
		conversationManager: conversation.NewConversationManager(),
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
	var service api.APIService
	switch r.URL.Path {
	case "/v1/chat/completions":
		service = h.openAIService
	case "/v1/messages":
		service = h.claudeService
	}

	// Determine conversation ID based on message history
	var conversationID string

	// Extract messages from request to generate fingerprint
	messages, err := extractMessagesFromRequest(bs, r.URL.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse messages: %v", err), http.StatusBadRequest)
		return
	}
	// Check if we have an existing conversation for this message history
	if existingConvID, exists := h.conversationManager.FindConversation(messages); exists {
		conversationID = existingConvID
		fmt.Printf("Using existing conversation: %s for message fingerprint: %s\n", conversationID)
	} else {
		// Create new conversation session
		newConvID, err := h.longCatClient.CreateSession(r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
			return
		}
		conversationID = newConvID
		h.conversationManager.SetConversation(messages, conversationID)
		fmt.Printf("Created new conversation: for message fingerprint: %s\n", conversationID)
	}

	// Determine if streaming is requested
	streaming := h.isStreamingRequest(bs, r.URL.Path)

	if !streaming {
		h.handleNonStreaming(w, r, service, bs, conversationID)
		return
	}

	h.handleStreaming(w, r, service, bs, conversationID)
}

// extractMessagesFromRequest extracts messages from OpenAI/Claude request
func extractMessagesFromRequest(requestBody []byte, path string) ([]types.Message, error) {
	switch path {
	case "/v1/chat/completions":
		var req api.ChatCompletionRequest
		if err := json.Unmarshal(requestBody, &req); err != nil {
			return nil, err
		}
		messages := []types.Message{}
		for _, m := range req.Messages {
			if str, ok := m.Content.(string); ok {
				messages = append(messages, types.Message{
					Content: str,
					Role:    m.Role,
				})
			}
			if ls, ok := m.Content.([]interface{}); ok {
				for _, v := range ls {
					if vm, ok := v.(map[string]interface{}); ok {
						messages = append(messages, types.Message{
							Content: vm["text"].(string),
							Role:    m.Role,
						})
					}
				}
			}
		}
		return messages, nil
	case "/v1/messages":
		var req api.ClaudeAPIRequest
		if err := json.Unmarshal(requestBody, &req); err != nil {
			return nil, err
		}

		// Convert Claude messages to our Message format
		messages := []types.Message{}
		for _, m := range req.System {
			messages = append(messages, types.Message{
				Content: m.Text,
				Role:    "system",
			})
		}
		for _, m := range req.Messages {
			if str, ok := m.Content.(string); ok {
				messages = append(messages, types.Message{
					Content: str,
					Role:    m.Role,
				})
			}
			if ls, ok := m.Content.([]interface{}); ok {
				for _, v := range ls {
					if vm, ok := v.(map[string]interface{}); ok {
						messages = append(messages, types.Message{
							Content: vm["text"].(string),
							Role:    m.Role,
						})
					}
				}
			}
		}
		return messages, nil
	}
	return nil, fmt.Errorf("unsupported endpoint")
}

func (h *UnifiedHandler) isStreamingRequest(requestBody []byte, path string) bool {
	switch path {
	case "/v1/chat/completions":
		var req api.ChatCompletionRequest
		if err := json.Unmarshal(requestBody, &req); err == nil {
			return req.Stream
		}
	case "/v1/messages":
		var req api.ClaudeAPIRequest
		if err := json.Unmarshal(requestBody, &req); err == nil {
			return req.Stream
		}
	}
	return false
}

func (h *UnifiedHandler) handleNonStreaming(w http.ResponseWriter, r *http.Request, service api.APIService, requestBody []byte, conversationID string) {
	resp, err := service.ProcessRequest(r.Context(), requestBody, conversationID)
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

func (h *UnifiedHandler) handleStreaming(w http.ResponseWriter, r *http.Request, service api.APIService, requestBody []byte, conversationID string) {
	// Set SSE headers with CORS support
	w.Header().Set("Content-Type", service.GetResponseContentType(true))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version")
	w.Header().Set("Access-Control-Expose-Headers", "*")

	resp, err := service.ProcessRequest(r.Context(), requestBody, conversationID)
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
	// Parse command-line flags
	var (
		updateCookies = flag.Bool("update-cookies", false, "Update stored cookies")
		clearCookies  = flag.Bool("clear-cookies", false, "Clear stored cookies")
		showVersion   = flag.Bool("version", false, "Show version information")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "LongCat API Wrapper - OpenAI/Claude Compatible API Gateway\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  COOKIE_PASSPORT_TOKEN  - LongCat authentication token (required)\n")
		fmt.Fprintf(os.Stderr, "  COOKIE_LXSDK_CUID     - LongCat session cookie\n")
		fmt.Fprintf(os.Stderr, "  COOKIE_LXSDK_S        - LongCat tracking cookie\n")
		fmt.Fprintf(os.Stderr, "  SERVER_PORT           - Server port (default: 8082)\n")
	}

	flag.Parse()

	// Handle special flags
	if *showVersion {
		fmt.Println("LongCat API Wrapper v1.0.0")
		return
	}

	if *clearCookies {
		homeDir, _ := os.UserHomeDir()
		configPath := homeDir + "/.config/longcat-web-api/config.json"

		if err := os.Remove(configPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No configuration file found")
			} else {
				log.Fatalf("Failed to clear configuration: %v", err)
			}
		} else {
			fmt.Println("✓ Configuration cleared successfully")
		}
		return
	}

	if *updateCookies {
		cookieManager := config.NewCookieManager()
		cookies, err := cookieManager.PromptForCookies()
		if err != nil {
			log.Fatalf("Failed to update cookies: %v", err)
		}
		config.AppConfig.Cookies = cookies
		fmt.Println("✓ Cookies updated successfully")
		// Continue to start the server with new cookies
	}

	// Ensure cookies are configured before starting
	ensureCookiesConfigured()

	handler := NewUnifiedHandler()

	serverAddr := config.AppConfig.GetServerAddress()
	fmt.Printf("\n=== LongCat API Wrapper ===\n")
	fmt.Printf("Starting OpenAI and Claude compatible server on %s\n", serverAddr)
	fmt.Println("\nEndpoints:")
	fmt.Println("  POST /v1/chat/completions (OpenAI compatible)")
	fmt.Println("  POST /v1/messages (Claude compatible)")
	fmt.Printf("\nServer ready at http://localhost%s\n\n", serverAddr)

	if err := http.ListenAndServe(serverAddr, handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// ensureCookiesConfigured checks if cookies are available and prompts for them if not
func ensureCookiesConfigured() {
	// Check if cookies are already configured
	if config.AppConfig.Cookies.PassportToken != "" {
		fmt.Println("✓ Cookies loaded from environment variables")
		return
	}

	// Try to load from config file
	cookieManager := config.NewCookieManager()
	cookies, err := cookieManager.LoadCookies()
	if err == nil && cookies.PassportToken != "" {
		config.AppConfig.Cookies = cookies
		fmt.Println("✓ Cookies loaded from config file")
		return
	}

	// No cookies found, prompt user
	fmt.Println("\n=== Cookie Configuration Required ===")
	fmt.Println("\nNo cookies found. Please provide authentication cookies to continue.")

	cookies, err = cookieManager.PromptForCookies()
	if err != nil {
		log.Fatalf("Failed to obtain cookies: %v", err)
	}

	// Update AppConfig with obtained cookies
	config.AppConfig.Cookies = cookies
	fmt.Println("✓ Cookies configured successfully")
}
