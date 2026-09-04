package mediaarchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"

	"github.com/yuqie6/productflow/internal/media"
)

const (
	zipRSSFileCount   = 100
	zipRSSExtraBudget = 64 * 1024 * 1024
)

// TestStreamingZipNearLimitImagesKeepsExtraRSSUnderBudget 是 opt-in 内存闸门：
// 100 张 GenerationMaxImageBytes 近上限图走 Begin/Add/Finish，额外 MaxRSS 与 HeapInuse ≤ 64MiB，
// 且两次写出的 ZIP 字节哈希相同。图库 HTTP 仍有 512MiB 总字节上限，本闸门测共享写入器，不走 BuildGalleryArchive。
func TestStreamingZipNearLimitImagesKeepsExtraRSSUnderBudget(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_ZIP_RSS") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_ZIP_RSS=1 to run the 100-image ZIP MaxRSS gate")
	}
	dir := t.TempDir()
	opts := Options{Filename: "near-limit.zip", Pattern: "zip-rss-*.zip", Dir: dir}

	runtime.GC()
	heapBefore := heapInuse()
	rssBefore := maxRSSBytes(t)

	firstHash, firstPath := writeNearLimitZip(t, opts)
	t.Cleanup(func() { _ = os.Remove(firstPath) })
	secondHash, secondPath := writeNearLimitZip(t, opts)
	t.Cleanup(func() { _ = os.Remove(secondPath) })
	if firstHash != secondHash {
		t.Fatalf("zip hash drifted %s vs %s", firstHash, secondHash)
	}

	runtime.GC()
	heapAfter := heapInuse()
	rssAfter := maxRSSBytes(t)
	extraHeap := int64(heapAfter) - int64(heapBefore)
	extraRSS := int64(rssAfter) - int64(rssBefore)
	if extraHeap < 0 {
		extraHeap = 0
	}
	if extraRSS < 0 {
		extraRSS = 0
	}
	t.Logf("zip sha256=%s extra_heap=%d extra_rss=%d budget=%d files=%d bytes_each=%d",
		firstHash, extraHeap, extraRSS, zipRSSExtraBudget, zipRSSFileCount, media.GenerationMaxImageBytes)
	if extraHeap > zipRSSExtraBudget {
		t.Fatalf("extra HeapInuse %d exceeds %d", extraHeap, zipRSSExtraBudget)
	}
	if extraRSS > zipRSSExtraBudget {
		t.Fatalf("extra MaxRSS %d exceeds %d", extraRSS, zipRSSExtraBudget)
	}
}

func writeNearLimitZip(t *testing.T, opts Options) (string, string) {
	t.Helper()
	w, err := Begin(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Abort()
	payload := nearLimitPNG()
	for i := 0; i < zipRSSFileCount; i++ {
		if err := w.Add(context.Background(), File{
			Name: fmt.Sprintf("%03d.png", i),
			Data: payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := w.Finish()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), out.Path
}

func nearLimitPNG() []byte {
	buf := bytes.Repeat([]byte{0}, media.GenerationMaxImageBytes)
	copy(buf, []byte("\x89PNG\r\n\x1a\n"))
	return buf
}

func heapInuse() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

func maxRSSBytes(t *testing.T) uint64 {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	rss := uint64(ru.Maxrss)
	if runtime.GOOS == "linux" {
		return rss * 1024
	}
	return rss
}
