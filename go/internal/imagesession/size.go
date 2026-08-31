package imagesession

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

var sizePattern = regexp.MustCompile(`^\d+x\d+$`)

const (
	minDimension        = 512
	dimensionMultiple   = 16
	minMaxDimension     = 512
	maxMaxDimension     = 8192
	defaultMaxDimension = 3840
)

// normalizeSize 把 WxH 收到设置页最大边长，并对齐 16 的倍数。非法格式或超范围返回 400。
func normalizeSize(value string, maxDimension int) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if !sizePattern.MatchString(normalized) {
		return "", apperr.Validation("图片尺寸必须使用 宽x高 格式，例如 1024x1024")
	}
	parts := strings.SplitN(normalized, "x", 2)
	width, _ := strconv.Atoi(parts[0])
	height, _ := strconv.Atoi(parts[1])
	if width <= 0 || height <= 0 {
		return "", apperr.Validation("图片尺寸宽高必须大于 0")
	}
	if maxDimension <= 0 {
		maxDimension = defaultMaxDimension
	}
	if maxDimension < minMaxDimension || maxDimension > maxMaxDimension {
		return "", apperr.Validation(fmt.Sprintf("生图最大单边必须在 %d-%d 之间", minMaxDimension, maxMaxDimension))
	}
	effective := maxDimension - (maxDimension % dimensionMultiple)
	maxPixels := effective * effective
	scale := math.Min(1, math.Min(float64(effective)/float64(width), float64(effective)/float64(height)))
	rw := minInt(effective, maxInt(minDimension, int(math.Round(float64(width)*scale))))
	rh := minInt(effective, maxInt(minDimension, int(math.Round(float64(height)*scale))))
	if rw*rh > maxPixels {
		pixelScale := math.Sqrt(float64(maxPixels) / float64(rw*rh))
		rw = maxInt(1, int(float64(rw)*pixelScale))
		rh = maxInt(1, int(float64(rh)*pixelScale))
	}
	rw = nearestMultiple(rw, effective)
	rh = nearestMultiple(rh, effective)
	return fmt.Sprintf("%dx%d", rw, rh), nil
}

// nearestMultiple 把边长收到最接近的 16 倍数，夹在 minDimension 与 maxDimension 之间。
func nearestMultiple(value, maxDimension int) int {
	lower := (value / dimensionMultiple) * dimensionMultiple
	upper := lower + dimensionMultiple
	best := -1
	bestDist := math.MaxInt
	for _, c := range []int{lower, upper} {
		if c >= minDimension && c <= maxDimension {
			d := absInt(c - value)
			if d < bestDist || (d == bestDist && (best < 0 || c < best)) {
				best = c
				bestDist = d
			}
		}
	}
	if best >= 0 {
		return best
	}
	if value < minDimension {
		return minDimension
	}
	return maxDimension
}

func parseSize(size string) (int, int) {
	parts := strings.SplitN(size, "x", 2)
	if len(parts) != 2 {
		return 1024, 1024
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	if w < 1 {
		w = 1024
	}
	if h < 1 {
		h = 1024
	}
	return w, h
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
