import { useState } from 'react'

import '../styles/pages-admin.css'

export interface TokenGateProps {
  onSave: (token: string) => void
}

/** 没有可用令牌时的入口；写入 localStorage 后由上层重新渲染。 */
export default function TokenGate({ onSave }: TokenGateProps) {
  const [value, setValue] = useState('')

  return (
    <main className="gate">
      <div className="auth-shell">
        <div className="auth-brand">
          <span className="brand-mark" aria-hidden="true">
            阿
          </span>
          <span className="auth-brand-copy">
            <strong>阿拉丁</strong>
            <span>AI 研发控制台</span>
          </span>
        </div>

        <section className="auth-card">
          <h1>访问令牌</h1>
          <p>输入访问令牌以进入阿拉丁 AI 研发控制台。</p>

          <form
            className="auth-form"
            onSubmit={(event) => {
              event.preventDefault()
              const token = value.trim()
              if (token) onSave(token)
            }}
          >
            <label className="field">
              <span>访问令牌</span>
              <input
                type="password"
                value={value}
                placeholder="访问令牌"
                aria-label="访问令牌"
                onChange={(event) => setValue(event.target.value)}
              />
            </label>

            <button type="submit" className="button primary" disabled={!value.trim()}>
              进入
            </button>
          </form>

          <div className="auth-hint">
            <span>本地开发默认令牌</span>
            <code>devtoken</code>
          </div>
        </section>
      </div>
    </main>
  )
}
