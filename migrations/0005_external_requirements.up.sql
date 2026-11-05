-- 0005 外部需求去重（G6）：同一来源 + URL 只建一次任务。

CREATE TABLE external_requirements (
  source     VARCHAR(32) NOT NULL,
  url        TEXT NOT NULL,
  url_hash   CHAR(64) NOT NULL,
  task_id    CHAR(36) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (source, url_hash)
);
