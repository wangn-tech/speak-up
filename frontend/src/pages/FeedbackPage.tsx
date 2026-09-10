import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'

import { api } from '../api/client'

const labels: Record<string, string> = { pronunciation: '发音', grammar: '语法', vocabulary: '词汇', fluency: '流利度', coherence: '逻辑结构' }

export function FeedbackPage() {
  const { id = '' } = useParams()
  const query = useQuery({
    queryKey: ['evaluation', id],
    queryFn: () => api.evaluations(id),
    refetchInterval: (result) => result.state.data?.[0]?.status === 'done' ? false : 3000,
  })
  const evaluation = query.data?.[0]
  const result = evaluation?.result
  if (!evaluation || evaluation.status === 'pending') {
    return <section className="page narrow"><div className="panel generating"><i /><h1>正在生成反馈</h1><p>我们正在整理本次表达中的亮点和改进机会。</p></div></section>
  }
  if (evaluation.status === 'failed' || !result) {
    return <section className="page narrow"><div className="panel"><h1>反馈生成失败</h1><p>本次练习记录已经保留，请稍后再试。</p></div></section>
  }
  return (
    <section className="page feedback-page">
      <p className="eyebrow">PRACTICE REVIEW</p><h1>这次，你说得怎么样？</h1>
      <div className="score-layout">
        <div className="score-card"><span>综合得分</span><strong>{result.overall_score}</strong><small>/ 100</small></div>
        <div className="dimension-card panel">
          {Object.entries(result.dimensions).map(([key, value]) => <div className="dimension" key={key}><span>{labels[key] ?? key}</span><div><i style={{ width: `${value}%` }} /></div><b>{value}</b></div>)}
        </div>
      </div>
      <div className="feedback-grid">
        <article className="panel"><h2>本次亮点</h2>{result.highlights.map((item) => <p key={item}>✓ {item}</p>)}</article>
        <article className="panel"><h2>下一步建议</h2>{result.suggestions.map((item) => <p key={item}>→ {item}</p>)}</article>
      </div>
      {result.experimental_pronunciation ? <p className="note">发音评分为 MVP 实验能力，仅供练习参考。</p> : null}
      <Link className="primary inline" to="/scenes">再练一次</Link>
    </section>
  )
}
