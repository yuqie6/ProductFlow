package schema

import "time"

// Users 对应表 users。可认证自然人账号；站点 Operator 用 is_operator，不复用 admin cookie。
type Users struct {
	ID           string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Email        string    `gorm:"column:email;type:varchar(320);not null"`
	PasswordHash string    `gorm:"column:password_hash;type:varchar(255);not null"`
	DisplayName  string    `gorm:"column:display_name;type:varchar(160);not null"`
	IsOperator   bool      `gorm:"column:is_operator;type:boolean;not null;default:false"`
	Status       string    `gorm:"column:status;type:varchar(32);not null"`
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (Users) TableName() string { return "users" }

// Merchants 对应表 merchants。租户根；B0 仅允许实例内唯一可运营商家。
type Merchants struct {
	ID        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Name      string    `gorm:"column:name;type:varchar(160);not null"`
	Status    string    `gorm:"column:status;type:varchar(32);not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (Merchants) TableName() string { return "merchants" }

// Memberships 对应表 memberships。用户在商家的角色与状态；授权每次读当前行。
type Memberships struct {
	ID         string     `gorm:"column:id;type:varchar(36);primaryKey"`
	MerchantID string     `gorm:"column:merchant_id;type:varchar(36);not null"`
	UserID     string     `gorm:"column:user_id;type:varchar(36);not null"`
	Role       string     `gorm:"column:role;type:varchar(32);not null"`
	Status     string     `gorm:"column:status;type:varchar(32);not null"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;type:timestamptz;not null"`
	RevokedAt  *time.Time `gorm:"column:revoked_at;type:timestamptz"`
}

func (Memberships) TableName() string { return "memberships" }

// MerchantInvites 对应表 merchant_invites。邀请制开通；token 只存哈希。
type MerchantInvites struct {
	ID         string     `gorm:"column:id;type:varchar(36);primaryKey"`
	MerchantID string     `gorm:"column:merchant_id;type:varchar(36);not null"`
	Email      string     `gorm:"column:email;type:varchar(320);not null"`
	Role       string     `gorm:"column:role;type:varchar(32);not null"`
	TokenHash  string     `gorm:"column:token_hash;type:varchar(64);not null"`
	InvitedBy  string     `gorm:"column:invited_by;type:varchar(36);not null"`
	ExpiresAt  time.Time  `gorm:"column:expires_at;type:timestamptz;not null"`
	AcceptedAt *time.Time `gorm:"column:accepted_at;type:timestamptz"`
	RevokedAt  *time.Time `gorm:"column:revoked_at;type:timestamptz"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
}

func (MerchantInvites) TableName() string { return "merchant_invites" }

// AuthSessions 对应表 auth_sessions。可撤销密码会话；cookie 只存 session id。
type AuthSessions struct {
	ID        string     `gorm:"column:id;type:varchar(36);primaryKey"`
	UserID    string     `gorm:"column:user_id;type:varchar(36);not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;type:timestamptz;not null"`
	RevokedAt *time.Time `gorm:"column:revoked_at;type:timestamptz"`
	CreatedAt time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
}

func (AuthSessions) TableName() string { return "auth_sessions" }
