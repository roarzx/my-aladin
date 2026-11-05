package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic 先写同目录临时文件并同步，再 rename 覆盖目标。
//
// 读者要么看到旧文件，要么看到完整新文件，不会读到半写内容（C4 验收）。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	name := tmp.Name()
	remove := func() { _ = os.Remove(name) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		remove()
		return fmt.Errorf("写入临时文件: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		remove()
		return fmt.Errorf("同步临时文件: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		remove()
		return fmt.Errorf("设置权限: %w", err)
	}
	if err := tmp.Close(); err != nil {
		remove()
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		remove()
		return fmt.Errorf("替换 %s: %w", path, err)
	}
	return nil
}
