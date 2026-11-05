package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// rpcError 是 JSON-RPC 错误对象。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Client 是 JSON-RPC 2.0 over JSONL 的客户端（C5）。
type Client struct {
	rw io.ReadWriteCloser

	writeMu sync.Mutex
	nextID  atomic.Uint64

	mu      sync.Mutex
	pending map[string]chan rpcMessage
	err     error

	onNotification func(method string, params json.RawMessage)
	onRequest      func(method string, params json.RawMessage) (json.RawMessage, *rpcError)

	closed    chan struct{}
	closeOnce sync.Once
}

// NewClient 构造客户端；调用 Start 启动读循环。
func NewClient(rw io.ReadWriteCloser) *Client {
	return &Client{
		rw:      rw,
		pending: make(map[string]chan rpcMessage),
		closed:  make(chan struct{}),
	}
}

// Start 启动读循环。
func (c *Client) Start() { go c.readLoop() }

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.rw)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		switch {
		case msg.Method != "" && len(msg.ID) > 0:
			go c.respond(msg)
		case msg.Method != "":
			if c.onNotification != nil {
				c.onNotification(msg.Method, msg.Params)
			}
		default:
			c.routeResponse(msg)
		}
	}
	c.fail(scanner.Err())
}

func (c *Client) respond(req rpcMessage) {
	var (
		result json.RawMessage
		rpcErr *rpcError
	)
	if c.onRequest != nil {
		result, rpcErr = c.onRequest(req.Method, req.Params)
	} else {
		rpcErr = &rpcError{Code: -32601, Message: "unsupported method"}
	}
	resp := rpcMessage{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
	_ = c.write(resp)
}

func (c *Client) routeResponse(msg rpcMessage) {
	key := string(msg.ID)
	c.mu.Lock()
	ch := c.pending[key]
	delete(c.pending, key)
	c.mu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

func (c *Client) fail(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		c.pending = make(map[string]chan rpcMessage)
		c.mu.Unlock()
		close(c.closed)
	})
}

// Call 发送请求并等待响应。
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := strconv.FormatUint(c.nextID.Add(1), 10)
	key := strconv.Quote(id)
	ch := make(chan rpcMessage, 1)

	c.mu.Lock()
	c.pending[key] = ch
	c.mu.Unlock()

	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req := rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(key), Method: method, Params: raw}
	if err := c.write(req); err != nil {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("acp: %s 失败: %s (%d)", method, resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil
	case <-c.closed:
		c.mu.Lock()
		readErr := c.err
		c.mu.Unlock()
		if readErr == nil {
			readErr = errors.New("acp: 连接已关闭")
		}
		return nil, readErr
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Notify 发送通知（无响应）。
func (c *Client) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(rpcMessage{JSONRPC: "2.0", Method: method, Params: raw})
}

func (c *Client) write(msg rpcMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.rw.Write(append(data, '\n'))
	return err
}

// Close 关闭底层连接并唤醒所有等待中的请求。
func (c *Client) Close() error {
	c.fail(errors.New("acp: 客户端已关闭"))
	return c.rw.Close()
}
