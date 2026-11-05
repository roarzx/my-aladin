package daemon

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 背压相关错误。
var (
	ErrBackpressureLimit   = errors.New("stream: 缓冲超过上限")
	ErrBackpressureTimeout = errors.New("stream: 持续背压超时")
)

// StreamConfig 控制缓冲水位与背压阈值。
type StreamConfig struct {
	HighWater     int           // 超过后暂停 Append
	LowWater      int           // 降到该值后恢复
	MaxBuffer     int           // 硬上限，超过即终止
	FlushInterval time.Duration // 后台排空间隔
	MaxPause      time.Duration // 持续暂停超过该时长即终止
}

func (c StreamConfig) withDefaults() StreamConfig {
	if c.HighWater <= 0 {
		c.HighWater = 64 << 10
	}
	if c.LowWater <= 0 {
		c.LowWater = c.HighWater / 4
	}
	if c.MaxBuffer <= 0 {
		c.MaxBuffer = 1 << 20
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = 10 * time.Millisecond
	}
	if c.MaxPause <= 0 {
		c.MaxPause = 30 * time.Second
	}
	return c
}

// Streamer 是有界缓冲的流式发送器（C7）。
//
// Append 在水位超过 HighWater 时阻塞（把背压传回 ACP 读循环），
// 缓冲超过 MaxBuffer 或持续暂停超过 MaxPause 时终止并返回错误。
type Streamer struct {
	mu   sync.Mutex
	cond *sync.Cond
	cfg  StreamConfig
	sink func([]byte) error

	buf         []byte
	paused      bool
	pausedSince time.Time
	err         error

	done      chan struct{}
	closeOnce sync.Once
}

// NewStreamer 构造发送器；sink 负责真正写出（例如 WS stream 消息）。
func NewStreamer(sink func([]byte) error, cfg StreamConfig) *Streamer {
	s := &Streamer{
		cfg:  cfg.withDefaults(),
		sink: sink,
		done: make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Start 启动后台排空与背压监控。
func (s *Streamer) Start(ctx context.Context) {
	go s.flushLoop(ctx)
	go s.watchLoop(ctx)
}

func (s *Streamer) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-ticker.C:
			if err := s.Flush(); err != nil {
				return
			}
		}
	}
}

// watchLoop 独立于 sink，保证 sink 阻塞时仍能发现持续背压。
func (s *Streamer) watchLoop(ctx context.Context) {
	interval := s.cfg.MaxPause / 4
	if interval <= 0 {
		interval = 5 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.paused && time.Since(s.pausedSince) > s.cfg.MaxPause {
				s.setErrLocked(ErrBackpressureTimeout)
			}
			s.mu.Unlock()
		}
	}
}

// Append 追加一段流式内容；暂停时阻塞，直到排空或终止。
func (s *Streamer) Append(delta []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.paused && s.err == nil {
		s.cond.Wait()
	}
	if s.err != nil {
		return s.err
	}
	if len(s.buf)+len(delta) > s.cfg.MaxBuffer {
		s.setErrLocked(ErrBackpressureLimit)
		return s.err
	}
	s.buf = append(s.buf, delta...)
	if len(s.buf) >= s.cfg.HighWater && !s.paused {
		s.paused = true
		s.pausedSince = time.Now()
	}
	return nil
}

// Flush 把当前缓冲写给 sink；sink 失败即终止。
func (s *Streamer) Flush() error {
	s.mu.Lock()
	if s.err != nil {
		err := s.err
		s.mu.Unlock()
		return err
	}
	if len(s.buf) == 0 {
		s.mu.Unlock()
		return nil
	}
	data := append([]byte(nil), s.buf...)
	s.buf = s.buf[:0]
	s.mu.Unlock()

	if err := s.sink(data); err != nil {
		s.setErr(err)
		return err
	}

	s.mu.Lock()
	if s.paused && len(s.buf) <= s.cfg.LowWater {
		s.paused = false
		s.pausedSince = time.Time{}
		s.cond.Broadcast()
	}
	s.mu.Unlock()
	return nil
}

// Paused 表示当前是否处于背压暂停。
func (s *Streamer) Paused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// Err 返回终止原因（未终止为 nil）。
func (s *Streamer) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// WaitResume 等待恢复；ctx 取消或终止时返回。
func (s *Streamer) WaitResume(ctx context.Context) error {
	for {
		s.mu.Lock()
		paused, err := s.paused, s.err
		s.mu.Unlock()
		if err != nil {
			return err
		}
		if !paused {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// Close 停止后台循环并等待一次排空。
func (s *Streamer) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	return s.Flush()
}

func (s *Streamer) setErrLocked(err error) {
	if s.err == nil {
		s.err = err
	}
	s.cond.Broadcast()
}

func (s *Streamer) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setErrLocked(err)
}
