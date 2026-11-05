//go:build realacp

package acp

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestRealAdapterValidate 用真实 claude-agent-acp 做一次 initialize 探活（不调用模型）。
//
//	ALADDIN_ACP_BIN=$HOME/.hercules/agents/claude-agent-acp/node_modules/.bin/claude-agent-acp \
//	ALADDIN_ACP_VERSION=0.94.0 \
//	go test -tags=realacp ./internal/transport/acp -run TestRealAdapterValidate -v
func TestRealAdapterValidate(t *testing.T) {
	bin := os.Getenv("ALADDIN_ACP_BIN")
	if bin == "" {
		t.Skip("未设置 ALADDIN_ACP_BIN")
	}
	p := NewProvider(Config{
		Bin:               bin,
		ExpectedVersion:   os.Getenv("ALADDIN_ACP_VERSION"),
		Cwd:               t.TempDir(),
		InitializeTimeout: 10 * time.Second,
	}, DefaultSpawner{}, nil)

	if err := p.Validate(context.Background()); err != nil {
		t.Fatalf("真实适配器探活失败: %v", err)
	}
}
