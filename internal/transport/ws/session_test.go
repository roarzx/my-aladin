package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/pkg/protocol"
)

type fakeConn struct {
	in     chan protocol.Envelope
	out    chan protocol.Envelope
	closed chan struct{}
	once   sync.Once
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		in:     make(chan protocol.Envelope, 256),
		out:    make(chan protocol.Envelope, 256),
		closed: make(chan struct{}),
	}
}

func (f *fakeConn) Read() (protocol.Envelope, error) {
	select {
	case env := <-f.in:
		return env, nil
	case <-f.closed:
		return protocol.Envelope{}, io.EOF
	}
}

func (f *fakeConn) Send(env protocol.Envelope) error {
	select {
	case f.out <- env:
		return nil
	case <-f.closed:
		return io.EOF
	}
}

func (f *fakeConn) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

func command(id string) protocol.Envelope {
	payload, _ := json.Marshal(protocol.CommandPayload{Method: "ping"})
	return protocol.Envelope{Type: protocol.TypeCommand, ID: id, Payload: payload}
}

func TestDispatchConcurrentDoesNotCross(t *testing.T) {
	conn := newFakeConn()
	session := NewSession(conn)
	go func() { _ = session.Run() }()

	const n = 100
	go func() {
		cmds := make([]protocol.Envelope, 0, n)
		for i := 0; i < n; i++ {
			select {
			case env := <-conn.out:
				cmds = append(cmds, env)
			case <-time.After(3 * time.Second):
				return
			}
		}
		// 逆序回复，确保按 id 而不是按顺序配对
		for i := len(cmds) - 1; i >= 0; i-- {
			cmd := cmds[i]
			payload, _ := json.Marshal(map[string]string{"echo": cmd.ID})
			select {
			case conn.in <- protocol.Envelope{Type: protocol.TypeResponse, ID: cmd.ID, Payload: payload}:
			case <-time.After(3 * time.Second):
				return
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("cmd-%d", i)
			resp, err := session.Dispatch(context.Background(), command(id))
			if err != nil {
				errs <- err
				return
			}
			var echo struct {
				Echo string `json:"echo"`
			}
			if err := json.Unmarshal(resp.Payload, &echo); err != nil || echo.Echo != id {
				errs <- fmt.Errorf("响应串号: 期望 %s，实际 %+v (err=%v)", id, echo, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestDispatchTimeout(t *testing.T) {
	conn := newFakeConn()
	session := NewSession(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := session.Dispatch(ctx, command("cmd-timeout"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("期望超时，实际: %v", err)
	}

	session.mu.Lock()
	pending := len(session.pending)
	session.mu.Unlock()
	if pending != 0 {
		t.Fatalf("超时后仍残留 %d 个等待中的命令", pending)
	}
}

func TestDispatchCancel(t *testing.T) {
	conn := newFakeConn()
	session := NewSession(conn)
	go func() { _ = session.Run() }()

	done := make(chan error, 1)
	go func() {
		_, err := session.Dispatch(context.Background(), command("cmd-cancel"))
		done <- err
	}()

	select {
	case <-conn.out: // 命令已发出
	case <-time.After(2 * time.Second):
		t.Fatal("命令未发出")
	}
	session.Cancel("cmd-cancel")

	select {
	case err := <-done:
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("期望 ErrCancelled，实际: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消未生效")
	}
}

func TestDispatchRejectsDuplicateID(t *testing.T) {
	conn := newFakeConn()
	session := NewSession(conn)

	done := make(chan struct{})
	go func() {
		_, _ = session.Dispatch(context.Background(), command("dup"))
		close(done)
	}()
	select {
	case <-conn.out:
	case <-time.After(2 * time.Second):
		t.Fatal("命令未发出")
	}

	// 第二个同 id 命令应立即报错，而不是覆盖等待者
	if _, err := session.Dispatch(context.Background(), command("dup")); err == nil {
		t.Fatal("重复 id 应被拒绝")
	}
	session.Cancel("dup")
	<-done
}

func TestRunCancelsPendingOnClose(t *testing.T) {
	conn := newFakeConn()
	session := NewSession(conn)
	runDone := make(chan error, 1)
	go func() { runDone <- session.Run() }()

	done := make(chan error, 1)
	go func() {
		_, err := session.Dispatch(context.Background(), command("cmd-close"))
		done <- err
	}()
	select {
	case <-conn.out:
	case <-time.After(2 * time.Second):
		t.Fatal("命令未发出")
	}

	_ = conn.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("连接关闭应取消等待，实际: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("连接关闭后等待未结束")
	}
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run 未返回")
	}
}
