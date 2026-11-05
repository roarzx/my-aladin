UPDATE flow_node_runs SET status = 'pending' WHERE status = 'stale';

ALTER TABLE flow_node_runs
  MODIFY status ENUM('pending','executing','completed','failed','skipped')
    NOT NULL DEFAULT 'pending';
