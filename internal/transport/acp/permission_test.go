package acp

import (
	"encoding/json"
	"testing"
)

func TestPermissionAutoApprove(t *testing.T) {
	params := json.RawMessage(`{"options":[
		{"optionId":"reject","kind":"reject_once"},
		{"optionId":"allow-once","kind":"allow_once"},
		{"optionId":"allow-with-updates","kind":"allow_always"}
	]}`)

	enabled := &Session{provider: &Provider{cfg: Config{AutoApprovePermissions: true}}}
	raw, rpcErr := enabled.handleRequest("session/request_permission", params)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	var response struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.Outcome.Outcome != "selected" || response.Outcome.OptionID != "allow-once" {
		t.Fatalf("response = %s", raw)
	}

	disabled := &Session{provider: &Provider{cfg: Config{}}}
	raw, _ = disabled.handleRequest("session/request_permission", params)
	if string(raw) != `{"outcome":{"outcome":"cancelled"}}` {
		t.Fatalf("disabled response = %s", raw)
	}
}
