package product

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestWriteKeepsFilesOnlyAfterCommit(t *testing.T) {
	_, gdb := testdb.Open(t)
	root := t.TempDir()
	svc := Service{DB: gdb, Media: media.Store{Files: storage.Local{Root: root}}}
	ctx := context.Background()
	pngBytes := tinyPNG(t)

	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		created, err := insertProduct(ctx, pgxTx, "生成图商品", nil, nil, nil)
		if err != nil {
			return err
		}
		if _, err := svc.Write(ctx, pgxTx, graph.GeneratedImageInput{
			ProductID: created.ID,
			Title:     "主图",
			Filename:  "hero.png",
			Bytes:     pngBytes,
			MIME:      "image/png",
		}); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	if n := countFiles(t, root); n != 0 {
		t.Fatalf("rolled back files still present: %d", n)
	}

	var assetID string
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		created, err := insertProduct(ctx, pgxTx, "提交生成图商品", nil, nil, nil)
		if err != nil {
			return err
		}
		id, err := svc.Write(ctx, pgxTx, graph.GeneratedImageInput{
			ProductID: created.ID,
			Title:     "主图",
			Filename:  "hero.png",
			Bytes:     pngBytes,
			MIME:      "image/png",
		})
		assetID = id
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if assetID == "" {
		t.Fatal("missing asset")
	}
	if n := countFiles(t, root); n == 0 {
		t.Fatal("committed write left no files")
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
