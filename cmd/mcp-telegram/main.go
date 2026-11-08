package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// MCP JSON-RPC 2.0 structures
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool definition for MCP tools/list
var tgSendTool = map[string]interface{}{
	"name":        "tg_send",
	"description": "Отправить текстовое сообщение, отчет или файл с сервера пользователю в Telegram чат.",
	"inputSchema": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Текст сообщения для отправки в Telegram (поддерживает HTML разметку).",
			},
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "Абсолютный путь к файлу на сервере, который нужно отправить как документ (необязательно).",
			},
		},
		"required": []string{},
	},
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	// Allow scanning large payloads
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, len(buf))

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			sendError(nil, -32700, "Parse error")
			continue
		}

		handleRequest(&req)
	}
}

func handleRequest(req *jsonRPCRequest) {
	switch req.Method {
	case "initialize":
		res := map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "superbot-mcp-telegram",
				"version": "1.0.0",
			},
		}
		sendResponse(req.ID, res)

	case "notifications/initialized":
		// No response required for notifications
		return

	case "tools/list":
		res := map[string]interface{}{
			"tools": []interface{}{tgSendTool},
		}
		sendResponse(req.ID, res)

	case "tools/call":
		var callParams struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			sendError(req.ID, -32602, "Invalid params")
			return
		}

		if callParams.Name != "tg_send" {
			sendError(req.ID, -32601, fmt.Sprintf("Unknown tool: %s", callParams.Name))
			return
		}

		text, _ := callParams.Arguments["text"].(string)
		filePath, _ := callParams.Arguments["file_path"].(string)

		// Forward to local SuperBot API (:43428)
		apiURL := os.Getenv("LOCAL_API_URL")
		if apiURL == "" {
			apiURL = "http://127.0.0.1:43428/api/send"
		}

		payload := map[string]string{
			"text":      text,
			"file_path": filePath,
		}
		jsonBytes, _ := json.Marshal(payload)

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			toolResult := map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("❌ Ошибка отправки через SuperBot API: %s", err.Error()),
					},
				},
				"isError": true,
			}
			sendResponse(req.ID, toolResult)
			return
		}
		defer resp.Body.Close()

		respBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			toolResult := map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": fmt.Sprintf("❌ SuperBot вернул ошибку (%d): %s", resp.StatusCode, string(respBytes)),
					},
				},
				"isError": true,
			}
			sendResponse(req.ID, toolResult)
			return
		}

		successMsg := "✅ Сообщение успешно доставлено пользователю в Telegram."
		if filePath != "" {
			successMsg = fmt.Sprintf("✅ Файл '%s' и сообщение успешно отправлены в Telegram.", filePath)
		}

		toolResult := map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": successMsg,
				},
			},
		}
		sendResponse(req.ID, toolResult)

	default:
		if req.ID != nil {
			sendError(req.ID, -32601, "Method not found")
		}
	}
}

func sendResponse(id interface{}, result interface{}) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	out, _ := json.Marshal(resp)
	fmt.Fprintf(os.Stdout, "%s\n", out)
	os.Stdout.Sync()
}

func sendError(id interface{}, code int, message string) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	}
	out, _ := json.Marshal(resp)
	fmt.Fprintf(os.Stdout, "%s\n", out)
	os.Stdout.Sync()
}
