package product

import (
	"context"
	"database/sql"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"gorm.io/gorm"
)

// Write 把图运行生成的图片写成商品图片身份（origin=workflow_generation）。
func (s Service) Write(ctx context.Context, tx *gorm.DB, in graph.GeneratedImageInput) (string, error) {
	compensation := &storage.Compensation{}
	obj, err := s.Media.Stage(ctx, tx, in.Bytes, in.MIME, compensation)
	if err != nil {
		compensation.Rollback()
		return "", err
	}
	filename := in.Filename
	if filename == "" {
		filename = in.Title + ".png"
	}
	asset, err := insertAssetOrigin(ctx, tx, in.ProductID, obj.ID, filename, "workflow_generation", in.ImageTypeKey)
	if err != nil {
		compensation.Rollback()
		return "", err
	}
	bindCompensation(tx, compensation)
	return asset.ID, nil
}

type compensatingConnPool struct {
	gorm.ConnPool
	files []*storage.Compensation
}

// SQLTx exposes the existing transaction for River InsertTx. Commit/Rollback
// still belong to this wrapper so generated-file compensation remains intact.
func (c *compensatingConnPool) SQLTx() *sql.Tx {
	tx, _ := c.ConnPool.(*sql.Tx)
	return tx
}

// Commit 先提交事务，失败则回滚已 stage 的媒体文件。
func (c *compensatingConnPool) Commit() error {
	committer, ok := c.ConnPool.(gorm.TxCommitter)
	if !ok {
		c.rollbackFiles()
		return gorm.ErrInvalidTransaction
	}
	if err := committer.Commit(); err != nil {
		c.rollbackFiles()
		return err
	}
	c.releaseFiles()
	return nil
}

// Rollback 回滚事务并删除已 stage 的媒体文件。
func (c *compensatingConnPool) Rollback() error {
	committer, ok := c.ConnPool.(gorm.TxCommitter)
	if !ok {
		c.rollbackFiles()
		return gorm.ErrInvalidTransaction
	}
	err := committer.Rollback()
	c.rollbackFiles()
	return err
}

func (c *compensatingConnPool) rollbackFiles() {
	for i := len(c.files) - 1; i >= 0; i-- {
		c.files[i].Rollback()
	}
	c.files = nil
}

func (c *compensatingConnPool) releaseFiles() {
	for _, file := range c.files {
		file.Release()
	}
	c.files = nil
}

func bindCompensation(tx *gorm.DB, compensation *storage.Compensation) {
	if tx == nil || tx.Statement == nil || compensation == nil {
		return
	}
	if existing, ok := tx.Statement.ConnPool.(*compensatingConnPool); ok {
		existing.files = append(existing.files, compensation)
		return
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		compensation.Release()
		return
	}
	tx.Statement.ConnPool = &compensatingConnPool{
		ConnPool: tx.Statement.ConnPool,
		files:    []*storage.Compensation{compensation},
	}
}
