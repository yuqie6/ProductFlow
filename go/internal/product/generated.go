package product

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/storage"
)

// Write 把图运行生成的图片写成商品图片身份（origin=workflow_generation）。
func (s Service) Write(ctx context.Context, tx pgx.Tx, in graph.GeneratedImageInput) (string, error) {
	var compensation storage.Compensation
	obj, err := s.Media.Stage(ctx, tx, in.Bytes, in.MIME, &compensation)
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
	compensation.Release()
	return asset.ID, nil
}
