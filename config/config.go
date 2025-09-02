package config

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	LongCatAPIURL     string
	LongCatSessionURL string
	ServerPort        string
	Timeout           int
	Cookies           CookieConfig
}

type CookieConfig struct {
	LxsdkCuid     string
	PassportToken string
	LxsdkS        string
}

var AppConfig *Config

func init() {
	LoadConfig()
}

func LoadConfig() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: .env file not found, using environment variables or defaults")
	}

	AppConfig = &Config{
		LongCatAPIURL:     getEnv("LONGCAT_API_URL", "https://longcat.chat/api/v1/chat-completion"),
		LongCatSessionURL: getEnv("LONGCAT_SESSION_URL", "https://longcat.chat/api/v1/session-create"),
		ServerPort:        getEnv("SERVER_PORT", "8082"),
		Timeout:           getEnvAsInt("TIMEOUT_SECONDS", 30),
		Cookies: CookieConfig{
			LxsdkCuid:     getEnv("COOKIE_LXSDK_CUID", ""),
			PassportToken: getEnv("COOKIE_PASSPORT_TOKEN", ""),
			LxsdkS:        getEnv("COOKIE_LXSDK_S", ""),
		},
	}

	validateConfig()
}

func validateConfig() {
	if AppConfig.Cookies.LxsdkCuid == "" {
		log.Println("Warning: COOKIE_LXSDK_CUID is not set")
	}
	if AppConfig.Cookies.PassportToken == "" {
		log.Fatal("Error: COOKIE_PASSPORT_TOKEN is required but not set")
	}
	if AppConfig.Cookies.LxsdkS == "" {
		log.Println("Warning: COOKIE_LXSDK_S is not set")
	}
	
	log.Println("Configuration loaded successfully")
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Printf("Warning: Invalid integer value for %s, using default: %d", key, defaultValue)
		return defaultValue
	}
	return value
}

func (c *Config) GetServerAddress() string {
	return fmt.Sprintf(":%s", c.ServerPort)
}