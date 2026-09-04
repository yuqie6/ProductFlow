// Command productflow-image-evals 管理内部生图质量测评：ingest 抽出 JSON、分层抽样、opt-in 整图对照。
//
// 原始图只写 STORAGE_ROOT/image-evals/。live run 需要 PRODUCTFLOW_RUN_IMAGE_EVALS=1、真实 prompt/image 绑定。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/imageeval"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/providers"
	"github.com/yuqie6/productflow/internal/settings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ingest":
		os.Exit(runIngest(os.Args[2:]))
	case "sample":
		os.Exit(runSample(os.Args[2:]))
	case "run":
		os.Exit(runLive(os.Args[2:]))
	case "report":
		os.Exit(runReport(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  productflow-image-evals ingest --json <file-or-dir>
  productflow-image-evals sample [--n 20] [--seed 1]
  productflow-image-evals run [--n 8] [--seed 1]
  productflow-image-evals report <run_id>`)
}

func storageRoot() (string, error) {
	return config.ResolveStorageRoot(os.Getenv("STORAGE_ROOT"))
}

func runIngest(args []string) int {
	path := flagValue(args, "--json")
	if path == "" {
		fmt.Fprintln(os.Stderr, "ingest --json <file-or-dir> is required")
		return 2
	}
	root, err := storageRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	files, err := jsonFiles(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Minute)
	defer stop()
	ok, fail := 0, 0
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
			fail++
			continue
		}
		var listing imageeval.ExtractListing
		if err := json.Unmarshal(raw, &listing); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
			fail++
			continue
		}
		m, err := imageeval.IngestListing(ctx, root, listing, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "reject %s: %v\n", listing.URL, err)
			fail++
			continue
		}
		fmt.Printf("admitted %s category=%s types=%d gold=%d\n", m.ID, m.Category, len(m.ImageTypes), len(m.Gold))
		ok++
	}
	fmt.Fprintf(os.Stderr, "admitted=%d rejected=%d\n", ok, fail)
	if ok == 0 {
		return 1
	}
	return 0
}

func runSample(args []string) int {
	n := flagInt(args, "--n", 20)
	seed := int64(flagInt(args, "--seed", 1))
	root, err := storageRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	pool, err := imageeval.LoadAdmitted(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sample, err := imageeval.SampleStratified(pool, n, seed)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(sampleIDs(sample))
	return 0
}

func runLive(args []string) int {
	n := flagInt(args, "--n", 8)
	seed := int64(flagInt(args, "--seed", 1))
	root, err := storageRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctxDB, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := db.Connect(ctxDB, config.NormalizePostgresURL(cfg.DatabaseURL))
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pool.Close()
	store := settings.NewStore(pool, cfg)
	image, err := providers.Image(context.Background(), store)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	prompt, err := store.ResolvePrompt(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	judge := imageeval.VisionJudge{
		BaseURL: prompt.BaseURL,
		APIKey:  prompt.APIKey,
		Model:   prompt.Model,
	}
	if envModel := strings.TrimSpace(os.Getenv("IMAGE_EVAL_JUDGE_MODEL")); envModel != "" {
		judge.Model = envModel
	}
	if envKey := strings.TrimSpace(os.Getenv("IMAGE_EVAL_JUDGE_API_KEY")); envKey != "" {
		judge.APIKey = envKey
	}
	if envBase := strings.TrimSpace(os.Getenv("IMAGE_EVAL_JUDGE_BASE_URL")); envBase != "" {
		judge.BaseURL = envBase
	}
	apiBase := strings.TrimSpace(os.Getenv("IMAGE_EVAL_API_BASE"))
	if apiBase == "" {
		port := strings.TrimSpace(os.Getenv("APP_PORT"))
		if port == "" {
			port = "29282"
		}
		apiBase = "http://127.0.0.1:" + port
	}
	ctx, stop := context.WithTimeout(context.Background(), 6*time.Hour)
	defer stop()
	report, err := imageeval.RunSampled(ctx, imageeval.RunConfig{
		StorageRoot: root,
		APIBase:     apiBase,
		AdminKey:    cfg.AdminAccessKey,
		Seed:        seed,
		N:           n,
		Commit:      gitHead(),
		Command:     "just image-evals-run",
		Image:       image,
		Judge:       judge,
		JudgeModel:  judge.Model,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if !report.Passed {
		return 1
	}
	return 0
}

func runReport(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "report <run_id> is required")
		return 2
	}
	root, err := storageRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report, err := imageeval.LoadRunReport(root, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	return 0
}

func jsonFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		out = append(out, filepath.Join(path, item.Name()))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no json files in %s", path)
	}
	return out, nil
}

func sampleIDs(items []imageeval.Manifest) []map[string]string {
	out := make([]map[string]string, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]string{"id": item.ID, "category": item.Category, "title": item.Title})
	}
	return out
}

func flagValue(args []string, name string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func flagInt(args []string, name string, fallback int) int {
	raw := flagValue(args, name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func gitHead() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
