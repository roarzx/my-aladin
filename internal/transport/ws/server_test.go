package ws

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/roarzx/my-aladin/pkg/protocol"
)

func testAuth(r *http.Request) (string, error) {
	if r.Header.Get("Authorization") == "Bearer machine-1" {
		return "machine-1", nil
	}
	return "", errors.New("bad token")
}

func newTestWSServer(t *testing.T, cfg Config) string {
	t.Helper()
	srv := NewServer(testAuth, cfg, nil)
	hs := httptest.NewServer(http.HandlerFunc(srv.HandleUpgrade))
	t.Cleanup(hs.Close)
	return "ws" + strings.TrimPrefix(hs.URL, "http")
}

func dialWS(t *testing.T, url, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	return websocket.DefaultDialer.Dial(url, header)
}

func sendEnvelope(t *testing.T, conn *websocket.Conn, env protocol.Envelope) {
	t.Helper()
	data, err := protocol.Encode(env)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("写消息: %v", err)
	}
}

func readEnvelope(t *testing.T, conn *websocket.Conn) protocol.Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("读消息: %v", err)
	}
	env, err := protocol.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return env
}

func TestHandshakeAndAuth(t *testing.T) {
	url := newTestWSServer(t, Config{
		HeartbeatInterval: time.Hour,
		HeartbeatTimeout:  2 * time.Second,
		HandshakeTimeout:  time.Second,
	})

	// 无令牌：升级被拒绝（401）
	_, resp, err := dialWS(t, url, "")
	if err == nil {
		t.Fatal("无令牌应失败")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("期望 401，实际: %+v", resp)
	}

	// 正确令牌 + 握手
	conn, _, err := dialWS(t, url, "machine-1")
	if err != nil {
		t.Fatalf("连接: %v", err)
	}
	defer func() { _ = conn.Close() }()

	payload, _ := json.Marshal(HandshakePayload{MachineID: "machine-1", CLIVersion: "test"})
	sendEnvelope(t, conn, protocol.Envelope{
		Type: protocol.TypeHandshake, ID: "hs-1", Payload: payload,
	})
	ack := readEnvelope(t, conn)
	if ack.Type != protocol.TypeHandshakeAck {
		t.Fatalf("期望 handshake_ack，实际 %q", ack.Type)
	}
}

func TestCommandBeforeHandshakeRejected(t *testing.T) {
	url := newTestWSServer(t, Config{
		HeartbeatInterval: time.Hour,
		HeartbeatTimeout:  2 * time.Second,
		HandshakeTimeout:  500 * time.Millisecond,
	})
	conn, _, err := dialWS(t, url, "machine-1")
	if err != nil {
		t.Fatalf("连接: %v", err)
	}
	defer func() { _ = conn.Close() }()

	sendEnvelope(t, conn, protocol.Envelope{Type: protocol.TypeCommand, ID: "c1"})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("握手前发送命令应被断开")
	}
}

func TestHeartbeatTimeoutDisconnects(t *testing.T) {
	url := newTestWSServer(t, Config{
		HeartbeatInterval: time.Hour, // 不主动发心跳，避免干扰
		HeartbeatTimeout:  150 * time.Millisecond,
		HandshakeTimeout:  time.Second,
	})
	conn, _, err := dialWS(t, url, "machine-1")
	if err != nil {
		t.Fatalf("连接: %v", err)
	}
	defer func() { _ = conn.Close() }()

	payload, _ := json.Marshal(HandshakePayload{MachineID: "machine-1"})
	sendEnvelope(t, conn, protocol.Envelope{Type: protocol.TypeHandshake, ID: "hs-2", Payload: payload})
	_ = readEnvelope(t, conn) // ack

	// 之后保持沉默，服务端应在超过心跳超时后断开
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
