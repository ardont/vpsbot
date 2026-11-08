package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"vpsbot/internal/antigravity"
	"vpsbot/internal/cgroups"
	"vpsbot/internal/config"
	"vpsbot/internal/server"
	"vpsbot/internal/sysinfo"
	"vpsbot/internal/telegram"
)

func main() {
	log.Println("==================================================")
	log.Println("           SuperBot (VPS Hybrid Daemon)           ")
	log.Println("==================================================")

	// 1. Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[FATAL] Configuration error: %v", err)
	}

	// 2. Initialize Telegram Bot API
	botAPI, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		log.Fatalf("[FATAL] Failed to connect to Telegram Bot API: %v", err)
	}
	log.Printf("[Init] Connected to Telegram as @%s", botAPI.Self.UserName)

	// 3. Initialize core modules
	sysMgr := sysinfo.New(cfg.AllowedBasePath)
	cgMgr := cgroups.New(cfg.VPNServiceName, cfg.AIServiceName)
	aiClient := antigravity.NewClient(cfg.AntigravityAPIURL)

	// 4. Initialize Local HTTP Server (:43428)
	localServer := server.New(cfg.LocalAPIPort, cfg.TelegramAdminID, botAPI)

	// 5. Initialize Telegram Router
	tgRouter := telegram.New(botAPI, cfg, sysMgr, cgMgr, aiClient)

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start local HTTP server
	go func() {
		if err := localServer.Start(); err != nil {
			log.Printf("[Local API] Error: %v", err)
		}
	}()

	// Start Telegram polling
	go func() {
		if err := tgRouter.StartPolling(ctx); err != nil {
			log.Printf("[Telegram] Polling error: %v", err)
		}
	}()

	log.Println("[Ready] SuperBot successfully initialized and running.")

	// Listen for shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("[Shutdown] Received signal: %v. Initiating graceful shutdown...", sig)

	cancel() // Stop telegram polling

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := localServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("[Local API] Shutdown error: %v", err)
	}

	log.Println("[Exit] SuperBot stopped cleanly.")
}

