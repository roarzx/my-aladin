-- 计划全部完成后任务进入 completed（此前复用 chatting，前端显示为「对话中」）。
ALTER TABLE tasks
  MODIFY COLUMN status ENUM('pending','plan_confirming','executing','paused',
                            'cancelling','chatting','cancelled','failed','completed')
    NOT NULL DEFAULT 'pending';

UPDATE tasks SET status = 'completed' WHERE status = 'chatting';
