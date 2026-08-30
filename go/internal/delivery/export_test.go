package delivery

import (
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestGetPresetMissingIsNotFound(t *testing.T) {
	_, err := GetPreset("not-a-real-preset")
	if err == nil || !apperr.IsNotFound(err) {
		t.Fatalf("want 404, got %v", err)
	}
	if err.(apperr.Error).Detail != "交付预设不存在" {
		t.Fatalf("detail %v", err)
	}
}

func TestExportImageFilenameMatchesPythonPattern(t *testing.T) {
	got := exportImageFilename("mug", "hero", 1, 1200, 1600, ".png")
	if got != "mug-hero-01-1200x1600.png" {
		t.Fatalf("got %s", got)
	}
}

func TestExportArchiveNameSuffix(t *testing.T) {
	if safeName("云白瓷", "product")+"-delivery-export.zip" == "product-delivery.zip" {
		t.Fatal("archive name must keep delivery-export suffix")
	}
	name := safeName("Mug", "product") + "-delivery-export.zip"
	if name != "Mug-delivery-export.zip" {
		t.Fatalf("name %s", name)
	}
}
