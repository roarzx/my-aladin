package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCollectArtifactsFiltersByTimeAndDirs(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "a.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := collectArtifacts(dir, time.Now().Add(-time.Minute))
	if len(files) != 1 || files[0].relPath != "index.html" {
		t.Fatalf("应只收集新文件且跳过依赖目录: %+v", files)
	}
	if files[0].contentType != "text/html; charset=utf-8" {
		t.Fatalf("contentType = %q", files[0].contentType)
	}
}

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"a.html": "text/html; charset=utf-8",
		"a.md":   "text/markdown; charset=utf-8",
		"a.png":  "image/png",
		"a.bin":  "application/octet-stream",
	}
	for name, want := range cases {
		if got := contentTypeFor(name); got != want {
			t.Errorf("%s: %q != %q", name, got, want)
		}
	}
}
