// Package imageeval 是内部生图质量测评：淘宝完整套图对照、一句直调对照、多模态打分闸门。
//
// 金标 bytes 只进评委，不进 image provider 参考输入。原始图落 STORAGE_ROOT/image-evals/，不提交仓库。
package imageeval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	RunSwitch           = "PRODUCTFLOW_RUN_IMAGE_EVALS"
	StorageDir          = "image-evals"
	PoolDir             = "pool"
	RunsDir             = "runs"
	MinGalleryImages    = 5
	MinDetailImages     = 8
	MinDistinctTypes    = 4
	MinRefImages        = 1
	MaxRefImages        = 6
	MinPixelEdge        = 400
	MinImageBytes       = 8 * 1024
	MinTypeConfidence   = 0.5
	ScoreMin            = 1
	ScoreMax            = 5
	JudgeK              = 1
	maxImagesPerListing = 48
)

// GeneratingTypes 是测评会建画布节点的图种。证据类不生成。
var GeneratingTypes = []string{
	"hero", "selling_point", "scene", "detail", "sku",
	"specifications", "dimensions", "packaging",
}

// RequiredGoldTypes 是完整套图金标最低图种：封面、卖点、以及细节或场景之一。
var RequiredGoldTypes = []string{"hero", "selling_point"}

// ExtractListing 是浏览器从淘宝详情页抽出的中间 JSON，尚未下载 bytes。
type ExtractListing struct {
	Source     string            `json:"source"`
	URL        string            `json:"url"`
	Title      string            `json:"title"`
	Shop       string            `json:"shop"`
	Category   string            `json:"category"`
	Query      string            `json:"query"`
	IsTmall    bool              `json:"is_tmall"`
	SalesHint  string            `json:"sales_hint"`
	Props      map[string]string `json:"props"`
	Gallery    []ExtractImage    `json:"gallery"`
	SKUImages  []ExtractImage    `json:"sku_images"`
	DetailImgs []ExtractImage    `json:"detail_images"`
}

// ExtractImage 是详情页上一张可见图的 CDN 地址。
type ExtractImage struct {
	URL           string `json:"url"`
	Alt           string `json:"alt"`
	Label         string `json:"label"`
	Role          string `json:"role"` // gallery | sku | detail
	Index         int    `json:"index"`
	SuggestedType string `json:"suggested_type,omitempty"`
}

// PoolImage 是已下载并分槽的一张图。
type PoolImage struct {
	Path       string  `json:"path"`
	SHA256     string  `json:"sha256"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	ByteSize   int     `json:"byte_size"`
	TypeKey    string  `json:"type_key"`
	Confidence float64 `json:"confidence"`
	SourceURL  string  `json:"source_url"`
	Role       string  `json:"role"`
}

// Manifest 是过线 case 的可提交元数据（不含像素）。
type Manifest struct {
	ID           string            `json:"id"`
	Source       string            `json:"source"`
	URL          string            `json:"url"`
	Title        string            `json:"title"`
	Shop         string            `json:"shop"`
	Category     string            `json:"category"`
	Query        string            `json:"query"`
	IsTmall      bool              `json:"is_tmall"`
	SalesHint    string            `json:"sales_hint"`
	Props        map[string]string `json:"props"`
	AdmittedAt   time.Time         `json:"admitted_at"`
	References   []PoolImage       `json:"references"`
	Gold         []PoolImage       `json:"gold"`
	ImageTypes   []TypeCount       `json:"image_types"`
	RejectReason string            `json:"reject_reason,omitempty"`
}

// TypeCount 是画布起始的图种张数。
type TypeCount struct {
	Key      string `json:"key"`
	Quantity int    `json:"quantity"`
}

// Scores 是评委四维 1–5。
type Scores struct {
	Fidelity   float64 `json:"fidelity"`
	Fit        float64 `json:"fit"`
	Utility    float64 `json:"utility"`
	Aesthetics float64 `json:"aesthetics"`
}

// Mean 是四维均分。
func (s Scores) Mean() float64 {
	return (s.Fidelity + s.Fit + s.Utility + s.Aesthetics) / 4
}

// SlotJudgement 是一个图种槽位的评委结果。
type SlotJudgement struct {
	ImageType   string   `json:"image_type"`
	Index       int      `json:"index"`
	Workbench   Scores   `json:"workbench"`
	Gold        *Scores  `json:"gold"`
	Naive       Scores   `json:"naive"`
	VsGold      string   `json:"vs_gold"`  // win | tie | lose | none
	VsNaive     string   `json:"vs_naive"` // win | tie | lose
	Notes       string   `json:"notes"`
	Passed      bool     `json:"passed"`
	FailReasons []string `json:"fail_reasons"`
}

// CaseReport 是一个 listing 的测评结果。
type CaseReport struct {
	CaseID    string          `json:"case_id"`
	Passed    bool            `json:"passed"`
	Slots     []SlotJudgement `json:"slots"`
	ProductID string          `json:"product_id,omitempty"`
	GraphID   string          `json:"graph_id,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// RunReport 是一次抽样测评。
type RunReport struct {
	RunID      string       `json:"run_id"`
	Commit     string       `json:"commit"`
	Seed       int64        `json:"seed"`
	N          int          `json:"n"`
	ModelImage string       `json:"image_model"`
	ModelJudge string       `json:"judge_model"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	Passed     bool         `json:"passed"`
	PassCount  int          `json:"pass_count"`
	FailCount  int          `json:"fail_count"`
	Cases      []CaseReport `json:"cases"`
	Command    string       `json:"command"`
}

// CategorySeeds 是淘宝检索分层，不用猜你喜欢。
var CategorySeeds = []struct {
	Key     string
	Queries []string
}{
	{Key: "3c", Queries: []string{"无线降噪耳机", "机械键盘", "无线鼠标"}},
	{Key: "appliance", Queries: []string{"空气炸锅", "无线吸尘器", "电热水壶"}},
	{Key: "home", Queries: []string{"陶瓷马克杯", "实木餐椅", "纯棉四件套"}},
	{Key: "womenswear", Queries: []string{"真丝衬衫女", "羊毛大衣女", "牛仔裤女"}},
	{Key: "menswear", Queries: []string{"纯棉T恤男", "休闲西裤男", "牛津纺衬衫男"}},
	{Key: "beauty", Queries: []string{"玻璃精华瓶", "防晒霜面部", "氨基酸洗面奶"}},
	{Key: "food", Queries: []string{"坚果礼盒", "罐装茶叶", "纯可可脂巧克力"}},
	{Key: "baby", Queries: []string{"婴儿推车", "玻璃奶瓶", "纯棉纱布浴巾"}},
	{Key: "sports", Queries: []string{"跑步鞋男", "瑜伽垫加厚", "速干运动T恤"}},
}

func CaseIDFromURL(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])[:16]
}

func FileSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ValidateScores(s Scores) error {
	for _, v := range []float64{s.Fidelity, s.Fit, s.Utility, s.Aesthetics} {
		if v < ScoreMin || v > ScoreMax {
			return fmt.Errorf("score %.2f out of %d-%d", v, ScoreMin, ScoreMax)
		}
	}
	return nil
}

func IsGeneratingType(key string) bool {
	for _, item := range GeneratingTypes {
		if item == key {
			return true
		}
	}
	return false
}
