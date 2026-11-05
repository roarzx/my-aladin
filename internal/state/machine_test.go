package state

import "testing"

func TestApplyInOrder(t *testing.T) {
	m := New()
	if !m.ApplyUpdate(Event{Seq: 1, Kind: "agent_message_chunk", Delta: "hello "}) {
		t.Fatal("第一条应被接受")
	}
	if !m.ApplyUpdate(Event{Seq: 2, Delta: "world", Final: true}) {
		t.Fatal("第二条应被接受")
	}
	snap := m.Snapshot()
	if snap.Text != "hello world" || !snap.Final || snap.Applied != 2 || snap.Dropped != 0 {
		t.Fatalf("快照 = %+v", snap)
	}
}

func TestOutOfOrderDropped(t *testing.T) {
	m := New()
	m.ApplyUpdate(Event{Seq: 2, Delta: "b"})
	if m.ApplyUpdate(Event{Seq: 1, Delta: "a"}) {
		t.Fatal("过期片段应被丢弃")
	}
	snap := m.Snapshot()
	if snap.Text != "b" || snap.Dropped != 1 {
		t.Fatalf("快照 = %+v", snap)
	}
}

func TestFinalDiscardsEverythingAfter(t *testing.T) {
	m := New()
	m.ApplyUpdate(Event{Seq: 1, Delta: "a", Final: true})
	if m.ApplyUpdate(Event{Seq: 2, Delta: "late"}) {
		t.Fatal("final 之后的内容应被丢弃")
	}
	if m.ApplyUpdate(Event{Seq: 0, Delta: "more"}) {
		t.Fatal("final 之后的内容应被丢弃")
	}
	snap := m.Snapshot()
	if snap.Text != "a" || !snap.Final || snap.Dropped != 2 {
		t.Fatalf("快照 = %+v", snap)
	}
}
