// Package buildinfo 提供注入到二进制中的版本信息。
package buildinfo

// 这两个变量由链接器注入：
//
//	go build -ldflags "-X github.com/roarzx/my-aladin/internal/buildinfo.Version=1.2.3"
var (
	Version = "0.1.0-dev"
	Commit  = "unknown"
)

// String 返回可直接打印的版本串。
func String() string {
	return Version + " (" + Commit + ")"
}
