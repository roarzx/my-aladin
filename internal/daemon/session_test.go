package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session_history.json")

	store, err := OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := SessionRecord{
		SessionID:    "s1",
		ACPSessionID: "acp-1",
		AgentID:      "agent-1",
		Kind:         "planning",
		Fingerprint:  "fp",
	}
	if err := store.Put(rec); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get("s1")
	if !ok {
		t.Fatal("重启后映射应可读回")
	}
	if got.ACPSessionID != "acp-1" || got.AgentID != "agent-1" || got.Fingerprint != "fp" {
		t.Fatalf("记录 = %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt 应被补齐")
	}

	if err := reopened.Delete("s1"); err != nil {
		t.Fatal(err)
	}
	again, err := OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Get("s1"); ok {
		t.Fatal("删除后不应命中")
	}
}

func TestSessionStoreCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session_history.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSessionStore(path); err == nil {
		t.Fatal("损坏的历史文件应报错，而不是静默重置")
	}
}

func TestSessionStoreAtomicReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session_history.json")
	store, err := OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(SessionRecord{SessionID: "seed", ACPSessionID: "acp"}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = store.Put(SessionRecord{
				SessionID:    "s-" + strings.Repeat("x", i%5),
				ACPSessionID: "acp",
			})
		}
	}()

	for i := 0; i < 200; i++ {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("读取历史: %v", err)
		}
		var file sessionFile
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("读到半写内容: %v\n%s", err, data)
		}
		time.Sleep(time.Millisecond)
	}
	wg.Wait()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("残留临时文件: %s", entry.Name())
		}
	}
}
