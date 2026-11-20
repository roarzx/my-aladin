-- Issue 改为「一个 Issue 一个长会话」：删除计划-步骤链路，新增按轮执行记录。

DROP TABLE IF EXISTS step_dependencies;
DROP TABLE IF EXISTS confirmations;
DROP TABLE IF EXISTS attempts;
DROP TABLE IF EXISTS plan_steps;
DROP TABLE IF EXISTS plans;

-- 会话类型增加 issue（一次 Issue 会话，跨轮复用 ACP session）。
ALTER TABLE sessions
  MODIFY COLUMN kind ENUM('planning','task_collaboration','conversation','issue') NOT NULL;

-- 任务状态收敛：计划确认、对话中、取消中不再存在。
UPDATE tasks SET status = 'pending'   WHERE status = 'plan_confirming';
UPDATE tasks SET status = 'completed' WHERE status = 'chatting';
UPDATE tasks SET status = 'cancelled' WHERE status = 'cancelling';

ALTER TABLE tasks
  MODIFY COLUMN status ENUM('pending','executing','paused','completed','cancelled','failed')
    NOT NULL DEFAULT 'pending';

-- 一轮执行：一次把提示词交给 Agent，直到 daemon 回报 turn_done / turn_fail。
CREATE TABLE issue_runs (
  id           CHAR(36) PRIMARY KEY,
  task_id      CHAR(36) NOT NULL,
  agent_id     CHAR(36) NOT NULL,
  session_id   CHAR(36) NULL,               -- sessions.id
  execution_id CHAR(36) NOT NULL,           -- 派发与回执的关联键
  status       ENUM('dispatched','running','completed','failed','cancelled') NOT NULL DEFAULT 'dispatched',
  reply        MEDIUMTEXT NULL,             -- 本轮助手输出（同时写入活动流）
  error_class  VARCHAR(32) NULL,
  detail       TEXT NULL,
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
                 ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_run_execution (execution_id),
  KEY idx_runs_task (task_id, created_at),
  CONSTRAINT fk_runs_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- 产物归属改到一轮执行（step_id/attempt_id 随计划链路一起废弃）。
ALTER TABLE artifacts ADD COLUMN run_id CHAR(36) NULL AFTER task_id;
