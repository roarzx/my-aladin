// Package ws 是控制面 ↔ daemon 的 WebSocket 传输（B6/B8/B9）。
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// Config 控制握手、心跳与超时。
type Config struct {
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	HandshakeTimeout  time.Duration
	WriteTimeout      time.Duration
	MaxMessageBytes   int64
}

// DefaultConfig 返回与 9.9 一致的默认值。
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval: 30 * time.Second,
		HeartbeatTimeout:  90 * time.Second,
		HandshakeTimeout:  10 * time.Second,
		WriteTimeout:      5 * time.Second,
		MaxMessageBytes:   1 << 20,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.HeartbeatInterval > 0 {
		d.HeartbeatInterval = c.HeartbeatInterval
	}
	if c.HeartbeatTimeout > 0 {
		d.HeartbeatTimeout = c.HeartbeatTimeout
	}
	if c.HandshakeTimeout > 0 {
		d.HandshakeTimeout = c.HandshakeTimeout
	}
	if c.WriteTimeout > 0 {
		d.WriteTimeout = c.WriteTimeout
	}
	if c.MaxMessageBytes > 0 {
		d.MaxMessageBytes = c.MaxMessageBytes
	}
	return d
}

// MachineAuthenticator 在升级前校验机器令牌；失败返回非 nil 错误（HTTP 401）。
type MachineAuthenticator func(r *http.Request) (machineID string, err error)

// HandshakePayload 是 handshake 信封的 payload。
type HandshakePayload struct {
	MachineID    string   `json:"machine_id"`
	CLIVersion   string   `json:"cli_version"`
	Capabilities []string `json:"capabilities"`
}

// Server 处理 daemon 的 WS 连接。
type Server struct {
	auth     MachineAuthenticator
	cfg      Config
	log      *slog.Logger
	upgrader websocket.Upgrader

	// Handler 在握手成功后运行；machineID 来自升级前的鉴权结果。
	// 为 nil 时只维持连接与心跳。
	Handler func(ctx context.Context, machineID string, c *Conn) error
}

// NewServer 构造 WS 服务端。
func NewServer(auth MachineAuthenticator, cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		auth: auth,
		cfg:  cfg.withDefaults(),
		log:  logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// 开发环境允许任意 Origin；生产部署由网关限制。
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

// ServeHTTP 让 Server 可直接挂到路由上。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.HandleUpgrade(w, r)
}

// HandleUpgrade 是 http.HandlerFunc：认证 → 握手 → 心跳 → 业务 Handler。
func (s *Server) HandleUpgrade(w http.ResponseWriter, r *http.Request) {
	machineID, err := s.auth(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	wsConn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := newConn(wsConn, s.cfg)
	defer func() { _ = conn.Close() }()

	env, err := conn.readWithTimeout(s.cfg.HandshakeTimeout)
	if err != nil || env.Type != protocol.TypeHandshake {
		s.log.Warn("握手失败", "machine", machineID, "err", err)
		return
	}
	var hs HandshakePayload
	if err := json.Unmarshal(env.Payload, &hs); err != nil || hs.MachineID != machineID {
		s.log.Warn("握手身份不匹配", "machine", machineID)
		return
	}
	s.log.Info("daemon 已连接", "machine", machineID, "cli", hs.CLIVersion)

	ackPayload, _ := json.Marshal(map[string]string{
		"server_version":     protocol.Version,
		"heartbeat_interval": s.cfg.HeartbeatInterval.String(),
		"heartbeat_timeout":  s.cfg.HeartbeatTimeout.String(),
	})
	if err := conn.Send(protocol.Envelope{
		Type:      protocol.TypeHandshakeAck,
		ID:        idgen.New(),
		TraceID:   env.TraceID,
		SessionID: env.SessionID,
		Payload:   ackPayload,
	}); err != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go s.heartbeatLoop(ctx, conn)

	if s.Handler != nil {
		if err := s.Handler(ctx, machineID, conn); err != nil {
			s.log.Info("连接结束", "machine", machineID, "err", err)
		}
		return
	}
	_ = conn.Keepalive(ctx)
}

func (s *Server) heartbeatLoop(ctx context.Context, c *Conn) {
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Send(protocol.Envelope{
				Type: protocol.TypeHeartbeat,
				ID:   idgen.New(),
			}); err != nil {
				_ = c.Close()
				return
			}
		}
	}
}
