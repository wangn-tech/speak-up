import { NavLink, Outlet, useNavigate } from 'react-router-dom'

import { clearAuth, loadAuth } from '../api/client'

export function Layout() {
  const navigate = useNavigate()
  const user = loadAuth()?.user
  return (
    <div className="app-shell">
      <header className="topbar">
        <NavLink className="brand" to="/scenes">SpeakUp<span>·</span></NavLink>
        <nav>
          <NavLink to="/scenes">场景</NavLink>
          <NavLink to="/history">历史</NavLink>
          <span className="user-name">{user?.nickname}</span>
          <button className="link-button" onClick={() => { clearAuth(); navigate('/login') }}>退出</button>
        </nav>
      </header>
      <main><Outlet /></main>
    </div>
  )
}
