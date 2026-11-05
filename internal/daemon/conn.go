package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// ErrRejected 表示服务端明确拒绝连接（令牌无效等），不应无限重试。
var ErrRejected = errors.New("daemon: 服务端拒绝连接")

// CommandHandler 处理服务端下发的命令，返回 response payload。
type CommandHandler func(ctx context.Context, env protocol.Envelope) (protocol.ResponsePayload, error)

// ClientOptions 是控制面连接参数。
type ClientOptions struct {
	ACPURL            string
	MachineID         string
	Token             string
	CLIVersion        string
	Capabilities      []string
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	HandshakeTimeout  time.Duration
	WriteTimeout      time.Duration
	MaxMessageBytes   int64
	Backoff           Backoff
	OnCommand         CommandHandler
	Logger            *slog.Logger
}

// Client 是 daemon 侧的控制面连接（B/C2）：握手、心跳、重连与命令响应。
type Client struct {
	opts ClientOptions
	log  *slog.Logger
}

// NewClient 构造客户端并补齐默认值。
func NewClient(opts ClientOptions) *Client {
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 30 * time.Second
	}
	if opts.HeartbeatTimeout <= 0 {
		opts.HeartbeatTimeout = 90 * time.Second
	}
	if opts.HandshakeTimeout <= 0 {
		opts.HandshakeTimeout = 10 * time.Second
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = 5 * time.Second
	}
	if opts.MaxMessageBytes <= 0 {
		opts.MaxMessageBytes = 1 << 20
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{opts: opts, log: logger}
}

// Run 维持与执行面的连接，直到 ctx 取消或服务端明确拒绝。
func (c *Client) Run(ctx context.Context) error {
	attempt := 0
	for {
		err := c.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrRejected) {
			return err
		}
		wait := c.opts.Backoff.Wait(attempt)
		attempt++
		c.log.Warn("连接中断，准备重连", "err", err, "wait", wait, "attempt", attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (c *Client) connectAndServe(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.opts.Token)

	dialer := websocket.Dialer{HandshakeTimeout: c.opts.HandshakeTimeout}
	conn, resp, err := dialer.DialContext(ctx, c.opts.ACPURL, header)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("%w: HTTP %d", ErrRejected, resp.StatusCode)
		}
		return err
	}
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(c.opts.MaxMessageBytes)

	writer := &envelopeWriter{conn: conn, timeout: c.opts.WriteTimeout}
	if err := c.handshake(conn, writer); err != nil {
		return err
	}
	c.log.Info("已连接控制面", "machine", c.opts.MachineID)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.heartbeat(runCtx, writer)

	for {
		env, err := c.readEnvelope(conn)
		if err != nil {
			return err
		}
		switch env.Type {
		case protocol.TypeHeartbeat, protocol.TypeHandshakeAck:
			continue
		case protocol.TypeCommand:
			go c.handleCommand(runCtx, writer, env)
		default:
			c.log.Debug("忽略消息", "type", env.Type)
		}
	}
}

func (c *Client) handshake(conn *websocket.Conn, writer *envelopeWriter) error {
	payload, err := json.Marshal(map[string]any{
		"machine_id":   c.opts.MachineID,
		"cli_version":  c.opts.CLIVersion,
		"capabilities": c.opts.Capabilities,
	})
	if err != nil {
		return err
	}
	if err := writer.send(protocol.Envelope{
		Type:    protocol.TypeHandshake,
		ID:      idgen.New(),
		TraceID: idgen.New(),
		Payload: payload,
	}); err != nil {
		return fmt.Errorf("发送握手: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(c.opts.HandshakeTimeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("等待握手确认: %w", err)
	}
	ack, err := protocol.Decode(data)
	if err != nil {
		return err
	}
	if ack.Type != protocol.TypeHandshakeAck {
		return fmt.Errorf("期望 handshake_ack，收到 %q", ack.Type)
	}
	return nil
}

func (c *Client) heartbeat(ctx context.Context, writer *envelopeWriter) {
	ticker := time.NewTicker(c.opts.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writer.send(protocol.Envelope{
				Type:    protocol.TypeHeartbeat,
				ID:      idgen.New(),
				TraceID: idgen.New(),
			}); err != nil {
				c.log.Warn("发送心跳失败", "err", err)
				_ = writer.conn.Close()
				return
			}
		}
	}
}

func (c *Client) readEnvelope(conn *websocket.Conn) (protocol.Envelope, error) {
	_ = conn.SetReadDeadline(time.Now().Add(c.opts.HeartbeatTimeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return protocol.Envelope{}, err
	}
	return protocol.Decode(data)
}

func (c *Client) handleCommand(ctx context.Context, writer *envelopeWriter, env protocol.Envelope) {
	payload := protocol.ResponsePayload{
		Accepted: false,
		Error:    &protocol.ResponseError{Code: "unsupported_method", Message: "未注册命令处理器"},
	}
	if c.opts.OnCommand != nil {
		out, err := c.opts.OnCommand(ctx, env)
		if err != nil {
			payload = protocol.ResponsePayload{
				Accepted: false,
				Error:    &protocol.ResponseError{Code: "handler_error", Message: err.Error()},
			}
		} else {
			payload = out
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		c.log.Error("编码响应失败", "id", env.ID, "err", err)
		return
	}
	resp := protocol.Envelope{
		Type:             protocol.TypeResponse,
		ID:               env.ID,
		TraceID:          env.TraceID,
		SessionID:        env.SessionID,
		ExecutionID:      env.ExecutionID,
		ExecutionAttempt: env.ExecutionAttempt,
		StepID:           env.StepID,
		Payload:          raw,
	}
	if err := writer.send(resp); err != nil {
		c.log.Warn("发送响应失败", "id", env.ID, "err", err)
	}
}

type envelopeWriter struct {
	mu      sync.Mutex
	conn    *websocket.Conn
	timeout time.Duration
}

func (w *envelopeWriter) send(env protocol.Envelope) error {
	data, err := protocol.Encode(env)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(w.timeout))
	return w.conn.WriteMessage(websocket.TextMessage, data)
}
