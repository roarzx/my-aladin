-- 每轮执行的用量快照：统计与看板不再从 activities 的 JSON 里反解。
-- model 是执行时 Agent 配置的快照，避免之后改模型导致历史归因漂移。
ALTER TABLE issue_runs
  ADD COLUMN input_tokens  BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER detail,
  ADD COLUMN output_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER input_tokens,
  ADD COLUMN model         VARCHAR(64) NULL AFTER output_tokens;
