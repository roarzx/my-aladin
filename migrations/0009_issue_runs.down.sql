-- 回滚只恢复结构，数据无法找回（plans/steps/attempts/confirmations 已删除）。
-- 新模型的执行记录随之删除。

ALTER TABLE tasks
  MODIFY COLUMN status ENUM('pending','plan_confirming','executing','paused',
                            'cancelling','chatting','cancelled','failed','completed')
    NOT NULL DEFAULT 'pending';

ALTER TABLE sessions
  MODIFY COLUMN kind ENUM('planning','task_collaboration','conversation') NOT NULL;

ALTER TABLE artifacts DROP COLUMN run_id;

DROP TABLE IF EXISTS issue_runs;

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
