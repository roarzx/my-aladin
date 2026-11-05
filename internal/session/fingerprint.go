package session

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Fingerprint 计算会话恢复指纹：MCP 配置、环境、初始 prompt 三项的规范化摘要。
//
// 任一输入变化都必须改变指纹；环境变量按排序后参与，避免顺序抖动导致误判（E3）。
func Fingerprint(mcpCfg string, env []string, prompt string) string {
	sorted := append([]string(nil), env...)
	sort.Strings(sorted)

	h := sha256.New()
	h.Write([]byte("mcp\x00"))
	h.Write([]byte(strings.TrimSpace(mcpCfg)))
	h.Write([]byte("\x00env\x00"))
	for _, item := range sorted {
		h.Write([]byte(item))
		h.Write([]byte("\x00"))
	}
	h.Write([]byte("prompt\x00"))
	h.Write([]byte(prompt))
	return hex.EncodeToString(h.Sum(nil))
}

// ResumableSession 是可判断能否复用的 Provider 会话。
type ResumableSession interface {
	SessionFingerprint() string
}

// CanResume 仅当三项指纹完全相等（即指纹相同）且已有记录时才允许复用。
func CanResume(session ResumableSession, fingerprint string) bool {
	if session == nil || fingerprint == "" {
		return false
	}
	return session.SessionFingerprint() == fingerprint
}
