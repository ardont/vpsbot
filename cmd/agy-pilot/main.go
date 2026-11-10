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
	"sync"
	"syscall"
	"time"
)

type AskRequest struct {
	Prompt         string `json:"prompt"`
	Query          string `json:"query,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	ResetSession   bool   `json:"reset_session,omitempty"`
}

type ToolEvent struct {
	StepIndex int    `json:"step_index"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Detail    string `json:"detail,omitempty"`
	Output    string `json:"output,omitempty"`
}

type AskResponse struct {
	Response       string      `json:"response"`
	Text           string      `json:"text,omitempty"`
	ConversationID string      `json:"conversation_id"`
	Question       string      `json:"question,omitempty"`
	Options        []string    `json:"options,omitempty"`
	Steps          []ToolEvent `json:"steps,omitempty"`
}

type AgentAPINewConvOutput struct {
	Response struct {
		NewConversation struct {
			ConversationID string `json:"conversationId"`
			Prompt         string `json:"prompt"`
		} `json:"newConversation"`
	} `json:"response"`
	Error string `json:"error,omitempty"`
}

type ToolCallItem struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type StreamEvent struct {
	Type           string      `json:"type"` // "start", "progress", "done", "error"
	ConversationID string      `json:"conversation_id,omitempty"`
	Action         string      `json:"action,omitempty"`
	Summary        string      `json:"summary,omitempty"`
	Detail         string      `json:"detail,omitempty"`
	Output         string      `json:"output,omitempty"`
	Steps          []ToolEvent `json:"steps,omitempty"`
	Response       string      `json:"response,omitempty"`
	Question       string      `json:"question,omitempty"`
	Options        []string    `json:"options,omitempty"`
	Error          string      `json:"error,omitempty"`
}

type TranscriptStep struct {
	StepIndex int            `json:"step_index"`
	Source    string         `json:"source"`
	Type      string         `json:"type"`
	Status    string         `json:"status"`
	Content   string         `json:"content"`
	Thinking  string         `json:"thinking,omitempty"`
	ToolCalls []ToolCallItem `json:"tool_calls,omitempty"`
}

type QuestionItem struct {
	Question      string   `json:"question"`
	Options       []string `json:"options"`
	IsMultiSelect bool     `json:"is_multi_select"`
}

func main() {
	port := os.Getenv("AGY_PILOT_PORT")
	if port == "" {
		port = "43430"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ask", handleAsk)
	mux.HandleFunc("/api/ask", handleAsk)
	mux.HandleFunc("/prompt", handleAsk)
	mux.HandleFunc("/query", handleAsk)
	mux.HandleFunc("/v1/chat/completions", handleAsk)

	server := &http.Server{
		Addr:         "127.0.0.1:" + port,
		Handler:      mux,
		ReadTimeout:  300 * time.Second,
		WriteTimeout: 300 * time.Second,
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

	log.Printf("[agy-pilot] Prompt: %s (session: %s, reset: %v)", truncate(prompt, 50), req.ConversationID, req.ResetSession)

	isStream := r.Header.Get("Accept") == "application/x-ndjson" || r.URL.Query().Get("stream") == "true"
	if isStream {
		if flusher, ok := w.(http.Flusher); ok {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()

			enc := json.NewEncoder(w)
			var encMu sync.Mutex
			onProgress := func(ev StreamEvent) {
				encMu.Lock()
				_ = enc.Encode(ev)
				flusher.Flush()
				encMu.Unlock()
			}

			resp, err := queryAntigravity(r.Context(), prompt, req.ConversationID, req.ResetSession, onProgress)
			if err != nil {
				log.Printf("[agy-pilot] Antigravity failed: %v", err)
				encMu.Lock()
				_ = enc.Encode(StreamEvent{Type: "error", Error: err.Error()})
				flusher.Flush()
				encMu.Unlock()
				return
			}

			encMu.Lock()
			_ = enc.Encode(StreamEvent{
				Type:           "done",
				Response:       resp.Response,
				Question:       resp.Question,
				Options:        resp.Options,
				ConversationID: resp.ConversationID,
				Steps:          resp.Steps,
			})
			flusher.Flush()
			encMu.Unlock()
			return
		}
	}

	resp, err := queryAntigravity(r.Context(), prompt, req.ConversationID, req.ResetSession, nil)
	if err != nil {
		log.Printf("[agy-pilot] Antigravity failed: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":"Antigravity execution failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
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
			var curToken, curPort string
			if m := csrfRe.FindStringSubmatch(l); len(m) > 1 {
				curToken = m[1]
			}
			if m := portRe.FindStringSubmatch(l); len(m) > 1 {
				curPort = m[1]
			}
			if curPort != "" && curToken != "" {
				// Prioritize the IDE session hub which includes --add-dir
				if strings.Contains(l, "--add-dir") {
					return "127.0.0.1:" + curPort, curToken
				}
				port = curPort
				token = curToken
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

func queryAntigravity(ctx context.Context, prompt string, targetConvID string, resetSession bool, onProgress func(StreamEvent)) (*AskResponse, error) {
	agyBin := "/root/.gemini/bin/agy"
	if _, err := os.Stat(agyBin); err != nil {
		return nil, fmt.Errorf("antigravity binary not found at %s: %w", agyBin, err)
	}

	hubAddr, csrfToken := discoverHub()
	cleanEnv := []string{
		"HOME=/root",
		"PATH=/root/.gemini/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"ANTIGRAVITY_LS_ADDRESS=" + hubAddr,
		"ANTIGRAVITY_CSRF_TOKEN=" + csrfToken,
		"ANTIGRAVITY_AGENTAPI_EXE=" + agyBin,
		"ANTIGRAVITY_APP_DATA_DIR=/root/.gemini/antigravity",
	}

	var convID string
	var initialOffset int64 = 0

	// Check if we can reuse targetConvID
	canReuse := false
	if targetConvID != "" && !resetSession {
		tPath := filepath.Join("/root/.gemini/antigravity/brain", targetConvID, ".system_generated/logs/transcript.jsonl")
		if stat, err := os.Stat(tPath); err == nil && stat.Size() > 0 {
			canReuse = true
			convID = targetConvID
			initialOffset = stat.Size()
		}
	}

	if canReuse {
		// Send message to active conversation
		log.Printf("[agy-pilot] Continuing session %s via send-message (offset: %d)...", convID, initialOffset)
		cmd := exec.CommandContext(ctx, agyBin, "agentapi", "send-message", convID, prompt)
		cmd.Dir = "/root"
		cmd.Env = cleanEnv
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			log.Printf("[agy-pilot] send-message failed (%v: %s), falling back to new conversation", err, stderr.String())
			canReuse = false
		}
	}

	if !canReuse {
		// Start a fresh conversation
		log.Printf("[agy-pilot] Creating new Antigravity session...")
		cmd := exec.CommandContext(ctx, agyBin, "agentapi", "new-conversation", prompt)
		cmd.Dir = "/root"
		cmd.Env = cleanEnv
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			errDetail := strings.TrimSpace(stderr.String())
			if errDetail == "" {
				errDetail = strings.TrimSpace(stdout.String())
			}
			return nil, fmt.Errorf("failed to run agy new-conversation: %w (details: %s)", err, errDetail)
		}

		var output AgentAPINewConvOutput
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			return nil, fmt.Errorf("failed to parse agentapi output: %w (stdout: %s)", err, stdout.String())
		}
		if output.Error != "" {
			return nil, fmt.Errorf("agentapi returned error: %s", output.Error)
		}

		convID = output.Response.NewConversation.ConversationID
		if convID == "" {
			return nil, fmt.Errorf("no conversationId in agentapi output: %s", stdout.String())
		}
		initialOffset = 0
		log.Printf("[agy-pilot] Started new Antigravity session: %s", convID)
	}

	if onProgress != nil {
		onProgress(StreamEvent{Type: "start", ConversationID: convID})
	}

	transcriptPath := filepath.Join("/root/.gemini/antigravity/brain", convID, ".system_generated/logs/transcript.jsonl")

	maxTotalDuration := 1200 * time.Second
	deadline := time.Now().Add(maxTotalDuration)
	inactivityTimeout := 300 * time.Second
	lastActivity := time.Now()
	var lastFileSize int64 = initialOffset
	reportedEvents := make(map[string]bool)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			now := time.Now()
			if now.After(deadline) {
				return nil, fmt.Errorf("общий таймаут выполнения запроса (20 мин) превышен в %s", convID)
			}

			stat, err := os.Stat(transcriptPath)
			if err != nil || stat.Size() <= initialOffset {
				if now.Sub(lastActivity) > inactivityTimeout {
					return nil, fmt.Errorf("timeout waiting for Antigravity response in %s", convID)
				}
				continue
			}

			// If file size changed, Antigravity is actively executing tool calls or outputting content
			if stat.Size() > lastFileSize {
				lastFileSize = stat.Size()
				lastActivity = now
			} else if now.Sub(lastActivity) > inactivityTimeout {
				return nil, fmt.Errorf("antigravity не отвечает более 300с в %s", convID)
			}

			events, reply, question, options, done, err := readTurnResponse(transcriptPath, initialOffset)
			if err != nil {
				continue
			}

			// Report intermediate tool progress to streaming client (deduplicated by step & output state)
			for _, ev := range events {
				key := fmt.Sprintf("%d:%s:%t", ev.StepIndex, ev.Summary, ev.Output != "")
				if !reportedEvents[key] {
					reportedEvents[key] = true
					if onProgress != nil && ev.Summary != "" {
						onProgress(StreamEvent{
							Type:           "progress",
							ConversationID: convID,
							Action:         ev.Name,
							Summary:        ev.Summary,
							Detail:         ev.Detail,
							Output:         ev.Output,
						})
					}
				}
			}

			if done {
				if reply == "" && question != "" {
					reply = "❓ " + question
				}
				if reply != "" || len(options) > 0 {
					log.Printf("[agy-pilot] Session %s replied (%d chars, %d options, %d steps)", convID, len(reply), len(options), len(events))
					return &AskResponse{
						Response:       reply,
						Text:           reply,
						ConversationID: convID,
						Question:       question,
						Options:        options,
						Steps:          events,
					}, nil
				}
			}
		}
	}
}

func readTurnResponse(path string, startOffset int64) ([]ToolEvent, string, string, []string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", "", nil, false, err
	}
	defer file.Close()

	if startOffset > 0 {
		if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
			return nil, "", "", nil, false, err
		}
	}

	scanner := bufio.NewScanner(file)
	// Allow scanning up to 2MB lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	var lastContent string
	var questionText string
	var optionsList []string
	var foundDone bool
	var events []ToolEvent

	for scanner.Scan() {
		line := scanner.Bytes()
		var step TranscriptStep
		if err := json.Unmarshal(line, &step); err == nil {
			if step.Source == "MODEL" && step.Type == "PLANNER_RESPONSE" {
				if step.Content != "" {
					lastContent = step.Content
				}

				hasTools := len(step.ToolCalls) > 0
				hasAskQuestion := false
				for _, tc := range step.ToolCalls {
					if tc.Name == "ask_question" {
						hasAskQuestion = true
						q, opts := parseAskQuestionArgs(tc.Args)
						if q != "" {
							questionText = q
						}
						if len(opts) > 0 {
							optionsList = opts
						}
					} else {
						summary, detail := extractToolInfo(tc)
						events = append(events, ToolEvent{
							StepIndex: step.StepIndex,
							Name:      tc.Name,
							Summary:   summary,
							Detail:    detail,
						})
					}
				}

				// The turn is complete when the model produced final content without further tool calls,
				// or when it explicitly called ask_question.
				if step.Status == "DONE" && (!hasTools || hasAskQuestion) {
					foundDone = true
				} else {
					foundDone = false
				}
			} else if (step.Type == "GENERIC" || step.Source == "MODEL") && step.Content != "" && len(events) > 0 {
				outText := step.Content
				if idx := strings.Index(outText, "Command output:\n"); idx != -1 {
					outText = outText[idx+len("Command output:\n"):]
				} else if idx := strings.Index(outText, "Command exited with code"); idx != -1 {
					outText = outText[idx:]
				}
				outText = strings.TrimSpace(outText)
				if len(outText) > 200 {
					outText = outText[:197] + "..."
				}
				if outText != "" && events[len(events)-1].Output == "" {
					events[len(events)-1].Output = outText
				}
			}
		}
	}

	if foundDone && (lastContent != "" || questionText != "" || len(optionsList) > 0) {
		return events, lastContent, questionText, optionsList, true, nil
	}
	return events, "", "", nil, false, nil
}

func extractToolInfo(tc ToolCallItem) (string, string) {
	if len(tc.Args) == 0 {
		return formatToolName(tc.Name), ""
	}

	var args map[string]interface{}
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		return formatToolName(tc.Name), ""
	}

	summary := ""
	detail := ""

	if s, ok := args["toolSummary"].(string); ok && s != "" {
		summary = cleanSummary(s)
	}

	if cmd, ok := args["CommandLine"].(string); ok && cmd != "" {
		detail = cleanSummary(cmd)
		if summary == "" {
			summary = "Команда: " + detail
			if len(summary) > 45 {
				summary = summary[:42] + "..."
			}
		}
	} else if p, ok := args["Path"].(string); ok && p != "" {
		detail = cleanSummary(p)
		if summary == "" {
			summary = formatToolName(tc.Name) + ": " + filepath.Base(detail)
		}
	} else if q, ok := args["query"].(string); ok && q != "" {
		detail = cleanSummary(q)
		if summary == "" {
			summary = "Поиск: " + detail
		}
	}

	if summary == "" {
		summary = formatToolName(tc.Name)
	}

	return summary, detail
}

func cleanSummary(s string) string {
	s = strings.Trim(s, "\"'\n\r\t ")
	return s
}

func formatToolName(name string) string {
	switch name {
	case "run_command":
		return "Команда терминала"
	case "read_file":
		return "Чтение файла"
	case "write_file":
		return "Запись файла"
	case "edit_file":
		return "Редактирование файла"
	case "search_web":
		return "Поиск в сети"
	case "list_plugin_accounts":
		return "Проверка плагинов"
	case "manage_task":
		return "Фоновая задача"
	default:
		return name
	}
}

func parseAskQuestionArgs(raw json.RawMessage) (string, []string) {
	var payload struct {
		Questions json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", nil
	}

	var items []QuestionItem
	if err := json.Unmarshal(payload.Questions, &items); err != nil {
		// Try string unmarshal
		var str string
		if err := json.Unmarshal(payload.Questions, &str); err == nil {
			_ = json.Unmarshal([]byte(str), &items)
		}
	}

	if len(items) > 0 {
		return items[0].Question, items[0].Options
	}
	return "", nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
