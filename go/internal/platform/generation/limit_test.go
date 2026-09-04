package generation

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestParseMaxConcurrent(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 3},
		{"0", 3},
		{"1", 1},
		{"7", 7},
		{"20", 20},
		{"21", 20},
		{"99", 20},
		{"x", 3},
		{"-1", 3},
		{"3.5", 3},
		{" 7", 3},
		{"07", 7},
	}
	for _, tc := range cases {
		if got := ParseMaxConcurrent(tc.raw); got != tc.want {
			t.Fatalf("ParseMaxConcurrent(%q)=%d want %d", tc.raw, got, tc.want)
		}
	}
}

func TestLoadMaxConcurrentMissingAndInvalid(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	if err := gdb.Where("key = ?", MaxConcurrentSettingKey).Delete(&schema.AppSettings{}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := LoadMaxConcurrent(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultMaxConcurrent {
		t.Fatalf("missing setting: got %d want %d", got, DefaultMaxConcurrent)
	}

	now := time.Now().UTC()
	if err := gdb.Create(&schema.AppSettings{
		Key: MaxConcurrentSettingKey, Value: "nope", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	got, err = LoadMaxConcurrent(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultMaxConcurrent {
		t.Fatalf("invalid setting: got %d want %d", got, DefaultMaxConcurrent)
	}
}

func TestLoadMaxConcurrentClamps(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := gdb.Where("key = ?", MaxConcurrentSettingKey).Delete(&schema.AppSettings{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&schema.AppSettings{
		Key: MaxConcurrentSettingKey, Value: "99", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := LoadMaxConcurrent(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	if got != MaxMaxConcurrent {
		t.Fatalf("clamp: got %d want %d", got, MaxMaxConcurrent)
	}
}
