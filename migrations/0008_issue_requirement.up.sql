-- Agent 驱动的 Issue 需要承载需求正文与告知文档：
-- `aladdin openapi issue create --requirement-content/--inform-doc` 写入这两列。
ALTER TABLE tasks
  ADD COLUMN requirement MEDIUMTEXT NULL AFTER name,
  ADD COLUMN inform_doc  MEDIUMTEXT NULL AFTER requirement;
