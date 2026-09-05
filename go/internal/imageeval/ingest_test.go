package imageeval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIngestListingAdmitsCompleteFixture(t *testing.T) {
	png := solidPNG(t, 800, 800)
	referencePNG := solidPNG(t, 801, 800)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		if strings.HasPrefix(r.URL.Path, "/sku/") {
			_, _ = w.Write(referencePNG)
			return
		}
		_, _ = w.Write(png)
	}))
	t.Cleanup(srv.Close)

	img := func(role string, n int) []ExtractImage {
		out := make([]ExtractImage, n)
		for i := 0; i < n; i++ {
			item := ExtractImage{URL: srv.URL + "/" + role + "/" + itoa(i), Role: role, Index: i}
			switch {
			case role == "gallery" && i == 0:
				item.SuggestedType = "hero"
			case role == "gallery" && i == 1:
				item.SuggestedType = "scene"
			case role == "gallery":
				item.SuggestedType = "detail"
			case role == "sku":
				item.SuggestedType = "sku"
			default:
				item.SuggestedType = "selling_point"
			}
			out[i] = item
		}
		return out
	}
	listing := ExtractListing{
		Source:     "taobao",
		URL:        "https://item.taobao.com/item.htm?id=ingest1",
		Title:      "陶瓷马克杯 旗舰店",
		Category:   "home",
		Query:      "陶瓷马克杯",
		IsTmall:    true,
		Gallery:    img("gallery", 5),
		SKUImages:  img("sku", 1),
		DetailImgs: img("detail", 8),
	}
	root := t.TempDir()
	m, err := IngestListing(context.Background(), root, listing, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Gold) == 0 || len(m.References) == 0 || len(m.ImageTypes) < MinDistinctTypes {
		t.Fatalf("%+v", m)
	}
	loaded, err := LoadAdmitted(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != m.ID {
		t.Fatalf("%+v", loaded)
	}
}

func TestIngestListingRejectsVirtual(t *testing.T) {
	listing := ExtractListing{
		Source:   "taobao",
		URL:      "https://item.taobao.com/item.htm?id=virtual1",
		Title:    "官方续费账号 CDK",
		Category: "3c",
	}
	_, err := IngestListing(context.Background(), t.TempDir(), listing, nil)
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestDownloadOnePNG(t *testing.T) {
	png := solidPNG(t, 800, 800)
	t.Logf("png bytes=%d", len(png))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	t.Cleanup(srv.Close)
	root := t.TempDir()
	item := ExtractImage{URL: srv.URL + "/shot.png", Role: "gallery", Index: 0, SuggestedType: "hero"}
	got, err := downloadOne(context.Background(), srv.Client(), "https://item.taobao.com/item.htm?id=1", root, "gallery", 0, item)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 800 || got.ByteSize < MinImageBytes {
		t.Fatalf("%+v bytes=%d", got, got.ByteSize)
	}
}
