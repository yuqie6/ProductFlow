package graph

import (
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// validateChangeSet 在 Apply 前做封闭 op 表与 client_ref 去重。空 ops 或未知类型返回 Validation。
// 公开 ChangeSet 的 DocumentOrigin / 禁改 key 在这里拦。Catalog 字段校验在 Apply 之后，不要提前放进来。
func validateChangeSet(cs ChangeSet) error {
	if len(cs.Operations) < 1 {
		return apperr.Validation("不支持的 Graph 操作")
	}
	seen := map[string]struct{}{}
	for _, operation := range cs.Operations {
		switch op := operation.(type) {
		case CreateNodeOp:
			if err := rejectForbiddenKeys(op.Config); err != nil {
				return err
			}
			if err := validateDocumentOriginMetadata(op.NodeType, op.DocumentOrigin); err != nil {
				return err
			}
			if err := claimCreateRef(seen, op.ClientRef); err != nil {
				return err
			}
		case CreateGroupOp:
			if err := claimCreateRef(seen, op.ClientRef); err != nil {
				return err
			}
		case ConnectNodesOp:
			if err := claimCreateRef(seen, op.ClientRef); err != nil {
				return err
			}
		case UpdateNodeConfigOp:
			if err := rejectForbiddenKeys(op.Config); err != nil {
				return err
			}
			if op.DocumentOrigin != nil {
				if _, ok := validDocumentOrigin(*op.DocumentOrigin); !ok {
					return apperr.Validation("不支持的 Graph 操作")
				}
			}
		case RenameNodeOp, DeleteNodeOp, DisconnectEdgeOp, MoveNodesOp, MoveNodesToGroupOp, RenameGroupOp, DissolveGroupOp, ReorderEdgesOp:
		default:
			return apperr.Validation("不支持的 Graph 操作")
		}
	}
	return nil
}

func validateDocumentOriginMetadata(nodeType NodeType, origin *string) error {
	if origin == nil {
		return nil
	}
	if !isContentNodeType(nodeType) {
		return apperr.Validation("不支持的 Graph 操作")
	}
	if _, ok := validDocumentOrigin(*origin); !ok {
		return apperr.Validation("不支持的 Graph 操作")
	}
	return nil
}

func claimCreateRef(seen map[string]struct{}, ref string) error {
	ref = strings.TrimSpace(ref)
	if _, ok := seen[ref]; ok {
		return apperr.Validation("ChangeSet 内部 client_ref 不能重复")
	}
	seen[ref] = struct{}{}
	return nil
}
