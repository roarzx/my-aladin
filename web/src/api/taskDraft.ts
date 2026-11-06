/**
 * 创建任务时填写的需求描述只保存在浏览器侧，用于驾驶舱「提交计划」的默认正文。
 * 后端 Task 没有对应字段（见 docs/main-path-plan.md 六、创建任务）。
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
