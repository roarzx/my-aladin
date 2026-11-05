// Package fakeruntime 是 Runtime 的可编排假实现（B5）。
//
// 它按脚本回放归一化事件，不依赖真实 Provider；故障注入见 ScriptStep。
package fakeruntime

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Event 是归一化事件的最小形态（9.7.1）。
type Event struct {
	Kind    string `yaml:"kind" json:"kind"`
	Delta   string `yaml:"delta,omitempty" json:"delta,omitempty"`
	DelayMS int    `yaml:"delay_ms,omitempty" json:"delay_ms,omitempty"`
}

// ScriptStep 描述一次执行的行为。
type ScriptStep struct {
	ExecutionID  string  `yaml:"execution_id"`
	Result       string  `yaml:"result"` // completed | failed
	ErrorClass   string  `yaml:"error_class"`
	FailDispatch bool    `yaml:"fail_dispatch"`
	Hang         bool    `yaml:"hang"`
	Events       []Event `yaml:"events"`
}

// Script 是回放脚本。
type Script struct {
	Steps []ScriptStep `yaml:"steps"`
}

// Load 从 YAML 文件读取脚本。
func Load(path string) (*Script, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Script
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// delay 便于测试覆盖，默认按 DelayMS 等待。
var delay = func(ms int) {
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}
