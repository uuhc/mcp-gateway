import React from 'react';
import { BrowserRouter as Router, Routes, Route, Navigate, useLocation } from 'react-router-dom';

import { Layout } from './components/Layout';
import { LoginPage } from './pages/auth/login';
import { ChatInterface } from './pages/chat/chat-interface';
import { ConfigVersionsPage } from './pages/gateway/config-versions';
import { GatewayManager } from './pages/gateway/gateway-manager';
import { TenantManagement } from './pages/users/tenant-management';
import { UserManagement } from './pages/users/user-management';

// Route guard component
function PrivateRoute({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const token = window.localStorage.getItem('token');

  if (!token) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return <>{children}</>;
}

// Main layout component
function MainLayout() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<GatewayManager />} />
        <Route path="/chat" element={<ChatInterface />} />
        <Route path="/chat/:sessionId" element={<ChatInterface />} />
        <Route path="/gateway/*" element={<GatewayManager />} />
        <Route path="/gateway" element={<PrivateRoute><GatewayManager /></PrivateRoute>} />
        <Route path="/gateway/configs/:name/versions" element={<PrivateRoute><ConfigVersionsPage /></PrivateRoute>} />
        <Route path="/config-versions" element={<PrivateRoute><ConfigVersionsPage /></PrivateRoute>} />
        <Route path="/users" element={<PrivateRoute><UserManagement /></PrivateRoute>} />
        <Route path="/tenants" element={<TenantManagement />} />
      </Routes>
    </Layout>
  );
}

export default function App() {
  return (
    <Router 
      basename={import.meta.env.VITE_BASE_URL}
      future={{ v7_relativeSplatPath: true, v7_startTransition: true }}
    >
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/*"
          element={
            <PrivateRoute>
              <MainLayout />
            </PrivateRoute>
          }
        />
      </Routes>
    </Router>
  );
}
