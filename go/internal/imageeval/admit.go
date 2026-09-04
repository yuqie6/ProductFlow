package imageeval

import (
	"fmt"
	"time"
)

// Admit 把已下载的 listing 变成过线 Manifest。薄套图、虚拟货、缺身份参考整单丢弃。
func Admit(listing ExtractListing, images []PoolImage) (Manifest, error) {
	if listing.Source != "" && listing.Source != "taobao" {
		return Manifest{}, fmt.Errorf("unsupported source %q", listing.Source)
	}
	if IsVirtualGoods(listing.Title) {
		return reject(listing, "virtual goods")
	}

	usable := make([]PoolImage, 0, len(images))
	for _, img := range images {
		if img.ByteSize < MinImageBytes {
			continue
		}
		if img.Width < MinPixelEdge && img.Height < MinPixelEdge {
			continue
		}
		if img.Confidence > 0 && img.Confidence < MinTypeConfidence {
			continue
		}
		usable = append(usable, img)
	}
	galleryN, detailN := 0, 0
	for _, img := range usable {
		switch img.Role {
		case "gallery":
			galleryN++
		case "detail":
			detailN++
		}
	}
	if galleryN < MinGalleryImages {
		return reject(listing, fmt.Sprintf("thin gallery: %d < %d", galleryN, MinGalleryImages))
	}
	if detailN < MinDetailImages {
		return reject(listing, fmt.Sprintf("thin detail modules: %d < %d", detailN, MinDetailImages))
	}

	var refs []PoolImage
	usedRefURL := map[string]struct{}{}
	takeRef := func(img PoolImage) {
		if len(refs) >= MaxRefImages {
			return
		}
		if _, used := usedRefURL[img.SourceURL]; used {
			return
		}
		refs = append(refs, img)
		usedRefURL[img.SourceURL] = struct{}{}
	}
	for _, img := range usable {
		if img.Role == "sku" || img.TypeKey == "sku" {
			takeRef(img)
		}
	}
	if len(refs) == 0 {
		for _, img := range usable {
			if img.Role == "gallery" && img.TypeKey != "hero" {
				takeRef(img)
				break
			}
		}
	}
	if len(refs) < MinRefImages {
		return reject(listing, "missing identity reference")
	}
	var gold []PoolImage
	for _, img := range usable {
		if _, used := usedRefURL[img.SourceURL]; used {
			continue
		}
		gold = append(gold, img)
	}
	if len(gold) == 0 {
		return reject(listing, "no gold images after reference split")
	}

	counts := map[string]int{}
	for _, img := range gold {
		if !IsGeneratingType(img.TypeKey) {
			continue
		}
		counts[img.TypeKey]++
	}
	if len(counts) < MinDistinctTypes {
		return reject(listing, fmt.Sprintf("distinct gold types %d < %d", len(counts), MinDistinctTypes))
	}
	for _, req := range RequiredGoldTypes {
		if counts[req] < 1 {
			return reject(listing, "missing gold type "+req)
		}
	}
	if counts["detail"] < 1 && counts["scene"] < 1 {
		return reject(listing, "missing gold detail or scene")
	}

	types := make([]TypeCount, 0, len(counts))
	for _, key := range GeneratingTypes {
		if n := counts[key]; n > 0 {
			if n > 6 {
				n = 6
			}
			types = append(types, TypeCount{Key: key, Quantity: n})
		}
	}
	id := CaseIDFromURL(listing.URL)
	if id == "" {
		return reject(listing, "empty listing url")
	}
	return Manifest{
		ID:         id,
		Source:     "taobao",
		URL:        listing.URL,
		Title:      listing.Title,
		Shop:       listing.Shop,
		Category:   listing.Category,
		Query:      listing.Query,
		IsTmall:    listing.IsTmall,
		SalesHint:  listing.SalesHint,
		Props:      listing.Props,
		AdmittedAt: time.Now().UTC(),
		References: refs,
		Gold:       gold,
		ImageTypes: types,
	}, nil
}

func reject(listing ExtractListing, reason string) (Manifest, error) {
	id := CaseIDFromURL(listing.URL)
	return Manifest{
		ID:           id,
		Source:       listing.Source,
		URL:          listing.URL,
		Title:        listing.Title,
		Category:     listing.Category,
		RejectReason: reason,
	}, fmt.Errorf("%s", reason)
}
