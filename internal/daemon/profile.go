package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/roarzx/my-aladin/internal/idgen"
)

// Profile 是 daemon 的本地状态目录。
type Profile struct {
	Name      string
	Dir       string
	MachineID string
}

// ResolveProfile 解析并创建 profile 目录，解析 machine ID（参数 → 环境变量 → 文件 → 生成）。
func ResolveProfile(baseDir, name, machineID string) (*Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("profile: 名称不能为空")
	}
	if strings.ContainsAny(name, `/\`) {
		return nil, fmt.Errorf("profile: 名称不能包含路径分隔符: %q", name)
	}

	dir := filepath.Join(expandHome(baseDir), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 profile 目录: %w", err)
	}

	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		machineID = strings.TrimSpace(os.Getenv("ALADDIN_MACHINE_ID"))
	}
	if machineID == "" {
		data, err := os.ReadFile(filepath.Join(dir, "machine_id"))
		switch {
		case err == nil:
			machineID = strings.TrimSpace(string(data))
		case errors.Is(err, os.ErrNotExist):
		default:
			return nil, fmt.Errorf("读取 machine_id: %w", err)
		}
	}
	if machineID == "" {
		machineID = idgen.New()
		if err := os.WriteFile(filepath.Join(dir, "machine_id"), []byte(machineID+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("写入 machine_id: %w", err)
		}
	}

	return &Profile{Name: name, Dir: dir, MachineID: machineID}, nil
}

func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
