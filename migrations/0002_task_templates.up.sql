-- 0002 任务模板表（B4）：创建任务时按模板展开为不可变的 config_snapshot。

CREATE TABLE task_templates (
  id           CHAR(36) PRIMARY KEY,
  workspace_id CHAR(36) NOT NULL,
  name         VARCHAR(128) NOT NULL,
  version      INT UNSIGNED NOT NULL DEFAULT 1,
  config       JSON NOT NULL,
  enabled      TINYINT(1) NOT NULL DEFAULT 1,
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_tpl_ws_name_ver (workspace_id, name, version),
  CONSTRAINT fk_tpl_ws FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
);
