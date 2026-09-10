import type { Evaluation, PracticeSession, Scene, SessionConnection, User } from '../types'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? 'http://127.0.0.1:8080/api/v1'
const STORAGE_KEY = 'speakup:auth:v1'

type AuthState = { accessToken: string; refreshToken: string; user: User }
type Envelope<T> = { code: number | string; msg: string; data: T }
type Page<T> = { page: number; page_size: number; total: number; list: T[] }

let refreshPromise: Promise<boolean> | null = null

export function loadAuth(): AuthState | null {
  try {
    const value = sessionStorage.getItem(STORAGE_KEY)
    return value ? (JSON.parse(value) as AuthState) : null
  } catch {
    return null
  }
}

export function saveAuth(auth: AuthState): void {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(auth))
  } catch {
    // Private mode may reject storage; the active page can still continue.
  }
}

export function clearAuth(): void {
  try {
    sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    // Ignore unavailable storage.
  }
}

async function request<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
  const auth = loadAuth()
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  if (auth?.accessToken) headers.set('Authorization', `Bearer ${auth.accessToken}`)
  const response = await fetch(`${API_BASE}${path}`, { ...init, headers })
  if (response.status === 401 && retry && auth?.refreshToken) {
    refreshPromise ??= refreshAccessToken(auth)
    const refreshed = await refreshPromise.finally(() => {
      refreshPromise = null
    })
    if (refreshed) return request<T>(path, init, false)
  }
  const envelope = (await response.json()) as Envelope<T>
  if (!response.ok || envelope.code !== 0) throw new Error(envelope.msg || '请求失败')
  return envelope.data
}

async function refreshAccessToken(auth: AuthState): Promise<boolean> {
  try {
    const response = await fetch(`${API_BASE}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: auth.refreshToken }),
    })
    const envelope = (await response.json()) as Envelope<{ access_token: string; refresh_token: string }>
    if (!response.ok || envelope.code !== 0) throw new Error(envelope.msg)
    saveAuth({ ...auth, accessToken: envelope.data.access_token, refreshToken: envelope.data.refresh_token })
    return true
  } catch {
    clearAuth()
    return false
  }
}

export async function authenticate(mode: 'login' | 'register', phone: string, password: string, nickname?: string): Promise<AuthState> {
  const data = await request<{ access_token: string; refresh_token: string; user: User }>(`/auth/${mode}`, {
    method: 'POST',
    body: JSON.stringify({ phone, password, nickname }),
  })
  const auth = { accessToken: data.access_token, refreshToken: data.refresh_token, user: data.user }
  saveAuth(auth)
  return auth
}

export const api = {
  scenes: () => request<Page<Scene>>('/scenes'),
  scene: (id: string) => request<Scene>(`/scenes/${id}`),
  createSession: (sceneId: string) =>
    request<SessionConnection>('/sessions', { method: 'POST', body: JSON.stringify({ scene_id: sceneId }) }),
  sessions: () => request<Page<PracticeSession>>('/sessions'),
  session: (id: string) => request<PracticeSession>(`/sessions/${id}`),
  endSession: (id: string) => request<{ status: string; evaluation_id: string }>(`/sessions/${id}/end`, { method: 'POST' }),
  evaluations: (sessionId: string) => request<Evaluation[]>(`/evaluations?session_id=${encodeURIComponent(sessionId)}`),
}
