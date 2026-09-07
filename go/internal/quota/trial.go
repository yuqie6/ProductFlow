package quota

import (
	"os"
	"strconv"
	"strings"
)

// DefaultTrialUnits 是 QUOTA_TRIAL_UNITS 未设置时的默认试用额度（内部单位）。
// 自托管/本地默认可生成，避免新商家立刻「可用额度不足」；生产可设 0 强制 Op Adjust。
const DefaultTrialUnits int64 = 100

// TrialSeedIdempotencyKey 是首次建账写入试用种子事件的幂等键。
const TrialSeedIdempotencyKey = "quota-trial-seed"

// TrialSeedReason 是试用种子调账事件的固定原因。
const TrialSeedReason = "trial seed"

// TrialUnitsFromEnv 读取 QUOTA_TRIAL_UNITS；空或非法值回落 DefaultTrialUnits；允许显式 0。
func TrialUnitsFromEnv() int64 {
	raw := strings.TrimSpace(os.Getenv("QUOTA_TRIAL_UNITS"))
	if raw == "" {
		return DefaultTrialUnits
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return DefaultTrialUnits
	}
	return n
}
