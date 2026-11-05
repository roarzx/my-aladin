package machine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// HashToken 返回机器令牌的 sha256（十六进制）；数据库只存哈希。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken 生成一个 32 字节随机令牌（64 位十六进制）。
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成机器令牌: %w", err)
	}
	return hex.EncodeToString(b), nil
}
