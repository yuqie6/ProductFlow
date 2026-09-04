// Package mediaarchive 把已核验字节写成哈希稳定的临时 ZIP，供图库批量下载与交付导出共用。
//
// 调用方在事务里只冻结条目名和期望身份，事务外逐文件核验字节后再 [Writer.Add]。
// HTTP 必须先 FileAttachment 再删除 Path；CreateTemp 之后的任何失败都会删掉临时文件。
package mediaarchive

import (
	"archive/zip"
	"context"
	"errors"
	"os"
)

const defaultPattern = "mediaarchive-*.zip"

// File 是一条已经核验过的 ZIP 条目。Name 是包内路径，Data 是文件字节。
type File struct {
	Name string
	Data []byte
}

// Archive 指向已写好的临时 ZIP。Filename 给 Content-Disposition；Path 给 FileAttachment。
// 调用方（HTTP）必须在流式写出之后删除 Path。
type Archive struct {
	Path     string
	Filename string
}

// Options 控制临时文件位置与下载名。Pattern 空则用 mediaarchive-*.zip；Dir 空则用系统临时目录。
type Options struct {
	Filename string // 下载文件名，不是 ZIP 条目名
	Pattern  string // os.CreateTemp 的 pattern
	Dir      string // os.CreateTemp 的目录；测试传入 t.TempDir()
}

type tempFile interface {
	Name() string
	Write([]byte) (int, error)
	Close() error
}

var createTemp = func(dir, pattern string) (tempFile, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return f, nil
}

var errClosed = errors.New("归档写入已关闭")

// Writer 按条目顺序写入临时 ZIP。Add 一次只持有当前文件字节。
type Writer struct {
	filename string
	tmp      tempFile
	zip      *zip.Writer
	done     bool
}

// Write 按 files 的顺序写成 Deflate ZIP（mtime 1980-01-01、模式 0o600、level 9、预计算 CRC）。
// 两次写入同一组 File 得到相同 ZIP 字节。ctx 已取消、或 CreateTemp 之后失败，都会删除临时文件。
func Write(ctx context.Context, opts Options, files []File) (Archive, error) {
	w, err := Begin(ctx, opts)
	if err != nil {
		return Archive{}, err
	}
	defer w.Abort()
	for _, f := range files {
		if err := w.Add(ctx, f); err != nil {
			return Archive{}, err
		}
	}
	return w.Finish()
}

// Begin 创建临时 ZIP。ctx 已取消时不建文件。
func Begin(ctx context.Context, opts Options) (*Writer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return newWriter(opts)
}

func newWriter(opts Options) (*Writer, error) {
	pattern := opts.Pattern
	if pattern == "" {
		pattern = defaultPattern
	}
	tmp, err := createTemp(opts.Dir, pattern)
	if err != nil {
		return nil, err
	}
	return &Writer{filename: opts.Filename, tmp: tmp, zip: zip.NewWriter(tmp)}, nil
}

// Add 写入一条已核验文件。ctx 取消或写入失败会删掉临时文件。
func (w *Writer) Add(ctx context.Context, f File) error {
	if err := ctx.Err(); err != nil {
		w.Abort()
		return err
	}
	return w.add(f.Name, f.Data)
}

func (w *Writer) add(name string, data []byte) error {
	if w == nil || w.done {
		return errClosed
	}
	if err := writeZipEntry(w.zip, name, data); err != nil {
		w.Abort()
		return err
	}
	return nil
}

// Finish 关闭 ZIP 并交出路径。失败会删掉临时文件。成功后 Abort 是空操作。
func (w *Writer) Finish() (Archive, error) {
	if w == nil || w.done {
		return Archive{}, errClosed
	}
	if w.zip != nil {
		if err := w.zip.Close(); err != nil {
			w.Abort()
			return Archive{}, err
		}
		w.zip = nil
	}
	name := w.tmp.Name()
	if err := w.tmp.Close(); err != nil {
		w.tmp = nil
		w.done = true
		_ = os.Remove(name)
		return Archive{}, err
	}
	w.tmp = nil
	w.done = true
	return Archive{Path: name, Filename: w.filename}, nil
}

// Abort 删除尚未交接的临时文件。Finish 成功后调用无效果。
func (w *Writer) Abort() {
	if w == nil || w.done {
		return
	}
	w.done = true
	if w.zip != nil {
		_ = w.zip.Close()
		w.zip = nil
	}
	if w.tmp == nil {
		return
	}
	name := w.tmp.Name()
	_ = w.tmp.Close()
	w.tmp = nil
	_ = os.Remove(name)
}
