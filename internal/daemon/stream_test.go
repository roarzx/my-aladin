package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type collectingSink struct {
	mu  sync.Mutex
	got []byte
}

func (c *collectingSink) fn(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, data...)
	return nil
}

func (c *collectingSink) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.got)
}

func TestStreamFlushes(t *testing.T) {
	sink := &collectingSink{}
	stream := NewStreamer(sink.fn, StreamConfig{HighWater: 8, LowWater: 2, MaxBuffer: 100})

	if err := stream.Append([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := stream.Flush(); err != nil {
		t.Fatal(err)
	}
	if sink.String() != "hello" {
		t.Fatalf("sink = %q", sink.String())
	}
}

func TestStreamPausesAndResumes(t *testing.T) {
	release := make(chan struct{})
	var (
		mu     sync.Mutex
		chunks []string
	)
	sink := func(data []byte) error {
		<-release
		mu.Lock()
		chunks = append(chunks, string(data))
		mu.Unlock()
		return nil
	}

	stream := NewStreamer(sink, StreamConfig{
		HighWater: 8, LowWater: 2, MaxBuffer: 100, FlushInterval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream.Start(ctx)
	defer func() { _ = stream.Close() }()

	if err := stream.Append([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if !stream.Paused() {
		t.Fatal("超过高水位应进入暂停")
	}

	blocked := make(chan error, 1)
	go func() { blocked <- stream.Append([]byte("abc")) }()
	time.Sleep(20 * time.Millisecond)
	select {
	case err := <-blocked:
		t.Fatalf("暂停期间 Append 不应返回: %v", err)
	default:
	}

	close(release)
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("恢复后 Append: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Append 未恢复")
	}
	if stream.Paused() {
		t.Fatal("排空后应解除暂停")
	}

	if err := stream.Flush(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	joined := strings.Join(chunks, "|")
	mu.Unlock()
	if joined != "0123456789|abc" {
		t.Fatalf("写入顺序 = %q", joined)
	}
}

func TestStreamRejectsOversizedAppend(t *testing.T) {
	stream := NewStreamer(func([]byte) error { return nil }, StreamConfig{
		HighWater: 8, LowWater: 2, MaxBuffer: 10,
	})
	err := stream.Append(make([]byte, 11))
	if !errors.Is(err, ErrBackpressureLimit) {
		t.Fatalf("期望 ErrBackpressureLimit，实际: %v", err)
	}
}

func TestStreamTerminatesOnSustainedBackpressure(t *testing.T) {
	block := make(chan struct{})
	defer close(block)

	stream := NewStreamer(func([]byte) error {
		<-block
		return nil
	}, StreamConfig{
		HighWater: 4, LowWater: 1, MaxBuffer: 100,
		FlushInterval: 2 * time.Millisecond, MaxPause: 30 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream.Start(ctx)

	if err := stream.Append([]byte("1234")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && stream.Err() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	if !errors.Is(stream.Err(), ErrBackpressureTimeout) {
		t.Fatalf("期望 ErrBackpressureTimeout，实际: %v", stream.Err())
	}
	if err := stream.Append([]byte("x")); !errors.Is(err, ErrBackpressureTimeout) {
		t.Fatalf("终止后 Append 应返回超时错误，实际: %v", err)
	}
	if err := stream.WaitResume(context.Background()); !errors.Is(err, ErrBackpressureTimeout) {
		t.Fatalf("WaitResume 应返回终止原因，实际: %v", err)
	}
}
