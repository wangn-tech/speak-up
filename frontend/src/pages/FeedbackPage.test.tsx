import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '../api/client'
import type { Evaluation } from '../types'
import { FeedbackPage } from './FeedbackPage'

vi.mock('../api/client', () => ({ api: { evaluations: vi.fn() } }))

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/feedback/session-1']}>
        <Routes><Route path="/feedback/:id" element={<FeedbackPage />} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function evaluation(status: Evaluation['status']): Evaluation {
  return {
    evaluation_id: 'evaluation-1',
    event_id: 'event-1',
    session_id: 'session-1',
    status,
    created_at: '2026-09-11T00:00:00Z',
  }
}

describe('FeedbackPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows the pending state', async () => {
    vi.mocked(api.evaluations).mockResolvedValue([evaluation('pending')])
    renderPage()
    expect(await screen.findByRole('heading', { name: '正在生成反馈' })).toBeInTheDocument()
  })

  it('shows the failed state', async () => {
    vi.mocked(api.evaluations).mockResolvedValue([evaluation('failed')])
    renderPage()
    expect(await screen.findByRole('heading', { name: '反馈生成失败' })).toBeInTheDocument()
  })

  it('shows the completed score and five dimensions', async () => {
    const done = evaluation('done')
    done.result = {
      overall_score: 88,
      dimensions: { pronunciation: 80, grammar: 90, vocabulary: 85, fluency: 87, coherence: 92 },
      highlights: ['Clear intent'],
      issues: [],
      suggestions: ['Use more detail'],
      experimental_pronunciation: true,
    }
    vi.mocked(api.evaluations).mockResolvedValue([done])
    renderPage()
    expect(await screen.findByText('88')).toBeInTheDocument()
    expect(screen.getByText('逻辑结构')).toBeInTheDocument()
  })
})
