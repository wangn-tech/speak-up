import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import { authenticate } from '../api/client'

export function AuthPage() {
  const navigate = useNavigate()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [phone, setPhone] = useState('13800000000')
  const [password, setPassword] = useState('speakup123')
  const [nickname, setNickname] = useState('Ada')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setLoading(true)
    setError('')
    try {
      await authenticate(mode, phone, password, nickname)
      navigate('/scenes')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="auth-page">
      <section className="auth-copy">
        <p className="eyebrow">AI ENGLISH PRACTICE</p>
        <h1>把每一次开口，<br />变成看得见的进步。</h1>
        <p>真实场景、即时回应、练后反馈。先说出来，再说得更好。</p>
      </section>
      <form className="panel auth-card" onSubmit={submit}>
        <h2>{mode === 'login' ? '欢迎回来' : '创建练习账号'}</h2>
        {mode === 'register' ? <label>昵称<input value={nickname} onChange={(event) => setNickname(event.target.value)} /></label> : null}
        <label>手机号<input inputMode="tel" value={phone} onChange={(event) => setPhone(event.target.value)} /></label>
        <label>密码<input type="password" value={password} onChange={(event) => setPassword(event.target.value)} /></label>
        {error ? <p className="error-text">{error}</p> : null}
        <button className="primary" disabled={loading}>{loading ? '请稍候…' : mode === 'login' ? '登录并开始' : '注册并开始'}</button>
        <button type="button" className="link-button" onClick={() => setMode(mode === 'login' ? 'register' : 'login')}>
          {mode === 'login' ? '没有账号？立即注册' : '已有账号？返回登录'}
        </button>
      </form>
    </div>
  )
}
