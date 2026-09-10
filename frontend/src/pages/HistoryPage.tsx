import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'

import { api } from '../api/client'

export function HistoryPage() {
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: api.sessions })
  return (
    <section className="page">
      <p className="eyebrow">YOUR PROGRESS</p><h1>练习历史</h1>
      <div className="history-list">
        {sessions.data?.list.map((session) => (
          <Link className="history-row panel" to={`/history/${session.session_id}`} key={session.session_id}>
            <div><strong>{session.scene_title}</strong><span>{new Date(session.started_at).toLocaleString()}</span></div>
            <span className={`status-pill ${session.status}`}>{session.status}</span>
            <b>{session.overall_score === undefined ? '—' : session.overall_score}</b>
          </Link>
        ))}
        {sessions.data?.list.length === 0 ? <div className="panel"><p>完成第一次练习后，这里会出现记录。</p></div> : null}
      </div>
    </section>
  )
}
