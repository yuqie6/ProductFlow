package product

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

type seededGallery struct {
	productID string
	folderID  string
	otherID   string
	assets    []string
	now       time.Time
}

func seedGallery(t *testing.T, ps *productServer) seededGallery {
	t.Helper()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	productID := clockid.New()
	otherID := clockid.New()
	folderID := clockid.New()
	otherFolderID := clockid.New()
	err := tx.With(ctx, ps.pool, func(pgxTx pgx.Tx) error {
		if _, err := pgxTx.Exec(ctx, `
			INSERT INTO products (id, name, created_at, updated_at) VALUES ($1, '图库测试商品', $2, $2), ($3, '其他商品', $2, $2)
		`, productID, now, otherID); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `
			INSERT INTO product_asset_folders (id, product_id, name, sort_order, created_at, updated_at)
			VALUES ($1, $2, '精选', 0, $3, $3), ($4, $5, '其他目录', 0, $3, $3)
		`, folderID, productID, now, otherFolderID, otherID); err != nil {
			return err
		}
		type spec struct {
			name, origin string
			imageType    *string
			folder       *string
			created      time.Time
		}
		hero := "hero"
		specs := []spec{
			{"上传 %_ 参考", "upload", nil, &folderID, now.Add(-40 * 24 * time.Hour)},
			{"主图 Alpha", "workflow_generation", &hero, nil, now.Add(-10 * 24 * time.Hour)},
			{"主图 beta", "workflow_generation", &hero, &folderID, now.Add(-1 * 24 * time.Hour)},
			{"会话生成", "image_session_attach", nil, nil, now.Add(-2 * 24 * time.Hour)},
			{"补充参考", "upload", nil, nil, now.Add(-3 * 24 * time.Hour)},
		}
		for i, item := range specs {
			mediaID := clockid.New()
			assetID := clockid.New()
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO media_objects (
					id, storage_path, mime_type, byte_size, width, height, sha256,
					verification_status, created_at, verified_at
				) VALUES ($1, $2, 'image/png', 1024, 100, 100, $3, 'verified', $4, $4)
			`, mediaID, "gallery/"+productID+"/"+item.name+".png", strings.Repeat("a", 64), item.created); err != nil {
				return err
			}
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO product_image_assets (
					id, product_id, media_object_id, origin_type, display_name, original_filename,
					image_type_key, user_folder_id, created_at, updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
			`, assetID, productID, mediaID, item.origin, item.name, item.name+".png", item.imageType, item.folder, item.created); err != nil {
				return err
			}
			_ = i
			specs[i].name = assetID
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 再读一次 id，上面循环把 name 改成了 asset id。
	rows, err := ps.pool.Query(ctx, `
		SELECT id FROM product_image_assets WHERE product_id = $1 ORDER BY created_at ASC, id ASC
	`, productID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	ps.svc.Now = freeze(now)
	t.Cleanup(func() {
		_, _ = ps.pool.Exec(ctx, `DELETE FROM products WHERE id = ANY($1)`, []string{productID, otherID})
	})
	return seededGallery{productID: productID, folderID: folderID, otherID: otherFolderID, assets: ids, now: now}
}

func TestGalleryDirectoriesBootstrapSearchAndDetail(t *testing.T) {
	ps := newProductServer(t)
	seed := seedGallery(t, ps)
	boot, err := ps.svc.GalleryBootstrap(context.Background(), seed.productID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, item := range boot.SystemDirectories {
		counts[item.Kind] = item.Count
	}
	if counts["all"] != 5 || counts["recent_generated"] != 3 || counts["uploads"] != 2 || counts["generated"] != 3 || counts["unorganized"] != 3 {
		t.Fatalf("%+v", counts)
	}
	if boot.UnorganizedCount != 3 || len(boot.UserFolders) != 1 || boot.UserFolders[0].Count != 2 {
		t.Fatalf("%+v", boot)
	}
	if len(boot.ImageTypes) != 2 || boot.ImageTypes[0].DirectoryKey != galleryUnclassifiedTypeKey || boot.ImageTypes[1].DirectoryKey != "hero" {
		t.Fatalf("%+v", boot.ImageTypes)
	}

	expect := map[string]int{
		"uploads": 2, "generated": 3, "recent_generated": 3, "unorganized": 3,
	}
	for kind, want := range expect {
		page, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{DirectoryKind: kind, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != want {
			t.Fatalf("%s got %d want %d", kind, len(page.Items), want)
		}
	}
	hero, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{DirectoryKind: "image_type", DirectoryKey: "hero", Limit: 50})
	if err != nil || len(hero.Items) != 2 {
		t.Fatalf("hero %+v %v", hero, err)
	}
	unclassified, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{DirectoryKind: "image_type", DirectoryKey: galleryUnclassifiedTypeKey, Limit: 50})
	if err != nil || len(unclassified.Items) != 3 {
		t.Fatalf("unclassified %+v %v", unclassified, err)
	}
	folder, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{DirectoryKind: "user_folder", DirectoryKey: seed.folderID, Limit: 50})
	if err != nil || len(folder.Items) != 2 {
		t.Fatalf("folder %+v %v", folder, err)
	}
	literal, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{Query: "%_", Limit: 50})
	if err != nil || len(literal.Items) != 1 || literal.Items[0].DisplayName != "上传 %_ 参考" {
		t.Fatalf("literal %+v %v", literal, err)
	}
	filename, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{Query: "ALPHA.PNG", Limit: 50})
	if err != nil || len(filename.Items) != 1 || filename.Items[0].DisplayName != "主图 Alpha" {
		t.Fatalf("filename %+v %v", filename, err)
	}

	_, err = ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{DirectoryKind: "user_folder", DirectoryKey: seed.otherID, Limit: 50})
	if !isAppErr(err, 404, "商品图片文件夹不存在") {
		t.Fatalf("%v", err)
	}
	_, err = ps.svc.GetGalleryAsset(context.Background(), seed.productID, "missing")
	if !isAppErr(err, 404, "商品图片不存在") {
		t.Fatalf("%v", err)
	}
}

func TestGalleryCursorBoundToFiltersAndSorts(t *testing.T) {
	ps := newProductServer(t)
	seed := seedGallery(t, ps)
	for _, sort := range []string{"created_asc", "created_desc", "name_asc", "name_desc"} {
		ids := collectGalleryIDs(t, ps, seed.productID, sort)
		if len(ids) != 5 || len(unique(ids)) != 5 {
			t.Fatalf("%s %v", sort, ids)
		}
	}
	first, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{Limit: 1})
	if err != nil || first.NextCursor == nil {
		t.Fatal(err)
	}
	_, err = ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{Query: "主图", After: *first.NextCursor, Limit: 1})
	if !isAppErr(err, 400, "图库分页 cursor 与当前查询条件不匹配") {
		t.Fatalf("%v", err)
	}
	_, err = ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{Sort: "created_asc", After: *first.NextCursor, Limit: 1})
	if !isAppErr(err, 400, "图库分页 cursor 与当前查询条件不匹配") {
		t.Fatalf("%v", err)
	}
	_, err = ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{After: "not-a-cursor", Limit: 1})
	if !isAppErr(err, 400, "图库分页 cursor 无效") {
		t.Fatalf("%v", err)
	}
}

func TestRecentGeneratedCursorKeepsTimeAnchor(t *testing.T) {
	ps := newProductServer(t)
	seed := seedGallery(t, ps)
	first, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{
		DirectoryKind: "recent_generated", Sort: "created_asc", Limit: 1,
	})
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("%+v %v", first, err)
	}
	ps.svc.Now = freeze(seed.now.Add(60 * 24 * time.Hour))
	second, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{
		DirectoryKind: "recent_generated", Sort: "created_asc", Limit: 10, After: *first.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 2 {
		t.Fatalf("anchor leaked future filter: %+v", second.Items)
	}
}

func TestGalleryRejectsInvalidDirectoryKeys(t *testing.T) {
	ps := newProductServer(t)
	seed := seedGallery(t, ps)
	_, err := ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{DirectoryKind: "image_type"})
	if !isAppErr(err, 400, "当前图库目录必须提供 directory_key") {
		t.Fatalf("%v", err)
	}
	_, err = ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{DirectoryKind: "all", DirectoryKey: "hero"})
	if !isAppErr(err, 400, "当前图库目录不接受 directory_key") {
		t.Fatalf("%v", err)
	}
	_, err = ps.svc.ListGalleryAssets(context.Background(), seed.productID, GalleryListInput{DirectoryKind: "source", DirectoryKey: "unknown"})
	if !isAppErr(err, 400, "图片来源 directory_key 无效") {
		t.Fatalf("%v", err)
	}
}

func TestGalleryFolderOptimisticLockAndMoveAtomic(t *testing.T) {
	ps := newProductServer(t)
	seed := seedGallery(t, ps)
	first, err := ps.svc.CreateGalleryFolder(context.Background(), seed.productID, "  新目录  ")
	if err != nil || first.Name != "新目录" {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := ps.svc.CreateGalleryFolder(context.Background(), seed.productID, "第二目录")
	if err != nil || first.SortOrder >= second.SortOrder {
		t.Fatalf("%+v %+v %v", first, second, err)
	}
	_, err = ps.svc.RenameGalleryFolder(context.Background(), seed.productID, first.ID, "新目录", "精选二组")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ps.svc.RenameGalleryFolder(context.Background(), seed.productID, first.ID, "新目录", "不会生效")
	if !isAppErr(err, 409, "文件夹名称已被其他操作修改") {
		t.Fatalf("%v", err)
	}
	target, err := ps.svc.CreateGalleryFolder(context.Background(), seed.productID, "目标目录")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ps.svc.MoveGalleryAssets(context.Background(), seed.productID, []GalleryAssetMove{
		{AssetID: seed.assets[0], ExpectedFolderID: &seed.folderID},
		{AssetID: seed.assets[1], ExpectedFolderID: nil},
	}, &target.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ps.svc.MoveGalleryAssets(context.Background(), seed.productID, []GalleryAssetMove{
		{AssetID: seed.assets[0], ExpectedFolderID: &seed.folderID},
		{AssetID: seed.assets[2], ExpectedFolderID: &seed.folderID},
	}, nil)
	if !isAppErr(err, 409, "图片所在文件夹已被其他操作修改") {
		t.Fatalf("%v", err)
	}
	deleted, err := ps.svc.DeleteGalleryFolder(context.Background(), seed.productID, target.ID, "目标目录")
	if err != nil || deleted.MovedToUnorganizedCount != 2 {
		t.Fatalf("%+v %v", deleted, err)
	}
}

func collectGalleryIDs(t *testing.T, ps *productServer, productID, sort string) []string {
	t.Helper()
	var ids []string
	after := ""
	for {
		page, err := ps.svc.ListGalleryAssets(context.Background(), productID, GalleryListInput{Sort: sort, After: after, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}
		if page.NextCursor == nil {
			return ids
		}
		after = *page.NextCursor
	}
}

func unique(ids []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func isAppErr(err error, status int, detail string) bool {
	got, ok := err.(apperr.Error)
	return ok && got.Status == status && strings.Contains(got.Detail, detail)
}
