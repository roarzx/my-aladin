package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/testkit/fakeserver"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

func TestResolveProfilePersistsMachineID(t *testing.T) {
	base := t.TempDir()

	p1, err := ResolveProfile(base, "dev", "")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if p1.Dir != filepath.Join(base, "dev") {
		t.Fatalf("Dir = %q", p1.Dir)
	}
	if p1.MachineID == "" {
		t.Fatal("应生成 machine_id")
	}
	if _, err := os.Stat(filepath.Join(p1.Dir, "machine_id")); err != nil {
		t.Fatalf("machine_id 未落盘: %v", err)
	}

	p2, err := ResolveProfile(base, "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	if p2.MachineID != p1.MachineID {
		t.Fatalf("重启后 machine_id 变化: %q -> %q", p1.MachineID, p2.MachineID)
	}

	other, err := ResolveProfile(base, "other", "")
	if err != nil {
		t.Fatal(err)
	}
	if other.MachineID == p1.MachineID {
		t.Fatal("不同 profile 应使用不同 machine_id")
	}
}

func TestResolveProfileExplicitID(t *testing.T) {
	p, err := ResolveProfile(t.TempDir(), "dev", "fixed-machine")
	if err != nil {
		t.Fatal(err)
	}
	if p.MachineID != "fixed-machine" {
		t.Fatalf("MachineID = %q", p.MachineID)
	}
}

func TestResolveProfileRejectsBadName(t *testing.T) {
	for _, name := range []string{"", "../escape", "a/b"} {
		if _, err := ResolveProfile(t.TempDir(), name, ""); err == nil {
			t.Fatalf("名称 %q 应被拒绝", name)
		}
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	b := Backoff{Base: 10 * time.Millisecond, Max: 40 * time.Millisecond}
	if d := b.Wait(0); d <= 0 || d > 11*time.Millisecond {
		t.Fatalf("首次等待 = %v", d)
	}
	if d := b.Wait(10); d > 44*time.Millisecond {
		t.Fatalf("等待未封顶: %v", d)
	}
}

func testClient(url, token string, hb time.Duration, handler CommandHandler) *Client {
	return NewClient(ClientOptions{
		ACPURL:            url,
		MachineID:         "machine-1",
		Token:             token,
		CLIVersion:        "test",
		HeartbeatInterval: hb,
		HeartbeatTimeout:  2 * time.Second,
		HandshakeTimeout:  time.Second,
		Backoff:           Backoff{Base: 10 * time.Millisecond, Max: 50 * time.Millisecond},
		OnCommand:         handler,
	})
}

func TestClientHandshakeAndHeartbeat(t *testing.T) {
	srv := fakeserver.New(t, fakeserver.Options{Token: "t"})
	client := testClient(srv.URL(), "t", 20*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Run(ctx) }()

	srv.WaitConnected(t, 2*time.Second)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case env := <-srv.Received():
			if env.Type == protocol.TypeHeartbeat {
				return
			}
		case <-deadline:
			t.Fatal("未收到心跳")
		}
	}
}

func TestClientReconnects(t *testing.T) {
	srv := fakeserver.New(t, fakeserver.Options{Token: "t"})
	client := testClient(srv.URL(), "t", time.Hour, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Run(ctx) }()

	srv.WaitConnected(t, 2*time.Second)
	srv.Drop()
	srv.WaitConnected(t, 3*time.Second)
}

func TestClientStopsWhenRejected(t *testing.T) {
	srv := fakeserver.New(t, fakeserver.Options{Reject: true})
	client := testClient(srv.URL(), "t", time.Hour, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.Run(ctx)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("期望 ErrRejected，实际: %v", err)
	}
}

func TestClientRespondsToCommand(t *testing.T) {
	handler := func(_ context.Context, env protocol.Envelope) (protocol.ResponsePayload, error) {
		data, _ := json.Marshal(map[string]string{"echo": env.ID})
		return protocol.ResponsePayload{Accepted: true, Data: data}, nil
	}
	srv := fakeserver.New(t, fakeserver.Options{Token: "t"})
	client := testClient(srv.URL(), "t", time.Hour, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Run(ctx) }()
	srv.WaitConnected(t, 2*time.Second)

	payload, _ := json.Marshal(protocol.CommandPayload{Method: "ping"})
	if err := srv.Send(protocol.Envelope{
		Type: protocol.TypeCommand, ID: "cmd-1", Payload: payload,
	}); err != nil {
		t.Fatalf("发送命令: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case env := <-srv.Received():
			if env.Type != protocol.TypeResponse || env.ID != "cmd-1" {
				continue
			}
			var resp protocol.ResponsePayload
			if err := json.Unmarshal(env.Payload, &resp); err != nil {
				t.Fatal(err)
			}
			if !resp.Accepted {
				t.Fatalf("响应未接受: %+v", resp)
			}
			var echo map[string]string
			_ = json.Unmarshal(resp.Data, &echo)
			if echo["echo"] != "cmd-1" {
				t.Fatalf("响应内容 = %+v", echo)
			}
			return
		case <-deadline:
			t.Fatal("未收到响应")
		}
	}
}
