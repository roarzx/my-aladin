// Package idgen 生成 UUID v4 字符串（与 CHAR(36) 主键匹配）。
package idgen

import (
	"crypto/rand"
	"encoding/hex"
)

// New 返回一个 RFC 4122 v4 UUID 字符串。
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 在受支持平台上不会失败；失败时无法安全生成 ID。
		panic("idgen: crypto/rand 不可用: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
