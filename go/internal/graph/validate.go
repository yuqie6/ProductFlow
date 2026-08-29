package graph

import (
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

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
		case RenameNodeOp, DeleteNodeOp, DisconnectEdgeOp, MoveNodesOp, MoveNodesToGroupOp, RenameGroupOp, DissolveGroupOp:
		default:
			return apperr.Validation("不支持的 Graph 操作")
		}
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
