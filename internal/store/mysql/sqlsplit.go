package mysql

import "strings"

// splitSQL 按分号切分 SQL 脚本，但忽略字符串、反引号标识符与注释里的分号。
// 迁移脚本均为 DDL，不支持 DELIMITER（存储过程）语法。
func splitSQL(src string) []string {
	var (
		out []string
		sb  strings.Builder
	)
	flush := func() {
		if stmt := strings.TrimSpace(sb.String()); stmt != "" && !isOnlyComments(stmt) {
			out = append(out, stmt)
		}
		sb.Reset()
	}

	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			// 行注释：保留到行尾，交给 MySQL 处理
			if j := strings.IndexByte(src[i:], '\n'); j >= 0 {
				sb.WriteString(src[i : i+j])
				i += j
			} else {
				sb.WriteString(src[i:])
				i = len(src)
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				sb.WriteString(src[i:])
				i = len(src)
			} else {
				sb.WriteString(src[i : i+2+end+2])
				i += 2 + end + 2
			}
		case c == '\'' || c == '"' || c == '`':
			quote := c
			sb.WriteByte(c)
			i++
			for i < len(src) {
				sb.WriteByte(src[i])
				if src[i] == '\\' && quote != '`' && i+1 < len(src) {
					sb.WriteByte(src[i+1])
					i += 2
					continue
				}
				if src[i] == quote {
					i++
					break
				}
				i++
			}
		case c == ';':
			flush()
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

// isOnlyComments 判断一段文本是否只包含空行与行注释。
func isOnlyComments(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		return false
	}
	return true
}
