// Command productflow-image-evals 管理内部生图质量测评：ingest 抽出 JSON、分层抽样、opt-in 整图对照。
//
// 原始图只写 STORAGE_ROOT/image-evals/。live run 需要 PRODUCTFLOW_RUN_IMAGE_EVALS=1、真实 prompt/image 绑定。
package main

import (
	"context"
	"encoding/json"
	"flag"
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
	case "prepare-inputs":
		os.Exit(runPrepareInputs(os.Args[2:]))
	case "prepare-annotations":
		os.Exit(runPrepareAnnotations(os.Args[2:]))
	case "annotate":
		os.Exit(runAnnotate(os.Args[2:]))
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
  productflow-image-evals prepare-inputs --out <new-json-file> [--n 8] [--seed 1]
  productflow-image-evals prepare-annotations --out <new-json-file> [--pool <pool-dir>] [--n 8] [--seed 1] [--types hero,selling_point,scene,detail]
  productflow-image-evals annotate --selection <selection-json> --out <new-report-dir> [--run-id <id>] [--model <model>] [--base-url <url>]
  productflow-image-evals run [--n 8] [--seed 1] [--product-inputs <json-file>]
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
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	n := flags.Int("n", 8, "sample count")
	seed := flags.Int64("seed", 1, "sample seed")
	inputs := flags.String("product-inputs", "", "frozen product input file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
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
	ctx, stop := context.WithTimeout(context.Background(), 6*time.Hour)
	defer stop()
	report, err := imageeval.RunSampled(ctx, imageeval.RunConfig{
		StorageRoot:       root,
		APIBase:           imageEvalAPIBase(),
		AdminKey:          cfg.AdminAccessKey,
		Seed:              *seed,
		N:                 *n,
		ProductInputsPath: *inputs,
		Commit:            gitHead(),
		Command:           "just image-evals-run",
		Image:             image,
		Judge:             judge,
		JudgeModel:        judge.Model,
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

func runPrepareInputs(args []string) int {
	flags := flag.NewFlagSet("prepare-inputs", flag.ContinueOnError)
	n := flags.Int("n", 8, "sample count")
	seed := flags.Int64("seed", 1, "sample seed")
	out := flags.String("out", "", "new output file; never overwrite an existing snapshot")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(*out) == "" {
		fmt.Fprintln(os.Stderr, "prepare-inputs requires --out <new-json-file>")
		return 2
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	snapshot, err := imageeval.PrepareProductInputs(ctx, imageeval.RunConfig{
		StorageRoot: root, APIBase: imageEvalAPIBase(), AdminKey: cfg.AdminAccessKey,
		N: *n, Seed: *seed, Commit: gitHead(),
	}, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("prepared=%d output=%s\n", len(snapshot.Inputs), *out)
	return 0
}

func runPrepareAnnotations(args []string) int {
	flags := flag.NewFlagSet("prepare-annotations", flag.ContinueOnError)
	n := flags.Int("n", 8, "sample count")
	seed := flags.Int64("seed", 1, "sample seed")
	pool := flags.String("pool", "", "admitted pool directory")
	out := flags.String("out", "", "new selection JSON; never overwrite an existing snapshot")
	types := flags.String("types", "", "optional comma-separated type cycle; one type per sampled case")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(*out) == "" {
		fmt.Fprintln(os.Stderr, "prepare-annotations requires --out <new-json-file>")
		return 2
	}
	var typeCycle []string
	if strings.TrimSpace(*types) != "" {
		for _, item := range strings.Split(*types, ",") {
			item = strings.TrimSpace(item)
			if item == "" || !imageeval.IsGeneratingType(item) {
				fmt.Fprintf(os.Stderr, "unsupported annotation image type %q\n", item)
				return 2
			}
			typeCycle = append(typeCycle, item)
		}
	}
	poolPath := strings.TrimSpace(*pool)
	if poolPath == "" {
		root, err := storageRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		poolPath = filepath.Join(imageeval.PoolRoot(root), imageeval.PoolDir)
	}
	var selection imageeval.AnnotationSelection
	var err error
	if len(typeCycle) > 0 {
		selection, err = imageeval.PrepareAnnotationSelectionForTypes(poolPath, *n, *seed, gitHead(), *out, typeCycle)
	} else {
		selection, err = imageeval.PrepareAnnotationSelection(poolPath, *n, *seed, gitHead(), *out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("prepared_annotation_selection=%s mode=%s cases=%d output=%s\n", imageeval.AnnotationSelectionSchemaVersion, selection.Mode, len(selection.Cases), *out)
	return 0
}

func runAnnotate(args []string) int {
	flags := flag.NewFlagSet("annotate", flag.ContinueOnError)
	selectionPath := flags.String("selection", "", "annotation selection JSON")
	out := flags.String("out", "", "new report directory; never overwrite an existing report")
	runID := flags.String("run-id", "", "optional stable run id")
	modelOverride := flags.String("model", "", "judge model override")
	baseOverride := flags.String("base-url", "", "judge base URL override")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(*selectionPath) == "" || strings.TrimSpace(*out) == "" {
		fmt.Fprintln(os.Stderr, "annotate requires --selection <selection-json> and --out <new-report-dir>")
		return 2
	}
	if strings.TrimSpace(os.Getenv(imageeval.RunSwitch)) != "1" {
		fmt.Fprintf(os.Stderr, "set %s=1 to run image annotations\n", imageeval.RunSwitch)
		return 1
	}
	selectionRaw, err := os.ReadFile(*selectionPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	selection, err := imageeval.LoadAnnotationSelection(*selectionPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	started := time.Now().UTC()
	generatedRunID := strings.TrimSpace(*runID)
	if generatedRunID == "" {
		generatedRunID = imageeval.AnnotationRunID(started, selectionRaw)
	}
	commit := gitHead()
	poolPath, err := filepath.Abs(selection.PoolPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	reservationModel := strings.TrimSpace(*modelOverride)
	if reservationModel == "" {
		reservationModel = strings.TrimSpace(os.Getenv("IMAGE_EVAL_JUDGE_MODEL"))
	}
	if err := imageeval.ReserveAnnotationOutput(*out, imageeval.AnnotationOutputReservation{
		SchemaVersion:   imageeval.AnnotationSchemaVersion,
		ContractVersion: imageeval.AnnotationContractVersion,
		RunID:           generatedRunID,
		Mode:            selection.Mode,
		SelectionPath:   *selectionPath,
		SelectionSHA256: imageeval.FileSHA256(selectionRaw),
		PoolPath:        poolPath,
		PoolIndexSHA256: selection.PoolIndexSHA256,
		PromptSHA256:    imageeval.FileSHA256([]byte(imageeval.AnnotationSystemPrompt)),
		Model:           reservationModel,
		Commit:          commit,
		ReservedAt:      started,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, stop := context.WithTimeout(context.Background(), 6*time.Hour)
	defer stop()
	judge, model, cleanup, err := resolveAnnotationJudge(ctx, *modelOverride, *baseOverride)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer cleanup()
	report, err := imageeval.RunAnnotations(ctx, imageeval.AnnotationRunConfig{
		SelectionPath: *selectionPath,
		RunID:         generatedRunID,
		Commit:        commit,
		Model:         model,
		Judge:         judge,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := imageeval.WriteAnnotationReport(*out, report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	records, completed, unknown, failed, assessmentUncomparable, comparisonUncomparable := 0, 0, 0, 0, 0, 0
	for _, item := range report.Cases {
		for _, record := range item.Records {
			records++
			switch record.Status {
			case imageeval.AnnotationStatusComplete:
				completed++
			case imageeval.AnnotationStatusUnknown:
				unknown++
			case imageeval.AnnotationStatusFailed:
				failed++
			case imageeval.AnnotationStatusUncomparable:
				assessmentUncomparable++
			}
			if record.Comparison != nil && record.Comparison.Status == imageeval.AnnotationComparisonUncomparable {
				comparisonUncomparable++
			}
		}
	}
	fmt.Printf("annotation_run=%s cases=%d records=%d complete=%d unknown=%d failed=%d assessment_uncomparable=%d comparison_uncomparable=%d json=%s markdown=%s\n",
		report.RunID, len(report.Cases), records, completed, unknown, failed, assessmentUncomparable, comparisonUncomparable,
		filepath.Join(*out, "report.json"), filepath.Join(*out, "report.md"))
	return 0
}

func resolveAnnotationJudge(ctx context.Context, modelOverride, baseOverride string) (imageeval.VisionJudge, string, func(), error) {
	model := strings.TrimSpace(modelOverride)
	if model == "" {
		model = strings.TrimSpace(os.Getenv("IMAGE_EVAL_JUDGE_MODEL"))
	}
	apiKey := strings.TrimSpace(os.Getenv("IMAGE_EVAL_JUDGE_API_KEY"))
	baseURL := strings.TrimSpace(baseOverride)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("IMAGE_EVAL_JUDGE_BASE_URL"))
	}
	var cleanup func()
	cleanup = func() {}
	if apiKey == "" || model == "" {
		cfg, err := config.Load()
		if err != nil {
			return imageeval.VisionJudge{}, "", cleanup, err
		}
		ctxDB, cancel := context.WithTimeout(ctx, 10*time.Second)
		pool, err := db.Connect(ctxDB, config.NormalizePostgresURL(cfg.DatabaseURL))
		cancel()
		if err != nil {
			return imageeval.VisionJudge{}, "", cleanup, err
		}
		cleanup = pool.Close
		binding, err := settings.NewStore(pool, cfg).ResolvePrompt(ctx)
		if err != nil {
			cleanup()
			cleanup = func() {}
			return imageeval.VisionJudge{}, "", cleanup, err
		}
		if apiKey == "" {
			apiKey = strings.TrimSpace(binding.APIKey)
		}
		if model == "" {
			model = strings.TrimSpace(binding.Model)
		}
		if baseURL == "" {
			baseURL = strings.TrimSpace(binding.BaseURL)
		}
	}
	if apiKey == "" || model == "" {
		cleanup()
		return imageeval.VisionJudge{}, "", func() {}, fmt.Errorf("annotation judge requires IMAGE_EVAL_JUDGE_API_KEY and IMAGE_EVAL_JUDGE_MODEL or a configured prompt binding")
	}
	return imageeval.VisionJudge{BaseURL: baseURL, APIKey: apiKey, Model: model}, model, cleanup, nil
}

func imageEvalAPIBase() string {
	if base := strings.TrimSpace(os.Getenv("IMAGE_EVAL_API_BASE")); base != "" {
		return base
	}
	port := strings.TrimSpace(os.Getenv("APP_PORT"))
	if port == "" {
		port = "29282"
	}
	return "http://127.0.0.1:" + port
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
