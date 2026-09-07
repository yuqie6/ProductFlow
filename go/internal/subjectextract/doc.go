// Package subjectextract 提供参考图主体蒙版/抠图与最小合成管线（IQ-CF-04）。
//
// 选型（任务证据）：仓库无现成抠图 provider。B0 默认引擎为角点采样背景色差分割
// （corner_chroma_local）：适合棚拍/近似纯色底的商品参考图，产出可核验 mask/cutout
// 字节与 SHA-256。B1 ComposeFromExtract：cutout + 纯色/垂直渐变背景 + 可选接触阴影占位
// + 安全区内等比放置 → PNG + subject_compose lineage；失败强制 route_qualified=false。
// 不宣称绝对像素保真；透明/反光/复杂背景须后续质检加深。禁止空 stub 恒 pass。
package subjectextract
