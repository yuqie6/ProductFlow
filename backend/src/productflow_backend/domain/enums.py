from __future__ import annotations

from enum import StrEnum


class ImageSessionAssetKind(StrEnum):
    """生图会话附件：用户上传参考图 / AI 生成图。"""

    REFERENCE_UPLOAD = "reference_upload"
    GENERATED_IMAGE = "generated_image"


class MediaVerificationStatus(StrEnum):
    """媒体对象的实际文件核验状态。"""

    VERIFIED = "verified"
    MISSING = "missing"
    LEGACY_PENDING = "legacy_pending"


class ProductImageOriginType(StrEnum):
    """商品图片进入商品命名空间的来源。"""

    UPLOAD = "upload"
    WORKFLOW_GENERATION = "workflow_generation"
    IMAGE_SESSION_ATTACH = "image_session_attach"
    LEGACY_IMPORT = "legacy_import"


class JobStatus(StrEnum):
    """连续生图任务状态：排队 -> 运行中 -> 成功/失败/未知/取消。"""

    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"
    UNKNOWN = "unknown"


class AsyncDispatchStatus(StrEnum):
    """可靠异步投递的数据库权威状态。"""

    PENDING = "pending"
    SENT = "sent"
    CONSUMED = "consumed"
    DEAD = "dead"


class AgentExecutionPhase(StrEnum):
    """Agent Turn 的可恢复执行阶段。"""

    CLAIMED = "claimed"
    MODEL = "model"
    TOOL = "tool"
    WAITING_INPUT = "waiting_input"
    EXTERNAL_JOB = "external_job"
    TERMINAL = "terminal"


class AgentCheckpointKind(StrEnum):
    """Agent Turn 的持久语义 checkpoint 类型。"""

    BEFORE_MODEL_REQUEST = "before_model_request"
    TOOL_EFFECT_INTENT = "tool_effect_intent"
    TOOL_EFFECT_RESULT = "tool_effect_result"
    QUESTION_REQUIRED = "question_required"
    EXTERNAL_JOB_SUBMITTED = "external_job_submitted"
    TERMINAL = "terminal"


class WorkflowNodeType(StrEnum):
    """WorkflowDraft 与配方 payload 使用的节点类型。"""

    PRODUCT_CONTEXT = "product_context"
    REFERENCE_IMAGE = "reference_image"
    PROMPT_GENERATION = "prompt_generation"
    IMAGE_GENERATION = "image_generation"


class GraphNodeType(StrEnum):
    """schema-v3 画布节点类型。持久化在 workflow_graphs。"""

    PRODUCT_SOURCE = "product_source"
    IMAGE_ASSET = "image_asset"
    CREATIVE_BRIEF = "creative_brief"
    VISUAL_SYSTEM = "visual_system"
    PROMPT_GENERATION = "prompt_generation"
    IMAGE_GENERATION = "image_generation"


class GraphEdgeDataType(StrEnum):
    """schema-v3 typed edge 的数据类型。"""

    PRODUCT_FACTS = "product_facts"
    IMAGE_ASSET = "image_asset"
    CREATIVE_BRIEF = "creative_brief"
    VISUAL_SYSTEM = "visual_system"
    PROMPT = "prompt"


class GraphEdgeRole(StrEnum):
    """schema-v3 typed edge 的输入角色。"""

    FACTS = "facts"
    REFERENCE = "reference"
    BRIEF = "brief"
    VISUAL_GUIDANCE = "visual_guidance"
    PROMPT = "prompt"


class GraphConfigStatus(StrEnum):
    """由当前 graph revision 推导的节点配置状态，不是执行状态。"""

    INCOMPLETE = "incomplete"
    READY = "ready"
    STALE = "stale"


class GraphActorType(StrEnum):
    """ChangeSet / operation group 的发起方。"""

    USER = "user"
    AGENT = "agent"
    RECIPE = "recipe"


class GraphHistoryKind(StrEnum):
    """operation group 在撤销栈上的角色。Redo 不得映射成再调一次 undo。"""

    EDIT = "edit"
    UNDO = "undo"
    REDO = "redo"


class GraphRunScope(StrEnum):
    """v3 运行范围。"""

    NODE = "node"
    TO_NODE = "to_node"
    GRAPH = "graph"


class GraphArtifactType(StrEnum):
    """v3 运行产物类型。"""

    CREATIVE_BRIEF = "creative_brief"
    VISUAL_SYSTEM = "visual_system"
    PROMPT = "prompt"
    IMAGE = "image"


class WorkflowDraftStatus(StrEnum):
    """Agent 工作流草案从收集到物化的持久化状态。"""

    COLLECTING = "collecting"
    AWAITING_CONFIRMATION = "awaiting_confirmation"
    CONFIRMED = "confirmed"
    MATERIALIZING = "materializing"
    READY = "ready"
    FAILED = "failed"
    CANCELLED = "cancelled"


class LibraryOrganizationDraftStatus(StrEnum):
    """全局素材整理 Draft 的审核和执行状态。"""

    AWAITING_CONFIRMATION = "awaiting_confirmation"
    CONFIRMED = "confirmed"
    FAILED = "failed"
    CANCELLED = "cancelled"


class WorkflowRecipeKind(StrEnum):
    """用户主动保存的完整工作流配方或局部片段。"""

    WORKFLOW_RECIPE = "workflow_recipe"
    RECIPE_FRAGMENT = "recipe_fragment"


class AgentConversationStatus(StrEnum):
    """工作流 Agent 会话在 ProductFlow 侧的业务投影状态。"""

    COLLECTING = "collecting"
    AWAITING_CONFIRMATION = "awaiting_confirmation"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELED = "canceled"
    UNKNOWN = "unknown"


class AgentConversationScope(StrEnum):
    """Agent conversation 的业务作用域。"""

    PRODUCT_WORKFLOW = "product_workflow"
    GLOBAL = "global"


class AgentSessionStatus(StrEnum):
    """全局 Agent Session 的生命周期状态。"""

    ACTIVE = "active"
    ARCHIVED = "archived"


class AgentTaskStatus(StrEnum):
    """全局 Agent 业务任务的持久化状态。"""

    QUEUED = "queued"
    RUNNING = "running"
    WAITING_USER = "waiting_user"
    AWAITING_CONFIRMATION = "awaiting_confirmation"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELED = "canceled"
    PAUSED = "paused"
    UNKNOWN = "unknown"


class AgentTurnStatus(StrEnum):
    """agent-harness Turn 状态的无损 ProductFlow 投影。"""

    QUEUED = "queued"
    RUNNING = "running"
    REQUIRES_INPUT = "requires_input"
    AWAITING_CONFIRMATION = "awaiting_confirmation"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCEL_REQUESTED = "cancel_requested"
    CANCELED = "canceled"
    UNKNOWN = "unknown"


class AgentToolStepKind(StrEnum):
    """Agent service 可安全投影到网页的语义工具步骤类别。"""

    LOAD_SKILL = "load_skill"
    INJECT_CONTEXT = "inject_context"
    ASK_QUESTION = "ask_question"
    INSPECT_IMAGE = "inspect_image"
    PROPOSE_DRAFT = "propose_draft"
    INSPECT_CONTEXT = "inspect_context"
    READ_HISTORY = "read_history"
    ORGANIZE_ASSETS = "organize_assets"
    REQUEST_WORKFLOW_RUN = "request_workflow_run"
    CREATE_PRODUCT = "create_product"


class AgentToolStepStatus(StrEnum):
    """Agent service 工具步骤的有界投影状态。"""

    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    UNKNOWN = "unknown"


class AgentToolMutationStatus(StrEnum):
    """Agent 可对账业务副作用在 ProductFlow 侧的持久化结果。"""

    PREPARED = "prepared"
    APPLIED = "applied"
    FAILED = "failed"
    UNKNOWN = "unknown"


class AgentWorkflowRunRequestStatus(StrEnum):
    """Agent 请求执行工作流的业务状态。"""

    AWAITING_CONFIRMATION = "awaiting_confirmation"
    CONFIRMED = "confirmed"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"


class ProductFactStatus(StrEnum):
    """草案中单条商品事实的确认状态。"""

    OBSERVED = "observed"
    USER_DECLARED = "user_declared"
    CONFIRMED = "confirmed"
    CONFLICTED = "conflicted"


class ProductFactSourceType(StrEnum):
    """草案商品事实的来源类型。"""

    USER = "user"
    IMAGE_OBSERVATION = "image_observation"
    AGENT_INFERENCE = "agent_inference"
    LEGACY_PRODUCT = "legacy_product"


class WorkflowRevealEventKind(StrEnum):
    """已物化工作流在前端逐步揭示时使用的只读事件类型。"""

    FOLDER = "folder"
    NODE = "node"
    EDGE = "edge"
    COMPLETED = "completed"


class WorkflowNodeStatus(StrEnum):
    """工作流节点运行状态。"""

    IDLE = "idle"
    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    UNKNOWN = "unknown"


class WorkflowRunStatus(StrEnum):
    """工作流运行记录状态。"""

    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"
    UNKNOWN = "unknown"
