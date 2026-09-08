package schema

import "time"

// OperatorProductActions retains authorized product management attempts even after deletion.
// No foreign key to the target product: deletion must preserve the audit snapshot.
type OperatorProductActions struct {
	ID            string    `gorm:"column:id;type:varchar(36);primaryKey"`
	ActorUserID   string    `gorm:"column:actor_user_id;type:varchar(36);not null"`
	ActorName     string    `gorm:"column:actor_name;type:varchar(160);not null"`
	MerchantID    string    `gorm:"column:merchant_id;type:varchar(36);not null"`
	ProductID     string    `gorm:"column:product_id;type:varchar(36);not null"`
	ProductName   string    `gorm:"column:product_name;type:varchar(255);not null"`
	Action        string    `gorm:"column:action;type:varchar(40);not null"`
	CreatedAt     time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	Result        string    `gorm:"column:result;type:varchar(16);not null"`
	FailureReason *string   `gorm:"column:failure_reason;type:text"`
}

func (OperatorProductActions) TableName() string { return "operator_product_actions" }
