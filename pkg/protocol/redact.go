package protocol

import (
	"encoding/json"
	"regexp"
	"strings"
)

// 脱敏规则：凭据、token、authorization、密码与环境变量值。
var (
	bearerPattern     = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]{6,}`)
	jsonSecretPattern = regexp.MustCompile(`(?i)"(token|secret|password|passwd|authorization|api[_-]?key|access[_-]?token|refresh[_-]?token)"\s*:\s*"[^"]*"`)
	kvSecretPattern   = regexp.MustCompile(`(?i)\b([A-Za-z0-9_]*(?:token|secret|password|passwd|authorization|api[_-]?key)[A-Za-z0-9_]*)\s*[:=]\s*("?[^\s",;]+"?)`)
)

// Redact 移除字符串中的明文凭据。
func Redact(text string) string {
	if text == "" {
		return text
	}
	out := bearerPattern.ReplaceAllString(text, "$1 ***")
	out = jsonSecretPattern.ReplaceAllString(out, `"$1":"***"`)
	out = kvSecretPattern.ReplaceAllString(out, "$1=***")
	return out
}

var sensitiveMarkers = []string{
	"token", "secret", "password", "passwd", "authorization", "api_key", "apikey",
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range sensitiveMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// RedactJSON 递归脱敏 JSON 对象中的敏感字段；解析失败时原样返回。
func RedactJSON(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	cleaned := redactValue(value)
	out, err := json.Marshal(cleaned)
	if err != nil {
		return raw
	}
	return out
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if isSensitiveKey(key) {
				typed[key] = "***"
				continue
			}
			typed[key] = redactValue(item)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = redactValue(item)
		}
		return typed
	case string:
		return Redact(typed)
	default:
		return value
	}
}
