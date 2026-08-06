import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { RequireAuth } from './auth/RequireAuth'
import { Layout } from './components/Layout'
import { CampaignsPage } from './pages/CampaignsPage'
import { HomePage } from './pages/HomePage'
import { LoginPage } from './pages/LoginPage'
import { OpsPage } from './pages/OpsPage'
import { SubTasksPage } from './pages/SubTasksPage'
import { TasksPage } from './pages/TasksPage'
import { TemplatesPage } from './pages/TemplatesPage'
import './styles/global.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route
            element={
              <RequireAuth>
                <Layout />
              </RequireAuth>
            }
          >
            <Route index element={<HomePage />} />
            <Route path="tasks" element={<TasksPage />} />
            <Route path="tasks/:id/subtasks" element={<SubTasksPage />} />
            <Route path="ops" element={<OpsPage />} />
            <Route path="ops/:id" element={<OpsPage />} />
            <Route path="templates" element={<TemplatesPage />} />
            <Route path="campaigns" element={<CampaignsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
)
