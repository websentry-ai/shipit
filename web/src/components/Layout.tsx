import { Link, Outlet, useLocation } from 'react-router-dom';
import { isAuthenticated, clearToken } from '../api/client';

export default function Layout() {
  const location = useLocation();
  const authenticated = isAuthenticated();

  const handleLogout = () => {
    clearToken();
    localStorage.removeItem('shipit_project');
    window.location.href = '/login';
  };

  const isActive = (path: string) => {
    if (path === '/') return location.pathname === '/';
    return location.pathname.startsWith(path);
  };

  return (
    <div className="min-h-screen flex flex-col bg-gray-50">
      {/* Header */}
      <header className="bg-white border-b border-gray-200 sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between h-16">
            <div className="flex items-center">
              <Link to="/" className="flex items-center">
                <span className="text-xl font-bold text-indigo-600">Shipit</span>
              </Link>
              {authenticated && (
                <nav className="ml-10 flex space-x-1">
                  <Link
                    to="/"
                    className={`px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                      isActive('/') && !location.pathname.startsWith('/projects') && !location.pathname.startsWith('/clusters') && !location.pathname.startsWith('/apps')
                        ? 'bg-indigo-100 text-indigo-700'
                        : 'text-gray-600 hover:bg-gray-100'
                    }`}
                  >
                    Apps
                  </Link>
                  <Link
                    to="/projects"
                    className={`px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                      location.pathname.startsWith('/projects') || location.pathname.startsWith('/clusters')
                        ? 'bg-indigo-100 text-indigo-700'
                        : 'text-gray-600 hover:bg-gray-100'
                    }`}
                  >
                    Infrastructure
                  </Link>
                </nav>
              )}
            </div>

            <div className="flex items-center">
              {authenticated ? (
                <button
                  onClick={handleLogout}
                  className="px-3 py-2 rounded-md text-sm font-medium text-gray-600 hover:bg-gray-100"
                >
                  Logout
                </button>
              ) : (
                <Link
                  to="/login"
                  className="px-3 py-2 rounded-md text-sm font-medium text-gray-600 hover:bg-gray-100"
                >
                  Login
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
      <footer className="bg-white border-t border-gray-200">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
          <p className="text-center text-sm text-gray-500">
            Shipit - Lightweight PaaS for Kubernetes
          </p>
        </div>
      </footer>
    </div>
  );
}
