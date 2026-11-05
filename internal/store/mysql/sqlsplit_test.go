package mysql

import (
	"strings"
	"testing"
)

func TestSplitSQL(t *testing.T) {
	script := `
-- 注释里的分号不应切分: a;b
CREATE TABLE a (id INT); -- 行尾注释
INSERT INTO a VALUES ('x;y'), ("z;w"), (` + "`t;u`" + `);
/* 块注释里的分号 ; 也不切分 */
CREATE TABLE b (
  id INT
)
`
	stmts := splitSQL(script)
	if len(stmts) != 3 {
		t.Fatalf("期望 3 条语句，实际 %d: %#v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "CREATE TABLE a") {
		t.Errorf("第 1 条语句不对: %q", stmts[0])
	}
	if !strings.Contains(stmts[1], "'x;y'") || !strings.Contains(stmts[1], "`t;u`") {
		t.Errorf("字符串内的分号被切断了: %q", stmts[1])
	}
	if !strings.Contains(stmts[2], "CREATE TABLE b") {
		t.Errorf("第 3 条语句不对: %q", stmts[2])
	}
}

func TestSplitSQLCommentOnly(t *testing.T) {
	if got := splitSQL("-- a\n\n-- b;\n"); len(got) != 0 {
		t.Fatalf("纯注释脚本不应产生语句: %#v", got)
	}
	if got := splitSQL(""); len(got) != 0 {
		t.Fatalf("空脚本不应产生语句: %#v", got)
	}
}

func TestSplitSQLEscapedQuote(t *testing.T) {
	stmts := splitSQL(`INSERT INTO t VALUES ('it''s;ok');`)
	if len(stmts) != 1 {
		t.Fatalf("期望 1 条语句，实际 %d: %#v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "'it''s;ok'") {
		t.Errorf("语句被切断了: %q", stmts[0])
	}
}
