import { useState, type FormEvent } from 'react'

import { toast } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   登录
   对照 hercules-frontend 的登录页：左侧品牌栏 + 右侧登录卡片。
   本地 Go 后端未提供账号与企微授权接口，提交只提示，不伪造登录态。
   ------------------------------------------------------------------ */

export default function Login() {
  const [account, setAccount] = useState('')
  const [password, setPassword] = useState('')
  const [notice, setNotice] = useState('')

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!account.trim()) {
      setNotice('请输入域账号')
      return
    }
    if (!password) {
      setNotice('请输入密码')
      return
    }
    setNotice('本地后端尚未提供该能力，账号登录不会真正生效。')
    toast('本地后端尚未提供该能力')
  }

  return (
    <main className="login-page">
      <section className="login-brand-pane">
        <div className="login-brand-mark" aria-hidden="true">
          阿
        </div>
        <h1>阿拉丁</h1>
        <p>AI 研发控制台</p>
      </section>

      <section className="login-card" aria-label="登录">
        <div className="login-card-head">
          <h2>登录</h2>
          <p>使用域账号登录阿拉丁 AI 研发控制台。</p>
        </div>

        <form className="auth-form" onSubmit={submit} noValidate>
          <label className="field">
            <span>域账号</span>
            <input
              value={account}
              placeholder="请输入域账号"
              autoComplete="username"
              onChange={(event) => {
                setAccount(event.target.value)
                setNotice('')
              }}
            />
          </label>

          <label className="field">
            <span>密码</span>
            <input
              type="password"
              value={password}
              placeholder="请输入密码"
              autoComplete="current-password"
              onChange={(event) => {
                setPassword(event.target.value)
                setNotice('')
              }}
            />
          </label>

          {notice ? (
            <p className="notice warn" role="status">
              {notice}
            </p>
          ) : null}

          <button type="submit" className="button primary">
            登录
          </button>
        </form>

        <div className="login-divider">其他登录方式</div>

        <button
          type="button"
          className="button ghost login-wecom"
          onClick={() => toast('本地后端尚未提供该能力')}
        >
          使用企微授权登录
        </button>
      </section>
    </main>
  )
}
