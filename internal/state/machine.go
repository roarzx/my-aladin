// Package state 是 CLI 侧流式状态机（D3）：按 seq 去重、final 后丢弃。
package state

import "strings"

// Event 是一条流式更新。
type Event struct {
	Seq   uint64
	Kind  string
	Delta string
	Final bool
}

// Snapshot 是当前状态。
type Snapshot struct {
	Seq     uint64
	Kind    string
	Text    string
	Final   bool
	Dropped int
	Applied int
}

// Machine 是单个 turn 的状态机。
type Machine struct {
	lastSeq uint64
	final   bool
	kind    string
	text    strings.Builder
	dropped int
	applied int
}

// New 构造状态机。
func New() *Machine { return &Machine{} }

// ApplyUpdate 应用一条更新；乱序或终态后到达的片段返回 false。
func (m *Machine) ApplyUpdate(ev Event) bool {
	if m.final {
		m.dropped++
		return false
	}
	if ev.Seq != 0 && ev.Seq <= m.lastSeq {
		m.dropped++
		return false
	}
	if ev.Seq != 0 {
		m.lastSeq = ev.Seq
	}
	if ev.Kind != "" {
		m.kind = ev.Kind
	}
	if ev.Delta != "" {
		m.text.WriteString(ev.Delta)
	}
	m.applied++
	if ev.Final {
		m.final = true
	}
	return true
}

// Snapshot 返回当前快照。
func (m *Machine) Snapshot() Snapshot {
	return Snapshot{
		Seq:     m.lastSeq,
		Kind:    m.kind,
		Text:    m.text.String(),
		Final:   m.final,
		Dropped: m.dropped,
		Applied: m.applied,
	}
}
