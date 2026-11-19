import { type ReactNode } from 'react'

/** 行内解析：**加粗** 与 `code`（不引入第三方 markdown 依赖）。 */
function renderInline(text: string, keyPrefix: string): ReactNode[] {
  const nodes: ReactNode[] = []
  const pattern = /(\*\*[^*]+\*\*|`[^`]+`)/g
  let last = 0
  let match: RegExpExecArray | null
  let index = 0
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > last) nodes.push(text.slice(last, match.index))
    const token = match[0]
    const key = `${keyPrefix}-${index}`
    if (token.startsWith('**')) {
      nodes.push(<strong key={key}>{token.slice(2, -2)}</strong>)
    } else {
      nodes.push(<code key={key}>{token.slice(1, -1)}</code>)
    }
    last = match.index + token.length
    index += 1
  }
  if (last < text.length) nodes.push(text.slice(last))
  return nodes
}

/**
 * 轻量 Markdown 渲染：标题、无序/有序列表、代码块、行内加粗与代码。
 * 输出 React 节点，不使用 dangerouslySetInnerHTML。
 */
export function MarkdownView({ text }: { text: string }) {
  if (!text.trim()) return null

  const blocks: ReactNode[] = []
  let list: string[] = []
  let listOrdered = false
  let code: string[] | null = null
  let key = 0

  const flushList = () => {
    if (list.length === 0) return
    const items = list.map((item, index) => (
      <li key={`${key}-${index}`}>{renderInline(item, `${key}-${index}`)}</li>
    ))
    blocks.push(
      listOrdered ? <ol key={`list-${key}`}>{items}</ol> : <ul key={`list-${key}`}>{items}</ul>,
    )
    list = []
    key += 1
  }

  const flushCode = () => {
    if (code === null) return
    blocks.push(
      <pre className="md-code" key={`code-${key}`}>
        <code>{code.join('\n')}</code>
      </pre>,
    )
    code = null
    key += 1
  }

  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trimEnd()

    if (line.trim().startsWith('```')) {
      if (code !== null) flushCode()
      else {
        flushList()
        code = []
      }
      continue
    }
    if (code !== null) {
      code.push(raw)
      continue
    }
    if (!line.trim()) {
      flushList()
      continue
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(line)
    if (heading) {
      flushList()
      const level = Math.min(heading[1].length + 2, 5)
      blocks.push(
        <p key={`h-${key}`} className={`md-heading md-h${level}`}>
          {renderInline(heading[2], `h-${key}`)}
        </p>,
      )
      key += 1
      continue
    }

    const bullet = /^[-*+]\s+(.*)$/.exec(line)
    if (bullet) {
      if (list.length > 0 && listOrdered) flushList()
      listOrdered = false
      list.push(bullet[1])
      continue
    }

    const ordered = /^\d+[.)]\s+(.*)$/.exec(line)
    if (ordered) {
      if (list.length > 0 && !listOrdered) flushList()
      listOrdered = true
      list.push(ordered[1])
      continue
    }

    flushList()
    blocks.push(
      <p key={`p-${key}`} className="md-paragraph">
        {renderInline(line, `p-${key}`)}
      </p>,
    )
    key += 1
  }

  flushList()
  flushCode()

  return <div className="md-body">{blocks}</div>
}
