package schema

// EnumDDL 与 ExtraDDL 夹在 CreateTable/AddColumn 前后执行。每条语句必须幂等（IF NOT EXISTS 或 duplicate_object）。

// EnumDDL 创建 PostgreSQL enum，含 Alembic 历史上留下、代码已不用的类型，删掉会让旧库 Apply 失败。
var EnumDDL = []string{
	`DO $enum$ BEGIN
CREATE TYPE agentcheckpointkind AS ENUM ('before_model_request', 'tool_effect_intent', 'tool_effect_result', 'question_required', 'external_job_submitted', 'terminal', 'model_response_bound', 'model_response_cursor');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE agentconversationscope AS ENUM ('product_workflow', 'global');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE agentconversationstatus AS ENUM ('collecting', 'awaiting_confirmation', 'completed', 'failed', 'canceled', 'unknown');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE agentexecutionphase AS ENUM ('claimed', 'model', 'tool', 'waiting_input', 'external_job', 'terminal');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE agentsessionstatus AS ENUM ('active', 'archived');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE agenttaskstatus AS ENUM ('queued', 'running', 'waiting_user', 'awaiting_confirmation', 'succeeded', 'failed', 'canceled', 'paused', 'unknown');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE agenttoolmutationstatus AS ENUM ('prepared', 'applied', 'failed', 'unknown');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE agentturnstatus AS ENUM ('queued', 'running', 'requires_input', 'awaiting_confirmation', 'succeeded', 'failed', 'cancel_requested', 'canceled', 'unknown');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE agentworkflowrunrequeststatus AS ENUM ('awaiting_confirmation', 'confirmed', 'succeeded', 'failed', 'cancelled');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE asyncdispatchstatus AS ENUM ('pending', 'sent', 'consumed', 'dead');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE copystatus AS ENUM ('draft', 'confirmed');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE imagesessionassetkind AS ENUM ('reference_upload', 'generated_image');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE jobstatus AS ENUM ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'unknown');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE libraryorganizationdraftstatus AS ENUM ('awaiting_confirmation', 'confirmed', 'failed', 'cancelled');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE localimageedittaskstatus AS ENUM ('draft', 'queued', 'running', 'succeeded', 'failed', 'cancelled', 'unknown');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE mediaverificationstatus AS ENUM ('verified', 'legacy_pending', 'missing');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE posterkind AS ENUM ('main_image', 'promo_poster');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE productimageorigintype AS ENUM ('upload', 'workflow_generation', 'image_session_attach', 'local_edit');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE sourceassetkind AS ENUM ('original_image', 'reference_image', 'processed_product_image');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE workflowdraftstatus AS ENUM ('collecting', 'awaiting_confirmation', 'confirmed', 'materializing', 'ready', 'failed', 'cancelled');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE workflownodestatus AS ENUM ('queued', 'running', 'succeeded', 'failed', 'unknown', 'skipped', 'cancelled');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE workflownodetype AS ENUM ('product_context', 'reference_image', 'copy_generation', 'image_generation', 'image_prompt');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE workflowrecipecreationsource AS ENUM ('user_extract', 'official_seed');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE workflowrecipekind AS ENUM ('workflow_recipe', 'recipe_fragment');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE workflowrecipeorigin AS ENUM ('official', 'user');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE workflowrevealeventkind AS ENUM ('folder', 'node', 'edge', 'completed');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
	`DO $enum$ BEGIN
CREATE TYPE workflowrunstatus AS ENUM ('running', 'succeeded', 'failed', 'cancelled', 'unknown');
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $enum$;`,
}

// ExtraDDL 补上 CreateTable/AddColumn 不管的 CHECK/UNIQUE/FK 与索引。改约束只改这里，不要在模型 tag 里再写一份。
var ExtraDDL = []string{
	`CREATE INDEX IF NOT EXISTS ix_operator_product_actions_merchant_created ON operator_product_actions (merchant_id, created_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_operator_product_actions_product_created ON operator_product_actions (merchant_id, product_id, created_at DESC, id DESC);`,
	`DO $c$ BEGIN
ALTER TABLE products DROP CONSTRAINT IF EXISTS uq_products_creation_idempotency_key;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE products ADD CONSTRAINT uq_products_creation_idempotency_key UNIQUE (merchant_id, creation_idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE products ADD CONSTRAINT ck_products_creation_idempotency_pair CHECK ((creation_idempotency_key IS NULL AND creation_request_hash IS NULL) OR (creation_idempotency_key IS NOT NULL AND length(creation_idempotency_key) > 0 AND creation_request_hash IS NOT NULL AND length(creation_request_hash) = 64));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TYPE agentcheckpointkind ADD VALUE IF NOT EXISTS 'model_response_bound';
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TYPE agentcheckpointkind ADD VALUE IF NOT EXISTS 'model_response_cursor';
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_model_invocations ADD CONSTRAINT fk_agent_model_invocations_turn_projection_id FOREIGN KEY (turn_projection_id) REFERENCES agent_turn_projections(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_model_invocations ADD CONSTRAINT fk_agent_model_invocations_execution_id FOREIGN KEY (execution_id) REFERENCES agent_turn_executions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_model_invocations ADD CONSTRAINT uq_agent_model_invocations_request UNIQUE (turn_projection_id, model_request_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_model_invocations ADD CONSTRAINT ck_agent_model_invocations_values CHECK (execution_mode IN ('foreground', 'background') AND status IN ('started', 'completed', 'failed', 'interrupted') AND usage_source IN ('provider', 'estimated', 'unavailable') AND (duration_ms IS NULL OR duration_ms >= 0) AND (input_tokens IS NULL OR input_tokens >= 0) AND (output_tokens IS NULL OR output_tokens >= 0) AND (total_tokens IS NULL OR total_tokens >= 0));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT ck_agent_conversations_creation_idempotency_pair CHECK (creation_idempotency_key IS NULL AND creation_request_hash IS NULL OR creation_idempotency_key IS NOT NULL AND length(creation_idempotency_key::text) > 0 AND creation_request_hash IS NOT NULL AND length(creation_request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT ck_agent_conversations_intake_idempotency_pair CHECK (intake_idempotency_key IS NULL AND intake_request_hash IS NULL OR intake_idempotency_key IS NOT NULL AND length(intake_idempotency_key::text) > 0 AND intake_request_hash IS NOT NULL AND length(intake_request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT ck_agent_conversations_scope_fields CHECK (scope_type = 'product_workflow'::agentconversationscope AND product_id IS NOT NULL OR scope_type = 'global'::agentconversationscope AND product_id IS NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT ck_agent_conversations_scope_type CHECK (scope_type = ANY (ARRAY['product_workflow'::agentconversationscope, 'global'::agentconversationscope]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT fk_agent_conversations_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT fk_agent_conversations_session_id FOREIGN KEY (session_id) REFERENCES agent_sessions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT uq_agent_conversations_creation_idempotency_key UNIQUE (creation_idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT uq_agent_conversations_harness_run_id UNIQUE (harness_run_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_page_context_snapshots ADD CONSTRAINT ck_agent_page_context_snapshots_digest CHECK (length(digest::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_page_context_snapshots ADD CONSTRAINT fk_agent_page_context_snapshots_task_id FOREIGN KEY (task_id) REFERENCES agent_tasks(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_sessions ADD CONSTRAINT fk_agent_sessions_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tasks ADD CONSTRAINT fk_agent_tasks_conversation_id FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tasks ADD CONSTRAINT fk_agent_tasks_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tasks ADD CONSTRAINT fk_agent_tasks_session_id FOREIGN KEY (session_id) REFERENCES agent_sessions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tasks ADD CONSTRAINT uq_agent_tasks_harness_run_id UNIQUE (harness_run_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tool_mutations ADD CONSTRAINT ck_agent_tool_mutations_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tool_mutations ADD CONSTRAINT fk_agent_tool_mutations_asset_id FOREIGN KEY (asset_id) REFERENCES product_image_assets(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tool_mutations ADD CONSTRAINT fk_agent_tool_mutations_conversation_id FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tool_mutations ADD CONSTRAINT uq_agent_tool_mutations_conversation_tool_key UNIQUE (conversation_id, tool_name, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_checkpoints ADD CONSTRAINT ck_agent_turn_checkpoints_positive_attempt CHECK (attempt > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_checkpoints ADD CONSTRAINT ck_agent_turn_checkpoints_positive_fencing CHECK (fencing_token > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_checkpoints ADD CONSTRAINT ck_agent_turn_checkpoints_positive_sequence CHECK (sequence > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_checkpoints ADD CONSTRAINT fk_agent_turn_checkpoints_execution_id FOREIGN KEY (execution_id) REFERENCES agent_turn_executions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_checkpoints ADD CONSTRAINT fk_agent_turn_checkpoints_turn_projection_id FOREIGN KEY (turn_projection_id) REFERENCES agent_turn_projections(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_checkpoints ADD CONSTRAINT uq_agent_turn_checkpoints_execution_attempt_sequence UNIQUE (execution_id, attempt, sequence);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_effect_reconciliations ADD CONSTRAINT ck_agent_turn_effect_reconciliations_effect_result CHECK (effect_result::text = ANY (ARRAY['applied'::character varying, 'failed'::character varying, 'unknown'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_effect_reconciliations ADD CONSTRAINT ck_agent_turn_effect_reconciliations_state CHECK (reconciliation_state::text = ANY (ARRAY['applied'::character varying, 'not_applied'::character varying, 'conflict'::character varying, 'unknown'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_effect_reconciliations ADD CONSTRAINT fk_agent_turn_effect_reconciliations_turn_projection_id FOREIGN KEY (turn_projection_id) REFERENCES agent_turn_projections(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_effect_reconciliations ADD CONSTRAINT uq_agent_turn_effect_reconciliations_projection_tool UNIQUE (turn_projection_id, tool_call_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_events ADD CONSTRAINT ck_agent_turn_events_attempt CHECK (attempt IS NULL OR attempt > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_events ADD CONSTRAINT ck_agent_turn_events_fencing CHECK (fencing_token IS NULL OR fencing_token > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_events ADD CONSTRAINT ck_agent_turn_events_positive_sequence CHECK (sequence > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_events ADD CONSTRAINT ck_agent_turn_events_schema_version CHECK (schema_version = 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_events ADD CONSTRAINT ck_agent_turn_events_ignorable CHECK (ignorable IN (TRUE, FALSE));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_events ADD CONSTRAINT fk_agent_turn_events_execution_id FOREIGN KEY (execution_id) REFERENCES agent_turn_executions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_events ADD CONSTRAINT fk_agent_turn_events_turn_projection_id FOREIGN KEY (turn_projection_id) REFERENCES agent_turn_projections(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_events ADD CONSTRAINT uq_agent_turn_events_projection_sequence UNIQUE (turn_projection_id, sequence);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_executions ADD CONSTRAINT ck_agent_turn_executions_non_negative_attempt CHECK (attempt >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_executions ADD CONSTRAINT ck_agent_turn_executions_non_negative_fencing CHECK (fencing_token >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_executions ADD CONSTRAINT fk_agent_turn_executions_turn_projection_id FOREIGN KEY (turn_projection_id) REFERENCES agent_turn_projections(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_executions ADD CONSTRAINT uq_agent_turn_executions_projection_id UNIQUE (turn_projection_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT ck_agent_turn_projections_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT ck_agent_turn_projections_terminal_reason_code CHECK (terminal_reason_code IS NULL OR terminal_reason_code = ANY (ARRAY['provider_failed', 'execution_interrupted', 'effect_reconciled', 'effect_conflict', 'effect_unknown', 'persistence_failed']));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT fk_agent_turn_proj_library_org_draft_rev_id FOREIGN KEY (library_organization_draft_revision_id) REFERENCES library_organization_draft_revisions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT fk_agent_turn_projections_conversation_id FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT fk_agent_turn_projections_page_context_snapshot_id FOREIGN KEY (page_context_snapshot_id) REFERENCES agent_page_context_snapshots(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT fk_agent_turn_projections_task_id FOREIGN KEY (task_id) REFERENCES agent_tasks(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT fk_agent_turn_projections_workflow_run_request_id FOREIGN KEY (workflow_run_request_id) REFERENCES agent_workflow_run_requests(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT uq_agent_turn_proj_library_org_draft_rev_id UNIQUE (library_organization_draft_revision_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT uq_agent_turn_projections_conversation_key UNIQUE (conversation_id, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT uq_agent_turn_projections_harness_turn_id UNIQUE (harness_turn_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_turn_projections ADD CONSTRAINT uq_agent_turn_projections_workflow_run_request_id UNIQUE (workflow_run_request_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT ck_agent_workflow_run_requests_graph_required CHECK (graph_id IS NOT NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT ck_agent_workflow_run_requests_positive_revision CHECK (expected_workflow_revision > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT ck_agent_workflow_run_requests_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT ck_agent_workflow_run_requests_source_step_id CHECK (length(source_step_id::text) > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT fk_agent_workflow_run_requests_conversation_id FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT fk_agent_workflow_run_requests_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT fk_agent_workflow_run_requests_graph_run_id FOREIGN KEY (graph_run_id) REFERENCES workflow_graph_runs(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT fk_agent_workflow_run_requests_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT fk_agent_workflow_run_requests_source_graph_run_id FOREIGN KEY (source_graph_run_id) REFERENCES workflow_graph_runs(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT fk_agent_workflow_run_requests_task_id FOREIGN KEY (task_id) REFERENCES agent_tasks(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT uq_agent_workflow_run_requests_conversation_key UNIQUE (conversation_id, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE async_dispatches ADD CONSTRAINT ck_async_dispatches_non_negative_attempts CHECK (attempts >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE async_dispatches ADD CONSTRAINT ck_async_dispatches_status CHECK (status = ANY (ARRAY['pending'::asyncdispatchstatus, 'sent'::asyncdispatchstatus, 'consumed'::asyncdispatchstatus, 'dead'::asyncdispatchstatus]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE async_dispatches ADD CONSTRAINT uq_async_dispatches_delivery_key UNIQUE (delivery_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT ck_delivery_rendition_jobs_active_attempt CHECK (status = 'running'::jobstatus AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NULL OR status <> 'running'::jobstatus AND active_attempt_id IS NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT ck_delivery_rendition_jobs_non_negative_attempts CHECK (attempts >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT ck_delivery_rendition_jobs_result_state CHECK (status = 'succeeded'::jobstatus AND result_asset_id IS NOT NULL AND finished_at IS NOT NULL OR status = 'failed'::jobstatus AND result_asset_id IS NULL AND finished_at IS NOT NULL OR (status = ANY (ARRAY['queued'::jobstatus, 'running'::jobstatus])) AND result_asset_id IS NULL AND finished_at IS NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT ck_delivery_rendition_jobs_schema_version CHECK (spec_schema_version = 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT ck_delivery_rendition_jobs_spec_hash CHECK (length(spec_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT ck_delivery_rendition_jobs_status CHECK (status = ANY (ARRAY['queued'::jobstatus, 'running'::jobstatus, 'succeeded'::jobstatus, 'failed'::jobstatus]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT fk_delivery_rendition_jobs_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT fk_delivery_rendition_jobs_result_asset_id FOREIGN KEY (result_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT fk_delivery_rendition_jobs_source_asset_id FOREIGN KEY (source_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT uq_delivery_rendition_jobs_result_asset_id UNIQUE (result_asset_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT uq_delivery_rendition_jobs_source_spec UNIQUE (source_asset_id, spec_hash);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_assets ADD CONSTRAINT fk_image_session_assets_media_object_id FOREIGN KEY (media_object_id) REFERENCES media_objects(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_assets ADD CONSTRAINT image_session_assets_session_id_fkey FOREIGN KEY (session_id) REFERENCES image_sessions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_generation_tasks ADD CONSTRAINT ck_image_session_generation_tasks_active_attempt CHECK (status = 'running'::jobstatus AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NULL OR status <> 'running'::jobstatus AND active_attempt_id IS NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_generation_tasks ADD CONSTRAINT ck_image_session_generation_tasks_non_negative_attempts CHECK (attempts >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_generation_tasks ADD CONSTRAINT fk_image_session_generation_tasks_base_asset_id FOREIGN KEY (base_asset_id) REFERENCES image_session_assets(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_generation_tasks ADD CONSTRAINT image_session_generation_tasks_session_id_fkey FOREIGN KEY (session_id) REFERENCES image_sessions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_provider_effects ADD CONSTRAINT ck_image_session_provider_effects_candidate_count CHECK (candidate_count >= 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_provider_effects ADD CONSTRAINT ck_image_session_provider_effects_candidate_start CHECK (candidate_start_index >= 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_provider_effects ADD CONSTRAINT ck_image_session_provider_effects_effect_result CHECK (effect_result::text = ANY (ARRAY['pending'::character varying, 'applied'::character varying, 'failed'::character varying, 'unknown'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_provider_effects ADD CONSTRAINT ck_image_session_provider_effects_reconciliation_state CHECK (reconciliation_state::text = ANY (ARRAY['not_requested'::character varying, 'applied'::character varying, 'not_applied'::character varying, 'unknown'::character varying, 'unsupported'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_provider_effects ADD CONSTRAINT ck_image_session_provider_effects_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_provider_effects ADD CONSTRAINT fk_image_session_provider_effects_generation_task_id FOREIGN KEY (generation_task_id) REFERENCES image_session_generation_tasks(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_provider_effects ADD CONSTRAINT uq_image_session_provider_effects_operation_key UNIQUE (operation_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_provider_effects ADD CONSTRAINT uq_image_session_provider_effects_task_candidate UNIQUE (generation_task_id, candidate_start_index);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_rounds ADD CONSTRAINT fk_image_session_rounds_base_asset_id FOREIGN KEY (base_asset_id) REFERENCES image_session_assets(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_rounds ADD CONSTRAINT image_session_rounds_generated_asset_id_fkey FOREIGN KEY (generated_asset_id) REFERENCES image_session_assets(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_session_rounds ADD CONSTRAINT image_session_rounds_session_id_fkey FOREIGN KEY (session_id) REFERENCES image_sessions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_draft_revisions ADD CONSTRAINT ck_library_organization_draft_revisions_artifact_origin_pair CHECK (source_turn_id IS NULL AND source_artifact_step_id IS NULL OR source_turn_id IS NOT NULL AND source_artifact_step_id IS NOT NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_draft_revisions ADD CONSTRAINT ck_library_organization_draft_revisions_payload_hash CHECK (length(payload_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_draft_revisions ADD CONSTRAINT ck_library_organization_draft_revisions_positive_version CHECK (version > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_draft_revisions ADD CONSTRAINT ck_library_organization_draft_revisions_schema_version CHECK (schema_version = 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_draft_revisions ADD CONSTRAINT fk_library_organization_draft_revisions_draft_id FOREIGN KEY (draft_id) REFERENCES library_organization_drafts(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_draft_revisions ADD CONSTRAINT uq_library_organization_draft_revisions_artifact_origin UNIQUE (draft_id, source_turn_id, source_artifact_step_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_draft_revisions ADD CONSTRAINT uq_library_organization_draft_revisions_draft_version UNIQUE (draft_id, version);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_drafts ADD CONSTRAINT ck_library_organization_drafts_confirmation_pair CHECK (confirmation_idempotency_key IS NULL AND confirmation_request_hash IS NULL OR confirmation_idempotency_key IS NOT NULL AND length(confirmation_idempotency_key::text) > 0 AND confirmation_request_hash IS NOT NULL AND length(confirmation_request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_drafts ADD CONSTRAINT fk_library_organization_drafts_confirmed_revision_id FOREIGN KEY (confirmed_revision_id) REFERENCES library_organization_draft_revisions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_drafts ADD CONSTRAINT fk_library_organization_drafts_conversation_id FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_drafts ADD CONSTRAINT fk_library_organization_drafts_current_revision_id FOREIGN KEY (current_revision_id) REFERENCES library_organization_draft_revisions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE library_organization_drafts ADD CONSTRAINT uq_library_organization_drafts_conversation_id UNIQUE (conversation_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT ck_local_image_edit_adoption_events_artifacts CHECK (from_artifact_id IS NOT NULL AND to_artifact_id IS NOT NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT ck_local_image_edit_adoption_events_type CHECK (event_type::text = ANY (ARRAY['adopt'::character varying, 'revert'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT fk_local_image_edit_adoption_events_from_artifact_id FOREIGN KEY (from_artifact_id) REFERENCES workflow_graph_artifacts(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT fk_local_image_edit_adoption_events_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT fk_local_image_edit_adoption_events_node_id FOREIGN KEY (node_id) REFERENCES workflow_graph_nodes(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT fk_local_image_edit_adoption_events_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT fk_local_image_edit_adoption_events_related_event_id FOREIGN KEY (related_event_id) REFERENCES local_image_edit_adoption_events(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT fk_local_image_edit_adoption_events_task_id FOREIGN KEY (task_id) REFERENCES local_image_edit_tasks(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_adoption_events ADD CONSTRAINT fk_local_image_edit_adoption_events_to_artifact_id FOREIGN KEY (to_artifact_id) REFERENCES workflow_graph_artifacts(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_provider_attempts ADD CONSTRAINT ck_local_image_edit_provider_attempts_effect_result CHECK (effect_result::text = ANY (ARRAY['pending'::character varying, 'applied'::character varying, 'failed'::character varying, 'unknown'::character varying, 'unsupported'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_provider_attempts ADD CONSTRAINT ck_local_image_edit_provider_attempts_phase CHECK (phase::text = ANY (ARRAY['claimed'::character varying, 'provider_pending'::character varying, 'provider_call'::character varying, 'provider_result_received'::character varying, 'succeeded'::character varying, 'failed'::character varying, 'unknown'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_provider_attempts ADD CONSTRAINT ck_local_image_edit_provider_attempts_positive_number CHECK (attempt_number >= 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_provider_attempts ADD CONSTRAINT ck_local_image_edit_provider_attempts_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_provider_attempts ADD CONSTRAINT fk_local_image_edit_provider_attempts_late_result_asset_id FOREIGN KEY (late_result_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_provider_attempts ADD CONSTRAINT fk_local_image_edit_provider_attempts_task_id FOREIGN KEY (task_id) REFERENCES local_image_edit_tasks(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_provider_attempts ADD CONSTRAINT uq_local_image_edit_provider_attempts_task_attempt UNIQUE (task_id, attempt_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_provider_attempts ADD CONSTRAINT uq_local_image_edit_provider_attempts_task_number UNIQUE (task_id, attempt_number);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_task_references ADD CONSTRAINT ck_local_image_edit_task_references_bounded_order CHECK (sort_order >= 0 AND sort_order < 6);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_task_references ADD CONSTRAINT fk_local_image_edit_task_references_asset_id FOREIGN KEY (asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_task_references ADD CONSTRAINT fk_local_image_edit_task_references_task_id FOREIGN KEY (task_id) REFERENCES local_image_edit_tasks(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_task_references ADD CONSTRAINT uq_local_image_edit_task_references_task_order UNIQUE (task_id, sort_order);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT ck_local_image_edit_tasks_active_attempt CHECK (status = 'running'::localimageedittaskstatus AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NULL OR status <> 'running'::localimageedittaskstatus AND active_attempt_id IS NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT ck_local_image_edit_tasks_attempts CHECK (attempts >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT ck_local_image_edit_tasks_provider_intent CHECK (request_hash IS NULL OR requested_provider_name IS NOT NULL AND requested_local_edit_mode IS NOT NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT ck_local_image_edit_tasks_request_hash CHECK (request_hash IS NULL OR length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT ck_local_image_edit_tasks_result_state CHECK (status = 'succeeded'::localimageedittaskstatus AND result_asset_id IS NOT NULL AND finished_at IS NOT NULL OR (status = ANY (ARRAY['draft'::localimageedittaskstatus, 'queued'::localimageedittaskstatus, 'running'::localimageedittaskstatus, 'failed'::localimageedittaskstatus, 'cancelled'::localimageedittaskstatus, 'unknown'::localimageedittaskstatus])) AND result_asset_id IS NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT ck_local_image_edit_tasks_revision CHECK (revision >= 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT ck_local_image_edit_tasks_source_media_hash CHECK (length(source_media_sha256::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT ck_local_image_edit_tasks_status CHECK (status = ANY (ARRAY['draft'::localimageedittaskstatus, 'queued'::localimageedittaskstatus, 'running'::localimageedittaskstatus, 'succeeded'::localimageedittaskstatus, 'failed'::localimageedittaskstatus, 'cancelled'::localimageedittaskstatus, 'unknown'::localimageedittaskstatus]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT fk_local_image_edit_tasks_mask_media_object_id FOREIGN KEY (mask_media_object_id) REFERENCES media_objects(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT fk_local_image_edit_tasks_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT fk_local_image_edit_tasks_result_asset_id FOREIGN KEY (result_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT fk_local_image_edit_tasks_source_artifact_asset_id FOREIGN KEY (source_artifact_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT fk_local_image_edit_tasks_source_artifact_id FOREIGN KEY (source_artifact_id) REFERENCES workflow_graph_artifacts(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT fk_local_image_edit_tasks_source_asset_id FOREIGN KEY (source_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT fk_local_image_edit_tasks_target_graph_id FOREIGN KEY (target_graph_id) REFERENCES workflow_graphs(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT fk_local_image_edit_tasks_target_node_id FOREIGN KEY (target_node_id) REFERENCES workflow_graph_nodes(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE local_image_edit_tasks ADD CONSTRAINT uq_local_image_edit_tasks_product_idempotency UNIQUE (product_id, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_asset_tags ADD CONSTRAINT fk_media_library_asset_tags_asset_id FOREIGN KEY (asset_id) REFERENCES media_library_assets(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_asset_tags ADD CONSTRAINT fk_media_library_asset_tags_tag_id FOREIGN KEY (tag_id) REFERENCES media_library_tags(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT ck_media_library_assets_provenance_hash CHECK (length(provenance_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT ck_media_library_assets_revision CHECK (revision >= 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT ck_media_library_assets_source_type CHECK (source_type::text = ANY (ARRAY['image_session_generated'::character varying, 'product_asset'::character varying, 'direct_upload'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT fk_media_library_assets_folder_id FOREIGN KEY (folder_id) REFERENCES media_library_folders(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT fk_media_library_assets_media_object_id FOREIGN KEY (media_object_id) REFERENCES media_objects(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT fk_media_library_assets_source_image_session_asset_id FOREIGN KEY (source_image_session_asset_id) REFERENCES image_session_assets(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT fk_media_library_assets_source_product_asset_id FOREIGN KEY (source_product_asset_id) REFERENCES product_image_assets(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets DROP CONSTRAINT IF EXISTS uq_media_library_assets_source;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT uq_media_library_assets_source UNIQUE (merchant_id, source_type, source_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_collection_keys ADD CONSTRAINT ck_media_library_collection_keys_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_collection_keys ADD CONSTRAINT fk_media_library_collection_keys_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_collection_keys DROP CONSTRAINT IF EXISTS uq_media_library_collection_keys_product_key;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_collection_keys ADD CONSTRAINT uq_media_library_collection_keys_product_key UNIQUE (merchant_id, product_id, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_folders DROP CONSTRAINT IF EXISTS uq_media_library_folders_normalized_name;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_folders ADD CONSTRAINT uq_media_library_folders_normalized_name UNIQUE (merchant_id, normalized_name);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_tags DROP CONSTRAINT IF EXISTS uq_media_library_tags_normalized_name;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_tags ADD CONSTRAINT uq_media_library_tags_normalized_name UNIQUE (merchant_id, normalized_name);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_upload_keys ADD CONSTRAINT ck_media_library_upload_keys_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_upload_keys DROP CONSTRAINT IF EXISTS uq_media_library_upload_keys_key;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_upload_keys ADD CONSTRAINT uq_media_library_upload_keys_key UNIQUE (merchant_id, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_objects ADD CONSTRAINT ck_media_objects_verified_metadata CHECK (verification_status <> 'verified'::mediaverificationstatus OR byte_size > 0 AND width > 0 AND height > 0 AND sha256 IS NOT NULL AND length(sha256::text) = 64 AND verified_at IS NOT NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_objects ADD CONSTRAINT uq_media_objects_storage_path UNIQUE (storage_path);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_asset_folders ADD CONSTRAINT ck_product_asset_folders_non_negative_sort_order CHECK (sort_order >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_asset_folders ADD CONSTRAINT fk_product_asset_folders_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_asset_folders ADD CONSTRAINT uq_product_asset_folders_product_name UNIQUE (product_id, name);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_fact_set_versions ADD CONSTRAINT ck_product_fact_set_versions_payload_hash CHECK (length(payload_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_fact_set_versions ADD CONSTRAINT ck_product_fact_set_versions_positive_version CHECK (version > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_fact_set_versions ADD CONSTRAINT fk_product_fact_set_versions_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_fact_set_versions ADD CONSTRAINT uq_product_fact_set_versions_product_version UNIQUE (product_id, version);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_image_assets ADD CONSTRAINT fk_product_image_assets_media_object_id FOREIGN KEY (media_object_id) REFERENCES media_objects(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_image_assets ADD CONSTRAINT fk_product_image_assets_parent_asset_id FOREIGN KEY (parent_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_image_assets ADD CONSTRAINT fk_product_image_assets_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_image_assets ADD CONSTRAINT fk_product_image_assets_source_image_session_asset_id FOREIGN KEY (source_image_session_asset_id) REFERENCES image_session_assets(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_image_assets ADD CONSTRAINT fk_product_image_assets_source_library_asset_id FOREIGN KEY (source_library_asset_id) REFERENCES media_library_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_image_assets ADD CONSTRAINT fk_product_image_assets_user_folder_id FOREIGN KEY (user_folder_id) REFERENCES product_asset_folders(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DROP TABLE IF EXISTS public.product_image_fidelity_checks;`,
	`DO $c$ BEGIN
ALTER TABLE products ADD CONSTRAINT ck_products_intake_pair CHECK (intake_schema_version IS NULL AND intake_json IS NULL OR intake_schema_version = 1 AND intake_json IS NOT NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE products ADD CONSTRAINT fk_products_cover_image_asset_id FOREIGN KEY (cover_image_asset_id) REFERENCES product_image_assets(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE products ADD CONSTRAINT fk_products_current_fact_set_version_id FOREIGN KEY (current_fact_set_version_id) REFERENCES product_fact_set_versions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE products ADD CONSTRAINT fk_products_current_delivery_adoption_version_id FOREIGN KEY (current_delivery_adoption_version_id) REFERENCES delivery_adoption_versions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_versions ADD CONSTRAINT ck_delivery_adoption_versions_positive_version CHECK (version > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_versions ADD CONSTRAINT uq_delivery_adoption_versions_product_version UNIQUE (product_id, version);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_versions ADD CONSTRAINT fk_delivery_adoption_versions_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_versions ADD CONSTRAINT fk_delivery_adoption_versions_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_versions ADD CONSTRAINT fk_delivery_adoption_versions_fact_set_version_id FOREIGN KEY (fact_set_version_id) REFERENCES product_fact_set_versions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_versions ADD CONSTRAINT fk_delivery_adoption_versions_visual_system_version_id FOREIGN KEY (visual_system_version_id) REFERENCES visual_system_versions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_slots ADD CONSTRAINT ck_delivery_adoption_slots_non_negative_sort CHECK (sort_order >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_slots ADD CONSTRAINT ck_delivery_adoption_slots_spec_hash CHECK (length(delivery_spec_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_slots ADD CONSTRAINT ck_delivery_adoption_slots_quality_status CHECK (quality_status = ANY (ARRAY['pass'::text, 'fail'::text, 'unchecked'::text]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_slots ADD CONSTRAINT uq_delivery_adoption_slots_version_slot UNIQUE (version_id, slot_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_slots ADD CONSTRAINT fk_delivery_adoption_slots_version_id FOREIGN KEY (version_id) REFERENCES delivery_adoption_versions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE delivery_adoption_slots ADD CONSTRAINT fk_delivery_adoption_slots_source_asset_id FOREIGN KEY (source_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_delivery_adoption_versions_product_created ON public.delivery_adoption_versions USING btree (product_id, created_at DESC, id DESC);`,
	`DO $c$ BEGIN
ALTER TABLE product_visual_selections ADD CONSTRAINT fk_product_visual_selections_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE product_visual_selections ADD CONSTRAINT fk_product_visual_selections_visual_system_version_id FOREIGN KEY (visual_system_version_id) REFERENCES visual_system_versions(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_product_visual_selections_version ON public.product_visual_selections USING btree (visual_system_version_id);`,
	`CREATE INDEX IF NOT EXISTS ix_delivery_adoption_slots_version_sort ON public.delivery_adoption_slots USING btree (version_id, sort_order, id);`,
	`DO $c$ BEGIN
ALTER TABLE provider_bindings ADD CONSTRAINT provider_bindings_provider_profile_id_fkey FOREIGN KEY (provider_profile_id) REFERENCES provider_profiles(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_version_references ADD CONSTRAINT ck_visual_system_version_references_non_negative_position CHECK ("position" >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_version_references ADD CONSTRAINT fk_visual_system_version_references_asset_id FOREIGN KEY (asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_version_references ADD CONSTRAINT fk_visual_system_version_references_version_id FOREIGN KEY (visual_system_version_id) REFERENCES visual_system_versions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_version_references ADD CONSTRAINT uq_visual_system_version_references_asset_role UNIQUE (visual_system_version_id, asset_id, role);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_version_references ADD CONSTRAINT uq_visual_system_version_references_position UNIQUE (visual_system_version_id, "position");
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_versions ADD CONSTRAINT ck_visual_system_versions_payload_hash CHECK (length(payload_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_versions ADD CONSTRAINT ck_visual_system_versions_positive_version CHECK (version > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_versions ADD CONSTRAINT ck_visual_system_versions_schema_version CHECK (schema_version = 1);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_versions ADD CONSTRAINT fk_visual_system_versions_system_id FOREIGN KEY (visual_system_id) REFERENCES visual_systems(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_system_versions ADD CONSTRAINT uq_visual_system_versions_system_version UNIQUE (visual_system_id, version);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT ck_workflow_graph_artifacts_input_digest CHECK (length(input_digest::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT ck_workflow_graph_artifacts_payload_hash CHECK (length(payload_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT ck_workflow_graph_artifacts_positive_revision CHECK (graph_revision > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT ck_workflow_graph_artifacts_schema_version CHECK (schema_version = 3);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT ck_workflow_graph_artifacts_type CHECK (artifact_type::text = ANY (ARRAY['creative_brief'::character varying, 'visual_system'::character varying, 'prompt'::character varying, 'image'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT fk_workflow_graph_artifacts_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT fk_workflow_graph_artifacts_node_id FOREIGN KEY (node_id) REFERENCES workflow_graph_nodes(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT fk_workflow_graph_artifacts_node_run_id FOREIGN KEY (node_run_id) REFERENCES workflow_graph_node_runs(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT fk_workflow_graph_artifacts_product_image_asset_id FOREIGN KEY (product_image_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT uq_workflow_graph_artifacts_node_run_id UNIQUE (node_run_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_edges ADD CONSTRAINT ck_workflow_graph_edges_data_type CHECK (data_type::text = ANY (ARRAY['product_facts'::character varying, 'image_asset'::character varying, 'creative_brief'::character varying, 'visual_system'::character varying, 'prompt'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_edges ADD CONSTRAINT ck_workflow_graph_edges_non_negative_order CHECK (sort_order >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_edges ADD CONSTRAINT ck_workflow_graph_edges_role CHECK (role::text = ANY (ARRAY['facts'::character varying, 'reference'::character varying, 'brief'::character varying, 'visual_guidance'::character varying, 'prompt'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_edges ADD CONSTRAINT fk_workflow_graph_edges_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_edges ADD CONSTRAINT fk_workflow_graph_edges_source_node_id FOREIGN KEY (source_node_id) REFERENCES workflow_graph_nodes(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_edges ADD CONSTRAINT fk_workflow_graph_edges_target_node_id FOREIGN KEY (target_node_id) REFERENCES workflow_graph_nodes(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_edges ADD CONSTRAINT uq_workflow_graph_edges_pair_role UNIQUE (graph_id, source_node_id, target_node_id, role);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_groups ADD CONSTRAINT ck_workflow_graph_groups_non_negative_order CHECK (sort_order >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_groups ADD CONSTRAINT fk_workflow_graph_groups_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs ADD CONSTRAINT ck_workflow_graph_node_runs_non_negative_order CHECK (sort_order >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs ADD CONSTRAINT ck_workflow_graph_node_runs_progress_phase CHECK (progress_phase IS NULL OR (progress_phase::text = ANY (ARRAY['claimed'::character varying, 'prepared'::character varying, 'provider_call'::character varying, 'provider_result_received'::character varying, 'unknown_provider_effect'::character varying, 'requeued_after_idle'::character varying]::text[])));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs ADD CONSTRAINT ck_workflow_graph_node_runs_status CHECK (status::text = ANY (ARRAY['queued'::character varying, 'running'::character varying, 'succeeded'::character varying, 'failed'::character varying, 'unknown'::character varying, 'skipped'::character varying, 'cancelled'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs ADD CONSTRAINT fk_workflow_graph_node_runs_node_id FOREIGN KEY (node_id) REFERENCES workflow_graph_nodes(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs ADD CONSTRAINT fk_workflow_graph_node_runs_run_id FOREIGN KEY (graph_run_id) REFERENCES workflow_graph_runs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT ck_workflow_graph_nodes_type CHECK (node_type::text = ANY (ARRAY['product_source'::character varying, 'image_asset'::character varying, 'creative_brief'::character varying, 'visual_system'::character varying, 'image_prompt'::character varying, 'image_generation'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT fk_workflow_graph_nodes_bound_image_asset_id FOREIGN KEY (bound_image_asset_id) REFERENCES product_image_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT fk_workflow_graph_nodes_current_artifact_id FOREIGN KEY (current_artifact_id) REFERENCES workflow_graph_artifacts(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT fk_workflow_graph_nodes_pending_candidate_artifact_id FOREIGN KEY (pending_candidate_artifact_id) REFERENCES workflow_graph_artifacts(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT ck_workflow_graph_artifacts_document_action CHECK (document_action IS NULL OR document_action IN ('complete', 'rewrite', 'replace'));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_artifacts ADD CONSTRAINT ck_workflow_graph_artifacts_base_document_hash CHECK (base_document_hash IS NULL OR length(base_document_hash) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT fk_workflow_graph_nodes_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT fk_workflow_graph_nodes_group_id FOREIGN KEY (group_id) REFERENCES workflow_graph_groups(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_proposals ADD CONSTRAINT ck_workflow_graph_proposals_non_negative_base CHECK (base_graph_revision >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_proposals ADD CONSTRAINT ck_workflow_graph_proposals_status CHECK (status::text = ANY (ARRAY['pending'::character varying, 'confirmed'::character varying, 'discarded'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_proposals ADD CONSTRAINT fk_workflow_graph_proposals_conversation_id FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_proposals ADD CONSTRAINT fk_workflow_graph_proposals_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_proposals ADD CONSTRAINT fk_workflow_graph_proposals_operation_group_id FOREIGN KEY (operation_group_id) REFERENCES workflow_operation_groups(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_provider_effects ADD CONSTRAINT ck_workflow_graph_provider_effects_effect_result CHECK (effect_result::text = ANY (ARRAY['pending'::character varying, 'applied'::character varying, 'failed'::character varying, 'unknown'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_provider_effects ADD CONSTRAINT ck_workflow_graph_provider_effects_reconciliation_state CHECK (reconciliation_state::text = ANY (ARRAY['not_requested'::character varying, 'applied'::character varying, 'not_applied'::character varying, 'unknown'::character varying, 'unsupported'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_provider_effects ADD CONSTRAINT ck_workflow_graph_provider_effects_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_provider_effects ADD CONSTRAINT fk_workflow_graph_provider_effects_node_run_id FOREIGN KEY (node_run_id) REFERENCES workflow_graph_node_runs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_provider_effects ADD CONSTRAINT uq_workflow_graph_provider_effects_node_run_id UNIQUE (node_run_id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_provider_effects ADD CONSTRAINT uq_workflow_graph_provider_effects_operation_key UNIQUE (operation_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs ADD CONSTRAINT ck_workflow_graph_runs_positive_revision CHECK (graph_revision > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs ADD CONSTRAINT ck_workflow_graph_runs_scope CHECK (run_scope::text = ANY (ARRAY['node'::character varying, 'to_node'::character varying, 'graph'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs ADD CONSTRAINT ck_workflow_graph_runs_status CHECK (status::text = ANY (ARRAY['running'::character varying, 'succeeded'::character varying, 'failed'::character varying, 'cancelled'::character varying, 'unknown'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs ADD CONSTRAINT fk_workflow_graph_runs_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graphs ADD CONSTRAINT ck_workflow_graphs_positive_revision CHECK (revision > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graphs ADD CONSTRAINT ck_workflow_graphs_schema_version CHECK (schema_version = 3);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graphs ADD CONSTRAINT fk_workflow_graphs_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_media_library_assets ADD CONSTRAINT fk_workflow_media_library_assets_library_asset_id FOREIGN KEY (media_library_asset_id) REFERENCES media_library_assets(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_media_library_assets ADD CONSTRAINT fk_workflow_media_library_assets_workflow_id FOREIGN KEY (workflow_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_operation_groups ADD CONSTRAINT ck_workflow_operation_groups_actor_type CHECK (actor_type::text = ANY (ARRAY['user'::character varying, 'agent'::character varying, 'recipe'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_operation_groups ADD CONSTRAINT ck_workflow_operation_groups_history_kind CHECK (history_kind::text = ANY (ARRAY['edit'::character varying, 'undo'::character varying, 'redo'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_operation_groups ADD CONSTRAINT ck_workflow_operation_groups_non_negative_base CHECK (base_revision >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_operation_groups ADD CONSTRAINT ck_workflow_operation_groups_revision_step CHECK (result_revision = (base_revision + 1));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_operation_groups ADD CONSTRAINT fk_workflow_operation_groups_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_operation_groups ADD CONSTRAINT uq_workflow_operation_groups_graph_revision UNIQUE (graph_id, result_revision);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT ck_workflow_recipe_applications_mode CHECK (mode::text = ANY (ARRAY['create'::character varying, 'merge'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT ck_workflow_recipe_applications_preview_v2 CHECK (schema_version = 1 OR schema_version = 2 AND preview_graph_revision IS NOT NULL AND preview_graph_revision >= 0 AND preview_digest IS NOT NULL AND length(preview_digest::text) = 64 AND updated_node_ids_json IS NOT NULL AND required_bindings_json IS NOT NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT ck_workflow_recipe_applications_request_hash CHECK (length(request_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT ck_workflow_recipe_applications_schema_version CHECK (schema_version = ANY (ARRAY[1, 2]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT fk_workflow_recipe_applications_graph_id FOREIGN KEY (graph_id) REFERENCES workflow_graphs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT fk_workflow_recipe_applications_operation_group_id FOREIGN KEY (operation_group_id) REFERENCES workflow_operation_groups(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT fk_workflow_recipe_applications_product_id FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT fk_workflow_recipe_applications_recipe_version_id FOREIGN KEY (recipe_version_id) REFERENCES workflow_recipe_versions(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_applications ADD CONSTRAINT uq_workflow_recipe_applications_product_key UNIQUE (product_id, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_versions ADD CONSTRAINT ck_workflow_recipe_versions_catalog_version CHECK (catalog_version > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_versions ADD CONSTRAINT ck_workflow_recipe_versions_payload_hash CHECK (length(payload_hash::text) = 64);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_versions ADD CONSTRAINT ck_workflow_recipe_versions_positive_version CHECK (version > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_versions ADD CONSTRAINT ck_workflow_recipe_versions_schema_version CHECK (schema_version = 3);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_versions ADD CONSTRAINT fk_workflow_recipe_versions_preferred_visual_system_version_id FOREIGN KEY (preferred_visual_system_version_id) REFERENCES visual_system_versions(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_versions ADD CONSTRAINT fk_workflow_recipe_versions_recipe_id FOREIGN KEY (recipe_id) REFERENCES workflow_recipes(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipe_versions ADD CONSTRAINT uq_workflow_recipe_versions_recipe_version UNIQUE (recipe_id, version);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipes ADD CONSTRAINT ck_workflow_recipes_origin_key CHECK (origin = 'official'::workflowrecipeorigin AND official_key IS NOT NULL OR origin = 'user'::workflowrecipeorigin AND official_key IS NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipes ADD CONSTRAINT fk_workflow_recipes_current_version_id FOREIGN KEY (current_version_id) REFERENCES workflow_recipe_versions(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipes DROP CONSTRAINT IF EXISTS uq_workflow_recipes_official_key;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipes ADD CONSTRAINT uq_workflow_recipes_official_key UNIQUE (merchant_id, official_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_agent_conversations_product_status ON public.agent_conversations USING btree (product_id, status);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_conversations_scope_updated ON public.agent_conversations USING btree (scope_type, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_conversations_session_updated ON public.agent_conversations USING btree (session_id, updated_at, id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_agent_conversations_session_global ON public.agent_conversations USING btree (session_id) WHERE (scope_type = 'global'::agentconversationscope);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_page_context_snapshots_task_created ON public.agent_page_context_snapshots USING btree (task_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_sessions_product_status_updated ON public.agent_sessions USING btree (product_id, status, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_sessions_status_updated ON public.agent_sessions USING btree (status, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_sessions_product_status_activity ON public.agent_sessions USING btree (product_id, status, activity_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_sessions_status_activity ON public.agent_sessions USING btree (status, activity_at DESC, id DESC);`,
	`UPDATE public.agent_sessions AS s
	 SET activity_at = v.rank
	 FROM (
	   SELECT s2.id,
	          GREATEST(s2.updated_at, COALESCE(MAX(c.updated_at), s2.updated_at)) AS rank
	     FROM public.agent_sessions s2
	     LEFT JOIN public.agent_conversations c ON c.session_id = s2.id
	    GROUP BY s2.id, s2.updated_at
	 ) v
	 WHERE s.id = v.id AND s.activity_at IS DISTINCT FROM v.rank;`,
	`CREATE OR REPLACE FUNCTION public.productflow_session_activity_from_session() RETURNS trigger
	 LANGUAGE plpgsql AS $$
	 BEGIN
	   IF TG_OP = 'INSERT' THEN
	     NEW.activity_at := GREATEST(NEW.updated_at, COALESCE(NEW.activity_at, NEW.updated_at));
	     RETURN NEW;
	   END IF;
	   NEW.activity_at := GREATEST(COALESCE(OLD.activity_at, NEW.updated_at), NEW.updated_at);
	   RETURN NEW;
	 END;
	 $$;`,
	`CREATE OR REPLACE FUNCTION public.productflow_session_activity_from_conversation() RETURNS trigger
	 LANGUAGE plpgsql AS $$
	 BEGIN
	   IF NEW.session_id IS NOT NULL THEN
	     UPDATE public.agent_sessions
	        SET activity_at = GREATEST(activity_at, NEW.updated_at)
	      WHERE id = NEW.session_id
	        AND activity_at < NEW.updated_at;
	   END IF;
	   RETURN NEW;
	 END;
	 $$;`,
	`DROP TRIGGER IF EXISTS trg_agent_sessions_activity ON public.agent_sessions;`,
	`CREATE TRIGGER trg_agent_sessions_activity
	 BEFORE INSERT OR UPDATE OF updated_at ON public.agent_sessions
	 FOR EACH ROW EXECUTE FUNCTION public.productflow_session_activity_from_session();`,
	`DROP TRIGGER IF EXISTS trg_agent_conversations_session_activity ON public.agent_conversations;`,
	`CREATE TRIGGER trg_agent_conversations_session_activity
	 AFTER INSERT OR UPDATE OF updated_at, session_id ON public.agent_conversations
	 FOR EACH ROW EXECUTE FUNCTION public.productflow_session_activity_from_conversation();`,
	`CREATE INDEX IF NOT EXISTS ix_agent_tasks_conversation_updated ON public.agent_tasks USING btree (conversation_id, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_tasks_session_status_updated ON public.agent_tasks USING btree (session_id, status, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_tasks_status_updated ON public.agent_tasks USING btree (status, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_tool_mutations_asset_id ON public.agent_tool_mutations USING btree (asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_turn_checkpoints_projection_sequence ON public.agent_turn_checkpoints USING btree (turn_projection_id, sequence);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_model_invocations_provider_response_id ON public.agent_model_invocations USING btree (provider_response_id) WHERE (provider_response_id IS NOT NULL);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_turn_effect_reconciliations_projection_created ON public.agent_turn_effect_reconciliations USING btree (turn_projection_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_turn_events_projection_sequence ON public.agent_turn_events USING btree (turn_projection_id, sequence);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_turn_executions_lease ON public.agent_turn_executions USING btree (lease_expires_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_turn_executions_owner ON public.agent_turn_executions USING btree (owner_id, lease_expires_at, id);`,
	`DROP INDEX IF EXISTS public.ix_agent_turn_projections_continuation_turn;`,
	`ALTER TABLE public.agent_turn_projections DROP COLUMN IF EXISTS continuation_turn_id;`,
	`CREATE INDEX IF NOT EXISTS ix_agent_turn_projections_conversation_created ON public.agent_turn_projections USING btree (conversation_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_turn_projections_status ON public.agent_turn_projections USING btree (status);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_turn_projections_task_created ON public.agent_turn_projections USING btree (task_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_workflow_run_requests_conversation_status_updated ON public.agent_workflow_run_requests USING btree (conversation_id, status, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_workflow_run_requests_task_status_updated ON public.agent_workflow_run_requests USING btree (task_id, status, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_async_dispatches_lease_expiry ON public.async_dispatches USING btree (status, lease_expires_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_async_dispatches_status_available ON public.async_dispatches USING btree (status, available_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_delivery_rendition_jobs_product_status_created ON public.delivery_rendition_jobs USING btree (product_id, status, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_delivery_rendition_jobs_source_created ON public.delivery_rendition_jobs USING btree (source_asset_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_image_session_assets_media_object_id ON public.image_session_assets USING btree (media_object_id);`,
	`CREATE INDEX IF NOT EXISTS ix_image_session_generation_tasks_session_id ON public.image_session_generation_tasks USING btree (session_id);`,
	`CREATE INDEX IF NOT EXISTS ix_image_session_generation_tasks_session_created ON public.image_session_generation_tasks USING btree (session_id, created_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_image_session_generation_tasks_status ON public.image_session_generation_tasks USING btree (status);`,
	`CREATE INDEX IF NOT EXISTS ix_image_session_provider_effects_reconciliation ON public.image_session_provider_effects USING btree (effect_result, reconciliation_state, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_image_session_rounds_base_asset_id ON public.image_session_rounds USING btree (base_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_image_session_rounds_generation_group_id ON public.image_session_rounds USING btree (generation_group_id);`,
	`CREATE INDEX IF NOT EXISTS ix_image_session_rounds_session_created ON public.image_session_rounds USING btree (session_id, created_at DESC, id DESC);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_image_session_rounds_generated_asset_id ON public.image_session_rounds USING btree (generated_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_image_sessions_updated ON public.image_sessions USING btree (updated_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_library_organization_drafts_status_updated ON public.library_organization_drafts USING btree (status, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_local_image_edit_adoption_events_node_created ON public.local_image_edit_adoption_events USING btree (node_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_local_image_edit_provider_attempts_task_created ON public.local_image_edit_provider_attempts USING btree (task_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_local_image_edit_task_references_asset ON public.local_image_edit_task_references USING btree (asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_local_image_edit_tasks_product_status_created ON public.local_image_edit_tasks USING btree (product_id, status, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_local_image_edit_tasks_source_asset ON public.local_image_edit_tasks USING btree (source_asset_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_local_image_edit_tasks_target_node_status ON public.local_image_edit_tasks USING btree (target_node_id, status, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_media_library_asset_tags_tag_id ON public.media_library_asset_tags USING btree (tag_id);`,
	`CREATE INDEX IF NOT EXISTS ix_media_library_assets_folder_id ON public.media_library_assets USING btree (folder_id);`,
	`CREATE INDEX IF NOT EXISTS ix_media_library_assets_media_object_id ON public.media_library_assets USING btree (media_object_id);`,
	`CREATE INDEX IF NOT EXISTS ix_media_library_assets_source_image_session_asset_id ON public.media_library_assets USING btree (source_image_session_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_media_library_assets_source_product_asset_id ON public.media_library_assets USING btree (source_product_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_asset_folders_product_sort ON public.product_asset_folders USING btree (product_id, sort_order, name, id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_image_assets_media_object_id ON public.product_image_assets USING btree (media_object_id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_image_assets_parent_asset_id ON public.product_image_assets USING btree (parent_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_image_assets_product_created ON public.product_image_assets USING btree (product_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_image_assets_product_folder_created ON public.product_image_assets USING btree (product_id, user_folder_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_image_assets_product_origin_created ON public.product_image_assets USING btree (product_id, origin_type, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_image_assets_product_type_created ON public.product_image_assets USING btree (product_id, image_type_key, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_image_assets_source_image_session_asset_id ON public.product_image_assets USING btree (source_image_session_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_product_image_assets_source_library_asset_id ON public.product_image_assets USING btree (source_library_asset_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_product_image_assets_product_library_asset ON public.product_image_assets USING btree (product_id, source_library_asset_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_product_image_assets_product_session_asset ON public.product_image_assets USING btree (product_id, source_image_session_asset_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_provider_bindings_purpose ON public.provider_bindings USING btree (purpose);`,
	`CREATE INDEX IF NOT EXISTS ix_provider_profiles_archived_at ON public.provider_profiles USING btree (archived_at);`,
	`CREATE INDEX IF NOT EXISTS ix_provider_profiles_enabled ON public.provider_profiles USING btree (enabled);`,
	`CREATE INDEX IF NOT EXISTS ix_visual_systems_archived_at ON public.visual_systems USING btree (archived_at);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_artifacts_graph_id ON public.workflow_graph_artifacts USING btree (graph_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_artifacts_node_id ON public.workflow_graph_artifacts USING btree (node_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_edges_graph_id ON public.workflow_graph_edges USING btree (graph_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_groups_graph_id ON public.workflow_graph_groups USING btree (graph_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_node_runs_run_node ON public.workflow_graph_node_runs USING btree (graph_run_id, node_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_workflow_graph_node_runs_one_active_per_node ON public.workflow_graph_node_runs USING btree (node_id) WHERE ((status)::text = ANY ((ARRAY['queued'::character varying, 'running'::character varying])::text[]));`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_nodes_bound_image_asset_id ON public.workflow_graph_nodes USING btree (bound_image_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_nodes_graph_id ON public.workflow_graph_nodes USING btree (graph_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_workflow_graph_proposals_one_pending_per_graph ON public.workflow_graph_proposals USING btree (graph_id) WHERE ((status)::text = 'pending'::text);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_provider_effects_reconciliation ON public.workflow_graph_provider_effects USING btree (effect_result, reconciliation_state, updated_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_runs_graph_id ON public.workflow_graph_runs USING btree (graph_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_runs_graph_started ON public.workflow_graph_runs USING btree (graph_id, started_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_runs_execution_lease ON public.workflow_graph_runs USING btree (execution_lease_expires_at, id) WHERE ((status)::text = 'running'::text);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_workflow_graph_runs_one_active_per_graph ON public.workflow_graph_runs USING btree (graph_id) WHERE ((status)::text = 'running'::text);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graphs_product_id ON public.workflow_graphs USING btree (product_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_workflow_graphs_one_active_per_product ON public.workflow_graphs USING btree (product_id) WHERE (active = true);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_media_library_assets_library_asset_id ON public.workflow_media_library_assets USING btree (media_library_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_media_library_assets_workflow_created ON public.workflow_media_library_assets USING btree (workflow_id, created_at, media_library_asset_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_operation_groups_graph_id ON public.workflow_operation_groups USING btree (graph_id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_recipe_applications_product_created ON public.workflow_recipe_applications USING btree (product_id, created_at, id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_recipes_archived_at ON public.workflow_recipes USING btree (archived_at);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_recipes_origin ON public.workflow_recipes USING btree (origin);`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs DROP CONSTRAINT IF EXISTS ck_workflow_graph_node_runs_status;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs ADD CONSTRAINT ck_workflow_graph_node_runs_status CHECK (status::text = ANY (ARRAY['queued'::character varying, 'running'::character varying, 'succeeded'::character varying, 'failed'::character varying, 'unknown'::character varying, 'skipped'::character varying, 'cancelled'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs ADD CONSTRAINT ck_workflow_graph_node_runs_planned_action CHECK (planned_action IS NULL OR (planned_action::text = ANY (ARRAY['generate'::character varying, 'reuse'::character varying, 'frozen'::character varying, 'blocked'::character varying]::text[])));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_node_runs ADD CONSTRAINT ck_workflow_graph_node_runs_non_negative_attempt_count CHECK (attempt_count >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs DROP CONSTRAINT IF EXISTS ck_workflow_graph_runs_status;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs ADD CONSTRAINT ck_workflow_graph_runs_status CHECK (status::text = ANY (ARRAY['queued'::character varying, 'running'::character varying, 'succeeded'::character varying, 'failed'::character varying, 'cancelled'::character varying, 'unknown'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs ADD CONSTRAINT ck_workflow_graph_runs_execution_lease_pair CHECK ((execution_lease_token IS NULL) = (execution_lease_expires_at IS NULL));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs DROP CONSTRAINT IF EXISTS ck_workflow_graph_runs_scope;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_runs ADD CONSTRAINT ck_workflow_graph_runs_scope CHECK (run_scope::text = ANY (ARRAY['node'::character varying, 'to_node'::character varying, 'graph'::character varying, 'selection'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_operation_groups DROP CONSTRAINT IF EXISTS ck_workflow_operation_groups_actor_type;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_operation_groups ADD CONSTRAINT ck_workflow_operation_groups_actor_type CHECK (actor_type::text = ANY (ARRAY['user'::character varying, 'agent'::character varying, 'recipe'::character varying, 'system'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes DROP CONSTRAINT IF EXISTS ck_workflow_graph_nodes_type;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT ck_workflow_graph_nodes_type CHECK (node_type::text = ANY (ARRAY['product_source'::character varying, 'image_asset'::character varying, 'creative_brief'::character varying, 'visual_system'::character varying, 'image_prompt'::character varying, 'image_generation'::character varying]::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes DROP CONSTRAINT IF EXISTS ck_workflow_graph_nodes_document_origin;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT ck_workflow_graph_nodes_document_origin CHECK (document_origin IS NULL OR (document_origin::text = ANY (ARRAY['seed'::character varying, 'generated'::character varying, 'authored'::character varying, 'collaborative'::character varying]::text[])));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`-- Copy a retired config_json origin key when present. Remaining seed/NULL
	-- rows are compared to current seed templates in graph.BackfillDocumentOrigin.
	UPDATE workflow_graph_nodes SET document_origin = CASE
		WHEN config_json->>'document_origin' IN ('seed', 'generated', 'authored') THEN config_json->>'document_origin'
		ELSE 'seed'
	END
		WHERE node_type IN ('creative_brief', 'visual_system', 'image_prompt')
		  AND (document_origin IS NULL OR document_origin = '');`,
	`UPDATE workflow_graph_nodes
		SET config_json = (config_json::jsonb - 'document_origin' - 'visual_overrides')::json
		WHERE config_json::jsonb ?| ARRAY['document_origin', 'visual_overrides'];`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes DROP CONSTRAINT IF EXISTS ck_workflow_graph_nodes_content_origin_required;
EXCEPTION WHEN undefined_object THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_nodes ADD CONSTRAINT ck_workflow_graph_nodes_content_origin_required CHECK (node_type NOT IN ('creative_brief', 'visual_system', 'image_prompt') OR document_origin IN ('seed', 'generated', 'authored', 'collaborative'));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT ck_agent_workflow_run_requests_document_action CHECK (document_action IS NULL OR (document_action IN ('complete', 'rewrite', 'replace') AND force = true AND run_scope = 'node' AND target_node_id IS NOT NULL));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_workflow_run_requests ADD CONSTRAINT ck_agent_workflow_run_requests_run_scope CHECK (run_scope IS NULL OR (run_scope::text = ANY (ARRAY['graph'::character varying, 'node'::character varying, 'to_node'::character varying, 'selection'::character varying]::text[])));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_run_events ADD CONSTRAINT ck_workflow_graph_run_events_positive_sequence CHECK (sequence > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_run_events ADD CONSTRAINT ck_workflow_graph_run_events_kind CHECK (kind IN ('run.queued', 'run.started', 'run.completed', 'run.failed', 'run.cancelled', 'run.unknown', 'node.claimed', 'node.started', 'node.progress', 'node.succeeded', 'node.failed', 'node.skipped', 'node.cancelled'));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_run_events ADD CONSTRAINT fk_workflow_graph_run_events_graph_run_id FOREIGN KEY (graph_run_id) REFERENCES workflow_graph_runs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_run_events ADD CONSTRAINT fk_workflow_graph_run_events_node_run_id FOREIGN KEY (node_run_id) REFERENCES workflow_graph_node_runs(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_graph_run_events ADD CONSTRAINT uq_workflow_graph_run_events_run_sequence UNIQUE (graph_run_id, sequence);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_graph_run_events_run_sequence ON public.workflow_graph_run_events USING btree (graph_run_id, sequence);`,

	// Identity: direct user ownership / merchants / registration challenges / password recovery / auth_sessions
	`DROP TABLE IF EXISTS public.merchant_invites;`,
	`DO $c$ BEGIN
ALTER TABLE users ADD CONSTRAINT uq_users_email UNIQUE (email);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE users ADD CONSTRAINT ck_users_status CHECK (status IN ('active', 'disabled'));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchants ADD CONSTRAINT ck_merchants_status CHECK (status IN ('active', 'suspended'));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $identity$ BEGIN
	IF to_regclass('public.memberships') IS NULL THEN
		RETURN;
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.memberships AS m
		LEFT JOIN public.users AS u ON u.id = m.user_id
		WHERE u.id IS NULL
	) THEN
		RAISE EXCEPTION 'cannot migrate memberships: membership references a missing user';
	END IF;
	IF EXISTS (
		SELECT 1
		FROM public.memberships AS m
		LEFT JOIN public.merchants AS merchant ON merchant.id = m.merchant_id
		WHERE merchant.id IS NULL
	) THEN
		RAISE EXCEPTION 'cannot migrate memberships: membership references a missing merchant';
	END IF;
	IF EXISTS (
		SELECT 1
		FROM public.memberships AS m
		WHERE m.status IS NULL OR m.status NOT IN ('active', 'revoked') OR m.merchant_id IS NULL OR m.user_id IS NULL
	) THEN
		RAISE EXCEPTION 'cannot migrate memberships: invalid historical membership row';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.users AS u
		LEFT JOIN LATERAL (
			SELECT count(*) FILTER (WHERE m.status = 'active') AS active_count,
				count(*) AS history_count,
				count(m.merchant_id) AS nonnull_merchant_count,
				count(DISTINCT m.merchant_id) AS merchant_count,
				min(m.merchant_id) AS merchant_id
			FROM public.memberships AS m
			WHERE m.user_id = u.id
		) AS history ON true
		WHERE NOT u.is_operator
		  AND (history.active_count <> 1
			OR history.history_count <> history.nonnull_merchant_count
			OR history.merchant_count <> 1
			OR history.merchant_id IS NULL)
	) THEN
		RAISE EXCEPTION 'cannot migrate memberships: every ordinary user needs exactly one active membership and one historical merchant';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.users AS u
		LEFT JOIN LATERAL (
			SELECT min(m.merchant_id) AS merchant_id
			FROM public.memberships AS m
			WHERE m.user_id = u.id
		) AS candidate ON true
		WHERE NOT u.is_operator
		  AND u.merchant_id IS NOT NULL
		  AND u.merchant_id <> candidate.merchant_id
	) THEN
		RAISE EXCEPTION 'cannot migrate memberships: existing ordinary ownership conflicts with membership history';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM (
			SELECT u.id, min(m.merchant_id) AS merchant_id
			FROM public.users AS u
			JOIN public.memberships AS m ON m.user_id = u.id
			WHERE NOT u.is_operator
			GROUP BY u.id
		) AS candidates
		GROUP BY candidates.merchant_id
		HAVING count(*) > 1
	) THEN
		RAISE EXCEPTION 'cannot migrate memberships: multiple ordinary users share one merchant';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.users AS existing
		JOIN (
			SELECT u.id, min(m.merchant_id) AS merchant_id
			FROM public.users AS u
			JOIN public.memberships AS m ON m.user_id = u.id
			WHERE NOT u.is_operator
			GROUP BY u.id
		) AS candidates ON candidates.merchant_id = existing.merchant_id
		WHERE NOT existing.is_operator AND existing.id <> candidates.id
	) THEN
		RAISE EXCEPTION 'cannot migrate memberships: membership merchant is already owned by another ordinary user';
	END IF;

	IF EXISTS (
		SELECT 1
		FROM public.users
		WHERE NOT is_operator AND merchant_id IS NOT NULL
		GROUP BY merchant_id
		HAVING count(*) > 1
	) THEN
		RAISE EXCEPTION 'cannot migrate memberships: existing ordinary ownership is not unique';
	END IF;

	UPDATE public.users AS u
	SET merchant_id = candidates.merchant_id
	FROM (
		SELECT u.id, min(m.merchant_id) AS merchant_id
		FROM public.users AS u
		JOIN public.memberships AS m ON m.user_id = u.id
		WHERE NOT u.is_operator
		GROUP BY u.id
	) AS candidates
	WHERE u.id = candidates.id;

	WITH operator_candidates AS (
		SELECT u.id,
			CASE
				WHEN history.active_count = 1
				 AND history.merchant_count = 1
				 AND history.history_count = history.nonnull_merchant_count
				 AND NOT EXISTS (
					SELECT 1
					FROM public.users AS ordinary
					WHERE NOT ordinary.is_operator
					  AND ordinary.merchant_id = history.merchant_id
				 )
				THEN history.merchant_id
				ELSE NULL
			END AS merchant_id
		FROM public.users AS u
		LEFT JOIN LATERAL (
			SELECT count(*) FILTER (WHERE m.status = 'active') AS active_count,
				count(*) AS history_count,
				count(m.merchant_id) AS nonnull_merchant_count,
				count(DISTINCT m.merchant_id) AS merchant_count,
				min(m.merchant_id) AS merchant_id
			FROM public.memberships AS m
			WHERE m.user_id = u.id
		) AS history ON true
		WHERE u.is_operator
	)
	UPDATE public.users AS u
	SET merchant_id = operator_candidates.merchant_id
	FROM operator_candidates
	WHERE u.id = operator_candidates.id;

	DROP TABLE IF EXISTS public.memberships;
END $identity$;`,
	`DO $c$ BEGIN
ALTER TABLE users ADD CONSTRAINT fk_users_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE users ADD CONSTRAINT ck_users_merchant_required_for_ordinary CHECK (is_operator OR merchant_id IS NOT NULL);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_users_merchant_id ON public.users USING btree (merchant_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_users_ordinary_merchant_id ON public.users USING btree (merchant_id) WHERE (is_operator = FALSE AND merchant_id IS NOT NULL);`,
	`DO $c$ BEGIN
ALTER TABLE registration_challenges ADD CONSTRAINT ck_registration_challenges_failed_attempts CHECK (failed_attempts >= 0 AND failed_attempts <= 5);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE registration_challenges ADD CONSTRAINT ck_registration_challenges_code_hash_nonempty CHECK (length(btrim(code_hash)) > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE auth_sessions ADD CONSTRAINT fk_auth_sessions_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_registration_challenges_email_created ON public.registration_challenges USING btree (email, created_at DESC);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_registration_challenges_active_email ON public.registration_challenges (email) WHERE consumed_at IS NULL;`,
	`DO $c$ BEGIN
ALTER TABLE password_recovery_challenges ADD CONSTRAINT fk_password_recovery_challenges_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE password_recovery_challenges ADD CONSTRAINT ck_password_recovery_challenges_failed_attempts CHECK (failed_attempts >= 0 AND failed_attempts <= 5);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE password_recovery_challenges ADD CONSTRAINT ck_password_recovery_challenges_code_hash_nonempty CHECK (length(btrim(code_hash)) > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_password_recovery_challenges_user_created ON public.password_recovery_challenges USING btree (user_id, created_at DESC);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_password_recovery_challenges_active_user ON public.password_recovery_challenges (user_id) WHERE consumed_at IS NULL;`,
	`CREATE INDEX IF NOT EXISTS ix_auth_sessions_user_id ON public.auth_sessions USING btree (user_id);`,

	// B1 root ownership: existing NULL merchant IDs fail explicitly; inserts must provide their merchant.
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM products WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: products contains NULL merchant_id';
END IF;
ALTER TABLE products ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM image_sessions WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: image_sessions contains NULL merchant_id';
END IF;
ALTER TABLE image_sessions ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM media_library_folders WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: media_library_folders contains NULL merchant_id';
END IF;
ALTER TABLE media_library_folders ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM media_library_tags WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: media_library_tags contains NULL merchant_id';
END IF;
ALTER TABLE media_library_tags ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM media_library_assets WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: media_library_assets contains NULL merchant_id';
END IF;
ALTER TABLE media_library_assets ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM media_library_upload_keys WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: media_library_upload_keys contains NULL merchant_id';
END IF;
ALTER TABLE media_library_upload_keys ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM media_library_collection_keys WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: media_library_collection_keys contains NULL merchant_id';
END IF;
ALTER TABLE media_library_collection_keys ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM workflow_recipes WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: workflow_recipes contains NULL merchant_id';
END IF;
ALTER TABLE workflow_recipes ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM visual_systems WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: visual_systems contains NULL merchant_id';
END IF;
ALTER TABLE visual_systems ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM agent_sessions WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: agent_sessions contains NULL merchant_id';
END IF;
ALTER TABLE agent_sessions ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM agent_tasks WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: agent_tasks contains NULL merchant_id';
END IF;
ALTER TABLE agent_tasks ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM agent_conversations WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: agent_conversations contains NULL merchant_id';
END IF;
ALTER TABLE agent_conversations ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE products ADD CONSTRAINT fk_products_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE image_sessions ADD CONSTRAINT fk_image_sessions_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_folders ADD CONSTRAINT fk_media_library_folders_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_tags ADD CONSTRAINT fk_media_library_tags_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_assets ADD CONSTRAINT fk_media_library_assets_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_upload_keys ADD CONSTRAINT fk_media_library_upload_keys_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE media_library_collection_keys ADD CONSTRAINT fk_media_library_collection_keys_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE workflow_recipes ADD CONSTRAINT fk_workflow_recipes_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE visual_systems ADD CONSTRAINT fk_visual_systems_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_sessions ADD CONSTRAINT fk_agent_sessions_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_tasks ADD CONSTRAINT fk_agent_tasks_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE agent_conversations ADD CONSTRAINT fk_agent_conversations_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_products_merchant_updated ON public.products USING btree (merchant_id, updated_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_image_sessions_merchant_updated ON public.image_sessions USING btree (merchant_id, updated_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_media_library_assets_merchant_created ON public.media_library_assets USING btree (merchant_id, created_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_media_library_folders_merchant_name ON public.media_library_folders USING btree (merchant_id, name, id);`,
	`CREATE INDEX IF NOT EXISTS ix_media_library_tags_merchant_name ON public.media_library_tags USING btree (merchant_id, name, id);`,
	`CREATE INDEX IF NOT EXISTS ix_workflow_recipes_merchant_updated ON public.workflow_recipes USING btree (merchant_id, updated_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_visual_systems_merchant_archived ON public.visual_systems USING btree (merchant_id, archived_at);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_sessions_merchant_activity ON public.agent_sessions USING btree (merchant_id, activity_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_tasks_merchant_updated ON public.agent_tasks USING btree (merchant_id, updated_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_agent_conversations_merchant_updated ON public.agent_conversations USING btree (merchant_id, updated_at DESC, id DESC);`,
	`DROP TRIGGER IF EXISTS trg_products_fill_merchant_id ON products;`,
	`DROP TRIGGER IF EXISTS trg_image_sessions_fill_merchant_id ON image_sessions;`,
	`DROP TRIGGER IF EXISTS trg_media_library_folders_fill_merchant_id ON media_library_folders;`,
	`DROP TRIGGER IF EXISTS trg_media_library_tags_fill_merchant_id ON media_library_tags;`,
	`DROP TRIGGER IF EXISTS trg_media_library_assets_fill_merchant_id ON media_library_assets;`,
	`DROP TRIGGER IF EXISTS trg_media_library_upload_keys_fill_merchant_id ON media_library_upload_keys;`,
	`DROP TRIGGER IF EXISTS trg_media_library_collection_keys_fill_merchant_id ON media_library_collection_keys;`,
	`DROP TRIGGER IF EXISTS trg_workflow_recipes_fill_merchant_id ON workflow_recipes;`,
	`DROP TRIGGER IF EXISTS trg_visual_systems_fill_merchant_id ON visual_systems;`,
	`DROP TRIGGER IF EXISTS trg_agent_sessions_fill_merchant_id ON agent_sessions;`,
	`DROP TRIGGER IF EXISTS trg_agent_tasks_fill_merchant_id ON agent_tasks;`,
	`DROP TRIGGER IF EXISTS trg_agent_conversations_fill_merchant_id ON agent_conversations;`,

	// MP-C price catalog B0: platform price versions + unit-price entries (seed default id = pv-placeholder-v0)
	`DO $c$ BEGIN
ALTER TABLE quota_price_versions ADD CONSTRAINT ck_quota_price_versions_label_nonempty CHECK (length(btrim(label)) > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE quota_price_versions ADD CONSTRAINT ck_quota_price_versions_currency_nonempty CHECK (length(btrim(currency)) > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE quota_price_entries ADD CONSTRAINT fk_quota_price_entries_version_id FOREIGN KEY (price_version_id) REFERENCES quota_price_versions(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE quota_price_entries ADD CONSTRAINT ck_quota_price_entries_code_nonempty CHECK (length(btrim(entry_code)) > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE quota_price_entries ADD CONSTRAINT ck_quota_price_entries_unit_price_positive CHECK (unit_price > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_quota_price_versions_one_default ON public.quota_price_versions ((is_default)) WHERE is_default;`,
	`CREATE INDEX IF NOT EXISTS ix_quota_price_entries_version ON public.quota_price_entries USING btree (price_version_id);`,
	`INSERT INTO quota_price_versions (id, label, currency, is_default, created_at, updated_at)
VALUES ('pv-placeholder-v0', '默认内部单价 v0', 'iu', TRUE, TIMESTAMPTZ '2026-09-07T00:00:00Z', TIMESTAMPTZ '2026-09-07T00:00:00Z')
ON CONFLICT (id) DO NOTHING;`,
	`INSERT INTO quota_price_entries (price_version_id, entry_code, unit_price, created_at) VALUES
('pv-placeholder-v0', 'image_session.generate', 1, TIMESTAMPTZ '2026-09-07T00:00:00Z'),
('pv-placeholder-v0', 'graph.image_generation', 1, TIMESTAMPTZ '2026-09-07T00:00:00Z'),
('pv-placeholder-v0', 'agent.model_request', 1, TIMESTAMPTZ '2026-09-07T00:00:00Z'),
('pv-placeholder-v0', 'localedit.edit', 1, TIMESTAMPTZ '2026-09-07T00:00:00Z'),
('pv-placeholder-v0', 'product.source_note', 1, TIMESTAMPTZ '2026-09-07T00:00:00Z')
ON CONFLICT (price_version_id, entry_code) DO NOTHING;`,

	// MP-C B0: merchant commercial quota ledger (separate from agent_model_invocations usage facts)
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_accounts ADD CONSTRAINT fk_merchant_quota_accounts_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_accounts ADD CONSTRAINT ck_merchant_quota_accounts_nonneg CHECK (available_units >= 0 AND reserved_units >= 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_holds ADD CONSTRAINT fk_merchant_quota_holds_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_holds ADD CONSTRAINT uq_merchant_quota_holds_idempotency UNIQUE (merchant_id, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_holds ADD CONSTRAINT ck_merchant_quota_holds_status CHECK (status IN ('reserved', 'settled', 'released', 'pending_reconciliation'));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_holds ADD CONSTRAINT ck_merchant_quota_holds_amount CHECK (amount_units > 0 AND (settled_units IS NULL OR settled_units >= 0));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_events ADD CONSTRAINT fk_merchant_quota_events_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_events ADD CONSTRAINT fk_merchant_quota_events_hold_id FOREIGN KEY (hold_id) REFERENCES merchant_quota_holds(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_events ADD CONSTRAINT uq_merchant_quota_events_idempotency UNIQUE (merchant_id, event_type, idempotency_key);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE merchant_quota_events ADD CONSTRAINT ck_merchant_quota_events_type CHECK (event_type IN ('reserve', 'settle', 'release', 'adjust', 'mark_unknown'));
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_merchant_quota_holds_merchant_status ON public.merchant_quota_holds USING btree (merchant_id, status, created_at DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_merchant_quota_events_merchant_created ON public.merchant_quota_events USING btree (merchant_id, created_at DESC, id DESC);`,

	// Brand entity B0: merchant-scoped brand root; optional visual_system link for inheritance wiring.
	`DO $c$ BEGIN
IF EXISTS (SELECT 1 FROM brands WHERE merchant_id IS NULL) THEN
  RAISE EXCEPTION 'merchant_id required: brands contains NULL merchant_id';
END IF;
ALTER TABLE brands ALTER COLUMN merchant_id SET NOT NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE brands ADD CONSTRAINT fk_brands_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE brands ADD CONSTRAINT fk_brands_visual_system_id FOREIGN KEY (visual_system_id) REFERENCES visual_systems(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`DO $c$ BEGIN
ALTER TABLE brands ADD CONSTRAINT ck_brands_name_nonempty CHECK (length(btrim(name)) > 0);
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_brands_merchant_updated ON public.brands USING btree (merchant_id, updated_at DESC, id DESC);`,
	`CREATE INDEX IF NOT EXISTS ix_brands_visual_system_id ON public.brands USING btree (visual_system_id);`,
	`DROP TRIGGER IF EXISTS trg_brands_fill_merchant_id ON brands;`,
	`DROP FUNCTION IF EXISTS public.productflow_fill_merchant_id();`,

	// Brand B1: product selects same-merchant brand; style merge reads Brand.visual_system_id current version.
	`DO $c$ BEGIN
ALTER TABLE products ADD CONSTRAINT fk_products_brand_id FOREIGN KEY (brand_id) REFERENCES brands(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
WHEN duplicate_table THEN NULL;
END $c$;`,
	`CREATE INDEX IF NOT EXISTS ix_products_brand_id ON public.products USING btree (brand_id);`,
}
