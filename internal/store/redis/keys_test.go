package redisstore

import "testing"

func TestKeys(t *testing.T) {
	k := Keys{}
	cases := map[string]string{
		k.Plan("t1"):         "plan:t1",
		k.TaskLock("t1"):     "lock:task:t1",
		k.Lease("a1"):        "lease:attempt:a1",
		k.RecoveryDirty():    "recovery:dirty",
		k.Backpressure("s1"): "bp:s1",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("键名 = %q, 期望 %q", got, want)
		}
	}
}
