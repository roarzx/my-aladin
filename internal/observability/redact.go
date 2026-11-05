package observability

import "github.com/roarzx/my-aladin/pkg/protocol"

// Redact 统一脱敏入口（H3）；实现在 protocol 包，编码事件时也复用同一规则。
func Redact(text string) string { return protocol.Redact(text) }

// RedactJSON 递归脱敏 JSON 敏感字段。
func RedactJSON(raw []byte) []byte { return protocol.RedactJSON(raw) }
