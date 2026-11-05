//go:build integration

package redisstore_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	redisstore "github.com/roarzx/my-aladin/internal/store/redis"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestUnackedStore(t *testing.T) {
	ctx := context.Background()
	client := testkit.NewRedis(t)
	store := redisstore.NewUnackedStore(client)

	session := fmt.Sprintf("unacked-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		client.Del(ctx, "unacked:"+session, "unacked:at:"+session)
	})

	if err := store.Track(ctx, session, "m1", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := store.Track(ctx, session, "m2", []byte("two")); err != nil {
		t.Fatal(err)
	}

	entries, err := store.Due(ctx, session, time.Now().Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("待重投数量 = %d", len(entries))
	}
	payloads := map[string]string{}
	for _, e := range entries {
		payloads[e.ID] = string(e.Payload)
	}
	if payloads["m1"] != "one" || payloads["m2"] != "two" {
		t.Fatalf("payload = %+v", payloads)
	}

	if err := store.Ack(ctx, session, "m1"); err != nil {
		t.Fatal(err)
	}
	entries, err = store.Due(ctx, session, time.Now().Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "m2" {
		t.Fatalf("ack 后 = %+v", entries)
	}
}
