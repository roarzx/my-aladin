package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/roarzx/my-aladin/pkg/protocol"
)

type inflightCall struct {
	done    chan struct{}
	payload protocol.ResponsePayload
	err     error
}

// Dispatcher 按 command id 去重（C3）：命中缓存直接返回；同一 id 并发只执行一次。
type Dispatcher struct {
	store   *IdempotentStore
	handler CommandHandler
	log     *slog.Logger

	mu       sync.Mutex
	inflight map[string]*inflightCall
}

// NewDispatcher 构造分发器；handler 为 nil 时返回 unsupported_method。
func NewDispatcher(store *IdempotentStore, handler CommandHandler, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if handler == nil {
		handler = func(context.Context, protocol.Envelope) (protocol.ResponsePayload, error) {
			return protocol.ResponsePayload{
				Accepted: false,
				Error:    &protocol.ResponseError{Code: "unsupported_method", Message: "未注册命令处理器"},
			}, nil
		}
	}
	return &Dispatcher{
		store:    store,
		handler:  handler,
		log:      logger,
		inflight: make(map[string]*inflightCall),
	}
}

// Handle 处理一条命令；重复命令返回上次结果，错误结果不缓存（允许重试）。
func (d *Dispatcher) Handle(ctx context.Context, env protocol.Envelope) (protocol.ResponsePayload, error) {
	if d.store != nil {
		if raw, ok := d.store.Get(env.ID); ok {
			var payload protocol.ResponsePayload
			if err := json.Unmarshal(raw, &payload); err == nil {
				d.log.Debug("重复命令，返回缓存结果", "id", env.ID)
				return payload, nil
			}
		}
	}

	d.mu.Lock()
	if call, ok := d.inflight[env.ID]; ok {
		d.mu.Unlock()
		select {
		case <-call.done:
			return call.payload, call.err
		case <-ctx.Done():
			return protocol.ResponsePayload{}, ctx.Err()
		}
	}
	call := &inflightCall{done: make(chan struct{})}
	d.inflight[env.ID] = call
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.inflight, env.ID)
		d.mu.Unlock()
	}()

	payload, err := d.handler(ctx, env)
	if err == nil && d.store != nil {
		if raw, mErr := json.Marshal(payload); mErr == nil {
			if putErr := d.store.Put(env.ID, raw); putErr != nil {
				d.log.Warn("记录命令结果失败", "id", env.ID, "err", putErr)
			}
		}
	}

	call.payload, call.err = payload, err
	close(call.done)
	return payload, err
}
