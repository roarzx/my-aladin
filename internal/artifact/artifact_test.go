//go:build integration

package artifact_test

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/roarzx/my-aladin/internal/artifact"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestSaveDedupesAndLists(t *testing.T) {
	db := testkit.NewDBRaw(t)
	ctx := context.Background()
	svc := &artifact.Service{DB: db, Root: t.TempDir()}
	taskID := idgen.New()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM artifacts WHERE task_id = ?`, taskID)
	})

	first, err := svc.Save(ctx, taskID, "", "index.html", "text/html", []byte("<html>hi</html>"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Save(ctx, taskID, "", "index.html", "text/html", []byte("<html>hi</html>"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("同样内容重复上传应幂等: %s != %s", first.ID, second.ID)
	}
	if first.SizeBytes != 15 {
		t.Fatalf("size = %d", first.SizeBytes)
	}

	items, err := svc.List(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != "index.html" {
		t.Fatalf("列表异常: %+v", items)
	}

	meta, file, err := svc.Open(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	data, _ := io.ReadAll(file)
	if string(data) != "<html>hi</html>" || meta.ContentType != "text/html" {
		t.Fatalf("读取异常: %q %+v", data, meta)
	}
}

func TestSaveWritesUnderRoot(t *testing.T) {
	db := testkit.NewDBRaw(t)
	root := t.TempDir()
	svc := &artifact.Service{DB: db, Root: root}
	taskID := idgen.New()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM artifacts WHERE task_id = ?`, taskID)
	})

	item, err := svc.Save(ctx(), taskID, "", "a/b/run.json", "application/json", []byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	_, path, err := svc.Get(ctx(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("产物应落盘: %v (%s)", err, path)
	}
}

func ctx() context.Context { return context.Background() }
