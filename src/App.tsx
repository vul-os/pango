import { Navigate, Route, Routes } from 'react-router-dom'
import AppShell from './components/layout/AppShell'
import RequireAuth from './components/layout/RequireAuth'
import LoginPage from './pages/LoginPage'
import RegisterPage from './pages/RegisterPage'
import JobsBoardPage from './pages/JobsBoardPage'
import JobDetailPage from './pages/JobDetailPage'
import RaiseJobPage from './pages/RaiseJobPage'
import BuildingsPage from './pages/BuildingsPage'
import BuildingDetailPage from './pages/BuildingDetailPage'
import InspectionsPage from './pages/InspectionsPage'
import InspectionDetailPage from './pages/InspectionDetailPage'
import ReportsPage from './pages/ReportsPage'
import SettingsPage from './pages/SettingsPage'
import NotFoundPage from './pages/NotFoundPage'

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />

      <Route
        path="/"
        element={
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        }
      >
        <Route index element={<Navigate to="/jobs" replace />} />
        <Route path="jobs" element={<JobsBoardPage />} />
        <Route path="jobs/new" element={<RaiseJobPage />} />
        <Route path="jobs/:id" element={<JobDetailPage />} />
        <Route path="buildings" element={<BuildingsPage />} />
        <Route path="buildings/:id" element={<BuildingDetailPage />} />
        <Route path="inspections" element={<InspectionsPage />} />
        <Route path="inspections/:id" element={<InspectionDetailPage />} />
        <Route path="reports" element={<ReportsPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
