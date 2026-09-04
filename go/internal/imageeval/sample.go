package imageeval

import (
	"fmt"
	"math/rand"
	"sort"
)

// SampleStratified 按类目分层随机抽 n 个过线 case。同一 seed 可复现。
func SampleStratified(pool []Manifest, n int, seed int64) ([]Manifest, error) {
	if n < 1 {
		return nil, fmt.Errorf("n must be >= 1")
	}
	if len(pool) == 0 {
		return nil, fmt.Errorf("empty admitted pool")
	}
	if n > len(pool) {
		n = len(pool)
	}
	byCat := map[string][]Manifest{}
	cats := make([]string, 0)
	for _, item := range pool {
		cat := item.Category
		if cat == "" {
			cat = "unknown"
		}
		if _, ok := byCat[cat]; !ok {
			cats = append(cats, cat)
		}
		byCat[cat] = append(byCat[cat], item)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		sort.Slice(byCat[cat], func(i, j int) bool { return byCat[cat][i].ID < byCat[cat][j].ID })
	}
	rng := rand.New(rand.NewSource(seed))
	picked := make([]Manifest, 0, n)
	pickedID := map[string]struct{}{}
	for len(picked) < n {
		progress := false
		for _, cat := range cats {
			if len(picked) >= n {
				break
			}
			bucket := byCat[cat]
			if len(bucket) == 0 {
				continue
			}
			idx := rng.Intn(len(bucket))
			item := bucket[idx]
			byCat[cat] = append(bucket[:idx], bucket[idx+1:]...)
			if _, ok := pickedID[item.ID]; ok {
				continue
			}
			pickedID[item.ID] = struct{}{}
			picked = append(picked, item)
			progress = true
		}
		if !progress {
			break
		}
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("stratified sample produced no cases")
	}
	return picked, nil
}
