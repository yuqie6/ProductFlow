package imageeval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/providers"
)

// RunConfig 是一次 opt-in 测评。PRODUCTFLOW_RUN_IMAGE_EVALS=1 才应调用 Live。
type RunConfig struct {
	StorageRoot       string
	APIBase           string
	AdminKey          string
	Seed              int64
	N                 int
	Commit            string
	Command           string
	Image             providers.ImageClient
	Judge             JudgeClient
	JudgeModel        string
	HTTPClient        *http.Client
	PollEvery         time.Duration
	PollFor           time.Duration
	ProductInputsPath string
}

// RunSampled 从过线池分层抽样，跑工作台臂、直调臂和评委闸门。
func RunSampled(ctx context.Context, cfg RunConfig) (RunReport, error) {
	if os.Getenv(RunSwitch) != "1" {
		return RunReport{}, fmt.Errorf("set %s=1 to run live image evals", RunSwitch)
	}
	if cfg.Image == nil || cfg.Judge == nil {
		return RunReport{}, fmt.Errorf("image client and judge are required")
	}
	pool, err := LoadAdmitted(cfg.StorageRoot)
	if err != nil {
		return RunReport{}, err
	}
	sample, err := SampleStratified(pool, cfg.N, cfg.Seed)
	if err != nil {
		return RunReport{}, err
	}
	inputs, inputSHA, err := resolveProductInputs(cfg.StorageRoot, sample, cfg.ProductInputsPath)
	if err != nil {
		return RunReport{}, err
	}
	client, err := cfg.http()
	if err != nil {
		return RunReport{}, err
	}
	if err := login(ctx, client, cfg.APIBase, cfg.AdminKey); err != nil {
		return RunReport{}, err
	}
	runID := time.Now().UTC().Format("20060102T150405Z") + "-" + CaseIDFromURL(fmt.Sprintf("%d:%d", cfg.Seed, len(sample)))[:8]
	report := RunReport{
		RunID:            runID,
		Commit:           cfg.Commit,
		Seed:             cfg.Seed,
		N:                len(sample),
		Command:          cfg.Command,
		StartedAt:        time.Now().UTC(),
		ModelImage:       cfg.Image.Name(),
		ModelJudge:       cfg.JudgeModel,
		ProductInputs:    inputs,
		ProductInputsSHA: inputSHA,
		InputMode:        "title_props",
	}
	if cfg.ProductInputsPath != "" {
		report.InputMode = "frozen_source_note"
	}
	if err := writeRun(cfg.StorageRoot, report); err != nil {
		return report, err
	}
	for i, item := range sample {
		caseReport := evaluateCase(ctx, cfg, client, item, inputs[i].SourceNote)
		report.Cases = append(report.Cases, caseReport)
		if caseReport.Passed {
			report.PassCount++
		} else {
			report.FailCount++
		}
	}
	report.FinishedAt = time.Now().UTC()
	report.Passed = report.FailCount == 0 && report.PassCount > 0
	if err := writeRun(cfg.StorageRoot, report); err != nil {
		return report, err
	}
	return report, nil
}

func (cfg RunConfig) http() (*http.Client, error) {
	if cfg.HTTPClient != nil {
		return cfg.HTTPClient, nil
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &http.Client{Jar: jar, Timeout: 2 * time.Minute}, nil
}

func evaluateCase(ctx context.Context, cfg RunConfig, client *http.Client, m Manifest, sourceNote string) CaseReport {
	out := CaseReport{CaseID: m.ID}
	productID, graphID, err := createDirect(ctx, client, cfg, m, sourceNote)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.ProductID = productID
	out.GraphID = graphID
	if err := submitAndWait(ctx, cfg, client, productID, graphID); err != nil {
		out.Error = err.Error()
		return out
	}
	generated, err := listGenerated(ctx, client, cfg.APIBase, productID, graphID)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	refBytes, err := loadRefs(cfg.StorageRoot, m)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	goldByType := map[string][]PoolImage{}
	for _, g := range m.Gold {
		goldByType[g.TypeKey] = append(goldByType[g.TypeKey], g)
	}
	workDir := filepath.Join(PoolRoot(cfg.StorageRoot), RunsDir, "scratch", m.ID)
	_ = os.MkdirAll(workDir, 0o755)
	passed := true
	for _, tc := range m.ImageTypes {
		prompt, err := NaivePrompt(tc.Key)
		if err != nil {
			out.Error = err.Error()
			return out
		}
		goldList := goldByType[tc.Key]
		genList := generated[tc.Key]
		if len(genList) == 0 {
			out.Error = fmt.Sprintf("no workbench image for type %s", tc.Key)
			return out
		}
		n := JudgeK
		if n > len(genList) {
			n = len(genList)
		}
		if n < 1 {
			out.Error = fmt.Sprintf("empty quantity for type %s", tc.Key)
			return out
		}
		for i := 0; i < n; i++ {
			naivePath := filepath.Join(workDir, fmt.Sprintf("naive-%s-%d.png", tc.Key, i))
			naiveReq := providers.GenerateRequest{
				Prompt:         prompt,
				Size:           "1024x1024",
				Count:          1,
				Refs:           refBytes,
				ImageTypeKey:   tc.Key,
				Mode:           providers.ModeChat,
				GenerationSpec: map[string]any{"quality_intent": "high", "reference_fidelity": "high"},
			}
			res, err := cfg.Image.Generate(ctx, naiveReq)
			if err != nil {
				out.Error = fmt.Sprintf("naive %s: %v", tc.Key, err)
				return out
			}
			if err := os.WriteFile(naivePath, res.Bytes, 0o644); err != nil {
				out.Error = err.Error()
				return out
			}
			in := JudgeInput{
				ImageType:      tc.Key,
				TypeJob:        TypeJobSummary(tc.Key),
				ReferencePaths: refPaths(cfg.StorageRoot, m),
				WorkbenchPath:  genListSafe(genList, i),
				NaivePath:      naivePath,
			}
			if i < len(goldList) {
				in.GoldPath = ResolvePoolFile(cfg.StorageRoot, m.ID, goldList[i].Path)
			}
			slot, err := cfg.Judge.Judge(ctx, in)
			if err != nil {
				out.Error = err.Error()
				return out
			}
			slot.ImageType = tc.Key
			slot.Index = i
			slot = GateSlot(slot)
			out.Slots = append(out.Slots, slot)
			if !slot.Passed {
				passed = false
			}
		}
	}
	out.Passed = passed && out.Error == ""
	return out
}

func genListSafe(items []string, i int) string {
	if i < len(items) {
		return items[i]
	}
	if len(items) > 0 {
		return items[0]
	}
	return ""
}

func loadRefs(storageRoot string, m Manifest) ([]providers.ImageRef, error) {
	out := make([]providers.ImageRef, 0, len(m.References))
	for _, ref := range m.References {
		body, err := os.ReadFile(ResolvePoolFile(storageRoot, m.ID, ref.Path))
		if err != nil {
			return nil, err
		}
		out = append(out, providers.ImageRef{Bytes: body, MIME: http.DetectContentType(body), Filename: filepath.Base(ref.Path)})
	}
	return out, nil
}

func refPaths(storageRoot string, m Manifest) []string {
	out := make([]string, 0, len(m.References))
	for _, ref := range m.References {
		out = append(out, ResolvePoolFile(storageRoot, m.ID, ref.Path))
	}
	return out
}

func login(ctx context.Context, client *http.Client, apiBase, adminKey string) error {
	base := strings.TrimRight(apiBase, "/")
	email := "operator@test.local"
	password := "test-password-ok"
	bootstrap, _ := json.Marshal(map[string]string{
		"admin_key": adminKey, "email": email, "password": password, "merchant_name": "开发商家",
	})
	breq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/auth/bootstrap", bytes.NewReader(bootstrap))
	if err != nil {
		return err
	}
	breq.Header.Set("Content-Type", "application/json")
	bresp, err := client.Do(breq)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, bresp.Body)
	bresp.Body.Close()
	if bresp.StatusCode != http.StatusOK && bresp.StatusCode != http.StatusConflict {
		return fmt.Errorf("bootstrap http %d", bresp.StatusCode)
	}
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/auth/session", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("login http %d: %s", resp.StatusCode, raw)
	}
	return nil
}

// evalTypeCounts 按过线种类建画布；k=1 每个图种一张，不把套图截成两三种随机图。
func evalTypeCounts(m Manifest) []TypeCount {
	out := make([]TypeCount, 0, len(m.ImageTypes))
	for _, tc := range m.ImageTypes {
		out = append(out, TypeCount{Key: tc.Key, Quantity: JudgeK})
	}
	return out
}

func createDirect(ctx context.Context, client *http.Client, cfg RunConfig, m Manifest, sourceNote string) (string, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", truncateRunes(m.Title, 80))
	_ = w.WriteField("category", m.Category)
	_ = w.WriteField("source_note", sourceNote)
	typesJSON, _ := json.Marshal(evalTypeCounts(m))
	_ = w.WriteField("image_types", string(typesJSON))
	for i, ref := range m.References {
		body, err := os.ReadFile(ResolvePoolFile(cfg.StorageRoot, m.ID, ref.Path))
		if err != nil {
			return "", "", err
		}
		part, err := w.CreateFormFile("images", fmt.Sprintf("ref-%d%s", i, filepath.Ext(ref.Path)))
		if err != nil {
			return "", "", err
		}
		if _, err := part.Write(body); err != nil {
			return "", "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.APIBase, "/")+"/api/v3/products", &buf)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("create product http %d: %s", resp.StatusCode, truncate(string(raw), 400))
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", err
	}
	var product struct {
		ID string `json:"id"`
	}
	var graph map[string]any
	if err := json.Unmarshal(parsed["product"], &product); err != nil {
		return "", "", err
	}
	if err := json.Unmarshal(parsed["graph"], &graph); err != nil {
		return "", "", err
	}
	graphID, _ := graph["id"].(string)
	if product.ID == "" || graphID == "" {
		return "", "", fmt.Errorf("create product missing ids")
	}
	return product.ID, graphID, nil
}

func submitAndWait(ctx context.Context, cfg RunConfig, client *http.Client, productID, graphID string) error {
	base := strings.TrimRight(cfg.APIBase, "/")
	body, _ := json.Marshal(map[string]any{"scope": "graph"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v3/products/%s/workflows/%s/runs", base, productID, graphID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("submit run http %d: %s", resp.StatusCode, truncate(string(raw), 400))
	}
	var submitted struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &submitted)
	every := cfg.PollEvery
	if every == 0 {
		every = 3 * time.Second
	}
	limit := cfg.PollFor
	if limit == 0 {
		limit = 45 * time.Minute
	}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		var status, reason string
		var err error
		if submitted.ID != "" {
			status, reason, err = runStatus(ctx, client, base, productID, graphID, submitted.ID)
		} else {
			status, reason, err = latestRunStatus(ctx, client, base, productID, graphID)
		}
		if err != nil {
			return err
		}
		switch status {
		case "succeeded":
			return nil
		case "failed", "cancelled", "unknown":
			return fmt.Errorf("graph run %s: %s", status, reason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
	return fmt.Errorf("graph run timed out")
}

func runStatus(ctx context.Context, client *http.Client, base, productID, graphID, runID string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v3/products/%s/workflows/%s/runs/%s", base, productID, graphID, runID), nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("get run http %d", resp.StatusCode)
	}
	var parsed struct {
		Status        string  `json:"status"`
		FailureReason *string `json:"failure_reason"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", err
	}
	reason := ""
	if parsed.FailureReason != nil {
		reason = *parsed.FailureReason
	}
	return parsed.Status, reason, nil
}

func latestRunStatus(ctx context.Context, client *http.Client, base, productID, graphID string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v3/products/%s/workflows/%s/runs", base, productID, graphID), nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("list runs http %d", resp.StatusCode)
	}
	var parsed struct {
		Items []struct {
			Status        string  `json:"status"`
			FailureReason *string `json:"failure_reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", err
	}
	if len(parsed.Items) == 0 {
		return "missing", "", nil
	}
	reason := ""
	if parsed.Items[0].FailureReason != nil {
		reason = *parsed.Items[0].FailureReason
	}
	return parsed.Items[0].Status, reason, nil
}

func listGenerated(ctx context.Context, client *http.Client, apiBase, productID, graphID string) (map[string][]string, error) {
	base := strings.TrimRight(apiBase, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v3/products/%s/workflows/%s", base, productID, graphID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get graph http %d", resp.StatusCode)
	}
	var graph struct {
		Nodes []struct {
			ID        string         `json:"id"`
			NodeType  string         `json:"node_type"`
			Config    map[string]any `json:"config"`
			PreviewID *string        `json:"preview_asset_id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		return nil, fmt.Errorf("get graph: %w", err)
	}
	out := map[string][]string{}
	tmp := os.TempDir()
	for _, node := range graph.Nodes {
		if node.NodeType != "image_generation" || node.PreviewID == nil || *node.PreviewID == "" {
			continue
		}
		key, _ := node.Config["image_type_key"].(string)
		url := fmt.Sprintf("%s/api/v2/product-image-assets/%s/download", base, *node.PreviewID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		path := filepath.Join(tmp, fmt.Sprintf("pf-eval-%s-%s.bin", node.ID, *node.PreviewID))
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return nil, err
		}
		out[key] = append(out[key], path)
	}
	return out, nil
}

func factsNote(m Manifest) string {
	parts := []string{m.Title}
	keys := make([]string, 0, len(m.Props))
	for k := range m.Props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+"："+m.Props[k])
	}
	return strings.Join(parts, "。")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func writeRun(storageRoot string, report RunReport) error {
	dir := filepath.Join(PoolRoot(storageRoot), RunsDir, report.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), raw, 0o644); err != nil {
		return err
	}
	latest := map[string]string{"run_id": report.RunID}
	lb, _ := json.MarshalIndent(latest, "", "  ")
	return os.WriteFile(filepath.Join(PoolRoot(storageRoot), "latest.json"), lb, 0o644)
}

// LoadRunReport 从落盘复算入口读取 report.json。
func LoadRunReport(storageRoot, runID string) (RunReport, error) {
	raw, err := os.ReadFile(filepath.Join(PoolRoot(storageRoot), RunsDir, runID, "report.json"))
	if err != nil {
		return RunReport{}, err
	}
	var report RunReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return RunReport{}, err
	}
	return report, nil
}
