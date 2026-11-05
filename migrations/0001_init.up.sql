-- ============ 工作区 ============
CREATE TABLE workspaces (
  id         CHAR(36) PRIMARY KEY,
  name       VARCHAR(128) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);

CREATE TABLE workspace_members (
  workspace_id CHAR(36) NOT NULL,
  user_id      BIGINT UNSIGNED NOT NULL,
  role         ENUM('owner','admin','member','viewer') NOT NULL,
  PRIMARY KEY (workspace_id, user_id),
  CONSTRAINT fk_wm_ws FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
);

-- ============ 任务 ============
CREATE TABLE tasks (
  id                  CHAR(36) PRIMARY KEY,
  workspace_id        CHAR(36) NOT NULL,
  created_by          BIGINT UNSIGNED NOT NULL,            -- 创建者：B2 权限矩阵按此判定“仅限自己创建的”
  name                VARCHAR(255) NOT NULL,
  mode                ENUM('ordinary','squad') NOT NULL DEFAULT 'ordinary',
  status              ENUM('pending','plan_confirming','executing','paused',
                           'cancelling','chatting','cancelled','failed') NOT NULL DEFAULT 'pending',
  version             BIGINT UNSIGNED NOT NULL DEFAULT 0,   -- 乐观锁
  activity_seq        BIGINT UNSIGNED NOT NULL DEFAULT 0,   -- 事件游标分配器
  plan_branch         VARCHAR(255) NULL,                    -- 统一计划分支
  config_snapshot     JSON NOT NULL,                        -- 模板展开后的执行快照
  cancel_requested_at DATETIME(3) NULL,
  created_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
                        ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_tasks_ws_status (workspace_id, status),
  CONSTRAINT fk_tasks_ws FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
);

CREATE TABLE task_agents (
  task_id    CHAR(36) NOT NULL,
  agent_ref  VARCHAR(32) NOT NULL,          -- 任务内短引用
  agent_id   CHAR(36) NOT NULL,
  role       ENUM('planner','member') NOT NULL,
  active     TINYINT(1) NOT NULL DEFAULT 1,
  PRIMARY KEY (task_id, agent_ref),
  UNIQUE KEY uk_task_agent (task_id, agent_id),
  CONSTRAINT fk_ta_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- 唯一 Planner：用生成列 + 唯一索引实现（NULL 不参与唯一约束）
ALTER TABLE task_agents
  ADD COLUMN planner_flag TINYINT(1)
    GENERATED ALWAYS AS (IF(role = 'planner', 1, NULL)) STORED;
CREATE UNIQUE INDEX uk_one_planner ON task_agents (task_id, planner_flag);

-- ============ 计划与步骤 ============
CREATE TABLE plans (
  id           CHAR(36) PRIMARY KEY,
  task_id      CHAR(36) NOT NULL,
  revision     INT UNSIGNED NOT NULL,
  content_hash CHAR(64) NOT NULL,
  content_md   MEDIUMTEXT NOT NULL,          -- 给人评审的 Markdown
  status       ENUM('idle','executing','paused','stopped','completed') NOT NULL,
  version      BIGINT UNSIGNED NOT NULL DEFAULT 0,   -- 乐观锁
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_plan_rev (task_id, revision),
  CONSTRAINT fk_plans_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE TABLE plan_steps (
  id        CHAR(36) PRIMARY KEY,
  plan_id   CHAR(36) NOT NULL,
  ordinal   INT UNSIGNED NOT NULL,           -- 执行顺序，Runner 按此推进
  title     VARCHAR(255) NOT NULL,
  body      MEDIUMTEXT NOT NULL,
  agent_ref VARCHAR(32) NOT NULL,
  status    ENUM('pending','executing','completed','failed','cancelled','reconciling') NOT NULL DEFAULT 'pending',
  version   BIGINT UNSIGNED NOT NULL DEFAULT 0,      -- 乐观锁
  UNIQUE KEY uk_step_ord (plan_id, ordinal),
  CONSTRAINT fk_steps_plan FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

-- 依赖只影响上下文注入，不影响执行顺序
CREATE TABLE step_dependencies (
  step_id    CHAR(36) NOT NULL,
  depends_on CHAR(36) NOT NULL,
  PRIMARY KEY (step_id, depends_on),
  CONSTRAINT chk_no_self CHECK (step_id <> depends_on)
);

CREATE TABLE confirmations (
  id           CHAR(36) PRIMARY KEY,
  plan_id      CHAR(36) NOT NULL,
  revision     INT UNSIGNED NOT NULL,
  content_hash CHAR(64) NOT NULL,
  actor_id     BIGINT UNSIGNED NOT NULL,
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_confirm (plan_id, revision)
);

-- ============ 执行尝试 ============
CREATE TABLE attempts (
  id            CHAR(36) PRIMARY KEY,
  step_id       CHAR(36) NOT NULL,
  attempt_no    INT UNSIGNED NOT NULL,
  execution_id  CHAR(36) NOT NULL,           -- 派发时固定，重发复用
  lease_epoch   BIGINT UNSIGNED NOT NULL DEFAULT 1,
  status        ENUM('dispatched','running','completed','failed','unknown') NOT NULL DEFAULT 'dispatched',
  reconcile_verdict ENUM('completed','failed') NULL,  -- 对账结论；非空即已裁决
  error_class   VARCHAR(32) NULL,            -- network|runtime|tool|verification|permission
  result_digest CHAR(64) NULL,
  commit_sha    CHAR(40) NULL,
  created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_attempt_no (step_id, attempt_no),
  UNIQUE KEY uk_execution   (execution_id),
  KEY idx_attempts_status (status),
  KEY idx_attempts_reconcile (status, lease_epoch)
);

-- ============ 会话与回复 ============
CREATE TABLE sessions (
  id                 CHAR(36) PRIMARY KEY,
  task_id            CHAR(36) NOT NULL,
  agent_id           CHAR(36) NOT NULL,
  kind               ENUM('planning','task_collaboration','conversation') NOT NULL,
  dispatch_state     ENUM('waiting_agent','dispatch_failed','dispatched') NOT NULL
                       DEFAULT 'waiting_agent',
  machine_id         CHAR(36) NULL,
  acp_session_id     VARCHAR(128) NULL,      -- Provider 侧会话
  resume_fingerprint CHAR(64) NULL,          -- 三重指纹合成值
  updated_at         DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
                       ON UPDATE CURRENT_TIMESTAMP(3),
  session_ordinal    INT UNSIGNED NOT NULL DEFAULT 1,  -- 指纹不匹配时新建，序号递增
  UNIQUE KEY uk_session_scope (task_id, agent_id, kind, session_ordinal)
);

CREATE TABLE reply_activities (
  id                CHAR(36) PRIMARY KEY,
  task_id           CHAR(36) NOT NULL,
  step_id           CHAR(36) NULL,           -- 规划会话为 NULL
  session_id        CHAR(36) NOT NULL,
  source_message_id CHAR(36) NOT NULL,
  execution_attempt INT UNSIGNED NOT NULL,
  phase             ENUM('accepted','thinking','tooling','responding',
                         'completed','failed','cancelled') NOT NULL,
  created_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  terminal_at       DATETIME(3) NULL,
  -- 未终态时 =1，终态时为 NULL；保证同一消息只有一个未终态回复
  open_flag         TINYINT(1) GENERATED ALWAYS AS
                      (IF(terminal_at IS NULL, 1, NULL)) STORED,
  UNIQUE KEY uk_one_open_reply (source_message_id, open_flag),
  KEY idx_reply_pending (task_id, terminal_at)
);
-- phase 是回复生命周期，不是业务状态；业务状态见 plan_steps.status

-- ============ 上下文 ============
CREATE TABLE context_entries (
  id           CHAR(36) PRIMARY KEY,
  task_id      CHAR(36) NOT NULL,
  scope_kind   ENUM('task','step','agent','conversation') NOT NULL,
  scope_id     CHAR(36) NOT NULL,
  kind         ENUM('decisions','step_summary','agent_memory',
                    'conversation_summary') NOT NULL,
  version      INT UNSIGNED NOT NULL,
  content      MEDIUMTEXT NOT NULL,
  content_hash CHAR(64) NOT NULL,
  UNIQUE KEY uk_ctx (task_id, scope_kind, scope_id, kind, version)
);
-- 对应 CLI 侧 .context_versions.json 的键：decisions/<task-id>、summary/<step-id>

-- ============ 事件流 ============
CREATE TABLE activities (
  task_id    CHAR(36) NOT NULL,
  seq        BIGINT UNSIGNED NOT NULL,       -- 每任务单调，由 tasks.activity_seq 分配
  event_id   CHAR(36) NOT NULL,
  kind       VARCHAR(64) NOT NULL,
  payload    JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (task_id, seq),
  UNIQUE KEY uk_event_id (event_id)
);

-- ============ 幂等与产物 ============
CREATE TABLE idempotency_records (
  workspace_id CHAR(36) NOT NULL,
  operation    VARCHAR(64) NOT NULL,
  idem_key     VARCHAR(128) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  status       ENUM('in_flight','completed') NOT NULL DEFAULT 'in_flight',
  response     JSON NULL,                    -- completed 后才有值
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  expires_at   DATETIME(3) NOT NULL,         -- 默认 24h，到期可清理
  PRIMARY KEY (workspace_id, operation, idem_key),
  KEY idx_idem_expiry (expires_at)
);
-- 并发规则：先 INSERT(status=in_flight) 占位；主键冲突时读取现有记录
--   · 状态 in_flight 且 request_hash 相同 → 返回 409 让客户端重试
--   · 状态 completed 且 request_hash 相同 → 直接返回缓存的 response
--   · request_hash 不同 → 返回 409 IDEMPOTENCY_KEY_REUSED

CREATE TABLE artifacts (
  id           CHAR(36) PRIMARY KEY,
  task_id      CHAR(36) NOT NULL,
  step_id      CHAR(36) NULL,
  attempt_id   CHAR(36) NULL,                -- Handoff.verification[].artifactId 指向此列
  path         VARCHAR(512) NOT NULL,
  sha256       CHAR(64) NOT NULL,
  size_bytes   BIGINT UNSIGNED NOT NULL,
  content_type VARCHAR(128) NOT NULL,
  KEY idx_artifact_step (task_id, step_id)
);

-- ============ 组织与身份 ============
CREATE TABLE users (
  id           BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  email        VARCHAR(255) NOT NULL,
  display_name VARCHAR(128) NOT NULL,
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_users_email (email)
);
-- workspace_members.user_id 外键指向 users.id

-- ============ 机器 ============
CREATE TABLE machines (
  id            CHAR(36) PRIMARY KEY,
  workspace_id  CHAR(36) NOT NULL,
  name          VARCHAR(128) NOT NULL,
  kind          ENUM('local','cloud') NOT NULL DEFAULT 'local',
  status        ENUM('online','offline','disabled') NOT NULL DEFAULT 'offline',
  capabilities  JSON NOT NULL,               -- ["attachments","git","flow_node",...]
  cli_version   VARCHAR(32) NULL,
  last_seen_at  DATETIME(3) NULL,
  UNIQUE KEY uk_machine_ws_name (workspace_id, name),
  KEY idx_machine_status (workspace_id, status)
);
-- 机器令牌不落库明文，只存哈希与有效期
CREATE TABLE machine_tokens (
  id          CHAR(36) PRIMARY KEY,
  machine_id  CHAR(36) NOT NULL,
  token_hash  CHAR(64) NOT NULL,
  expires_at  DATETIME(3) NOT NULL,
  revoked_at  DATETIME(3) NULL,
  UNIQUE KEY uk_token_hash (token_hash),
  KEY idx_token_machine (machine_id)
);

-- ============ Agent ============
CREATE TABLE agents (
  id           CHAR(36) PRIMARY KEY,
  workspace_id CHAR(36) NOT NULL,
  name         VARCHAR(128) NOT NULL,
  owner_id     BIGINT UNSIGNED NOT NULL,
  visibility   ENUM('private','shared') NOT NULL DEFAULT 'private',
  runtime_type ENUM('claude_code','codex','kimi') NOT NULL,   -- v1 只使用 claude_code
  machine_id   CHAR(36) NOT NULL,
  work_dir     VARCHAR(512) NOT NULL,
  model        VARCHAR(64) NULL,
  thinking_level VARCHAR(32) NULL,
  system_prompt MEDIUMTEXT NOT NULL,
  capability_set JSON NOT NULL,              -- skills / mcps / plugins 引用
  env_vars     JSON NOT NULL,
  enabled      TINYINT(1) NOT NULL DEFAULT 1,
  UNIQUE KEY uk_agent_ws_name (workspace_id, name),
  KEY idx_agent_visible (workspace_id, visibility, enabled)
);

-- ============ 仓库 ============
CREATE TABLE repositories (
  id           CHAR(36) PRIMARY KEY,
  workspace_id CHAR(36) NOT NULL,
  url          VARCHAR(512) NOT NULL,
  UNIQUE KEY uk_repo_url (workspace_id, url)
);
CREATE TABLE task_repositories (
  task_id        CHAR(36) NOT NULL,
  repository_id  CHAR(36) NOT NULL,
  base_sha       CHAR(40) NOT NULL,
  target_branch  VARCHAR(255) NOT NULL,
  PRIMARY KEY (task_id, repository_id)
);

-- ============ 小队 ============
CREATE TABLE squad_snapshots (
  id           CHAR(36) PRIMARY KEY,
  task_id      CHAR(36) NOT NULL,
  squad_id     CHAR(36) NOT NULL,
  leader_agent_id CHAR(36) NOT NULL,
  member_ids   JSON NOT NULL,
  instructions MEDIUMTEXT NOT NULL,
  approval_policy JSON NOT NULL,             -- {"required":true,"chain":[...]}
  frozen_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_squad_task (task_id)
);
CREATE TABLE work_items (
  id          CHAR(36) PRIMARY KEY,
  task_id     CHAR(36) NOT NULL,
  ordinal     INT UNSIGNED NOT NULL,
  owner_agent_id CHAR(36) NOT NULL,
  status      ENUM('proposed','reviewing','awaiting_approval','ready',
                   'running','blocked','done','cancelled') NOT NULL DEFAULT 'proposed',
  version     BIGINT UNSIGNED NOT NULL DEFAULT 0,
  UNIQUE KEY uk_wi_ordinal (task_id, ordinal)
);
CREATE TABLE proposals (
  id          CHAR(36) PRIMARY KEY,
  work_item_id CHAR(36) NOT NULL,
  revision    INT UNSIGNED NOT NULL,
  content     MEDIUMTEXT NOT NULL,
  content_hash CHAR(64) NOT NULL,
  UNIQUE KEY uk_proposal_rev (work_item_id, revision)
);
CREATE TABLE approvals (
  id          CHAR(36) PRIMARY KEY,
  proposal_id CHAR(36) NOT NULL,
  step_order  INT UNSIGNED NOT NULL,         -- 审批链顺序
  approver_id BIGINT UNSIGNED NOT NULL,
  decision    ENUM('pending','approved','rejected') NOT NULL DEFAULT 'pending',
  comment     TEXT NULL,
  decided_at  DATETIME(3) NULL,
  UNIQUE KEY uk_approval_step (proposal_id, step_order)
);

-- ============ 流程 ============
CREATE TABLE flow_templates (
  id          CHAR(36) PRIMARY KEY,
  workspace_id CHAR(36) NOT NULL,
  name        VARCHAR(128) NOT NULL,
  type        ENUM('acceptance_test') NOT NULL DEFAULT 'acceptance_test',
  version     INT UNSIGNED NOT NULL DEFAULT 1,
  enabled     TINYINT(1) NOT NULL DEFAULT 1,
  nodes       JSON NOT NULL,                 -- 固定节点定义
  UNIQUE KEY uk_flow_ws_name_ver (workspace_id, name, version)
);
CREATE TABLE flow_runs (
  id            CHAR(36) PRIMARY KEY,
  task_id       CHAR(36) NOT NULL,
  template_id   CHAR(36) NOT NULL,
  template_version INT UNSIGNED NOT NULL,
  template_snapshot JSON NOT NULL,           -- 冻结的模板快照
  inputs        JSON NOT NULL,
  status        ENUM('queued','running','succeeded','failed','cancelled')
                  NOT NULL DEFAULT 'queued',
  version       BIGINT UNSIGNED NOT NULL DEFAULT 0
);
CREATE TABLE flow_node_runs (
  id           CHAR(36) PRIMARY KEY,
  run_id       CHAR(36) NOT NULL,
  node_key     VARCHAR(64) NOT NULL,         -- environment_prepare / mars_acceptance / agent_prompt
  ordinal      INT UNSIGNED NOT NULL,
  status       ENUM('pending','executing','completed','failed','skipped') NOT NULL DEFAULT 'pending',
  attempt_no   INT UNSIGNED NOT NULL DEFAULT 1,
  lease_owner  CHAR(36) NULL,
  lease_epoch  BIGINT UNSIGNED NOT NULL DEFAULT 1,
  lease_expires_at DATETIME(3) NULL,
  result       JSON NULL,
  UNIQUE KEY uk_node_ordinal (run_id, ordinal)
);

-- ============ 交接 ============
CREATE TABLE handoffs (
  id          CHAR(36) PRIMARY KEY,
  task_id     CHAR(36) NOT NULL,
  from_step_id CHAR(36) NOT NULL,
  to_step_id  CHAR(36) NULL,
  objective   TEXT NOT NULL,
  payload     JSON NOT NULL,                 -- decisions / changes / verification / unresolved
  created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
