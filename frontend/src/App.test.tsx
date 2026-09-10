import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { App } from './App'

describe('App', () => {
  beforeEach(() => sessionStorage.clear())

  it('shows login for an unauthenticated visitor', () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/scenes']}><App /></MemoryRouter>
      </QueryClientProvider>,
    )
    expect(screen.getByRole('heading', { name: '欢迎回来' })).toBeInTheDocument()
  })
})
