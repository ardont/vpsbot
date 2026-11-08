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
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type AskRequest struct {
	Prompt string `json:"prompt"`
	Query  string `json:"query,omitempty"`
}

type AskResponse struct {
	Response string `json:"response"`
	Text     string `json:"text,omitempty"`
}

type AgentAPIOutput struct {
	Response struct {
		NewConversation struct {
			ConversationID string `json:"conversationId"`
			Prompt         string `json:"prompt"`
		} `json:"newConversation"`
	} `json:"response"`
}

type TranscriptStep struct {
	StepIndex int    `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Content   string `json:"content"`
}

func main() {
	port := os.Getenv("AGY_PILOT_PORT")
	if port == "" {
		port = "43430"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/ask", handleAsk)
	mux.HandleFunc("/prompt", handleAsk)
	mux.HandleFunc("/query", handleAsk)
	mux.HandleFunc("/v1/chat/completions", handleAsk)

	server := &http.Server{
		Addr:         "127.0.0.1:" + port,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("[agy-pilot] Starting Antigravity Pilot Bridge on http://127.0.0.1:%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[agy-pilot] HTTP server error: %v", err)
		}
	}()

	<-stop
	log.Println("[agy-pilot] Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	addr, token := discoverHub()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"service":     "agy-pilot",
		"hub_address": addr,
		"hub_ready":   token != "",
		"time":        time.Now().UTC().Format(time.RFC3339),
	})
}

func handleAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"only POST method accepted"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var req AskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(req.Query)
	}

	if prompt == "" {
		http.Error(w, `{"error":"prompt cannot be empty"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[agy-pilot] Received prompt: %s", truncate(prompt, 60))

	reply, err := queryAntigravity(r.Context(), prompt)
	if err != nil {
		log.Printf("[agy-pilot] Antigravity failed: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":"Antigravity execution failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AskResponse{
		Response: reply,
		Text:     reply,
	})
}

var (
	csrfRe = regexp.MustCompile(`--csrf_token[=\s]+([a-f0-9\-]+)`)
	portRe = regexp.MustCompile(`--hub-port[=\s]+(\d+)`)
)

func discoverHub() (string, string) {
	out, err := exec.Command("ps", "-eo", "pid,command").Output()
	if err != nil {
		return "127.0.0.1:43427", "05bf9066-0574-41fe-8c51-c6136c97b348"
	}

	var port, token string
	lines := strings.Split(string(out), "\n")
	for _, l := range lines {
		if strings.Contains(l, "/agy") && strings.Contains(l, "--hub") {
			if m := csrfRe.FindStringSubmatch(l); len(m) > 1 {
				token = m[1]
			}
			if m := portRe.FindStringSubmatch(l); len(m) > 1 {
				port = m[1]
			}
		}
	}

	if port == "" {
		port = "43427"
	}
	if token == "" {
		token = "05bf9066-0574-41fe-8c51-c6136c97b348"
	}

	return "127.0.0.1:" + port, token
}

func queryAntigravity(ctx context.Context, prompt string) (string, error) {
	agyBin := "/root/.gemini/bin/agy"
	if _, err := os.Stat(agyBin); err != nil {
		return "", fmt.Errorf("antigravity binary not found at %s: %w", agyBin, err)
	}

	hubAddr, csrfToken := discoverHub()

	cmd := exec.CommandContext(ctx, agyBin, "agentapi", "new-conversation", "--model=flash", prompt)
	cmd.Env = []string{
		"HOME=/root",
		"PATH=/root/.gemini/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"ANTIGRAVITY_LS_ADDRESS=" + hubAddr,
		"ANTIGRAVITY_CSRF_TOKEN=" + csrfToken,
		"ANTIGRAVITY_AGENTAPI_EXE=" + agyBin,
		"ANTIGRAVITY_APP_DATA_DIR=/root/.gemini/antigravity",
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to run agy agentapi: %w (stderr: %s)", err, stderr.String())
	}

	var output AgentAPIOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return "", fmt.Errorf("failed to parse agentapi output: %w", err)
	}

	convID := output.Response.NewConversation.ConversationID
	if convID == "" {
		return "", fmt.Errorf("no conversationId in agentapi output: %s", stdout.String())
	}

	log.Printf("[agy-pilot] Started Antigravity session %s, waiting for response...", convID)

	transcriptPath := filepath.Join("/root/.gemini/antigravity/brain", convID, ".system_generated/logs/transcript.jsonl")

	deadline := time.Now().Add(45 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return "", fmt.Errorf("timeout waiting for Antigravity response in %s", convID)
			}

			if _, err := os.Stat(transcriptPath); err != nil {
				continue
			}

			reply, done, err := readFinalResponse(transcriptPath)
			if err != nil {
				continue
			}
			if done && reply != "" {
				log.Printf("[agy-pilot] Antigravity replied for %s (%d chars)", convID, len(reply))
				return reply, nil
			}
		}
	}
}

func readFinalResponse(path string) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lastContent string
	var foundDone bool

	for scanner.Scan() {
		line := scanner.Bytes()
		var step TranscriptStep
		if err := json.Unmarshal(line, &step); err == nil {
			if step.Source == "MODEL" && step.Type == "PLANNER_RESPONSE" {
				if step.Content != "" {
					lastContent = step.Content
				}
				if step.Status == "DONE" {
					foundDone = true
				}
			}
		}
	}

	if foundDone && lastContent != "" {
		return lastContent, true, nil
	}
	return "", false, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
