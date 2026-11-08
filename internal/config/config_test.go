package config_test

import (
	"os"
	"testing"
	"vpsbot/internal/config"
)

func TestConfigLoad_Success(t *testing.T) {
	_ = os.Setenv("TG_BOT_TOKEN", "123456:TEST_TOKEN")
	_ = os.Setenv("TG_ADMIN_ID", "5342950903")
	defer func() {
		_ = os.Unsetenv("TG_BOT_TOKEN")
		_ = os.Unsetenv("TG_ADMIN_ID")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.TelegramBotToken != "123456:TEST_TOKEN" {
		t.Errorf("expected token '123456:TEST_TOKEN', got: %s", cfg.TelegramBotToken)
	}
	if cfg.TelegramAdminID != 5342950903 {
		t.Errorf("expected admin ID 5342950903, got: %d", cfg.TelegramAdminID)
	}
	if cfg.LocalAPIPort != "43428" {
		t.Errorf("expected default local port 43428, got: %s", cfg.LocalAPIPort)
	}
	if cfg.VPNServiceName != "x-ui.service" {
		t.Errorf("expected default vpn service x-ui.service, got: %s", cfg.VPNServiceName)
	}
	if cfg.AIServiceName != "omniroute.service" {
		t.Errorf("expected default ai service omniroute.service, got: %s", cfg.AIServiceName)
	}
}

func TestConfigLoad_MissingToken(t *testing.T) {
	_ = os.Unsetenv("TG_BOT_TOKEN")
	_ = os.Setenv("TG_ADMIN_ID", "5342950903")
	defer func() {
		_ = os.Unsetenv("TG_ADMIN_ID")
	}()

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error due to missing TG_BOT_TOKEN, got nil")
	}
}

