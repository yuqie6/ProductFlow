package schema

import "time"

// Users 对应表 users。可认证自然人账号；站点 Operator 用 is_operator，不复用 admin cookie。
type Users struct {
	ID           string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Email        string    `gorm:"column:email;type:varchar(320);not null"`
	PasswordHash string    `gorm:"column:password_hash;type:varchar(255);not null"`
	DisplayName  string    `gorm:"column:display_name;type:varchar(160);not null"`
	IsOperator   bool      `gorm:"column:is_operator;type:boolean;not null;default:false"`
	MerchantID   *string   `gorm:"column:merchant_id;type:varchar(36)"`
	Status       string    `gorm:"column:status;type:varchar(32);not null"`
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (Users) TableName() string { return "users" }

// Merchants 对应表 merchants。账号商品、素材、任务和额度的隔离根。
type Merchants struct {
	ID        string    `gorm:"column:id;type:varchar(36);primaryKey"`
	Name      string    `gorm:"column:name;type:varchar(160);not null"`
	Status    string    `gorm:"column:status;type:varchar(32);not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null"`
}

func (Merchants) TableName() string { return "merchants" }

// RegistrationChallenges 对应表 registration_challenges；验证码只存 bcrypt 哈希。
type RegistrationChallenges struct {
	ID                string     `gorm:"column:id;type:varchar(36);primaryKey"`
	Email             string     `gorm:"column:email;type:varchar(320);not null"`
	CodeHash          string     `gorm:"column:code_hash;type:varchar(255);not null"`
	FailedAttempts    int        `gorm:"column:failed_attempts;type:integer;not null;default:0"`
	ResendAvailableAt time.Time  `gorm:"column:resend_available_at;type:timestamptz;not null"`
	ExpiresAt         time.Time  `gorm:"column:expires_at;type:timestamptz;not null"`
	ConsumedAt        *time.Time `gorm:"column:consumed_at;type:timestamptz"`
	CreatedAt         time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
}

func (RegistrationChallenges) TableName() string { return "registration_challenges" }

// PasswordRecoveryChallenges 对应表 password_recovery_challenges；密码恢复验证码只存 bcrypt 哈希。
// 该表与注册 challenge 分开，避免两个用途共享消费与重放语义。
type PasswordRecoveryChallenges struct {
	ID                string     `gorm:"column:id;type:varchar(36);primaryKey"`
	UserID            string     `gorm:"column:user_id;type:varchar(36);not null"`
	CodeHash          string     `gorm:"column:code_hash;type:varchar(255);not null"`
	FailedAttempts    int        `gorm:"column:failed_attempts;type:integer;not null;default:0"`
	ResendAvailableAt time.Time  `gorm:"column:resend_available_at;type:timestamptz;not null"`
	ExpiresAt         time.Time  `gorm:"column:expires_at;type:timestamptz;not null"`
	ConsumedAt        *time.Time `gorm:"column:consumed_at;type:timestamptz"`
	CreatedAt         time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
}

func (PasswordRecoveryChallenges) TableName() string { return "password_recovery_challenges" }

// AuthSessions 对应表 auth_sessions。可撤销密码会话；cookie 只存 session id。
type AuthSessions struct {
	ID        string     `gorm:"column:id;type:varchar(36);primaryKey"`
	UserID    string     `gorm:"column:user_id;type:varchar(36);not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;type:timestamptz;not null"`
	RevokedAt *time.Time `gorm:"column:revoked_at;type:timestamptz"`
	CreatedAt time.Time  `gorm:"column:created_at;type:timestamptz;not null"`
}

func (AuthSessions) TableName() string { return "auth_sessions" }
