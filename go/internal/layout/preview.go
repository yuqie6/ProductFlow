package layout

import "fmt"

// Frame 是预览侧层外接框（与导出 Plan 对齐）。
type Frame struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	ZIndex int    `json:"z_index"`
}

// PreviewFrames 从 Document 提取层框，供浏览器预览与导出 Plan 比较。
// 不调度 Graph；不声称字形级像素与 Go 栅格完全一致——框级上限见选型备忘。
func PreviewFrames(doc Document) []Frame {
	layers := sortedLayers(doc.Layers)
	out := make([]Frame, 0, len(layers))
	for _, layer := range layers {
		out = append(out, Frame{
			ID: layer.ID, Type: layer.Type,
			X: layer.X, Y: layer.Y, Width: layer.Width, Height: layer.Height,
			ZIndex: layer.ZIndex,
		})
	}
	return out
}

// CompareFrames 检查预览框与导出 Plan 层框；tolerancePx 为各边允许差（备忘上限 1）。
func CompareFrames(preview []Frame, plan LayoutPlan, tolerancePx int) error {
	if len(preview) != len(plan.Layers) {
		return fmt.Errorf("层数不一致 preview=%d plan=%d", len(preview), len(plan.Layers))
	}
	for i := range preview {
		p := preview[i]
		l := plan.Layers[i]
		if p.ID != l.ID || p.Type != l.Type {
			return fmt.Errorf("层 %d id/type 不一致", i)
		}
		if abs(p.X-l.X) > tolerancePx || abs(p.Y-l.Y) > tolerancePx ||
			abs(p.Width-l.Width) > tolerancePx || abs(p.Height-l.Height) > tolerancePx {
			return fmt.Errorf("层 %q 框差超限 preview=(%d,%d,%d,%d) plan=(%d,%d,%d,%d)",
				p.ID, p.X, p.Y, p.Width, p.Height, l.X, l.Y, l.Width, l.Height)
		}
	}
	return nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
