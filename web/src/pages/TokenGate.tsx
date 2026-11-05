import { useState } from 'react'

export interface TokenGateProps {
  onSave: (token: string) => void
}

/** 没有可用令牌时的入口；写入 localStorage 后由上层重新渲染。 */
export default function TokenGate({ onSave }: TokenGateProps) {
  const [value, setValue] = useState('')

  return (
    <main className="gate">
      <h1>阿拉丁</h1>
      <p>请输入访问令牌（本地开发默认是 <code>devtoken</code>）。</p>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          const token = value.trim()
          if (token) onSave(token)
        }}
      >
        <input
          type="password"
          value={value}
          placeholder="访问令牌"
          aria-label="访问令牌"
          onChange={(event) => setValue(event.target.value)}
        />
        <button type="submit" disabled={!value.trim()}>
          进入
        </button>
      </form>
    </main>
  )
}
