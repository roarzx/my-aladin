-- 0004 节点状态增加 stale（G5）：上游重跑后下游必须重算。

ALTER TABLE flow_node_runs
  MODIFY status ENUM('pending','executing','completed','failed','skipped','stale')
    NOT NULL DEFAULT 'pending';
