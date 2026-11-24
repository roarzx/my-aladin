-- 评测模式：把 Agent 用于 SWE-bench 之类的外部判分任务。
-- 开启后 daemon 不上报产物、不追加「平台上报」提示词，单轮超时放宽。
ALTER TABLE agents ADD COLUMN eval_mode TINYINT(1) NOT NULL DEFAULT 0 AFTER enabled;
