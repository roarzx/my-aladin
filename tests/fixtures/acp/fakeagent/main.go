// Command fakeagent 是测试用的 ACP 对端（JSON-RPC 2.0 over JSONL）。
//
// 模式由 FAKE_ACP_MODE 控制：fail_init / hang / crash / refuse。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		if v := os.Getenv("FAKE_ACP_VERSION"); v != "" {
			fmt.Println(v)
			return
		}
		fmt.Println("0.100.0")
		return
	}

	mode := os.Getenv("FAKE_ACP_MODE")
	out := bufio.NewWriter(os.Stdout)
	defer func() { _ = out.Flush() }()

	write := func(value any) {
		data, _ := json.Marshal(value)
		_, _ = out.Write(data)
		_ = out.WriteByte('\n')
		_ = out.Flush()
	}
	result := func(id json.RawMessage, value any) {
		write(map[string]any{"jsonrpc": "2.0", "id": id, "result": value})
	}

	const sessionID = "acp-session-1"
	fsError := 0

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.Method == "" {
			// 客户端对反向请求的响应
			if strings.TrimSpace(string(msg.ID)) == `"fs-1"` && msg.Error != nil {
				fsError = msg.Error.Code
			}
			continue
		}

		switch msg.Method {
		case "initialize":
			if mode == "fail_init" {
				write(map[string]any{
					"jsonrpc": "2.0", "id": msg.ID,
					"error": map[string]any{"code": -32000, "message": "init failed"},
				})
				continue
			}
			result(msg.ID, map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": true},
			})
			// 反向请求：验证客户端对未支持方法返回 -32601
			write(map[string]any{
				"jsonrpc": "2.0", "id": "fs-1", "method": "fs/read_text_file",
				"params": map[string]any{"path": "/tmp/x"},
			})
		case "session/new":
			result(msg.ID, map[string]any{"sessionId": sessionID})
		case "session/load":
			result(msg.ID, map[string]any{})
		case "session/prompt":
			if mode == "write_file" {
				if dir, err := os.Getwd(); err == nil {
					_ = os.WriteFile(dir+"/agent-output.txt", []byte("hello from fake agent\n"), 0o600)
				}
			}
			switch mode {
			case "hang":
				continue // 不回复，等待取消或被杀
			case "crash":
				os.Exit(1)
			}
			write(map[string]any{
				"jsonrpc": "2.0", "method": "session/update",
				"params": map[string]any{
					"sessionId": sessionID,
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": "hello"},
					},
				},
			})
			write(map[string]any{
				"jsonrpc": "2.0", "method": "session/update",
				"params": map[string]any{
					"sessionId": sessionID,
					"update": map[string]any{
						"sessionUpdate": "tool_call",
						"toolCallId":    "t1",
						"title":         "read",
						"kind":          "read",
						"status":        "pending",
					},
				},
			})
			stop := "end_turn"
			if mode == "refuse" {
				stop = "refusal"
			}
			result(msg.ID, map[string]any{
				"stopReason": stop,
				"usage":      map[string]any{"inputTokens": 10, "outputTokens": 5},
				"fsError":    fsError,
			})
		case "session/cancel":
			if mode == "hang" {
				os.Exit(0)
			}
		}
	}
}
