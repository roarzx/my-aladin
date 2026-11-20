/**
 * 任务草稿：需求正文现在随创建请求一起提交（tasks.requirement），
 * 这里只保留表单的本地暂存能力，供尚未提交的编辑内容恢复。
 */
const PREFIX = 'aladdin:task-draft:'

function key(taskId: string): string {
  return `${PREFIX}${taskId}`
}

export function saveTaskDraft(taskId: string, description: string): void {
  if (!taskId || !description.trim()) return
  try {
    sessionStorage.setItem(key(taskId), description)
  } catch {
    /* 隐私模式下 storage 可能不可用，忽略即可 */
  }
}

export function readTaskDraft(taskId: string): string {
  if (!taskId) return ''
  try {
    return sessionStorage.getItem(key(taskId)) ?? ''
  } catch {
    return ''
  }
}

export function clearTaskDraft(taskId: string): void {
  if (!taskId) return
  try {
    sessionStorage.removeItem(key(taskId))
  } catch {
    /* ignore */
  }
}
