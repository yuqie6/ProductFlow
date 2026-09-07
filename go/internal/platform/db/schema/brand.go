package schema

import "time"

// Brands 对应表 brands。
// 商家内品牌主档（ROADMAP §6.3 / Brand 实体 B0）；可挂接本商家 visual_systems 作为后续继承接线点。
type Brands struct {
	ID             string    `gorm:"column:id;type:varchar(36);primaryKey"`
	MerchantID     string    `gorm:"column:merchant_id;type:varchar(36);not null"`
	Name           string    `gorm:"column:name;type:varchar(160);not null"`
	VisualSystemID *string   `gorm:"column:visual_system_id;type:varchar(36)"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (Brands) TableName() string { return "brands" }
