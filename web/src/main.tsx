import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { RequireAuth } from './auth/RequireAuth'
import { Layout } from './components/Layout'
import { AuditLogsPage } from './pages/AuditLogsPage'
import { CampaignsPage } from './pages/CampaignsPage'
import { HomePage } from './pages/HomePage'
import { LoginPage } from './pages/LoginPage'
import { NotificationsPage } from './pages/NotificationsPage'
import { OpsPage } from './pages/OpsPage'
import { ProgressPage } from './pages/ProgressPage'
import { RecordsPage } from './pages/RecordsPage'
import { SettingsPermissionsPage } from './pages/SettingsPermissionsPage'
import { SettingsRolesPage } from './pages/SettingsRolesPage'
import { SettingsUsersPage } from './pages/SettingsUsersPage'
import { SubTasksPage } from './pages/SubTasksPage'
import { TasksPage } from './pages/TasksPage'
import { TemplateFormPage } from './pages/TemplateFormPage'
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
            <Route path="progress" element={<ProgressPage />} />
            <Route path="notifications" element={<NotificationsPage />} />
            <Route path="audit-logs" element={<AuditLogsPage />} />
            <Route path="settings/roles" element={<SettingsRolesPage />} />
            <Route path="settings/permissions" element={<SettingsPermissionsPage />} />
            <Route path="settings/users" element={<SettingsUsersPage />} />
            <Route path="settings/rbac" element={<Navigate to="/settings/roles" replace />} />
            <Route path="ops" element={<OpsPage />} />
            <Route path="ops/:id/records" element={<RecordsPage />} />
            <Route path="ops/:id" element={<OpsPage />} />
            <Route path="templates" element={<TemplatesPage />} />
            <Route path="templates/new" element={<TemplateFormPage />} />
            <Route path="templates/:id/edit" element={<TemplateFormPage />} />
            <Route path="campaigns" element={<CampaignsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
)
