import { Link, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { Button } from './ui/Button';

export default function Layout() {
  const location = useLocation();
  const { user, logout } = useAuth();

  const handleLogout = async () => {
    localStorage.removeItem('shipit_project');
    await logout();
  };

  const isActive = (path: string) => {
    if (path === '/') return location.pathname === '/';
    return location.pathname.startsWith(path);
  };

  return (
    <div className="min-h-screen flex flex-col bg-background">
      {/* Header */}
      <header className="bg-surface border-b border-border sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between h-14">
            <div className="flex items-center">
              <Link to="/" className="flex items-center gap-2">
                <div className="w-8 h-8 rounded-lg bg-accent flex items-center justify-center">
                  <svg className="w-5 h-5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
                  </svg>
                </div>
                <span className="text-lg font-semibold text-text-primary">Shipit</span>
              </Link>
              {user && (
                <nav className="ml-8 flex items-center">
                  <Link
                    to="/"
                    className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                      isActive('/') && !location.pathname.startsWith('/projects') && !location.pathname.startsWith('/clusters') && !location.pathname.startsWith('/apps') && !location.pathname.startsWith('/settings')
                        ? 'bg-accent-muted text-accent'
                        : 'text-text-secondary hover:text-text-primary hover:bg-surface-hover'
                    }`}
                  >
                    Apps
                  </Link>
                  <Link
                    to="/projects"
                    className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                      location.pathname.startsWith('/projects') || location.pathname.startsWith('/clusters')
                        ? 'bg-accent-muted text-accent'
                        : 'text-text-secondary hover:text-text-primary hover:bg-surface-hover'
                    }`}
                  >
                    Infrastructure
                  </Link>
                </nav>
              )}
            </div>

            <div className="flex items-center gap-2">
              {user ? (
                <>
                  <Link
                    to="/settings"
                    className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                      location.pathname === '/settings'
                        ? 'bg-accent-muted text-accent'
                        : 'text-text-secondary hover:text-text-primary hover:bg-surface-hover'
                    }`}
                  >
                    {user.picture_url ? (
                      <img
                        src={user.picture_url}
                        alt={user.name || user.email}
                        className="w-6 h-6 rounded-full ring-1 ring-border"
                      />
                    ) : (
                      <div className="w-6 h-6 rounded-full bg-accent-muted flex items-center justify-center">
                        <span className="text-xs font-medium text-accent">
                          {(user.name || user.email)[0].toUpperCase()}
                        </span>
                      </div>
                    )}
                    <span className="hidden sm:inline">{user.name || user.email.split('@')[0]}</span>
                  </Link>
                  <Button variant="ghost" size="sm" onClick={handleLogout}>
                    Logout
                  </Button>
                </>
              ) : (
                <Link to="/login">
                  <Button variant="primary" size="sm">
                    Login
                  </Button>
                </Link>
              )}
            </div>
          </div>
        </div>
      </header>

      {/* Main content */}
      <main className="flex-1">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
          <Outlet />
        </div>
      </main>

      {/* Footer */}
      <footer className="bg-surface border-t border-border">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
          <p className="text-center text-sm text-text-muted">
            Shipit - Lightweight PaaS for Kubernetes
          </p>
        </div>
      </footer>
    </div>
  );
}
