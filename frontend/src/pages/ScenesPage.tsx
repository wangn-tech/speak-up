import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'

import { api } from '../api/client'

export function ScenesPage() {
  const navigate = useNavigate()
  const scenes = useQuery({ queryKey: ['scenes'], queryFn: api.scenes })

  async function start(sceneId: string) {
    const connection = await api.createSession(sceneId)
    navigate(`/conversation/${connection.session_id}`, { state: connection })
  }

  return (
    <section className="page">
      <p className="eyebrow">TODAY'S PRACTICE</p>
      <h1>今天想练什么？</h1>
      <p className="lede">选择一个场景，用三分钟完成一次真实的英语表达。</p>
      {scenes.isLoading ? <p>正在加载场景…</p> : null}
      {scenes.error ? <p className="error-text">{scenes.error.message}</p> : null}
      <div className="scene-grid">
        {scenes.data?.list.map((scene, index) => (
          <article className={`scene-card tone-${index + 1}`} key={scene.scene_id}>
            <div><span className="tag">{scene.category}</span><span className="level">{scene.difficulty}</span></div>
            <h2>{scene.title}</h2>
            <p>{scene.description}</p>
            <div className="objective"><strong>练习目标</strong><span>{scene.objective}</span></div>
            <button className="primary" onClick={() => void start(scene.scene_id)}>开始练习 →</button>
          </article>
        ))}
      </div>
    </section>
  )
}
