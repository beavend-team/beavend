import React, { useEffect, useState } from 'react';
import {
  api,
  AuthConfig,
  TestTokenResult,
} from '../api/client';
import {
  KeyRound,
  ShieldCheck,
  Search,
  Save,
  CheckCircle2,
  XCircle,
  Copy,
  Check,
  Info,
  Clock,
  ExternalLink,
} from 'lucide-react';
import { Card, CardHeader } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Alert } from '../components/ui/Feedback';
import { Input, Textarea } from '../components/ui/Form';

interface IdentityPageProps {
  currentProject: string;
  currentEnv: string;
}

export const IdentityPage: React.FC<IdentityPageProps> = ({
  currentProject,
  currentEnv,
}) => {
  // Auth configuration state
  const [authProvider, setAuthProvider] = useState<'firebase' | 'supabase' | 'oidc' | 'jwt'>('firebase');
  const [firebaseProjectId, setFirebaseProjectId] = useState('');
  const [supabaseIssuer, setSupabaseIssuer] = useState('');
  const [oidcIssuer, setOidcIssuer] = useState('');
  const [oidcAudience, setOidcAudience] = useState('');
  const [oidcJwksUrl, setOidcJwksUrl] = useState('');
  const [clockSkewSeconds, setClockSkewSeconds] = useState(60);
  const [authLoading, setAuthLoading] = useState(false);
  const [authSaving, setAuthSaving] = useState(false);
  const [authSaveSuccess, setAuthSaveSuccess] = useState<string | null>(null);
  const [authSaveError, setAuthSaveError] = useState<string | null>(null);

  // JWT Inspector state
  const [testTokenInput, setTestTokenInput] = useState('');
  const [testingToken, setTestingToken] = useState(false);
  const [tokenResult, setTokenResult] = useState<TestTokenResult | null>(null);
  const [claimsCopied, setClaimsCopied] = useState(false);

  const loadAuthConfig = async () => {
    setAuthLoading(true);
    setAuthSaveSuccess(null);
    setAuthSaveError(null);
    try {
      const cfg = await api.getEnvironmentAuthConfig(currentProject, currentEnv);
      if (cfg?.provider) {
        const p = cfg.provider.toLowerCase();
        if (p === 'firebase') {
          setAuthProvider('firebase');
          setFirebaseProjectId(cfg.project_id || '');
        } else if (p === 'supabase') {
          setAuthProvider('supabase');
          setSupabaseIssuer(cfg.issuer || '');
        } else if (p === 'jwt') {
          setAuthProvider('jwt');
          setOidcIssuer(cfg.issuer || '');
          setOidcAudience(Array.isArray(cfg.audience) ? cfg.audience.join(', ') : '');
          setOidcJwksUrl(cfg.jwks_url || '');
        } else {
          setAuthProvider('oidc');
          setOidcIssuer(cfg.issuer || '');
          setOidcAudience(Array.isArray(cfg.audience) ? cfg.audience.join(', ') : '');
          setOidcJwksUrl(cfg.jwks_url || '');
        }
        if (cfg.clock_skew) {
          setClockSkewSeconds(Math.round(Number(cfg.clock_skew) / 1e9));
        }
      } else {
        // Reset defaults when empty
        setAuthProvider('firebase');
        setFirebaseProjectId('');
        setSupabaseIssuer('');
        setOidcIssuer('');
        setOidcAudience('');
        setOidcJwksUrl('');
        setClockSkewSeconds(60);
      }
    } catch (_) {
      // ignore
    } finally {
      setAuthLoading(false);
    }
  };

  useEffect(() => {
    loadAuthConfig();
  }, [currentProject, currentEnv]);

  const handleSaveAuthConfig = async (e: React.FormEvent) => {
    e.preventDefault();
    setAuthSaving(true);
    setAuthSaveSuccess(null);
    setAuthSaveError(null);

    const payload: AuthConfig = {
      provider: authProvider,
      clock_skew: Number(clockSkewSeconds) * 1e9,
    };

    if (authProvider === 'firebase') {
      const trimmedProj = firebaseProjectId.trim();
      payload.project_id = trimmedProj;
      payload.issuer = `https://securetoken.google.com/${trimmedProj}`;
      payload.audience = [trimmedProj];
      payload.jwks_url = 'https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com';
    } else if (authProvider === 'supabase') {
      const trimmedIssuer = supabaseIssuer.trim();
      payload.issuer = trimmedIssuer;
      payload.audience = ['authenticated'];
      payload.jwks_url = `${trimmedIssuer.replace(/\/+$/, '')}/.well-known/jwks.json`;
    } else if (authProvider === 'jwt' || authProvider === 'oidc') {
      payload.provider = authProvider;
      payload.issuer = oidcIssuer.trim();
      payload.jwks_url = oidcJwksUrl.trim();
      payload.audience = oidcAudience.split(',').map((s) => s.trim()).filter(Boolean);
    }

    try {
      await api.updateEnvironmentAuthConfig(currentProject, currentEnv, payload);
      setAuthSaveSuccess(`Identity provider configuration saved for ${currentProject}/${currentEnv}. Snapshot synchronized.`);
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
      const res = await api.testToken({ token: testTokenInput.trim() });
      setTokenResult(res);
    } catch (err: any) {
      setTokenResult({ valid: false, error: err.message || 'Token verification failed' });
    } finally {
      setTestingToken(false);
    }
  };

  const handleCopyClaims = async () => {
    if (!tokenResult?.claims) return;
    await navigator.clipboard.writeText(JSON.stringify(tokenResult.claims, null, 2));
    setClaimsCopied(true);
    setTimeout(() => setClaimsCopied(false), 2000);
  };

  return (
    <div className="space-y-6 max-w-[1000px]">
      {/* Page Header */}
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
            <KeyRound className="w-5 h-5 text-amber-400" />
            Identity & Authentication
          </h1>
          <p className="text-sm text-neutral-500 mt-1">
            Configure identity provider validation (Firebase, Supabase, OIDC, JWT) and verify end-user tokens.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs px-2.5 py-1 rounded-full bg-neutral-900 border border-neutral-800 text-neutral-400 font-mono">
            Project: <span className="text-neutral-200 font-semibold">{currentProject}</span>
          </span>
          <span className="text-xs px-2.5 py-1 rounded-full bg-blue-500/10 border border-blue-500/20 text-blue-400 font-mono">
            Env: <span className="text-blue-300 font-semibold">{currentEnv}</span>
          </span>
        </div>
      </div>

      {/* Architecture Context Banner */}
      <div className="bg-neutral-900/60 border border-neutral-800 rounded-xl p-4 flex gap-3 text-xs text-neutral-400 leading-relaxed">
        <Info className="w-4 h-4 text-emerald-400 flex-shrink-0 mt-0.5" />
        <div>
          <span className="font-semibold text-neutral-200">Zero-DB Identity Validation:</span>
          {' '}Beavend does not store end-user credentials or passwords. The runtime validates client JWTs in memory against your identity provider's public keys (cached JWKS) on every request, then automatically injects verified claims (like user ID) into downstream tools.
        </div>
      </div>

      {/* Identity Provider Configuration Card */}
      <Card padding="lg">
        <CardHeader
          title="Identity Provider Configuration"
          description="Select and configure the issuer responsible for signing client tokens for this environment."
          icon={<ShieldCheck className="w-4 h-4 text-emerald-400" />}
        />

        {authSaveSuccess && (
          <Alert variant="success" className="mb-4" onDismiss={() => setAuthSaveSuccess(null)}>
            {authSaveSuccess}
          </Alert>
        )}
        {authSaveError && (
          <Alert variant="error" className="mb-4" onDismiss={() => setAuthSaveError(null)}>
            {authSaveError}
          </Alert>
        )}

        <form onSubmit={handleSaveAuthConfig} className="space-y-5">
          {/* Provider Selection Cards */}
          <div>
            <label className="block text-xs font-medium text-neutral-400 mb-2">
              Identity Provider Type
            </label>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              {[
                { id: 'firebase', label: 'Firebase Auth', desc: 'Google Identity Toolkit', tag: 'Google Cloud' },
                { id: 'supabase', label: 'Supabase Auth', desc: 'GoTrue JWT & JWKS', tag: 'Postgres' },
                { id: 'oidc', label: 'Generic OIDC', desc: 'Auth0, Clerk, Keycloak', tag: 'OpenID Connect' },
                { id: 'jwt', label: 'Custom JWT', desc: 'Custom JWKS & Public Keys', tag: 'Stateless' },
              ].map(({ id, label, desc, tag }) => {
                const isActive = authProvider === id;
                return (
                  <button
                    key={id}
                    type="button"
                    onClick={() => setAuthProvider(id as any)}
                    className={`
                      p-4 rounded-xl border text-left transition-all relative overflow-hidden
                      ${isActive
                        ? 'bg-neutral-800/80 border-emerald-500/50 text-neutral-100 shadow-sm ring-1 ring-emerald-500/20'
                        : 'bg-neutral-950 border-neutral-800 text-neutral-500 hover:border-neutral-700'
                      }
                    `}
                  >
                    <div className="flex items-center justify-between mb-1">
                      <p className="text-xs font-semibold text-neutral-100">{label}</p>
                      {isActive && <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />}
                    </div>
                    <p className="text-[11px] text-neutral-500">{desc}</p>
                    <span className="inline-block mt-2 text-[9px] font-mono uppercase tracking-wider text-neutral-600 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800">
                      {tag}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Firebase fields */}
          {authProvider === 'firebase' && (
            <div className="bg-neutral-950 border border-neutral-800 rounded-xl p-4 space-y-3">
              <Input
                label="Firebase Project ID"
                required
                placeholder="e.g. my-firebase-app"
                value={firebaseProjectId}
                onChange={(e) => setFirebaseProjectId(e.target.value)}
                className="font-mono text-xs"
                hint="Issuer and Google JWKS endpoint will be automatically configured based on this Project ID."
              />
            </div>
          )}

          {/* Supabase fields */}
          {authProvider === 'supabase' && (
            <div className="bg-neutral-950 border border-neutral-800 rounded-xl p-4 space-y-3">
              <Input
                label="Supabase Project URL / Issuer"
                required
                placeholder="https://xyzproject.supabase.co/auth/v1"
                value={supabaseIssuer}
                onChange={(e) => setSupabaseIssuer(e.target.value)}
                className="font-mono text-xs"
                hint="Auto-derives JWKS endpoint (/.well-known/jwks.json) and enforces 'authenticated' audience."
              />
            </div>
          )}

          {/* OIDC / JWT fields */}
          {(authProvider === 'oidc' || authProvider === 'jwt') && (
            <div className="bg-neutral-950 border border-neutral-800 rounded-xl p-4 space-y-3">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <Input
                  label="Issuer URL (`iss`)"
                  required
                  placeholder="https://auth.yourdomain.com"
                  value={oidcIssuer}
                  onChange={(e) => setOidcIssuer(e.target.value)}
                  className="font-mono text-xs"
                />
                <Input
                  label="Expected Audience (`aud`)"
                  placeholder="my-app, my-web-app"
                  value={oidcAudience}
                  onChange={(e) => setOidcAudience(e.target.value)}
                  className="font-mono text-xs"
                  hint="Comma-separated list of accepted audience claims."
                />
              </div>
              <Input
                label="JWKS URL"
                required
                placeholder="https://auth.yourdomain.com/.well-known/jwks.json"
                value={oidcJwksUrl}
                onChange={(e) => setOidcJwksUrl(e.target.value)}
                className="font-mono text-xs"
                hint="Public endpoint serving JSON Web Key Sets used to verify cryptographic signatures."
              />
            </div>
          )}

          {/* Clock skew & Save */}
          <div className="flex items-center justify-between pt-2 border-t border-neutral-800/80">
            <div className="flex items-center gap-2">
              <Clock className="w-3.5 h-3.5 text-neutral-500" />
              <label className="text-xs text-neutral-400">Clock skew tolerance:</label>
              <input
                type="number"
                min="0"
                max="300"
                value={clockSkewSeconds}
                onChange={(e) => setClockSkewSeconds(Number(e.target.value))}
                className="w-16 bg-neutral-950 border border-neutral-800 rounded-lg px-2 py-1 text-xs font-mono text-neutral-200 focus:outline-none focus:border-neutral-600 text-center"
              />
              <span className="text-xs text-neutral-600">seconds</span>
            </div>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              loading={authSaving || authLoading}
              icon={<Save className="w-3.5 h-3.5" />}
            >
              Save Configuration
            </Button>
          </div>
        </form>
      </Card>

      {/* JWT Inspector Card */}
      <Card padding="lg">
        <CardHeader
          title="JWT Token Inspector & Debugger"
          description="Test your client token against the current environment JWKS configuration without sending a chat request."
          icon={<Search className="w-4 h-4 text-blue-400" />}
        />

        <form onSubmit={handleInspectToken} className="space-y-3">
          <Textarea
            rows={3}
            placeholder="Paste Bearer JWT token (eyJhbGciOiJSUzI1NiIs...)"
            value={testTokenInput}
            onChange={(e) => setTestTokenInput(e.target.value)}
            mono
          />
          <div className="flex justify-between items-center">
            <p className="text-[11px] text-neutral-600">
              Tokens are verified locally against the active JWKS cache. No token data is stored.
            </p>
            <Button
              type="submit"
              variant="secondary"
              size="sm"
              loading={testingToken}
              disabled={!testTokenInput.trim()}
              icon={<Search className="w-3.5 h-3.5" />}
            >
              Verify & Inspect Token
            </Button>
          </div>
        </form>

        {tokenResult && (
          <div
            className={`mt-4 border rounded-xl p-4 space-y-4 ${
              tokenResult.valid
                ? 'bg-emerald-500/5 border-emerald-500/20'
                : 'bg-red-500/5 border-red-500/20'
            }`}
          >
            {/* Status Bar */}
            <div className="flex items-center gap-2">
              {tokenResult.valid ? (
                <CheckCircle2 className="w-4 h-4 text-emerald-400 flex-shrink-0" />
              ) : (
                <XCircle className="w-4 h-4 text-red-400 flex-shrink-0" />
              )}
              <span
                className={`text-xs font-bold uppercase tracking-wider ${
                  tokenResult.valid ? 'text-emerald-400' : 'text-red-400'
                }`}
              >
                {tokenResult.valid ? 'Token Valid & Signature Verified' : 'Token Verification Failed'}
              </span>
              {tokenResult.expires_at && (
                <span className="text-[11px] text-neutral-500 ml-auto">
                  Expires: {new Date(tokenResult.expires_at).toLocaleString()}
                </span>
              )}
            </div>

            {/* Error Message */}
            {tokenResult.error && (
              <div className="text-xs font-mono text-red-300 bg-red-500/10 p-3 rounded-lg border border-red-500/20">
                {tokenResult.error}
              </div>
            )}

            {/* Valid Token Details */}
            {tokenResult.valid && (
              <div className="space-y-3">
                <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
                  {[
                    { label: 'Subject (`sub`)', value: tokenResult.subject || '(none)' },
                    { label: 'Issuer (`iss`)', value: tokenResult.issuer || '(none)' },
                    { label: 'Audience (`aud`)', value: tokenResult.audience || '(none)' },
                  ].map((item) => (
                    <div key={item.label} className="bg-neutral-950 p-2.5 rounded-lg border border-neutral-800">
                      <p className="text-[10px] uppercase text-neutral-600 font-bold">{item.label}</p>
                      <p className="text-[11px] font-mono text-neutral-300 mt-0.5 truncate" title={item.value}>
                        {item.value}
                      </p>
                    </div>
                  ))}
                </div>

                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <p className="text-[11px] font-medium text-neutral-400">Decoded Claims Payload</p>
                    <button
                      type="button"
                      onClick={handleCopyClaims}
                      className="text-[11px] text-neutral-500 hover:text-neutral-300 flex items-center gap-1 transition-colors"
                    >
                      {claimsCopied ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                      {claimsCopied ? 'Copied' : 'Copy JSON'}
                    </button>
                  </div>
                  <pre className="bg-neutral-950 p-3 rounded-lg text-[11px] font-mono text-emerald-400 border border-neutral-800 overflow-x-auto max-h-56 leading-relaxed">
                    {JSON.stringify(tokenResult.claims, null, 2)}
                  </pre>
                </div>
              </div>
            )}
          </div>
        )}
      </Card>
    </div>
  );
};
