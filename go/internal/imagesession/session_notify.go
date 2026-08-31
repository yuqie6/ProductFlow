package imagesession

import (
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"gorm.io/gorm"
)

func publishSession(ctx context.Context, tx *gorm.DB, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return notify.Publish(ctx, tx, notify.ChannelImageSession, sessionID)
}

func notifyTaskSession(ctx context.Context, tx *gorm.DB, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	var sessionID string
	if err := tx.WithContext(ctx).Model(&schema.ImageSessionGenerationTasks{}).
		Select("session_id").Where("id = ?", taskID).Limit(1).Scan(&sessionID).Error; err != nil {
		return err
	}
	return publishSession(ctx, tx, sessionID)
}
