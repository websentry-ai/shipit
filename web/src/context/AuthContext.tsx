import React, { createContext, useContext, useEffect, useState } from 'react';
import type { User } from '../types';
import { getMe, login as apiLogin, logout as apiLogout } from '../api/client';

interface AuthContextType {
  user: User | null;
  loading: boolean;
  error: string | null;
  login: () => void;
  logout: () => Promise<void>;
  checkAuth: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const checkAuth = async () => {
    try {
      setLoading(true);
      setError(null);
      const currentUser = await getMe();
      setUser(currentUser);
    } catch (err) {
      // Not authenticated or error
      setUser(null);
      // Don't set error for auth check - it's expected to fail when not logged in
    } finally {
      setLoading(false);
    }
  };

  const login = () => {
    apiLogin();
  };

  const logout = async () => {
    await apiLogout();
    setUser(null);
  };

  // Check auth on mount
  useEffect(() => {
    checkAuth();
  }, []);

  return (
    <AuthContext.Provider value={{ user, loading, error, login, logout, checkAuth }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}
