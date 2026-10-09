import React, { useState } from 'react';
import { api, AdminUser } from '../api/client';
import { Logo } from '../components/Logo';
import { Lock, Mail, ArrowRight, Shield } from 'lucide-react';
import { Alert } from '../components/ui/Feedback';
import { Button } from '../components/ui/Button';

interface LoginPageProps {
  onLoginSuccess: (user: AdminUser) => void;
}

export const LoginPage: React.FC<LoginPageProps> = ({ onLoginSuccess }) => {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const res = await api.login({ email, password });
      onLoginSuccess(res.user);
    } catch (err: any) {
      setError(err.message || 'Login failed. Check your credentials and try again.');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-neutral-950 flex flex-col justify-center items-center px-4">
      {/* Background grid */}
      <div className="fixed inset-0 bg-[linear-gradient(to_right,#1a1a1a_1px,transparent_1px),linear-gradient(to_bottom,#1a1a1a_1px,transparent_1px)] bg-[size:64px_64px] pointer-events-none" />

      <div className="relative w-full max-w-[380px]">
        {/* Logo & brand */}
        <div className="flex flex-col items-center mb-8 text-center">
          <div className="w-14 h-14 bg-neutral-900 border border-neutral-800 rounded-2xl flex items-center justify-center p-2.5 mb-4 shadow-xl">
            <Logo theme="dark" className="w-full h-full object-contain" />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-neutral-100">Beavend</h1>
          <p className="text-sm text-neutral-500 mt-1">Control Plane Dashboard</p>
        </div>

        {/* Card */}
        <div className="bg-neutral-900 border border-neutral-800 rounded-2xl p-6 shadow-2xl shadow-black/50">
          <div className="mb-5">
            <h2 className="text-sm font-semibold text-neutral-200">Sign in to your workspace</h2>
            <p className="text-xs text-neutral-600 mt-0.5">Enter your admin credentials below</p>
          </div>

          {error && (
            <Alert variant="error" className="mb-4" onDismiss={() => setError(null)}>
              {error}
            </Alert>
          )}

          <form onSubmit={handleSubmit} className="space-y-3">
            <div className="space-y-1">
              <label className="block text-xs font-medium text-neutral-400">Email address</label>
              <div className="relative">
                <Mail className="w-4 h-4 text-neutral-600 absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  type="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="admin@yourorg.com"
                  className="w-full bg-neutral-950 border border-neutral-800 rounded-lg pl-9 pr-4 py-2.5 text-sm text-neutral-100 placeholder-neutral-600 focus:outline-none focus:border-neutral-600 transition-colors"
                />
              </div>
            </div>

            <div className="space-y-1">
              <label className="block text-xs font-medium text-neutral-400">Password</label>
              <div className="relative">
                <Lock className="w-4 h-4 text-neutral-600 absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  type="password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••••••"
                  className="w-full bg-neutral-950 border border-neutral-800 rounded-lg pl-9 pr-4 py-2.5 text-sm text-neutral-100 placeholder-neutral-600 focus:outline-none focus:border-neutral-600 transition-colors"
                />
              </div>
            </div>

            <button
              type="submit"
              disabled={loading}
              className="w-full mt-1 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white font-medium py-2.5 px-4 rounded-lg transition-colors flex items-center justify-center gap-2 shadow-lg shadow-emerald-900/30 text-sm"
            >
              {loading ? (
                <>
                  <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                  </svg>
                  Authenticating...
                </>
              ) : (
                <>
                  Sign In
                  <ArrowRight className="w-4 h-4" />
                </>
              )}
            </button>
          </form>
        </div>

        {/* Footer */}
        <div className="mt-6 text-center">
          <p className="text-[11px] text-neutral-700 flex items-center justify-center gap-1.5">
            <Shield className="w-3 h-3" />
            Administrative access only. Sessions are JWT-secured.
          </p>
        </div>
      </div>
    </div>
  );
};
