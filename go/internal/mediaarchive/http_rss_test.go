package mediaarchive_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/rand/v2"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

func TestGalleryArchiveHTTPMemory(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_ZIP_HTTP_RSS") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_ZIP_HTTP_RSS=1 for the isolated gallery HTTP memory gate")
	}
	if runtime.GOOS != "linux" || os.Getenv("DATABASE_URL") == "" {
		t.Fatal("gallery HTTP memory gate requires Linux and DATABASE_URL")
	}
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_zip_http_%d", time.Now().UnixNano()))
	root := t.TempDir()
	archiveDir := t.TempDir()
	t.Setenv("TMPDIR", archiveDir)
	ctx := context.Background()
	const size = 1280
	rng := rand.New(rand.NewPCG(1, 2))
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(img.Pix); i += 4 {
		n := rng.Uint32()
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(n), byte(n>>8), byte(n>>16), 255
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	imageBytes := encoded.Len()
	sum := sha256.Sum256(encoded.Bytes())
	digest := hex.EncodeToString(sum[:])
	if imageBytes < 4<<20 || imageBytes*100 >= 512<<20 {
		t.Fatalf("fixture bytes=%d must exercise 400-512 MiB at 100 images", imageBytes)
	}
	if err := os.WriteFile(filepath.Join(root, "source.png"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO products(id,name,created_at,updated_at) VALUES ('zip-product','archive fixture',NOW(),NOW())`); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 100)
	for i := range ids {
		ids[i] = fmt.Sprintf("zip-asset-%03d", i)
		path := fmt.Sprintf("image-%03d.png", i)
		if err := os.Link(filepath.Join(root, "source.png"), filepath.Join(root, path)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO media_objects(id,storage_path,mime_type,byte_size,width,height,sha256,verification_status,created_at,verified_at)
		 VALUES ($1,$2,'image/png',$3,$4,$4,$5,'verified',NOW(),NOW())`, ids[i], path, imageBytes, size, digest); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO product_image_assets(id,product_id,media_object_id,origin_type,display_name,original_filename,created_at,updated_at)
		 VALUES ($1,'zip-product',$1,'upload',$2,$2,NOW(),NOW())`, ids[i], path); err != nil {
			t.Fatal(err)
		}
	}
	img = nil
	encoded = bytes.Buffer{}
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "zip-http-test"})))
	packingRequestDone := make(chan struct{})
	engine.Use(func(c *gin.Context) {
		if c.GetHeader("X-Archive-Cancel-Probe") == "1" {
			defer close(packingRequestDone)
		}
		c.Next()
	})
	store := settings.NewStore(pool, config.Config{AdminAccessRequired: true})
	auth.HTTP{AdminAccessKey: "zip-key", Store: store}.Register(engine)
	product.HTTP{Service: product.Service{DB: db, Media: media.Store{Files: storage.Local{Root: root}}}, Settings: store}.Register(engine)
	srv := httptest.NewServer(engine)
	defer srv.Close()
	client := &http.Client{Timeout: 2 * time.Minute}
	post := func(t *testing.T, payload []byte, cookies []*http.Cookie, path string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	const path = "/api/v2/products/zip-product/image-assets/download-archive"
	unauthorized := post(t, []byte(`{"asset_ids":["zip-asset-000"]}`), nil, path)
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated download status=%d", unauthorized.StatusCode)
	}
	login := post(t, []byte(`{"admin_key":"zip-key"}`), nil, "/api/auth/session")
	login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", login.StatusCode)
	}
	for _, count := range []int{10, 100} {
		t.Run(fmt.Sprintf("images_%d", count), func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"asset_ids": ids[:count]})
			if err != nil {
				t.Fatal(err)
			}
			debug.FreeOSMemory()
			baseline := galleryRSS(t)
			started := time.Now()
			resp := post(t, payload, login.Cookies(), path)
			headersAfter := time.Since(started)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/zip" {
				raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				t.Fatalf("download status=%d body=%s", resp.StatusCode, raw)
			}
			disposition, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
			if err != nil || disposition != "attachment" || params["filename"] != "archive fixture-images.zip" {
				t.Fatalf("content disposition=%s params=%v err=%v", disposition, params, err)
			}
			download, err := os.CreateTemp(root, "download-*.zip")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(download.Name())
			written, copyErr := io.Copy(download, resp.Body)
			closeErr := download.Close()
			elapsed := time.Since(started)
			if copyErr != nil || closeErr != nil {
				t.Fatalf("copy=%v close=%v", copyErr, closeErr)
			}
			var usage syscall.Rusage
			if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
				t.Fatal(err)
			}
			// The process high-water mark also includes setup and earlier requests.
			peakRSS := max(baseline, uint64(usage.Maxrss)*1024)
			extra := peakRSS - baseline
			t.Logf("ZIP_HTTP images=%d bytes_each=%d source_bytes=%d response_bytes=%d headers_after=%s total=%s rss_baseline=%d process_peak_rss=%d extra_rss_upper_bound=%d budget=%d", count, imageBytes, count*imageBytes, written, headersAfter, elapsed, baseline, peakRSS, extra, 128<<20)
			if extra > 128<<20 {
				t.Errorf("extra RSS upper bound=%d exceeds 128MiB", extra)
			}
			archive, err := zip.OpenReader(download.Name())
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			if len(archive.File) != count {
				t.Fatalf("archive entries=%d want %d", len(archive.File), count)
			}
			for i, file := range archive.File {
				reader, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256.New()
				n, err := io.Copy(hash, reader)
				reader.Close()
				if err != nil || n != int64(imageBytes) || hex.EncodeToString(hash.Sum(nil)) != digest || file.Name != fmt.Sprintf("image-%03d.png", i) {
					t.Fatalf("invalid ZIP entry %s bytes=%d err=%v", file.Name, n, err)
				}
			}
			leftovers, err := filepath.Glob(filepath.Join(archiveDir, "gallery-archive-*.zip"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary archives=%v err=%v", leftovers, err)
			}
		})
	}
	t.Run("disconnect_cleanup", func(t *testing.T) {
		payload, err := json.Marshal(map[string]any{"asset_ids": ids[:10]})
		if err != nil {
			t.Fatal(err)
		}
		resp := post(t, payload, login.Cookies(), path)
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("download status=%d", resp.StatusCode)
		}
		_, err = io.CopyN(io.Discard, resp.Body, 32<<10)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			leftovers, err := filepath.Glob(filepath.Join(archiveDir, "gallery-archive-*.zip"))
			if err != nil {
				t.Fatal(err)
			}
			if len(leftovers) == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("disconnected download left %v", leftovers)
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	t.Run("packing_cancel_cleanup", func(t *testing.T) {
		entered := make(chan struct{})
		var reads atomic.Int64
		var probeFailed atomic.Bool
		const callback = "test:archive_packing_cancel"
		if err := db.Callback().Query().After("gorm:query").Register(callback, func(q *gorm.DB) {
			if q.Statement.Table != "media_objects" || q.Error != nil {
				return
			}
			if reads.Load() == 0 {
				files, err := filepath.Glob(filepath.Join(archiveDir, "gallery-archive-*.zip"))
				if err != nil {
					probeFailed.Store(true)
					return
				}
				if len(files) == 0 {
					return
				}
			}
			if reads.Add(1) == 1 {
				close(entered)
				select {
				case <-q.Statement.Context.Done():
				case <-time.After(5 * time.Second):
					probeFailed.Store(true)
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer func() {
			select {
			case <-packingRequestDone:
			case <-time.After(6 * time.Second):
				t.Error("packing handler still active during probe cleanup")
			}
			db.Callback().Query().Remove(callback)
		}()
		payload, err := json.Marshal(map[string]any{"asset_ids": ids})
		if err != nil {
			t.Fatal(err)
		}
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, srv.URL+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Archive-Cancel-Probe", "1")
		for _, cookie := range login.Cookies() {
			req.AddCookie(cookie)
		}
		result := make(chan error, 1)
		go func() {
			resp, err := client.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			result <- err
		}()
		select {
		case <-entered:
		case <-requestCtx.Done():
			<-result
			t.Fatal("request did not reach file verification with a temporary ZIP")
		}
		started := time.Now()
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("client cancellation=%v", err)
		}
		select {
		case <-packingRequestDone:
		case <-time.After(2 * time.Second):
			t.Fatal("canceled packing handler did not finish within 2s")
		}
		files, err := filepath.Glob(filepath.Join(archiveDir, "gallery-archive-*.zip"))
		if err != nil || len(files) != 0 || reads.Load() != 1 || probeFailed.Load() {
			t.Fatalf("files=%v reads_after_begin=%d probe_failed=%v err=%v", files, reads.Load(), probeFailed.Load(), err)
		}
		t.Logf("ZIP_CANCEL selected=100 media_reads_after_begin=%d handler_and_cleanup_after=%s", reads.Load(), time.Since(started))
	})
	t.Run("limits", func(t *testing.T) {
		payload, err := json.Marshal(map[string]any{"asset_ids": append(append([]string{}, ids...), "extra")})
		if err != nil {
			t.Fatal(err)
		}
		resp := post(t, payload, login.Cookies(), path)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("101 assets status=%d", resp.StatusCode)
		}
		if _, err := pool.Exec(ctx, `UPDATE media_objects SET byte_size=$2 WHERE id=$1`, ids[0], (512<<20)+1); err != nil {
			t.Fatal(err)
		}
		resp = post(t, []byte(`{"asset_ids":["zip-asset-000"]}`), login.Cookies(), path)
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusBadRequest || !bytes.Contains(raw, []byte("512 MiB")) {
			t.Fatalf("oversized archive status=%d body=%s err=%v", resp.StatusCode, raw, err)
		}
		leftovers, err := filepath.Glob(filepath.Join(archiveDir, "gallery-archive-*.zip"))
		if err != nil || len(leftovers) != 0 {
			t.Fatalf("rejected archive leftovers=%v err=%v", leftovers, err)
		}
	})
}

func galleryRSS(t *testing.T) uint64 {
	t.Helper()
	file, err := os.Open("/proc/self/statm")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var total, resident uint64
	if _, err := fmt.Fscan(file, &total, &resident); err != nil {
		t.Fatal(err)
	}
	return resident * uint64(os.Getpagesize())
}
