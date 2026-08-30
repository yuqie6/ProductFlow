package testdb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/config"
)

func TestPoolUsesIsolatedDatabase(t *testing.T) {
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	liveName, err := databaseName(config.NormalizePostgresURL(raw))
	if err != nil {
		t.Fatal(err)
	}
	pool := Pool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var current string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&current); err != nil {
		t.Fatal(err)
	}
	want := isolatedName(liveName)
	if current != want {
		t.Fatalf("current_database=%s want %s", current, want)
	}
	if !strings.HasSuffix(liveName, testDBSuffix) && current == liveName {
		t.Fatalf("testdb connected to live database %s", liveName)
	}
}
