-- 0003 小队定义表（F1）：创建 Issue 时从这里冻结为 squad_snapshots。
--
-- 快照与定义分离：后续修改 squads 不影响已冻结的任务语义。

CREATE TABLE squads (
  id              CHAR(36) PRIMARY KEY,
  workspace_id    CHAR(36) NOT NULL,
  name            VARCHAR(128) NOT NULL,
  leader_agent_id CHAR(36) NOT NULL,
  member_ids      JSON NOT NULL,
  instructions    MEDIUMTEXT NOT NULL,
  approval_policy JSON NOT NULL,             -- {"required":true,"chain":[{"order":1,"approverId":1}]}
  version         BIGINT UNSIGNED NOT NULL DEFAULT 1,
  enabled         TINYINT(1) NOT NULL DEFAULT 1,
  created_at      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
                    ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_squad_ws_name (workspace_id, name)
);
