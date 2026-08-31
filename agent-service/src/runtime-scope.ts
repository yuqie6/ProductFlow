import {
  type ProductFlowContract,
  type Scope,
  resolvedToolContractVersion,
  validateScope,
} from "./contracts.js";
import type { RuntimeLookup } from "./runtime-manager.js";
import { RuntimeError } from "./store.js";

/** ProductFlow contract 是 runtime scope 的唯一来源；本地 session 文件不能改写 scope。 */
export function scopeFromContract(contract: ProductFlowContract, lookup: RuntimeLookup): Scope {
  const expectedVersion = resolvedToolContractVersion(contract.draft_schema);
  if (contract.schema_version !== 1 || contract.tool_contract_version !== expectedVersion) {
    throw new RuntimeError(
      502,
      "contract_mismatch",
      `ProductFlow Agent contract mismatch: schema_version=${contract.schema_version} tool_contract_version=${contract.tool_contract_version}`,
    );
  }
  const conversationID = contract.conversation_id.trim();
  const taskID = contract.task_id?.trim() || null;
  if (lookup.conversationID && conversationID !== lookup.conversationID) {
    throw new RuntimeError(502, "scope_mismatch", "ProductFlow returned a different conversation scope");
  }
  if (lookup.taskID && taskID !== lookup.taskID) {
    throw new RuntimeError(502, "scope_mismatch", "ProductFlow returned a different task scope");
  }
  if (contract.scope_type !== "global" && contract.scope_type !== "product_workflow") {
    throw new RuntimeError(502, "contract_mismatch", `ProductFlow returned an invalid Agent scope_type ${contract.scope_type}`);
  }
  const scope: Scope = {
    schema_version: 1,
    scope_type: contract.scope_type,
    conversation_id: conversationID,
    task_id: taskID,
    task_goal: contract.task_goal,
    product_id: contract.product_id?.trim() || null,
    run_id: contract.harness_run_id.trim(),
    system_prompt: contract.system_prompt,
    draft_schema: contract.draft_schema,
    current_draft_version: contract.current_draft_version,
    has_live_graph: Boolean(contract.has_live_graph),
  };
  try {
    validateScope(scope);
  } catch (error) {
    if (error instanceof RuntimeError) throw error;
    throw new RuntimeError(
      502,
      "contract_mismatch",
      error instanceof Error ? error.message : "ProductFlow returned an invalid Agent contract",
    );
  }
  return scope;
}
