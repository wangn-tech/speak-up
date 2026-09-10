import { Navigate, Route, Routes } from 'react-router-dom'

import { loadAuth } from './api/client'
import { Layout } from './components/Layout'
import { AuthPage } from './pages/AuthPage'
import { ConversationPage } from './pages/ConversationPage'
import { FeedbackPage } from './pages/FeedbackPage'
import { HistoryPage } from './pages/HistoryPage'
import { ScenesPage } from './pages/ScenesPage'
import { SessionPage } from './pages/SessionPage'

function ProtectedLayout() {
  return loadAuth() ? <Layout /> : <Navigate replace to="/login" />
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<AuthPage />} />
      <Route element={<ProtectedLayout />}>
        <Route path="/scenes" element={<ScenesPage />} />
        <Route path="/conversation/:id" element={<ConversationPage />} />
        <Route path="/feedback/:id" element={<FeedbackPage />} />
        <Route path="/history" element={<HistoryPage />} />
        <Route path="/history/:id" element={<SessionPage />} />
      </Route>
      <Route path="*" element={<Navigate replace to={loadAuth() ? '/scenes' : '/login'} />} />
    </Routes>
  )
}
