/** 主题：暗色为默认，可切回浅色；选择存在 localStorage。 */
export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'aladdin_theme'

export function storedTheme(): Theme {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'light' ? 'light' : 'dark'
  } catch {
    return 'dark'
  }
}

/** 把主题写到 <html data-theme>，CSS 靠这个变量切换。 */
export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    /* 忽略存储异常（隐私模式等） */
  }
}

export function toggleTheme(current: Theme): Theme {
  const next: Theme = current === 'dark' ? 'light' : 'dark'
  applyTheme(next)
  return next
}
