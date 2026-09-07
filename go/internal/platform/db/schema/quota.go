package schema

import "time"

// QuotaPriceVersions 对应表 quota_price_versions。
// 平台级价格版本目录（MP-C 价格目录 B0）；id 由配额账本引用，≠法币/支付。
type QuotaPriceVersions struct {
	ID        string    `gorm:"column:id;type:varchar(80);primaryKey"`
	Label     string    `gorm:"column:label;type:varchar(200);not null"`
	Currency  string    `gorm:"column:currency;type:varchar(16);not null"`
	IsDefault bool      `gorm:"column:is_default;type:boolean;not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (QuotaPriceVersions) TableName() string { return "quota_price_versions" }

// QuotaPriceEntries 对应表 quota_price_entries。
// 某价格版本下入口/动作码 → 内部单位单价。
type QuotaPriceEntries struct {
	PriceVersionID string    `gorm:"column:price_version_id;type:varchar(80);primaryKey"`
	EntryCode      string    `gorm:"column:entry_code;type:varchar(80);primaryKey"`
	UnitPrice      int64     `gorm:"column:unit_price;type:bigint;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (QuotaPriceEntries) TableName() string { return "quota_price_entries" }

// MerchantQuotaAccounts 对应表 merchant_quota_accounts。
// 商家商业额度余额账本（MP-C B0）：available + reserved；与 agent_model_invocations 平台调用事实分离。
type MerchantQuotaAccounts struct {
	MerchantID      string    `gorm:"column:merchant_id;type:varchar(36);primaryKey"`
	Currency        string    `gorm:"column:currency;type:varchar(16);not null"`
	AvailableUnits  int64     `gorm:"column:available_units;type:bigint;not null"`
	ReservedUnits   int64     `gorm:"column:reserved_units;type:bigint;not null"`
	PriceVersionID  string    `gorm:"column:price_version_id;type:varchar(80);not null"`
	CreatedAt       time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt       time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (MerchantQuotaAccounts) TableName() string { return "merchant_quota_accounts" }

// MerchantQuotaHolds 对应表 merchant_quota_holds。
// 一次消费操作的预留行；状态机：reserved → settled|released|pending_reconciliation。
type MerchantQuotaHolds struct {
	ID              string     `gorm:"column:id;type:varchar(36);primaryKey"`
	MerchantID      string     `gorm:"column:merchant_id;type:varchar(36);not null"`
	IdempotencyKey  string     `gorm:"column:idempotency_key;type:varchar(200);not null"`
	AmountUnits     int64      `gorm:"column:amount_units;type:bigint;not null"`
	SettledUnits    *int64     `gorm:"column:settled_units;type:bigint"`
	Status          string     `gorm:"column:status;type:varchar(32);not null"`
	PriceVersionID  string     `gorm:"column:price_version_id;type:varchar(80);not null"`
	CreatedAt       time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (MerchantQuotaHolds) TableName() string { return "merchant_quota_holds" }

// MerchantQuotaEvents 对应表 merchant_quota_events。
// 追加式额度事件；不直接改余额掩盖历史。
type MerchantQuotaEvents struct {
	ID              string    `gorm:"column:id;type:varchar(36);primaryKey"`
	MerchantID      string    `gorm:"column:merchant_id;type:varchar(36);not null"`
	HoldID          *string   `gorm:"column:hold_id;type:varchar(36)"`
	EventType       string    `gorm:"column:event_type;type:varchar(32);not null"`
	AmountUnits     int64     `gorm:"column:amount_units;type:bigint;not null"`
	IdempotencyKey  string    `gorm:"column:idempotency_key;type:varchar(200);not null"`
	AvailableAfter  int64     `gorm:"column:available_after;type:bigint;not null"`
	ReservedAfter   int64     `gorm:"column:reserved_after;type:bigint;not null"`
	PriceVersionID  string    `gorm:"column:price_version_id;type:varchar(80);not null"`
	Reason          *string   `gorm:"column:reason;type:text"`
	ActorUserID     *string   `gorm:"column:actor_user_id;type:varchar(36)"`
	CreatedAt       time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (MerchantQuotaEvents) TableName() string { return "merchant_quota_events" }
