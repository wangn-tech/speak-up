export type User = { user_id: string; phone: string; nickname: string }

export type Scene = {
  scene_id: string
  title: string
  description: string
  difficulty: string
  category: string
  objective: string
  roles: string[]
  outline_steps: string[]
  starter_prompt: string
}

export type Turn = {
  seq: number
  user_text: string
  assistant_text: string
  ts_start: number
  ts_end: number
}

export type PracticeSession = {
  session_id: string
  scene_id: string
  scene_title: string
  status: string
  started_at: string
  ended_at?: string
  overall_score?: number
  turns?: Turn[]
}

export type EvaluationResult = {
  overall_score: number
  dimensions: Record<'pronunciation' | 'grammar' | 'vocabulary' | 'fluency' | 'coherence', number>
  highlights: string[]
  issues: string[]
  suggestions: string[]
  experimental_pronunciation: boolean
}

export type Evaluation = {
  evaluation_id: string
  event_id: string
  session_id: string
  status: 'pending' | 'done' | 'failed'
  overall_score?: number
  result?: EvaluationResult
  created_at: string
}

export type SessionConnection = {
  session_id: string
  ws_token: string
  ws_url: string
  expires_in: number
}
