import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { setToken } from '../api/client';

export default function Login() {
  const [token, setTokenValue] = useState('');
  const [error, setError] = useState('');
  const navigate = useNavigate();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!token.trim()) {
      setError('API token is required');
      return;
    }
    setToken(token.trim());
    navigate('/');
  };

  return (
    <div className="min-h-[60vh] flex items-center justify-center">
      <div className="max-w-md w-full">
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-gray-900">Shipit</h1>
          <p className="mt-2 text-gray-600">
            Enter your API token to access the dashboard
          </p>
        </div>

        <form onSubmit={handleSubmit} className="bg-white shadow rounded-lg p-6">
          <div className="mb-4">
            <label
              htmlFor="token"
              className="block text-sm font-medium text-gray-700 mb-2"
            >
              API Token
            </label>
            <input
              type="password"
              id="token"
              value={token}
              onChange={(e) => setTokenValue(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              placeholder="Enter your API token"
            />
            {error && (
              <p className="mt-1 text-sm text-red-600">{error}</p>
            )}
          </div>

          <button
            type="submit"
            className="w-full bg-indigo-600 text-white py-2 px-4 rounded-md hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
          >
            Login
          </button>

          <p className="mt-4 text-sm text-gray-500 text-center">
            Get your API token from your shipit server administrator
          </p>
        </form>
      </div>
    </div>
  );
}
