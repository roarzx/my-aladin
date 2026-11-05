package daemon

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIdempotentPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.jsonl")

	first, err := OpenIdempotentStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Put("cmd-1", []byte(`{"accepted":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := OpenIdempotentStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()

	raw, ok := second.Get("cmd-1")
	if !ok {
		t.Fatal("重启后应能识别重复命令")
	}
	if string(raw) != `{"accepted":true}` {
		t.Fatalf("payload = %s", raw)
	}
}

func TestIdempotentExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	now := time.Now()

	first, err := OpenIdempotentStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first.now = func() time.Time { return now }
	if err := first.Put("cmd-1", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	second, err := OpenIdempotentStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	second.now = func() time.Time { return now.Add(2 * time.Hour) }

	if _, ok := second.Get("cmd-1"); ok {
		t.Fatal("过期记录不应命中")
	}
}

func TestIdempotentLastWriteWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	store, err := OpenIdempotentStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	_ = store.Put("cmd-1", []byte(`{"n":1}`))
	_ = store.Put("cmd-1", []byte(`{"n":2}`))
	raw, ok := store.Get("cmd-1")
	if !ok || string(raw) != `{"n":2}` {
		t.Fatalf("payload = %s", raw)
	}
	if store.Len() != 1 {
		t.Fatalf("Len = %d", store.Len())
	}
}
