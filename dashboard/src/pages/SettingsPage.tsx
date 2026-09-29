import React, { useEffect, useState } from 'react';
import {
  api,
  AdminUser,
  Project,
  Environment,
  AuthConfig,
  TestTokenResult,
  DiffReport,
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
  FileCode,
  Download,
  Upload,
  Copy,
  Check,
  RefreshCw,
} from 'lucide-react';

interface SettingsPageProps {
  currentUser: AdminUser | null;
  projects: Project[];
  currentProject: string;
  currentEnv: string;
  onRefreshProjects: () => void;
}

type SettingsTab = 'auth' | 'projects' | 'configcode' | 'users' | 'compliance';

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

  // Config as Code State
  const [exportedYAML, setExportedYAML] = useState('');
  const [loadingExport, setLoadingExport] = useState(false);
  const [importYAML, setImportYAML] = useState('');
  const [importing, setImporting] = useState(false);
  const [diffReport, setDiffReport] = useState<DiffReport | null>(null);
  const [importSuccess, setImportSuccess] = useState<string | null>(null);
  const [importError, setImportError] = useState<string | null>(null);
  const [copiedYAML, setCopiedYAML] = useState(false);

  const isOwner = currentUser?.role === 'owner';

  const handleExport = async () => {
    setLoadingExport(true);
    setImportError(null);
    try {
      const yml = await api.exportConfig(currentProject, currentEnv);
      setExportedYAML(yml);
    } catch (err: any) {
      setImportError(err.message || 'Failed to export configuration');
    } finally {
      setLoadingExport(false);
    }
  };

  const handleDownloadYAML = () => {
    if (!exportedYAML) return;
    const blob = new Blob([exportedYAML], { type: 'application/x-yaml' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${currentProject}-${currentEnv}.yaml`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleCopyYAML = async () => {
    if (!exportedYAML) return;
    await navigator.clipboard.writeText(exportedYAML);
    setCopiedYAML(true);
    setTimeout(() => setCopiedYAML(false), 2000);
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (event) => {
      const text = event.target?.result as string;
      setImportYAML(text);
      setDiffReport(null);
      setImportSuccess(null);
      setImportError(null);
    };
    reader.readAsText(file);
  };

  const handlePreviewDiff = async () => {
    if (!importYAML.trim()) {
      setImportError('Please provide YAML configuration to preview');
      return;
    }
    setImporting(true);
    setImportError(null);
    setImportSuccess(null);
    try {
      const report = await api.importConfig(currentProject, currentEnv, importYAML, true);
      setDiffReport(report);
    } catch (err: any) {
      setImportError(err.message || 'Failed to generate diff plan');
    } finally {
      setImporting(false);
    }
  };

  const handleApplyImport = async () => {
    if (!importYAML.trim()) return;
    if (!confirm(`Apply configuration to project "${currentProject}" environment "${currentEnv}"?`)) {
      return;
    }
    setImporting(true);
    setImportError(null);
    setImportSuccess(null);
    try {
      const report = await api.importConfig(currentProject, currentEnv, importYAML, false);
      setDiffReport(report);
      setImportSuccess(
        `Successfully applied configuration! Created: ${report.summary.total_created}, Updated: ${report.summary.total_updated}, Unchanged: ${report.summary.total_unchanged}. Active snapshot invalidated.`
      );
      onRefreshProjects();
    } catch (err: any) {
      setImportError(err.message || 'Failed to apply configuration');
    } finally {
      setImporting(false);
    }
  };

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
          <button
            onClick={() => setActiveTab('configcode')}
            className={`px-3 py-1.5 rounded-lg transition-colors flex items-center gap-1.5 ${
              activeTab === 'configcode'
                ? 'bg-slate-800 text-slate-100 font-semibold shadow-sm'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <FileCode className="w-3.5 h-3.5 text-emerald-400" />
            Config as Code
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

      {/* Tab: Config as Code (GitOps & Multi-Environment Promotion) */}
      {activeTab === 'configcode' && (
        <div className="space-y-6">
          {/* Header Card */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6">
            <div className="flex items-start justify-between">
              <div>
                <h2 className="text-base font-semibold text-slate-100 flex items-center gap-2">
                  <FileCode className="w-5 h-5 text-emerald-400" />
                  Configuration as Code & GitOps
                </h2>
                <p className="text-xs text-slate-400 mt-1">
                  Export and reconcile complete declarative project specifications (<code>beavend_version: "0.1"</code>).
                  Promote configurations from development to production or version control your entire stack in Git.
                </p>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs px-2.5 py-1 bg-slate-800 border border-slate-700 text-slate-300 rounded-lg font-mono">
                  Project: <strong className="text-emerald-400">{currentProject}</strong> / <strong className="text-blue-400">{currentEnv}</strong>
                </span>
              </div>
            </div>
          </div>

          {/* Feedback Banners */}
          {importSuccess && (
            <div className="p-4 bg-emerald-950/40 border border-emerald-800/60 rounded-xl text-xs text-emerald-300 flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>{importSuccess}</span>
            </div>
          )}
          {importError && (
            <div className="p-4 bg-rose-950/40 border border-rose-800/60 rounded-xl text-xs text-rose-300 flex items-center gap-2">
              <AlertCircle className="w-4 h-4 text-rose-400 shrink-0" />
              <span>{importError}</span>
            </div>
          )}

          {/* Two-Column Grid: Export & Import */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Left: Export */}
            <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4 flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between pb-3 border-b border-slate-800">
                  <h3 className="text-sm font-semibold text-slate-200 flex items-center gap-2">
                    <Download className="w-4 h-4 text-emerald-400" />
                    Export Configuration
                  </h3>
                  <button
                    onClick={handleExport}
                    disabled={loadingExport}
                    className="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white rounded-lg text-xs font-medium flex items-center gap-1.5 transition-colors"
                  >
                    {loadingExport ? (
                      <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <Download className="w-3.5 h-3.5" />
                    )}
                    Generate YAML
                  </button>
                </div>
                <p className="text-xs text-slate-400 mt-3">
                  Generates deterministic YAML containing all AI Apps, Prompts, Models, Tools, Policies, Limits, and Auth settings. Sensitive secrets are safely masked with <code>[SECRET_MASKED]</code> or environment references.
                </p>

                {exportedYAML && (
                  <div className="mt-4 relative">
                    <pre className="bg-slate-950 border border-slate-800 rounded-xl p-3 text-[11px] font-mono text-slate-300 max-h-96 overflow-y-auto leading-relaxed">
                      {exportedYAML}
                    </pre>
                    <div className="absolute top-2 right-2 flex items-center gap-1.5">
                      <button
                        onClick={handleCopyYAML}
                        className="px-2 py-1 bg-slate-800/80 hover:bg-slate-700 text-slate-200 rounded text-[10px] flex items-center gap-1 backdrop-blur-sm transition-colors"
                      >
                        {copiedYAML ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                        {copiedYAML ? 'Copied!' : 'Copy'}
                      </button>
                      <button
                        onClick={handleDownloadYAML}
                        className="px-2 py-1 bg-emerald-600/80 hover:bg-emerald-500 text-white rounded text-[10px] flex items-center gap-1 backdrop-blur-sm transition-colors"
                      >
                        <Download className="w-3 h-3" />
                        Download
                      </button>
                    </div>
                  </div>
                )}
              </div>
            </div>

            {/* Right: Import & Reconcile */}
            <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
              <div className="flex items-center justify-between pb-3 border-b border-slate-800">
                <h3 className="text-sm font-semibold text-slate-200 flex items-center gap-2">
                  <Upload className="w-4 h-4 text-blue-400" />
                  Import & Reconcile
                </h3>
                <label className="cursor-pointer px-3 py-1.5 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-lg text-xs font-medium flex items-center gap-1.5 transition-colors border border-slate-700">
                  <Upload className="w-3.5 h-3.5 text-blue-400" />
                  Upload .yaml
                  <input
                    type="file"
                    accept=".yaml,.yml,.json"
                    onChange={handleFileUpload}
                    className="hidden"
                  />
                </label>
              </div>

              <textarea
                value={importYAML}
                onChange={(e) => setImportYAML(e.target.value)}
                placeholder="Paste YAML configuration document here..."
                rows={10}
                className="w-full bg-slate-950 border border-slate-800 rounded-xl p-3 text-xs font-mono text-slate-100 focus:outline-none focus:border-blue-500 transition-colors leading-relaxed"
              />

              <div className="flex items-center justify-between pt-1">
                <button
                  onClick={handlePreviewDiff}
                  disabled={importing || !importYAML.trim()}
                  className="px-4 py-2 bg-slate-800 hover:bg-slate-700 disabled:opacity-40 text-slate-200 rounded-xl text-xs font-medium flex items-center gap-1.5 border border-slate-700 transition-colors"
                >
                  <Search className="w-3.5 h-3.5 text-blue-400" />
                  Preview Changes (Dry Run)
                </button>
                <button
                  onClick={handleApplyImport}
                  disabled={importing || !importYAML.trim()}
                  className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 text-white rounded-xl text-xs font-medium flex items-center gap-1.5 transition-colors shadow-sm"
                >
                  {importing ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
                  Apply Configuration
                </button>
              </div>

              {/* Diff Plan Card */}
              {diffReport && (
                <div className="mt-4 p-4 bg-slate-950 border border-slate-800 rounded-xl space-y-3">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-semibold text-slate-200">Diff Plan</span>
                    <div className="flex items-center gap-2 text-[11px]">
                      <span className="px-2 py-0.5 rounded bg-emerald-950/80 text-emerald-400 border border-emerald-800/60">
                        {diffReport.summary.total_created} to create
                      </span>
                      <span className="px-2 py-0.5 rounded bg-amber-950/80 text-amber-400 border border-amber-800/60">
                        {diffReport.summary.total_updated} to update
                      </span>
                      <span className="px-2 py-0.5 rounded bg-slate-800 text-slate-400">
                        {diffReport.summary.total_unchanged} unchanged
                      </span>
                    </div>
                  </div>

                  <div className="divide-y divide-slate-800/60 max-h-60 overflow-y-auto pr-1">
                    {diffReport.resources.map((res, idx) => (
                      <div key={idx} className="py-2 text-xs flex flex-col gap-1">
                        <div className="flex items-center justify-between font-mono">
                          <span className="text-slate-300 font-medium">
                            {res.type}: <strong className="text-slate-100">{res.id}</strong>
                          </span>
                          <span
                            className={`text-[10px] font-bold px-1.5 py-0.5 rounded uppercase ${
                              res.action === 'create'
                                ? 'bg-emerald-900/60 text-emerald-300 border border-emerald-700/60'
                                : res.action === 'update'
                                ? 'bg-amber-900/60 text-amber-300 border border-amber-700/60'
                                : 'bg-slate-800/80 text-slate-400'
                            }`}
                          >
                            {res.action}
                          </span>
                        </div>
                        {res.changes && res.changes.length > 0 && (
                          <div className="pl-3 border-l-2 border-slate-800 text-[11px] text-slate-400 font-mono space-y-0.5">
                            {res.changes.map((c, i) => (
                              <div key={i}>{c}</div>
                            ))}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
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
