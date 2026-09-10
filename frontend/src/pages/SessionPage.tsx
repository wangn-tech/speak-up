import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'

import { api } from '../api/client'

export function SessionPage() {
  const { id = '' } = useParams()
  const session = useQuery({ queryKey: ['session', id], queryFn: () => api.session(id) })
  return (
    <section className="page narrow">
      <Link className="back" to="/history">← 返回历史</Link>
      <h1>{session.data?.scene_title ?? '练习详情'}</h1>
      <div className="messages detail-messages">
        {session.data?.turns?.map((turn) => (
          <div key={turn.seq}>
            <div className="message user"><small>YOU</small><p>{turn.user_text}</p></div>
            <div className="message assistant"><small>SPEAKUP</small><p>{turn.assistant_text}</p></div>
          </div>
        ))}
      </div>
      {session.data?.status === 'finished' ? <Link className="primary inline" to={`/feedback/${id}`}>查看反馈</Link> : null}
    </section>
  )
}
