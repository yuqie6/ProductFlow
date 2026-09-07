package imageeval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PrepareAnnotationSelection samples an admitted pool and writes a new,
// immutable reference-mode selection. It never overwrites an existing file.
// One quality reference is selected for each manifest image type.
func PrepareAnnotationSelection(poolPath string, n int, seed int64, commit, outputPath string) (AnnotationSelection, error) {
	return prepareAnnotationSelection(poolPath, n, seed, commit, outputPath, nil)
}

// PrepareAnnotationSelectionForTypes is the bounded variant used when a run
// intentionally rotates a small type cycle across cases. Each selected case
// still has exactly one explicit quality reference for its chosen type.
func PrepareAnnotationSelectionForTypes(poolPath string, n int, seed int64, commit, outputPath string, typeCycle []string) (AnnotationSelection, error) {
	for _, imageType := range typeCycle {
		if !IsGeneratingType(imageType) {
			return AnnotationSelection{}, fmt.Errorf("unsupported annotation image type %q", imageType)
		}
	}
	if len(typeCycle) == 0 {
		return AnnotationSelection{}, fmt.Errorf("annotation type cycle is empty")
	}
	return prepareAnnotationSelection(poolPath, n, seed, commit, outputPath, append([]string(nil), typeCycle...))
}

func prepareAnnotationSelection(poolPath string, n int, seed int64, commit, outputPath string, typeCycle []string) (AnnotationSelection, error) {
	poolPath, err := absoluteDir(poolPath)
	if err != nil {
		return AnnotationSelection{}, err
	}
	if strings.TrimSpace(outputPath) == "" {
		return AnnotationSelection{}, fmt.Errorf("annotation selection output path is required")
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return AnnotationSelection{}, err
	}
	if _, err := os.Stat(outputPath); err == nil {
		return AnnotationSelection{}, fmt.Errorf("annotation selection output already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return AnnotationSelection{}, err
	}

	pool, indexSHA, err := loadAnnotationPool(poolPath)
	if err != nil {
		return AnnotationSelection{}, err
	}
	sample, err := SampleStratified(pool, n, seed)
	if err != nil {
		return AnnotationSelection{}, err
	}
	selection := AnnotationSelection{
		SchemaVersion:   AnnotationSelectionSchemaVersion,
		ContractVersion: AnnotationContractVersion,
		Mode:            AnnotationModeReference,
		PoolPath:        poolPath,
		PoolIndexSHA256: indexSHA,
		CreatedAt:       time.Now().UTC(),
		Commit:          firstNonEmptyString(commit, "unknown"),
		N:               len(sample),
		Seed:            seed,
		Cases:           make([]AnnotationCaseSelection, 0, len(sample)),
	}
	for i, m := range sample {
		manifestRaw, err := os.ReadFile(filepath.Join(poolPath, m.ID, "manifest.json"))
		if err != nil {
			return AnnotationSelection{}, fmt.Errorf("read manifest %s: %w", m.ID, err)
		}
		var selectedTypes []string
		if len(typeCycle) > 0 {
			selectedTypes = []string{typeCycle[i%len(typeCycle)]}
		}
		caseSelection, err := makeAnnotationCaseSelection(poolPath, m, FileSHA256(manifestRaw), selectedTypes)
		if err != nil {
			return AnnotationSelection{}, err
		}
		selection.Cases = append(selection.Cases, caseSelection)
	}
	if err := ValidateAnnotationSelection(selection); err != nil {
		return AnnotationSelection{}, err
	}
	if err := writeSelectionExclusive(outputPath, selection); err != nil {
		return AnnotationSelection{}, err
	}
	return selection, nil
}

// LoadAnnotationSelection reads the strict selection shape. Byte content is
// checked when RunAnnotations builds its plan, after the selected pool is read.
func LoadAnnotationSelection(path string) (AnnotationSelection, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AnnotationSelection{}, err
	}
	var selection AnnotationSelection
	if err := decodeStrictJSON(raw, &selection); err != nil {
		return AnnotationSelection{}, fmt.Errorf("annotation selection: %w", err)
	}
	if err := ValidateAnnotationSelection(selection); err != nil {
		return AnnotationSelection{}, err
	}
	return selection, nil
}

func ValidateAnnotationSelection(selection AnnotationSelection) error {
	if selection.SchemaVersion != AnnotationSelectionSchemaVersion {
		return fmt.Errorf("annotation selection schema version %q is unsupported", selection.SchemaVersion)
	}
	if selection.ContractVersion != AnnotationContractVersion {
		return fmt.Errorf("annotation selection contract version %q is unsupported", selection.ContractVersion)
	}
	if err := selection.validateMode(); err != nil {
		return err
	}
	if strings.TrimSpace(selection.PoolPath) == "" {
		return fmt.Errorf("annotation selection pool path is required")
	}
	if err := validateSHA(selection.PoolIndexSHA256, "pool index"); err != nil {
		return err
	}
	if selection.N < 1 || selection.N != len(selection.Cases) {
		return fmt.Errorf("annotation selection n=%d cases=%d is inconsistent", selection.N, len(selection.Cases))
	}
	seenCases := make(map[string]struct{}, len(selection.Cases))
	for i, item := range selection.Cases {
		if err := validateAnnotationCaseSelection(selection.Mode, item); err != nil {
			return fmt.Errorf("selection case %d: %w", i, err)
		}
		if _, ok := seenCases[item.CaseID]; ok {
			return fmt.Errorf("annotation selection duplicate case %q", item.CaseID)
		}
		seenCases[item.CaseID] = struct{}{}
	}
	return nil
}

func validateAnnotationCaseSelection(mode string, item AnnotationCaseSelection) error {
	if strings.TrimSpace(item.CaseID) == "" {
		return fmt.Errorf("case id is required")
	}
	if strings.TrimSpace(item.Category) == "" {
		return fmt.Errorf("category is required")
	}
	if err := validateSHA(item.ManifestSHA256, "manifest"); err != nil {
		return err
	}
	if len(item.ImageTypes) == 0 {
		return fmt.Errorf("image types are required")
	}
	types := make(map[string]struct{}, len(item.ImageTypes))
	for _, key := range item.ImageTypes {
		if strings.TrimSpace(key) == "" || !IsGeneratingType(key) {
			return fmt.Errorf("unsupported image type %q", key)
		}
		if _, ok := types[key]; ok {
			return fmt.Errorf("duplicate image type %q", key)
		}
		types[key] = struct{}{}
	}
	if len(item.IdentityReferences) == 0 {
		return fmt.Errorf("identity references are required")
	}
	seenIDs := make(map[string]struct{})
	seenSHA := make(map[string]string)
	for _, asset := range item.IdentityReferences {
		if err := validateSelectionAsset(asset, "identity_reference", types); err != nil {
			return err
		}
		if err := addAssetIdentity(seenIDs, seenSHA, asset); err != nil {
			return err
		}
	}
	qualityByType := make(map[string]int, len(item.QualityReferences))
	for _, asset := range item.QualityReferences {
		if err := validateSelectionAsset(asset, "quality_reference", types); err != nil {
			return err
		}
		qualityByType[asset.ImageType]++
		if err := addAssetIdentity(seenIDs, seenSHA, asset); err != nil {
			return err
		}
	}
	for _, key := range item.ImageTypes {
		qualityCount := qualityByType[key]
		if mode == AnnotationModeReference && qualityCount != 1 {
			return fmt.Errorf("image type %q needs exactly one quality reference, got %d", key, qualityCount)
		}
		if mode == AnnotationModeComparison && qualityCount > 1 {
			return fmt.Errorf("image type %q has multiple quality references: %d", key, qualityCount)
		}
	}
	if mode == AnnotationModeReference && len(item.Candidates) != 0 {
		return fmt.Errorf("reference mode cannot contain candidates")
	}
	seenVariants := map[string]struct{}{}
	for _, asset := range item.Candidates {
		if err := validateSelectionAsset(asset, "evaluated_result", types); err != nil {
			return err
		}
		if strings.TrimSpace(asset.Variant) == "" {
			return fmt.Errorf("candidate %q variant is required", asset.AssetID)
		}
		key := asset.ImageType + "\x00" + asset.Variant
		if _, ok := seenVariants[key]; ok {
			return fmt.Errorf("duplicate candidate %s/%s", asset.ImageType, asset.Variant)
		}
		seenVariants[key] = struct{}{}
		if err := addAssetIdentity(seenIDs, seenSHA, asset); err != nil {
			return err
		}
	}
	if mode == AnnotationModeComparison {
		if len(item.Candidates) == 0 {
			return fmt.Errorf("comparison mode requires explicit candidates for case %s", item.CaseID)
		}
		for _, key := range item.ImageTypes {
			found := false
			for _, candidate := range item.Candidates {
				if candidate.ImageType == key {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("comparison case %s has no candidate for image type %q", item.CaseID, key)
			}
		}
	}
	return nil
}

func validateSelectionAsset(asset AnnotationAsset, role string, types map[string]struct{}) error {
	if strings.TrimSpace(asset.AssetID) == "" {
		return fmt.Errorf("%s asset id is required", role)
	}
	if strings.TrimSpace(asset.Path) == "" {
		return fmt.Errorf("asset %q path is required", asset.AssetID)
	}
	if err := validateSHA(asset.SHA256, "asset "+asset.AssetID); err != nil {
		return err
	}
	if asset.Width < 1 || asset.Height < 1 {
		return fmt.Errorf("asset %q dimensions are required", asset.AssetID)
	}
	if asset.Role != role {
		return fmt.Errorf("asset %q role %q, expected %q", asset.AssetID, asset.Role, role)
	}
	if role != "identity_reference" {
		if _, ok := types[asset.ImageType]; !ok {
			return fmt.Errorf("asset %q has image type %q outside case types", asset.AssetID, asset.ImageType)
		}
	}
	switch asset.Source {
	case "manifest.references":
		if filepath.IsAbs(asset.Path) {
			return fmt.Errorf("manifest asset %q path must be relative", asset.AssetID)
		}
		if role != "identity_reference" && role != "evaluated_result" {
			return fmt.Errorf("asset %q source manifest.references is invalid for role %q", asset.AssetID, role)
		}
	case "manifest.gold":
		if filepath.IsAbs(asset.Path) {
			return fmt.Errorf("manifest asset %q path must be relative", asset.AssetID)
		}
		if role != "quality_reference" && role != "evaluated_result" {
			return fmt.Errorf("asset %q source manifest.gold is invalid for role %q", asset.AssetID, role)
		}
	case "external":
		if role != "evaluated_result" {
			return fmt.Errorf("external asset %q must be an evaluated result", asset.AssetID)
		}
		if !filepath.IsAbs(asset.Path) {
			return fmt.Errorf("external asset %q path must be absolute", asset.AssetID)
		}
	default:
		return fmt.Errorf("asset %q source %q is unsupported", asset.AssetID, asset.Source)
	}
	return nil
}

func addAssetIdentity(ids map[string]struct{}, hashes map[string]string, asset AnnotationAsset) error {
	if _, ok := ids[asset.AssetID]; ok {
		return fmt.Errorf("duplicate asset id %q", asset.AssetID)
	}
	ids[asset.AssetID] = struct{}{}
	if prior, ok := hashes[asset.SHA256]; ok {
		return fmt.Errorf("assets %q and %q share bytes SHA256", prior, asset.AssetID)
	}
	hashes[asset.SHA256] = asset.AssetID
	return nil
}

func makeAnnotationCaseSelection(poolPath string, m Manifest, manifestSHA string, selectedTypes []string) (AnnotationCaseSelection, error) {
	item := AnnotationCaseSelection{
		CaseID:         m.ID,
		Category:       m.Category,
		Title:          m.Title,
		ManifestSHA256: manifestSHA,
		ImageTypes:     make([]string, 0, len(m.ImageTypes)),
		Candidates:     []AnnotationAsset{},
	}
	for i, ref := range m.References {
		asset, err := poolAsset(poolPath, m.ID, ref, fmt.Sprintf("reference:%d", i), "identity_reference", "manifest.references", firstNonEmptyString(ref.TypeKey, "sku"))
		if err != nil {
			return AnnotationCaseSelection{}, err
		}
		item.IdentityReferences = append(item.IdentityReferences, asset)
	}
	selectedSet := make(map[string]struct{}, len(selectedTypes))
	for _, key := range selectedTypes {
		selectedSet[key] = struct{}{}
	}
	for _, tc := range m.ImageTypes {
		if len(selectedTypes) > 0 {
			if _, ok := selectedSet[tc.Key]; !ok {
				continue
			}
		}
		item.ImageTypes = append(item.ImageTypes, tc.Key)
		var selected *PoolImage
		for i := range m.Gold {
			if m.Gold[i].TypeKey == tc.Key {
				candidate := m.Gold[i]
				selected = &candidate
				break
			}
		}
		if selected == nil {
			return AnnotationCaseSelection{}, fmt.Errorf("case %s has image type %q without gold bytes", m.ID, tc.Key)
		}
		asset, err := poolAsset(poolPath, m.ID, *selected, "quality:"+tc.Key, "quality_reference", "manifest.gold", tc.Key)
		if err != nil {
			return AnnotationCaseSelection{}, err
		}
		item.QualityReferences = append(item.QualityReferences, asset)
	}
	if len(selectedTypes) > 0 && len(item.ImageTypes) != len(selectedTypes) {
		return AnnotationCaseSelection{}, fmt.Errorf("case %s is missing one or more requested annotation types", m.ID)
	}
	return item, nil
}

func poolAsset(poolPath, caseID string, img PoolImage, assetID, role, source, imageType string) (AnnotationAsset, error) {
	path := filepath.Join(poolPath, caseID, filepath.Clean(img.Path))
	if !pathWithin(filepath.Join(poolPath, caseID), path) {
		return AnnotationAsset{}, fmt.Errorf("asset %q path escapes case directory", assetID)
	}
	asset := AnnotationAsset{
		AssetID:       assetID,
		Path:          img.Path,
		SHA256:        strings.ToLower(strings.TrimSpace(img.SHA256)),
		Width:         img.Width,
		Height:        img.Height,
		ByteSize:      img.ByteSize,
		ImageType:     imageType,
		Role:          role,
		Source:        source,
		SourceURL:     img.SourceURL,
		SourceRole:    img.Role,
		SourceTypeKey: img.TypeKey,
		Confidence:    img.Confidence,
	}
	if err := verifyAnnotationAsset(path, asset); err != nil {
		return AnnotationAsset{}, fmt.Errorf("case %s asset %s: %w", caseID, assetID, err)
	}
	return asset, nil
}

func loadAnnotationPool(poolPath string) ([]Manifest, string, error) {
	indexPath := filepath.Join(poolPath, "index.json")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, "", err
	}
	var index poolIndex
	if err := decodeStrictJSON(raw, &index); err != nil {
		return nil, "", fmt.Errorf("annotation pool index: %w", err)
	}
	if len(index.Cases) == 0 {
		return nil, "", fmt.Errorf("annotation pool is empty")
	}
	entries := append([]indexEntry(nil), index.Cases...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	out := make([]Manifest, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" {
			return nil, "", fmt.Errorf("annotation pool index contains empty case id")
		}
		if _, ok := seen[entry.ID]; ok {
			return nil, "", fmt.Errorf("annotation pool index duplicates case %s", entry.ID)
		}
		seen[entry.ID] = struct{}{}
		m, err := loadAnnotationManifest(poolPath, entry.ID)
		if err != nil {
			// Match LoadAdmitted: a stale, rejected, or contaminated manifest does
			// not make the remaining valid pool unusable.
			continue
		}
		if entry.Category != "" && m.Category != entry.Category {
			continue
		}
		out = append(out, m)
	}
	return out, FileSHA256(raw), nil
}

func loadAnnotationManifest(poolPath, caseID string) (Manifest, error) {
	caseRoot := filepath.Join(poolPath, caseID)
	if !pathWithin(poolPath, caseRoot) {
		return Manifest{}, fmt.Errorf("case path escapes pool: %s", caseID)
	}
	raw, err := os.ReadFile(filepath.Join(caseRoot, "manifest.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest %s: %w", caseID, err)
	}
	var m Manifest
	if err := decodeStrictJSON(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest %s: %w", caseID, err)
	}
	if m.ID != caseID || m.URL == "" || len(m.References) == 0 || len(m.Gold) == 0 || len(m.ImageTypes) == 0 {
		return Manifest{}, fmt.Errorf("invalid manifest %s", caseID)
	}
	if m.RejectReason != "" {
		return Manifest{}, fmt.Errorf("rejected manifest %s", caseID)
	}
	refHashes := map[string]struct{}{}
	for _, ref := range m.References {
		if _, ok := refHashes[ref.SHA256]; ok {
			return Manifest{}, fmt.Errorf("manifest %s duplicates reference bytes", caseID)
		}
		refHashes[ref.SHA256] = struct{}{}
	}
	for _, gold := range m.Gold {
		if _, overlap := refHashes[gold.SHA256]; overlap {
			return Manifest{}, fmt.Errorf("manifest %s: reference/gold content overlap", caseID)
		}
	}
	return m, nil
}

func writeSelectionExclusive(path string, selection AnnotationSelection) error {
	raw, err := json.MarshalIndent(selection, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		return err
	}
	return file.Sync()
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("trailing JSON value")
	} else if err != io.EOF {
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func validateSHA(value, label string) error {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("%s SHA256 must be 64 hex characters", label)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s SHA256 is invalid: %w", label, err)
	}
	return nil
}

func verifyAnnotationAsset(path string, asset AnnotationAsset) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if asset.ByteSize > 0 && len(raw) != asset.ByteSize {
		return fmt.Errorf("byte size %d does not match selection %d", len(raw), asset.ByteSize)
	}
	if got := FileSHA256(raw); !strings.EqualFold(got, asset.SHA256) {
		return fmt.Errorf("bytes SHA256 %s does not match selection %s", got, asset.SHA256)
	}
	return nil
}

func absoluteDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("pool path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("pool path is not a directory: %s", abs)
	}
	return filepath.Clean(abs), nil
}

func pathWithin(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(pathAbs))
	if err != nil || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "." || filepath.Clean(rootAbs) == filepath.Clean(pathAbs)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
