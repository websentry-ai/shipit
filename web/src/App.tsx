import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { isAuthenticated } from './api/client';
import Layout from './components/Layout';
import Login from './pages/Login';
import Projects from './pages/Projects';
import Clusters from './pages/Clusters';
import Apps from './pages/Apps';
import AppDetail from './pages/AppDetail';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30000,
      retry: 1,
    },
  },
});

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  if (!isAuthenticated()) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <Layout />
              </ProtectedRoute>
            }
          >
            <Route index element={<Projects />} />
            <Route path="projects/:projectId" element={<Clusters />} />
            <Route path="clusters/:clusterId" element={<Apps />} />
            <Route path="apps/:appId" element={<AppDetail />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

export default App;
