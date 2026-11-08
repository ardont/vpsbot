package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration variables for SuperBot.
type Config struct {
	TelegramBotToken  string
	TelegramAdminID   int64
	AntigravityAPIURL string
	LocalAPIPort      string
	VPNServiceName    string
	AIServiceName     string
	AllowedBasePath   string
	UsersDBPath       string
}

// Load loads settings from .env and environment variables.
func Load() (*Config, error) {
	// Try loading from current dir, then standard /opt/superbot/.env
	_ = godotenv.Load(".env")
	_ = godotenv.Load("/opt/superbot/.env")

	token := strings.TrimSpace(os.Getenv("TG_BOT_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("TG_BOT_TOKEN не задан в .env или переменных окружения")
	}

	adminIDStr := strings.TrimSpace(os.Getenv("TG_ADMIN_ID"))
	if adminIDStr == "" {
		return nil, fmt.Errorf("TG_ADMIN_ID не задан в .env или переменных окружения")
	}

	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("некорректный TG_ADMIN_ID '%s': должен быть целым числом", adminIDStr)
	}

	antigravityURL := strings.TrimSpace(os.Getenv("ANTIGRAVITY_API_URL"))
	if antigravityURL == "" {
		antigravityURL = "http://localhost:43427"
	}

	localPort := strings.TrimSpace(os.Getenv("LOCAL_API_PORT"))
	if localPort == "" {
		localPort = "43428"
	}

	vpnService := strings.TrimSpace(os.Getenv("VPN_SERVICE_NAME"))
	if vpnService == "" {
		vpnService = "x-ui.service"
	}

	aiService := strings.TrimSpace(os.Getenv("AI_SERVICE_NAME"))
	if aiService == "" {
		aiService = "omniroute.service"
	}

	allowedBase := strings.TrimSpace(os.Getenv("ALLOWED_BASE_PATH"))
	if allowedBase == "" {
		allowedBase = "/"
	}

	usersDB := strings.TrimSpace(os.Getenv("USERS_DB_PATH"))
	if usersDB == "" {
		usersDB = "/opt/superbot/users.json"
	}

	return &Config{
		TelegramBotToken:  token,
		TelegramAdminID:   adminID,
		AntigravityAPIURL: antigravityURL,
		LocalAPIPort:      localPort,
		VPNServiceName:    vpnService,
		AIServiceName:     aiService,
		AllowedBasePath:   allowedBase,
		UsersDBPath:       usersDB,
	}, nil
}
