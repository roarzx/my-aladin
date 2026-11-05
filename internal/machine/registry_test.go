package machine

import (
	"encoding/json"
	"testing"
)

func TestCapabilitiesRoundTrip(t *testing.T) {
	caps := []Capability{CapAttachments, CapGit}
	raw, err := EncodeCapabilities(caps)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `["attachments","git"]` {
		t.Fatalf("编码 = %s", raw)
	}
	got, err := ParseCapabilities(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != CapAttachments || got[1] != CapGit {
		t.Fatalf("解析 = %+v", got)
	}

	empty, err := EncodeCapabilities(nil)
	if err != nil || string(empty) != `[]` {
		t.Fatalf("空能力编码 = %s, err = %v", empty, err)
	}
}

func TestSupports(t *testing.T) {
	m := Machine{Capabilities: []Capability{CapAttachments}}
	if !m.Supports(CapAttachments) {
		t.Error("应支持 attachments")
	}
	if m.Supports(CapFlowNode) {
		t.Error("未声明的能力不应支持")
	}
}

func TestParseCapabilitiesRejectsBadJSON(t *testing.T) {
	if _, err := ParseCapabilities(json.RawMessage(`{"a":1}`)); err == nil {
		t.Fatal("非数组应报错")
	}
	if caps, err := ParseCapabilities(nil); err != nil || caps != nil {
		t.Fatalf("空输入 = %+v, err = %v", caps, err)
	}
}
