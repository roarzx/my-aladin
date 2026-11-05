package ws

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/roarzx/my-aladin/pkg/protocol"
)

// Conn 是带协议编解码与超时控制的 WS 连接。
type Conn struct {
	ws        *websocket.Conn
	cfg       Config
	writeMu   sync.Mutex
	closeOnce sync.Once
}

func newConn(ws *websocket.Conn, cfg Config) *Conn {
	return &Conn{ws: ws, cfg: cfg}
}

// Read 读取并校验一条信封；超过 HeartbeatTimeout 未收到消息则返回错误。
func (c *Conn) Read() (protocol.Envelope, error) {
	return c.readWithTimeout(c.cfg.HeartbeatTimeout)
}

func (c *Conn) readWithTimeout(timeout time.Duration) (protocol.Envelope, error) {
	c.ws.SetReadLimit(c.cfg.MaxMessageBytes)
	_ = c.ws.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := c.ws.ReadMessage()
	if err != nil {
		return protocol.Envelope{}, err
	}
	return protocol.Decode(data)
}

// Send 编码并发送一条信封。
func (c *Conn) Send(env protocol.Envelope) error {
	data, err := protocol.Encode(env)
	if err != nil {
		return err
	}
	return c.SendRaw(data)
}

// SendRaw 发送已编码的数据（可靠重投复用原字节，保证 id 不变）。
func (c *Conn) SendRaw(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// Close 关闭连接（幂等）。
func (c *Conn) Close() error {
	c.closeOnce.Do(func() { _ = c.ws.Close() })
	return nil
}

// Keepalive 持续读取直到超时或关闭；心跳消息只用于刷新读截止时间。
func (c *Conn) Keepalive(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := c.Read(); err != nil {
			return err
		}
	}
}
