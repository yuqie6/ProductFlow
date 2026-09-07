package brand

import "time"

// View 是商家内 Brand 投影。
type View struct {
	ID             string    `json:"id"`
	MerchantID     string    `json:"merchant_id"`
	Name           string    `json:"name"`
	VisualSystemID *string   `json:"visual_system_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreateInput 新建品牌。
type CreateInput struct {
	Name           string
	VisualSystemID *string
}

// UpdateInput 部分更新；nil 指针表示不改该字段。
type UpdateInput struct {
	Name           *string
	VisualSystemID **string // 外层 nil=不改；内层 nil=清空挂接
}
