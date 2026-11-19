package protocol

import (
	"encoding/json"
	"testing"
)

// 回归：计数类字段（名字里带 token）是数字，不能被脱敏成字符串，
// 否则 step_done 等事件会解析失败（曾导致任务被误判租约过期）。
func TestRedactJSONKeepsNumericUsage(t *testing.T) {
	raw := []byte(`{"input_tokens":123,"output_tokens":45,"token":"secret-value","nested":{"api_key":"abc"}}`)
	cleaned := RedactJSON(raw)

	var value map[string]any
	if err := json.Unmarshal(cleaned, &value); err != nil {
		t.Fatalf("脱敏结果不是合法 JSON: %v (%s)", err, cleaned)
	}
	if value["input_tokens"] != float64(123) {
		t.Fatalf("input_tokens 被破坏: %#v", value["input_tokens"])
	}
	if value["output_tokens"] != float64(45) {
		t.Fatalf("output_tokens 被破坏: %#v", value["output_tokens"])
	}
	if value["token"] != "***" {
		t.Fatalf("字符串凭据应被脱敏: %#v", value["token"])
	}
	nested, ok := value["nested"].(map[string]any)
	if !ok || nested["api_key"] != "***" {
		t.Fatalf("嵌套凭据应被脱敏: %#v", value["nested"])
	}
}

func TestEncodeEventKeepsUsageNumbers(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"kind": "step_done",
		"data": map[string]any{"input_tokens": 10, "output_tokens": 20},
	})
	data, err := Encode(Envelope{ID: "evt-1", Type: TypeEvent, ExecutionID: "exec-1", ExecutionAttempt: func() *uint32 { v := uint32(1); return &v }(), Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Payload struct {
			Data struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"data"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env.Payload.Data.InputTokens != 10 || env.Payload.Data.OutputTokens != 20 {
		t.Fatalf("Encode 破坏了用量字段: %+v", env.Payload.Data)
	}
}
