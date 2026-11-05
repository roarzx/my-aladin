package ws

import (
	"context"
	"errors"
	"sync"

	"github.com/roarzx/my-aladin/pkg/protocol"
)

// ErrCancelled 表示命令已被显式取消。
var ErrCancelled = errors.New("session: 命令已取消")

// EnvelopeConn 是 Session 依赖的最小连接能力（便于测试替换）。
type EnvelopeConn interface {
	Read() (protocol.Envelope, error)
	Send(protocol.Envelope) error
	Close() error
}

type pendingCall struct {
	resp      chan protocol.Envelope
	cancelled chan struct{}
	once      sync.Once
}

func (p *pendingCall) cancel() {
	p.once.Do(func() { close(p.cancelled) })
}

// Session 在一条连接上做命令/响应配对（B8）。
type Session struct {
	conn EnvelopeConn

	mu       sync.Mutex
	pending  map[string]*pendingCall
	handlers []func(protocol.Envelope)
}

// NewSession 构造会话。
func NewSession(conn EnvelopeConn) *Session {
	return &Session{conn: conn, pending: make(map[string]*pendingCall)}
}

// OnEnvelope 注册非响应消息的处理函数（事件/流/心跳）。
func (s *Session) OnEnvelope(h func(protocol.Envelope)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, h)
}

// Run 是读循环：响应按 id 配对，其余交给 handlers；返回时取消所有等待中的命令。
func (s *Session) Run() error {
	for {
		env, err := s.conn.Read()
		if err != nil {
			s.cancelAll()
			return err
		}
		if env.Type == protocol.TypeResponse {
			s.mu.Lock()
			call := s.pending[env.ID]
			s.mu.Unlock()
			if call != nil {
				select {
				case call.resp <- env:
				default:
				}
			}
			continue
		}
		s.mu.Lock()
		handlers := append([]func(protocol.Envelope){}, s.handlers...)
		s.mu.Unlock()
		for _, h := range handlers {
			h(env)
		}
	}
}

// Dispatch 发送命令并等待同一 id 的响应；ctx 取消或 Cancel 会立即返回。
func (s *Session) Dispatch(ctx context.Context, cmd protocol.Envelope) (protocol.Envelope, error) {
	if cmd.Type != protocol.TypeCommand {
		return protocol.Envelope{}, errors.New("session: Dispatch 只接受 command")
	}
	call := &pendingCall{
		resp:      make(chan protocol.Envelope, 1),
		cancelled: make(chan struct{}),
	}

	s.mu.Lock()
	if _, dup := s.pending[cmd.ID]; dup {
		s.mu.Unlock()
		return protocol.Envelope{}, errors.New("session: 重复的命令 id")
	}
	s.pending[cmd.ID] = call
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, cmd.ID)
		s.mu.Unlock()
	}()

	if err := s.conn.Send(cmd); err != nil {
		return protocol.Envelope{}, err
	}

	select {
	case resp := <-call.resp:
		return resp, nil
	case <-call.cancelled:
		return protocol.Envelope{}, ErrCancelled
	case <-ctx.Done():
		return protocol.Envelope{}, ctx.Err()
	}
}

// Cancel 取消一个等待中的命令。
func (s *Session) Cancel(id string) {
	s.mu.Lock()
	call := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if call != nil {
		call.cancel()
	}
}

func (s *Session) cancelAll() {
	s.mu.Lock()
	calls := make([]*pendingCall, 0, len(s.pending))
	for _, c := range s.pending {
		calls = append(calls, c)
	}
	s.pending = make(map[string]*pendingCall)
	s.mu.Unlock()
	for _, c := range calls {
		c.cancel()
	}
}

// Close 关闭底层连接。
func (s *Session) Close() error { return s.conn.Close() }
