// Package fakeserver 是脚本化的 WS 对端（C13），让 daemon 测试不依赖真实控制面。
package fakeserver

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// Options 控制对端行为。
type Options struct {
	// Token 非空时校验 `Authorization: Bearer <Token>`。
	Token string
	// Reject 为 true 时所有升级返回 401。
	Reject bool
}

// Server 是可脚本化的 WS 对端。
type Server struct {
	opts     Options
	ln       net.Listener
	httpSrv  *http.Server
	upgrader websocket.Upgrader

	writeMu sync.Mutex
	mu      sync.Mutex
	conns   map[*websocket.Conn]struct{}

	received  chan protocol.Envelope
	connected chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

// New 启动一个本地对端；测试结束自动关闭。
func New(t *testing.T, opts Options) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听: %v", err)
	}
	s := &Server{
		opts: opts,
		ln:   ln,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
		conns:     make(map[*websocket.Conn]struct{}),
		received:  make(chan protocol.Envelope, 128),
		connected: make(chan struct{}, 8),
		closed:    make(chan struct{}),
	}
	s.httpSrv = &http.Server{Handler: http.HandlerFunc(s.handle)}
	go func() { _ = s.httpSrv.Serve(ln) }()
	t.Cleanup(s.Close)
	return s
}

// URL 返回 daemon 侧应连接的地址。
func (s *Server) URL() string { return "ws://" + s.ln.Addr().String() }

// Received 返回客户端发来的消息流（含心跳与响应）。
func (s *Server) Received() <-chan protocol.Envelope { return s.received }

// WaitConnected 等待一次成功的握手。
func (s *Server) WaitConnected(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-s.connected:
	case <-time.After(timeout):
		t.Fatal("等待 daemon 连接超时")
	}
}

// Send 向当前所有连接广播一条消息。
func (s *Server) Send(env protocol.Envelope) error {
	data, err := protocol.Encode(env)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.conns {
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return err
		}
	}
	return nil
}

// Drop 关闭当前所有连接，用于测试重连。
func (s *Server) Drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = make(map[*websocket.Conn]struct{})
}

// Close 停止对端。
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.httpSrv.Close()
	})
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if s.opts.Reject ||
		(s.opts.Token != "" && r.Header.Get("Authorization") != "Bearer "+s.opts.Token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return
	}
	env, err := protocol.Decode(data)
	if err != nil || env.Type != protocol.TypeHandshake {
		return
	}

	ackPayload, _ := json.Marshal(map[string]string{"server_version": protocol.Version})
	ackData, err := protocol.Encode(protocol.Envelope{
		Type:    protocol.TypeHandshakeAck,
		ID:      idgen.New(),
		TraceID: env.TraceID,
		Payload: ackPayload,
	})
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, ackData); err != nil {
		return
	}

	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
	select {
	case s.connected <- struct{}{}:
	default:
	}

	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		env, err := protocol.Decode(data)
		if err != nil {
			continue
		}
		select {
		case s.received <- env:
		case <-s.closed:
		}
	}

	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}
