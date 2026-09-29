import React, { useEffect, useState } from 'react';
import {
  api,
  AdminUser,
  Project,
  Environment,
  AuthConfig,
  TestTokenResult,
} from '../api/client';
import {
  Settings,
  Users,
  Plus,
  Trash2,
  Layers,
  KeyRound,
  CheckCircle2,
  XCircle,
  AlertCircle,
  ShieldAlert,
  Save,
  Search,
  ExternalLink,
} from 'lucide-react';

interface SettingsPageProps {
  currentUser: AdminUser | null;
  projects: Project[];
  currentProject: string;
  currentEnv: string;
  onRefreshProjects: () => void;
}

type SettingsTab = 'auth' | 'projects' | 'users' | 'compliance';

export const SettingsPage: React.FC<SettingsPageProps> = ({
  currentUser,
  projects,
  currentProject,
  currentEnv,
  onRefreshProjects,
}) => {
  const [activeTab, setActiveTab] = useState<SettingsTab>('auth');
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [loadingUsers, setLoadingUsers] = useState(false);

  // Auth Provider Configuration State
  const [authProvider, setAuthProvider] = useState<'firebase' | 'supabase' | 'oidc'>('firebase');
  const [firebaseProjectId, setFirebaseProjectId] = useState('');
  const [supabaseIssuer, setSupabaseIssuer] = useState('');
  const [oidcIssuer, setOidcIssuer] = useState('');
  const [oidcAudience, setOidcAudience] = useState('');
  const [oidcJwksUrl, setOidcJwksUrl] = useState('');
  const [clockSkewSeconds, setClockSkewSeconds] = useState(60);
  const [authSaving, setAuthSaving] = useState(false);
  const [authSaveSuccess, setAuthSaveSuccess] = useState<string | null>(null);
  const [authSaveError, setAuthSaveError] = useState<string | null>(null);

  // JWT Inspector State
  const [testTokenInput, setTestTokenInput] = useState('');
  const [testingToken, setTestingToken] = useState(false);
  const [tokenResult, setTokenResult] = useState<TestTokenResult | null>(null);

  // New Project State
  const [newProjId, setNewProjId] = useState('');
  const [newProjName, setNewProjName] = useState('');
  const [newProjDesc, setNewProjDesc] = useState('');

  // New Environment State
  const [newEnvName, setNewEnvName] = useState('');

  // New Admin User State
  const [newUserEmail, setNewUserEmail] = useState('');
  const [newUserPassword, setNewUserPassword] = useState('');
  const [newUserRole, setNewUserRole] = useState<'owner' | 'editor' | 'viewer'>('editor');

  // Privacy Purge State
  const [purgeIssuer, setPurgeIssuer] = useState('https://securetoken.google.com/my-firebase-app');
  const [purgeSubject, setPurgeSubject] = useState('');
  const [purgeSuccess, setPurgeSuccess] = useState<string | null>(null);

  const isOwner = currentUser?.role === 'owner';

  const loadAuthConfig = async () => {
    try {
      const cfg = await api.getEnvironmentAuthConfig(currentProject, currentEnv);
      if (cfg && cfg.provider) {
        const p = cfg.provider.toLowerCase();
        if (p === 'firebase') {
          setAuthProvider('firebase');
          setFirebaseProjectId(cfg.project_id || '');
        } else if (p === 'supabase') {
          setAuthProvider('supabase');
          setSupabaseIssuer(cfg.issuer || '');
        } else {
          setAuthProvider('oidc');
          setOidcIssuer(cfg.issuer || '');
          setOidcAudience(Array.isArray(cfg.audience) ? cfg.audience.join(', ') : '');
          setOidcJwksUrl(cfg.jwks_url || '');
        }
      }
    } catch (_) {}
  };

  const loadUsers = async () => {
    if (!isOwner) return;
    setLoadingUsers(true);
    try {
      const data = await api.listUsers().catch(() => []);
      setUsers(data);
    } finally {
      setLoadingUsers(false);
    }
  };

  useEffect(() => {
    loadAuthConfig();
    loadUsers();
  }, [currentProject, currentEnv, currentUser]);

  const handleSaveAuthConfig = async (e: React.FormEvent) => {
    e.preventDefault();
    setAuthSaving(true);
    setAuthSaveSuccess(null);
    setAuthSaveError(null);

    const payload: AuthConfig = {
      provider: authProvider,
      clock_skew: Number(clockSkewSeconds) * 1000000000, // nanoseconds duration
    };

    if (authProvider === 'firebase') {
      payload.project_id = firebaseProjectId.trim();
      payload.issuer = `https://securetoken.google.com/${firebaseProjectId.trim()}`;
      payload.audience = [firebaseProjectId.trim()];
      payload.jwks_url =
        'https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com';
    } else if (authProvider === 'supabase') {
      payload.issuer = supabaseIssuer.trim();
      payload.audience = ['authenticated'];
      payload.jwks_url = `${supabaseIssuer.replace(/\/+$/, '')}/.well-known/jwks.json`;
    } else {
      payload.issuer = oidcIssuer.trim();
      payload.jwks_url = oidcJwksUrl.trim();
      payload.audience = oidcAudience
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
    }

    try {
      await api.updateEnvironmentAuthConfig(currentProject, currentEnv, payload);
      setAuthSaveSuccess(
        `Successfully updated Identity Provider for ${currentProject}/${currentEnv}! Active snapshot invalidated.`
      );
    } catch (err: any) {
      setAuthSaveError(err.message || 'Failed to update auth configuration');
    } finally {
      setAuthSaving(false);
    }
  };

  const handleInspectToken = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!testTokenInput.trim() || testingToken) return;

    setTestingToken(true);
    setTokenResult(null);

    try {
      const res = await api.testToken({
        token: testTokenInput.trim(),
      });
      setTokenResult(res);
    } catch (err: any) {
      setTokenResult({
        valid: false,
        error: err.message || 'Failed to execute token verification test',
      });
    } finally {
      setTestingToken(false);
    }
  };

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.createProject({
        id: newProjId,
        name: newProjName,
        description: newProjDesc,
      });
      setNewProjId('');
      setNewProjName('');
      setNewProjDesc('');
      onRefreshProjects();
      alert('Project created successfully!');
    } catch (err: any) {
      alert(`Error creating project: ${err.message}`);
    }
  };

  const handleCreateEnv = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.createEnvironment(currentProject, {
        name: newEnvName,
        description: `${newEnvName} environment`,
      });
      setNewEnvName('');
      onRefreshProjects();
      alert('Environment created successfully!');
    } catch (err: any) {
      alert(`Error creating environment: ${err.message}`);
    }
  };

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.createUser({
        email: newUserEmail,
        password: newUserPassword,
        role: newUserRole,
      });
      setNewUserEmail('');
      setNewUserPassword('');
      loadUsers();
      alert('Admin user created successfully!');
    } catch (err: any) {
      alert(`Error creating user: ${err.message}`);
    }
  };

  const handleDeleteUser = async (id: string) => {
    if (!confirm(`Delete admin user ${id}?`)) return;
    try {
      await api.deleteUser(id);
      loadUsers();
    } catch (err: any) {
      alert(`Error deleting user: ${err.message}`);
    }
  };

  const handlePrivacyPurge = async (e: React.FormEvent) => {
    e.preventDefault();
    if (
      !confirm(
        `Permanently purge all logs and data for subject "${purgeSubject}"? This cannot be undone.`
      )
    )
      return;
    try {
      await api.privacyPurge(purgeIssuer, purgeSubject);
      setPurgeSuccess(`Successfully purged all records for subject: ${purgeSubject}`);
      setPurgeSubject('');
    } catch (err: any) {
      alert(`Privacy purge failed: ${err.message}`);
    }
  };

  return (
    <div className="space-y-6 max-w-5xl">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-100 flex items-center gap-2">
            <Settings className="w-6 h-6 text-emerald-400" />
            Project & System Settings
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Configure authentication providers, environments, administrative RBAC, and data privacy operations.
          </p>
        </div>

        {/* Sub-tabs */}
        <div className="flex items-center bg-slate-900 border border-slate-800 rounded-xl p-1 text-xs font-medium">
          <button
            onClick={() => setActiveTab('auth')}
            className={`px-3 py-1.5 rounded-lg transition-colors flex items-center gap-1.5 ${
              activeTab === 'auth'
                ? 'bg-slate-800 text-slate-100 font-semibold shadow-sm'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <KeyRound className="w-3.5 h-3.5 text-amber-400" />
            Identity & Auth Providers
          </button>
          <button
            onClick={() => setActiveTab('projects')}
            className={`px-3 py-1.5 rounded-lg transition-colors flex items-center gap-1.5 ${
              activeTab === 'projects'
                ? 'bg-slate-800 text-slate-100 font-semibold shadow-sm'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <Layers className="w-3.5 h-3.5 text-blue-400" />
            Projects & Envs
          </button>
          {isOwner && (
            <button
              onClick={() => setActiveTab('users')}
              className={`px-3 py-1.5 rounded-lg transition-colors flex items-center gap-1.5 ${
                activeTab === 'users'
                  ? 'bg-slate-800 text-slate-100 font-semibold shadow-sm'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              <Users className="w-3.5 h-3.5 text-purple-400" />
              Admin Users
            </button>
          )}
          <button
            onClick={() => setActiveTab('compliance')}
            className={`px-3 py-1.5 rounded-lg transition-colors flex items-center gap-1.5 ${
              activeTab === 'compliance'
                ? 'bg-slate-800 text-slate-100 font-semibold shadow-sm'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <ShieldAlert className="w-3.5 h-3.5 text-rose-400" />
            Privacy & Purge
          </button>
        </div>
      </div>

      {/* Tab 1: Identity & Auth Providers */}
      {activeTab === 'auth' && (
        <div className="space-y-6">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
                  <KeyRound className="w-5 h-5 text-amber-400" />
                  Identity Provider Configuration
                </h2>
                <p className="text-xs text-slate-400 mt-0.5">
                  Applied to environment{' '}
                  <span className="font-mono text-emerald-400 font-semibold">
                    {currentProject} / {currentEnv}
                  </span>
                  . Validates incoming end-user JWTs on the request hot path via cached JWKS.
                </p>
              </div>
            </div>

            {authSaveSuccess && (
              <div className="p-3 bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 rounded-xl text-xs flex items-center gap-2">
                <CheckCircle2 className="w-4 h-4 flex-shrink-0" />
                <span>{authSaveSuccess}</span>
              </div>
            )}

            {authSaveError && (
              <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 flex-shrink-0" />
                <span>{authSaveError}</span>
              </div>
            )}

            <form onSubmit={handleSaveAuthConfig} className="space-y-4 pt-1">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1.5">
                  Select Identity Provider Model
                </label>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                  <button
                    type="button"
                    onClick={() => setAuthProvider('firebase')}
                    className={`p-3.5 rounded-xl border text-left transition-all ${
                      authProvider === 'firebase'
                        ? 'bg-amber-500/10 border-amber-500/40 text-amber-300'
                        : 'bg-slate-950 border-slate-800 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <div className="font-semibold text-xs text-slate-200">Firebase Auth</div>
                    <div className="text-[11px] text-slate-500 mt-1">Google Identity Toolkit & Firebase Projects</div>
                  </button>

                  <button
                    type="button"
                    onClick={() => setAuthProvider('supabase')}
                    className={`p-3.5 rounded-xl border text-left transition-all ${
                      authProvider === 'supabase'
                        ? 'bg-emerald-500/10 border-emerald-500/40 text-emerald-300'
                        : 'bg-slate-950 border-slate-800 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <div className="font-semibold text-xs text-slate-200">Supabase Auth</div>
                    <div className="text-[11px] text-slate-500 mt-1">Supabase GoTrue JWT & JWKS validation</div>
                  </button>

                  <button
                    type="button"
                    onClick={() => setAuthProvider('oidc')}
                    className={`p-3.5 rounded-xl border text-left transition-all ${
                      authProvider === 'oidc'
                        ? 'bg-blue-500/10 border-blue-500/40 text-blue-300'
                        : 'bg-slate-950 border-slate-800 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <div className="font-semibold text-xs text-slate-200">Generic OIDC / Custom JWKS</div>
                    <div className="text-[11px] text-slate-500 mt-1">Auth0, Clerk, Keycloak, or internal OIDC</div>
                  </button>
                </div>
              </div>

              {/* Firebase Inputs */}
              {authProvider === 'firebase' && (
                <div className="bg-slate-950 border border-slate-800/80 p-4 rounded-xl space-y-3">
                  <div>
                    <label className="block text-xs font-semibold text-slate-300 mb-1">
                      Firebase Project ID
                    </label>
                    <input
                      type="text"
                      required
                      placeholder="e.g. my-awesome-app"
                      value={firebaseProjectId}
                      onChange={(e) => setFirebaseProjectId(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 placeholder-slate-600 focus:outline-none focus:border-amber-500"
                    />
                    <span className="text-[11px] text-slate-500 mt-1 block">
                      Auto-configures Issuer <code className="font-mono text-slate-400">https://securetoken.google.com/&lt;project_id&gt;</code> and Google JWKS endpoint.
                    </span>
                  </div>
                </div>
              )}

              {/* Supabase Inputs */}
              {authProvider === 'supabase' && (
                <div className="bg-slate-950 border border-slate-800/80 p-4 rounded-xl space-y-3">
                  <div>
                    <label className="block text-xs font-semibold text-slate-300 mb-1">
                      Supabase Project URL / Issuer
                    </label>
                    <input
                      type="text"
                      required
                      placeholder="https://xyzproject.supabase.co/auth/v1"
                      value={supabaseIssuer}
                      onChange={(e) => setSupabaseIssuer(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500"
                    />
                    <span className="text-[11px] text-slate-500 mt-1 block">
                      Auto-derives JWKS URL from <code className="font-mono text-slate-400">/.well-known/jwks.json</code> with audience <code className="font-mono text-slate-400">authenticated</code>.
                    </span>
                  </div>
                </div>
              )}

              {/* Generic OIDC Inputs */}
              {authProvider === 'oidc' && (
                <div className="bg-slate-950 border border-slate-800/80 p-4 rounded-xl space-y-3">
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <div>
                      <label className="block text-xs font-semibold text-slate-300 mb-1">
                        Issuer URL (`iss`)
                      </label>
                      <input
                        type="text"
                        required
                        placeholder="https://auth.yourdomain.com"
                        value={oidcIssuer}
                        onChange={(e) => setOidcIssuer(e.target.value)}
                        className="w-full bg-slate-900 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 placeholder-slate-600 focus:outline-none focus:border-blue-500"
                      />
                    </div>
                    <div>
                      <label className="block text-xs font-semibold text-slate-300 mb-1">
                        Expected Audience (`aud`)
                      </label>
                      <input
                        type="text"
                        placeholder="my-mobile-app, my-web-app"
                        value={oidcAudience}
                        onChange={(e) => setOidcAudience(e.target.value)}
                        className="w-full bg-slate-900 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 placeholder-slate-600 focus:outline-none focus:border-blue-500"
                      />
                    </div>
                  </div>

                  <div>
                    <label className="block text-xs font-semibold text-slate-300 mb-1">
                      JWKS Keyset URL (`jwks_url`)
                    </label>
                    <input
                      type="text"
                      required
                      placeholder="https://auth.yourdomain.com/.well-known/jwks.json"
                      value={oidcJwksUrl}
                      onChange={(e) => setOidcJwksUrl(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 placeholder-slate-600 focus:outline-none focus:border-blue-500"
                    />
                  </div>
                </div>
              )}

              <div className="flex items-center justify-between pt-2">
                <div className="flex items-center gap-2">
                  <label className="text-xs text-slate-400">Clock Skew Tolerance:</label>
                  <input
                    type="number"
                    min="0"
                    max="300"
                    value={clockSkewSeconds}
                    onChange={(e) => setClockSkewSeconds(Number(e.target.value))}
                    className="w-16 bg-slate-950 border border-slate-800 rounded-lg px-2 py-1 text-xs font-mono text-slate-200"
                  />
                  <span className="text-xs text-slate-500">sec</span>
                </div>

                <button
                  type="submit"
                  disabled={authSaving}
                  className="px-5 py-2 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white rounded-xl text-xs font-semibold flex items-center gap-2 transition-colors shadow-lg shadow-emerald-900/20"
                >
                  <Save className="w-4 h-4" />
                  <span>{authSaving ? 'Saving...' : 'Save Auth Configuration'}</span>
                </button>
              </div>
            </form>
          </div>

          {/* Interactive JWT Token Inspector */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <div>
              <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
                <Search className="w-5 h-5 text-blue-400" />
                Live JWT Token Inspector & Tester
              </h2>
              <p className="text-xs text-slate-400 mt-0.5">
                Paste an end-user identity token to verify signature validation, examine claims normalization, and test policy evaluation.
              </p>
            </div>

            <form onSubmit={handleInspectToken} className="space-y-3">
              <textarea
                rows={3}
                placeholder="Paste Bearer JWT token (eyJhbGciOiJSUzI1NiIs...)"
                value={testTokenInput}
                onChange={(e) => setTestTokenInput(e.target.value)}
                className="w-full bg-slate-950 border border-slate-800 rounded-xl p-3 text-xs font-mono text-slate-200 focus:outline-none focus:border-blue-500"
              />

              <div className="flex justify-end">
                <button
                  type="submit"
                  disabled={testingToken || !testTokenInput.trim()}
                  className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white rounded-xl text-xs font-semibold flex items-center gap-1.5 transition-colors shadow-lg shadow-blue-900/20"
                >
                  <Search className="w-3.5 h-3.5" />
                  <span>{testingToken ? 'Testing Token...' : 'Inspect & Verify Token'}</span>
                </button>
              </div>
            </form>

            {/* Results Viewer */}
            {tokenResult && (
              <div
                className={`border rounded-2xl p-5 space-y-4 ${
                  tokenResult.valid
                    ? 'bg-emerald-500/5 border-emerald-500/30'
                    : 'bg-rose-500/5 border-rose-500/30'
                }`}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    {tokenResult.valid ? (
                      <CheckCircle2 className="w-5 h-5 text-emerald-400" />
                    ) : (
                      <XCircle className="w-5 h-5 text-rose-400" />
                    )}
                    <span
                      className={`text-sm font-bold uppercase tracking-wider ${
                        tokenResult.valid ? 'text-emerald-400' : 'text-rose-400'
                      }`}
                    >
                      {tokenResult.valid ? 'Token Validated Successfully' : 'Token Verification Failed'}
                    </span>
                  </div>

                  {tokenResult.expires_at && (
                    <span className="text-[11px] font-mono text-slate-400">
                      Expires: {new Date(tokenResult.expires_at).toLocaleString()}
                    </span>
                  )}
                </div>

                {tokenResult.error && (
                  <div className="text-xs font-mono text-rose-300 bg-rose-500/10 p-3 rounded-xl border border-rose-500/20">
                    {tokenResult.error}
                  </div>
                )}

                {tokenResult.valid && (
                  <div className="space-y-3">
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                      <div className="bg-slate-950/80 p-3 rounded-xl border border-slate-800">
                        <span className="text-[10px] uppercase font-bold text-slate-500">Subject (`sub`)</span>
                        <p className="text-xs font-mono text-slate-200 mt-0.5 truncate">{tokenResult.subject}</p>
                      </div>
                      <div className="bg-slate-950/80 p-3 rounded-xl border border-slate-800">
                        <span className="text-[10px] uppercase font-bold text-slate-500">Issuer (`iss`)</span>
                        <p className="text-xs font-mono text-slate-200 mt-0.5 truncate">{tokenResult.issuer}</p>
                      </div>
                      <div className="bg-slate-950/80 p-3 rounded-xl border border-slate-800">
                        <span className="text-[10px] uppercase font-bold text-slate-500">Audience (`aud`)</span>
                        <p className="text-xs font-mono text-slate-200 mt-0.5 truncate">
                          {tokenResult.audience || '(none)'}
                        </p>
                      </div>
                    </div>

                    <div>
                      <span className="text-xs font-semibold text-slate-300 block mb-1">
                        Decoded Identity Claims (Feeds CEL Policies & Limits)
                      </span>
                      <pre className="bg-slate-950 p-4 rounded-xl text-xs font-mono text-emerald-400 border border-slate-800 overflow-x-auto max-h-60">
                        {JSON.stringify(tokenResult.claims, null, 2)}
                      </pre>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Tab 2: Projects & Environments */}
      {activeTab === 'projects' && (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
          <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
            <Layers className="w-5 h-5 text-blue-400" />
            Projects & Environments
          </h2>

          {isOwner && (
            <form onSubmit={handleCreateProject} className="grid grid-cols-1 md:grid-cols-4 gap-3 pt-2">
              <input
                type="text"
                required
                placeholder="Project ID (e.g. proj_prod)"
                value={newProjId}
                onChange={(e) => setNewProjId(e.target.value)}
                className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 font-mono focus:outline-none focus:border-emerald-500"
              />
              <input
                type="text"
                required
                placeholder="Project Name"
                value={newProjName}
                onChange={(e) => setNewProjName(e.target.value)}
                className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
              />
              <input
                type="text"
                placeholder="Description"
                value={newProjDesc}
                onChange={(e) => setNewProjDesc(e.target.value)}
                className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
              />
              <button
                type="submit"
                className="bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-xs font-medium py-2 px-4 transition-colors"
              >
                Add Project
              </button>
            </form>
          )}

          <div className="divide-y divide-slate-800/80 pt-2">
            {projects.map((p) => (
              <div key={p.id} className="py-2.5 flex items-center justify-between text-xs font-mono">
                <span className="font-semibold text-slate-200">
                  {p.name} ({p.id})
                </span>
                <span className="text-slate-500">{new Date(p.created_at).toLocaleDateString()}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Tab 3: Admin Users Management (Owner Only) */}
      {activeTab === 'users' && isOwner && (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
          <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
            <Users className="w-5 h-5 text-purple-400" />
            Admin Users & RBAC
          </h2>

          <form onSubmit={handleCreateUser} className="grid grid-cols-1 md:grid-cols-4 gap-3 pt-2">
            <input
              type="email"
              required
              placeholder="Admin Email"
              value={newUserEmail}
              onChange={(e) => setNewUserEmail(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
            />
            <input
              type="password"
              required
              placeholder="Password (min 8 chars)"
              value={newUserPassword}
              onChange={(e) => setNewUserPassword(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
            />
            <select
              value={newUserRole}
              onChange={(e) => setNewUserRole(e.target.value as any)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
            >
              <option value="viewer">Viewer (Read-only)</option>
              <option value="editor">Editor (Config write)</option>
              <option value="owner">Owner (Full admin)</option>
            </select>
            <button
              type="submit"
              className="bg-purple-600 hover:bg-purple-500 text-white rounded-xl text-xs font-medium py-2 px-4 transition-colors"
            >
              Create Admin
            </button>
          </form>

          <div className="divide-y divide-slate-800/80 pt-2">
            {users.map((u) => (
              <div key={u.id} className="py-2.5 flex items-center justify-between text-xs">
                <div>
                  <span className="font-semibold text-slate-200 mr-2">{u.email}</span>
                  <span className="font-mono text-[10px] uppercase font-bold px-1.5 py-0.5 rounded bg-slate-800 text-slate-400 border border-slate-700">
                    {u.role}
                  </span>
                </div>
                {u.id !== currentUser?.id && (
                  <button
                    onClick={() => handleDeleteUser(u.id)}
                    className="p-1 text-slate-500 hover:text-rose-400"
                    title="Delete User"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Tab 4: Compliance & Privacy Purge */}
      {activeTab === 'compliance' && (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
          <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
            <ShieldAlert className="w-5 h-5 text-rose-400" />
            GDPR Privacy Purge (Subject Deletion)
          </h2>
          <p className="text-xs text-slate-400">
            Permanently purges all cached response keys, tool execution logs, and usage records associated with an authenticated end-user subject across all environments.
          </p>

          {purgeSuccess && (
            <div className="p-3 bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 rounded-xl text-xs flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 flex-shrink-0" />
              <span>{purgeSuccess}</span>
            </div>
          )}

          <form onSubmit={handlePrivacyPurge} className="space-y-3 pt-2">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">
                  Identity Issuer URL
                </label>
                <input
                  type="text"
                  required
                  value={purgeIssuer}
                  onChange={(e) => setPurgeIssuer(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 focus:outline-none focus:border-rose-500"
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">
                  Subject Identifier (`sub`)
                </label>
                <input
                  type="text"
                  required
                  placeholder="e.g. usr_9981247"
                  value={purgeSubject}
                  onChange={(e) => setPurgeSubject(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 focus:outline-none focus:border-rose-500"
                />
              </div>
            </div>

            <div className="flex justify-end pt-1">
              <button
                type="submit"
                className="px-4 py-2 bg-rose-600 hover:bg-rose-500 text-white rounded-xl text-xs font-medium transition-colors shadow-lg shadow-rose-900/20"
              >
                Execute Privacy Purge
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
};
