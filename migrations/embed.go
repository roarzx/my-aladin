// Package migrations 以嵌入文件的方式提供 SQL 迁移。
//
// 新增迁移：放置 `NNNN_name.up.sql` 与 `NNNN_name.down.sql`，编号递增。
// 已发布的迁移不得修改，只能追加新文件（见 dev_spec 9.3）。
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

//go:embed *.sql
var embedded embed.FS

// Migration 是一个版本的 up/down 脚本。
type Migration struct {
	Version int64
	Name    string
	Up      string
	Down    string
}

var filePattern = regexp.MustCompile(`^(\d+)_([A-Za-z0-9_]+)\.(up|down)\.sql$`)

// All 返回按版本升序排列的迁移列表。
func All() ([]Migration, error) {
	entries, err := fs.ReadDir(embedded, ".")
	if err != nil {
		return nil, err
	}

	byVersion := make(map[int64]*Migration)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := filePattern.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		version, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("解析迁移编号 %q: %w", entry.Name(), err)
		}
		content, err := embedded.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}
		mi, ok := byVersion[version]
		if !ok {
			mi = &Migration{Version: version, Name: m[2]}
			byVersion[version] = mi
		}
		switch m[3] {
		case "up":
			mi.Up = string(content)
		case "down":
			mi.Down = string(content)
		}
	}

	list := make([]Migration, 0, len(byVersion))
	for _, mi := range byVersion {
		if mi.Up == "" {
			return nil, fmt.Errorf("迁移 %04d_%s 缺少 up 脚本", mi.Version, mi.Name)
		}
		list = append(list, *mi)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Version < list[j].Version })
	return list, nil
}

// LatestVersion 返回最新迁移版本；读取失败时返回 0（由调用方决定如何处理）。
func LatestVersion() int64 {
	all, err := All()
	if err != nil || len(all) == 0 {
		return 0
	}
	return all[len(all)-1].Version
}
