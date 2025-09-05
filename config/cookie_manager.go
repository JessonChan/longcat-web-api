package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// CookieManager handles cookie parsing and storage
type CookieManager struct {
	configPath string
}

// SavedConfig represents the configuration saved to file
type SavedConfig struct {
	Cookies CookieConfig `json:"cookies"`
}

// NewCookieManager creates a new cookie manager
func NewCookieManager() *CookieManager {
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".config", "longcat-web-api")
	
	// Create config directory if it doesn't exist
	os.MkdirAll(configDir, 0755)
	
	return &CookieManager{
		configPath: filepath.Join(configDir, "config.json"),
	}
}

// ParseRawCookies parses raw cookie string from browser
func (cm *CookieManager) ParseRawCookies(rawCookies string) (CookieConfig, error) {
	cookies := CookieConfig{}
	
	// Split by semicolon and parse each cookie
	parts := strings.Split(rawCookies, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		
		// Split key=value
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		
		switch key {
		case "_lxsdk_cuid":
			cookies.LxsdkCuid = value
		case "passport_token_key":
			cookies.PassportToken = value
		case "_lxsdk_s":
			cookies.LxsdkS = value
		}
	}
	
	// Validate required cookies
	if cookies.PassportToken == "" {
		return cookies, fmt.Errorf("missing required cookie: passport_token_key")
	}
	
	return cookies, nil
}

// SaveCookies saves cookies to config file
func (cm *CookieManager) SaveCookies(cookies CookieConfig) error {
	config := SavedConfig{
		Cookies: cookies,
	}
	
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	
	err = ioutil.WriteFile(cm.configPath, data, 0600)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	
	fmt.Printf("Configuration saved to: %s\n", cm.configPath)
	return nil
}

// LoadCookies loads cookies from config file
func (cm *CookieManager) LoadCookies() (CookieConfig, error) {
	data, err := ioutil.ReadFile(cm.configPath)
	if err != nil {
		return CookieConfig{}, err
	}
	
	var config SavedConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return CookieConfig{}, fmt.Errorf("failed to parse config file: %w", err)
	}
	
	return config.Cookies, nil
}

// PromptForCookies interactively prompts user for cookies
func (cm *CookieManager) PromptForCookies() (CookieConfig, error) {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Println("\n=== Cookie Configuration Required ===")
	fmt.Println("\nTo get your cookies:")
	fmt.Println("1. Open https://longcat.chat in your browser and login")
	fmt.Println("2. Open Developer Tools (F12)")
	fmt.Println("3. Go to Application/Storage → Cookies → https://longcat.chat")
	fmt.Println("4. Find these cookies and copy their values:")
	fmt.Println("   • _lxsdk_cuid")
	fmt.Println("   • passport_token_key (required)")
	fmt.Println("   • _lxsdk_s")
	fmt.Println("\n5. Copy the entire cookie string from the browser")
	fmt.Println("   (You can select all cookies and copy as a single string)")
	fmt.Println("\nExample format:")
	fmt.Println("_lxsdk_cuid=xxx; passport_token_key=yyy; _lxsdk_s=zzz")
	fmt.Print("\nPaste your cookies here and press Enter:\n> ")
	
	cookieString, err := reader.ReadString('\n')
	if err != nil {
		return CookieConfig{}, fmt.Errorf("failed to read input: %w", err)
	}
	
	cookieString = strings.TrimSpace(cookieString)
	
	// Check if user wants to quit
	if cookieString == "" || strings.ToLower(cookieString) == "quit" || strings.ToLower(cookieString) == "exit" {
		return CookieConfig{}, fmt.Errorf("cookie configuration cancelled")
	}
	
	cookies, err := cm.ParseRawCookies(cookieString)
	if err != nil {
		return CookieConfig{}, err
	}
	
	// Show what was parsed
	fmt.Println("\n✓ Cookies parsed successfully:")
	if cookies.LxsdkCuid != "" {
		fmt.Printf("  _lxsdk_cuid: %s...%s\n", cookies.LxsdkCuid[:min(4, len(cookies.LxsdkCuid))], 
			cookies.LxsdkCuid[max(0, len(cookies.LxsdkCuid)-4):])
	}
	if cookies.PassportToken != "" {
		fmt.Printf("  passport_token_key: %s...%s\n", cookies.PassportToken[:min(4, len(cookies.PassportToken))],
			cookies.PassportToken[max(0, len(cookies.PassportToken)-4):])
	}
	if cookies.LxsdkS != "" {
		fmt.Printf("  _lxsdk_s: %s...%s\n", cookies.LxsdkS[:min(4, len(cookies.LxsdkS))],
			cookies.LxsdkS[max(0, len(cookies.LxsdkS)-4):])
	}
	
	// Ask if user wants to save
	fmt.Print("\nSave these cookies for future use? (y/n): ")
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	
	if response == "y" || response == "yes" {
		if err := cm.SaveCookies(cookies); err != nil {
			fmt.Printf("Warning: Failed to save cookies: %v\n", err)
		} else {
			fmt.Println("✓ Cookies saved successfully")
		}
	}
	
	return cookies, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// GetCookies attempts to get cookies from various sources
func (cm *CookieManager) GetCookies() (CookieConfig, error) {
	// 1. Try environment variables first
	if AppConfig != nil && AppConfig.Cookies.PassportToken != "" {
		return AppConfig.Cookies, nil
	}
	
	// 2. Try loading from config file
	cookies, err := cm.LoadCookies()
	if err == nil && cookies.PassportToken != "" {
		fmt.Println("Loaded cookies from config file")
		return cookies, nil
	}
	
	// 3. Prompt user for cookies
	return cm.PromptForCookies()
}