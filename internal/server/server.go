package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// SendRequest is the payload sent by Antigravity or local scripts to deliver messages to TG.
type SendRequest struct {
	Text      string `json:"text"`
	FilePath  string `json:"file_path,omitempty"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// Server provides a local HTTP endpoint for Antigravity and scripts to push notifications.
type Server struct {
	port    string
	adminID int64
	bot     *tgbotapi.BotAPI
	srv     *http.Server
}

// New creates a new local HTTP API server.
func New(port string, adminID int64, bot *tgbotapi.BotAPI) *Server {
	s := &Server{
		port:    port,
		adminID: adminID,
		bot:     bot,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", s.handleSend)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleHealth)

	s.srv = &http.Server{
		Addr:    "127.0.0.1:" + port,
		Handler: mux,
	}

	return s
}

// Start runs the HTTP server in a blocking call.
func (s *Server) Start() error {
	log.Printf("[Local API] Listening on http://127.0.0.1:%s", s.port)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("local server failed: %w", err)
	}
	return nil
}

// Shutdown gracefully terminates the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "running",
		"service":  "superbot-local-api",
		"admin_id": s.adminID,
	})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"only POST method is accepted"}`, http.StatusMethodNotAllowed)
		return
	}

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Text) == "" && strings.TrimSpace(req.FilePath) == "" {
		http.Error(w, `{"error":"either text or file_path must be provided"}`, http.StatusBadRequest)
		return
	}

	// Send file if provided
	if req.FilePath != "" {
		cleanPath := filepath.Clean(req.FilePath)
		info, err := os.Stat(cleanPath)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"file not found: %s"}`, cleanPath), http.StatusNotFound)
			return
		}
		if info.IsDir() {
			http.Error(w, fmt.Sprintf(`{"error":"path is a directory: %s"}`, cleanPath), http.StatusBadRequest)
			return
		}

		doc := tgbotapi.NewDocument(s.adminID, tgbotapi.FilePath(cleanPath))
		if req.Text != "" {
			doc.Caption = req.Text
			if req.ParseMode != "" {
				doc.ParseMode = req.ParseMode
			}
		}
		if _, err := s.bot.Send(doc); err != nil {
			log.Printf("[Local API] Error sending document to TG: %v", err)
			http.Error(w, fmt.Sprintf(`{"error":"telegram send error: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
	} else if req.Text != "" {
		// Send text message
		msg := tgbotapi.NewMessage(s.adminID, req.Text)
		if req.ParseMode != "" {
			msg.ParseMode = req.ParseMode
		} else {
			msg.ParseMode = "HTML"
		}
		if _, err := s.bot.Send(msg); err != nil {
			log.Printf("[Local API] Error sending message to TG: %v", err)
			http.Error(w, fmt.Sprintf(`{"error":"telegram send error: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "sent",
		"chat_id": s.adminID,
	})
}

