UPDATE tasks SET status = 'chatting' WHERE status = 'completed';

ALTER TABLE tasks
  MODIFY COLUMN status ENUM('pending','plan_confirming','executing','paused',
                            'cancelling','chatting','cancelled','failed')
    NOT NULL DEFAULT 'pending';
