import React, { useEffect, useState } from 'react';
import { api, parseAuthConfig } from '../api/client';
import {
  Bot,
  Cpu,
  Wrench,
  ShieldCheck,
  Activity,
  Zap,
  Shield,
  Database,
  Network,
  ArrowRight,
  TrendingUp,
  Clock,
} from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Sparkline } from '../components/ui/Feedback';
import { formatNumber } from '../lib/utils';

interface StatCardProps {
  label: string;
  value: number | string;
  loading: boolean;
  icon: React.ReactNode;
  color: string;
  sparkData?: number[];
  trend?: string;
}

const StatCard: React.FC<StatCardProps> = ({ label, value, loading, icon, color, sparkData, trend }) => (
  <Card>
    <div className="flex items-start justify-between">
      <div className={`w-9 h-9 rounded-lg flex items-center justify-center ${color}`}>
        {icon}
      </div>
      {sparkData && <Sparkline data={sparkData} height={28} width={64} />}
    </div>
    <div className="mt-3">
      <p className="text-xs text-neutral-500 font-medium">{label}</p>
      <p className="text-2xl font-bold text-neutral-100 mt-0.5 tracking-tight">
        {loading ? (
          <span className="inline-block w-8 h-6 bg-neutral-800 rounded animate-pulse" />
        ) : (
          value
        )}
      </p>
      {trend && !loading && (
        <p className="text-[11px] text-neutral-600 mt-1 flex items-center gap-1">
          <TrendingUp className="w-3 h-3 text-emerald-500" />
          {trend}
        </p>
      )}
    </div>
  </Card>
);

interface InvariantItemProps {
  title: string;
  description: string;
  icon: React.ReactNode;
  status?: 'active' | 'warning';
}

const InvariantItem: React.FC<InvariantItemProps> = ({ title, description, icon, status = 'active' }) => (
  <div className="flex items-start gap-3 p-3.5 rounded-lg bg-neutral-950/60 border border-neutral-800/80">
    <div className={`w-7 h-7 rounded-lg flex items-center justify-center flex-shrink-0 mt-0.5 ${
      status === 'active' ? 'bg-emerald-500/10 text-emerald-400' : 'bg-amber-500/10 text-amber-400'
    }`}>
      {icon}
    </div>
    <div className="min-w-0">
      <div className="flex items-center gap-2">
        <p className="text-xs font-semibold text-neutral-200">{title}</p>
        <Badge variant={status === 'active' ? 'success' : 'warning'} dot>
          {status}
        </Badge>
      </div>
      <p className="text-[11px] text-neutral-600 mt-0.5 leading-relaxed">{description}</p>
    </div>
  </div>
);

export const OverviewPage: React.FC = () => {
  const [appsCount, setAppsCount] = useState(0);
  const [modelsCount, setModelsCount] = useState(0);
  const [toolsCount, setToolsCount] = useState(0);
  const [policiesCount, setPoliciesCount] = useState(0);
  const [providersCount, setProvidersCount] = useState(0);
  const [hasAuthConfig, setHasAuthConfig] = useState(false);
  const [loading, setLoading] = useState(true);
  const [usageData, setUsageData] = useState<any[]>([]);
  const [totalRequests, setTotalRequests] = useState(0);
  const [totalTokens, setTotalTokens] = useState(0);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [apps, models, tools, policies, providers, auth, usage] = await Promise.all([
          api.listApps().catch(() => []),
          api.listModels().catch(() => []),
          api.listTools().catch(() => []),
          api.listPolicies().catch(() => []),
          api.listProviders().catch(() => []),
          api.getEnvironmentAuthConfig(api.currentProject, api.currentEnv).catch(() => null),
          api.getUsage('hour').catch(() => []),
        ]);
        setAppsCount(apps.length);
        setModelsCount(models.length);
        setToolsCount(tools.length);
        setPoliciesCount(policies.length);
        setProvidersCount(providers.length);
        const appsWithAuth = apps.filter((a) => {
          const cfg = parseAuthConfig(a.auth || a.auth_config);
          return cfg && cfg.provider && cfg.provider !== 'anonymous';
        });
        const hasEnvAuth = Boolean(auth?.provider && auth.provider !== 'anonymous');
        const hasActiveAuth = hasEnvAuth || appsWithAuth.length > 0;
        setHasAuthConfig(hasActiveAuth);
        setUsageData(usage);
        setTotalRequests(usage.reduce((s: number, u: any) => s + (u.total_requests ?? u.request_count ?? 0), 0));
        setTotalTokens(usage.reduce((s: number, u: any) => s + (u.total_tokens ?? 0), 0));
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, []);

  const curlExample = `curl -X POST "http://localhost:8080/v1/apps/{app_id}/chat" \\
  -H "Content-Type: application/json" \\
  -H "X-Project-ID: ${api.currentProject}" \\
  -H "X-Environment: ${api.currentEnv}" \\
  -H "Authorization: Bearer <USER_JWT_OR_APP_KEY>" \\
  -d '{"messages":[{"role":"user","content":"Hello!"}]}'`;

  return (
    <div className="space-y-6 max-w-[1100px]">
      {/* Page Header */}
      <div>
        <h1 className="text-xl font-bold text-neutral-100 tracking-tight">Overview</h1>
        <p className="text-sm text-neutral-500 mt-0.5">
          AI runtime status, system health, and quick-start integration guide.
        </p>
      </div>

      {/* Stats Row */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          label="AI Applications"
          value={appsCount}
          loading={loading}
          icon={<Bot className="w-4 h-4" />}
          color="bg-emerald-500/10 text-emerald-400"
          trend="Configured applications"
        />
        <StatCard
          label="Model Routings"
          value={modelsCount}
          loading={loading}
          icon={<Cpu className="w-4 h-4" />}
          color="bg-blue-500/10 text-blue-400"
          trend={`${providersCount} provider(s) configured`}
        />
        <StatCard
          label="Registered Tools"
          value={toolsCount}
          loading={loading}
          icon={<Wrench className="w-4 h-4" />}
          color="bg-amber-500/10 text-amber-400"
          trend="Custom executors"
        />
        <StatCard
          label="CEL Policies"
          value={policiesCount}
          loading={loading}
          icon={<ShieldCheck className="w-4 h-4" />}
          color="bg-purple-500/10 text-purple-400"
          trend="Authorization rules"
        />
      </div>

      {/* Usage Stats (last 24h from real API) */}
      {(totalRequests > 0 || totalTokens > 0) && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          <StatCard
            label="Requests (24h)"
            value={formatNumber(totalRequests)}
            loading={loading}
            icon={<Activity className="w-4 h-4" />}
            color="bg-emerald-500/10 text-emerald-400"
            sparkData={usageData.slice(-7).map((u: any) => u.total_requests ?? u.request_count ?? 0)}
            trend="From usage rollups"
          />
          <StatCard
            label="Tokens (24h)"
            value={formatNumber(totalTokens)}
            loading={loading}
            icon={<Zap className="w-4 h-4" />}
            color="bg-yellow-500/10 text-yellow-400"
            sparkData={usageData.slice(-7).map((u: any) => u.total_tokens ?? 0)}
            trend="Input + output"
          />
          <div className="col-span-2 bg-neutral-900 border border-neutral-800 rounded-xl p-4 flex items-center gap-3">
            <div className="w-8 h-8 rounded-lg bg-emerald-500/10 border border-emerald-500/15 flex items-center justify-center flex-shrink-0">
              <TrendingUp className="w-4 h-4 text-emerald-400" />
            </div>
            <div>
              <p className="text-xs font-medium text-neutral-300">Live usage data from <code className="text-emerald-400 text-[11px]">/v1/admin/usage</code></p>
              <p className="text-[11px] text-neutral-600 mt-0.5">
                Aggregated hourly rollups ({usageData.length} bucket{usageData.length !== 1 ? 's' : ''} in the last 24h)
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Main content grid */}
      <div className="grid grid-cols-1 lg:grid-cols-5 gap-4">
        {/* Runtime Invariants */}
        <div className="lg:col-span-3 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold text-neutral-300 flex items-center gap-2">
              <Activity className="w-4 h-4 text-neutral-500" />
              Runtime Architecture &amp; Health
            </h2>
            <Badge variant={hasAuthConfig && providersCount > 0 ? 'success' : 'warning'} dot>
              {hasAuthConfig && providersCount > 0 ? 'All systems operational' : 'Setup incomplete'}
            </Badge>
          </div>

          <div className="space-y-2">
            <InvariantItem
              title="Identity &amp; Auth Validation"
              description={hasAuthConfig ? 'End-user JWT verification active with automated JWKS key rotation (app & environment auth providers active).' : '⚠️ No auth provider configured for this environment or applications. Configure Identity & Auth in an AI App or Environment.'}
              icon={<Shield className="w-3.5 h-3.5" />}
              status={hasAuthConfig ? 'active' : 'warning'}
            />
            <InvariantItem
              title="AI Model Providers"
              description={providersCount > 0 ? `${providersCount} upstream provider credential(s) active with AES-256-GCM envelope encryption.` : '⚠️ No upstream providers configured. Configure LiteLLM or OpenAI in Models & Routing.'}
              icon={<Cpu className="w-3.5 h-3.5" />}
              status={providersCount > 0 ? 'active' : 'warning'}
            />
            <InvariantItem
              title="Zero DB Reads on the Hot Path"
              description="Runtime instances compile configuration into atomic in-memory snapshots. Config changes propagate via Redis pub/sub without interrupting active requests."
              icon={<Database className="w-3.5 h-3.5" />}
            />
            <InvariantItem
              title="SSRF Guard &amp; Confused-Deputy Defense"
              description="All HTTP tool executor calls use DNS-rebinding-safe dialers that block loopback, private ranges, and cloud metadata endpoints. Identity fields are injected from verified JWT claims."
              icon={<Shield className="w-3.5 h-3.5" />}
            />
            <InvariantItem
              title="Bounded Agent Loop &amp; Stateless Replication"
              description="Tool execution is capped at 5 iterations. Suspended confirmation state is persisted to Redis with 5-minute TTL, enabling any replica to resume requests."
              icon={<Network className="w-3.5 h-3.5" />}
            />
            <InvariantItem
              title="Async Usage Pipeline"
              description="Token usage and cost are written via a bounded channel pipeline with batched async writes; never in the request hot path."
              icon={<Zap className="w-3.5 h-3.5" />}
            />
          </div>
        </div>

        {/* Quick Start */}
        <div className="lg:col-span-2 space-y-3">
          <h2 className="text-sm font-semibold text-neutral-300 flex items-center gap-2">
            <Zap className="w-4 h-4 text-neutral-500" />
            Quick Integration
          </h2>

          <Card padding="md">
            <div className="flex items-center gap-2 mb-2.5">
              <span className="text-[11px] text-neutral-500">Target Env:</span>
              <Badge variant="purple">{api.currentEnv}</Badge>
              <span className="text-[11px] text-neutral-500 ml-1">Project:</span>
              <Badge variant="mono">{api.currentProject}</Badge>
            </div>
            <p className="text-xs text-neutral-500 mb-3">
              Pass your authenticated end-user JWT or scoped App API Key (<code className="text-neutral-400">bv_app_sec_...</code>) in the Authorization header.
              Requests are dynamically routed to the active <b className="text-neutral-300 font-mono text-[11px]">{api.currentEnv}</b> environment.
            </p>
            <div className="bg-neutral-950 border border-neutral-800 rounded-lg p-3 font-mono text-[11px] text-neutral-400 overflow-x-auto">
              <pre className="whitespace-pre">{curlExample}</pre>
            </div>
            <p className="text-[11px] text-neutral-600 mt-2">
              Stream responses via <code className="text-neutral-400 font-mono">POST /v1/apps/{'{id}'}/stream</code>
            </p>
          </Card>

          {/* Snapshot health */}
          <Card padding="sm">
            <div className="flex items-center justify-between mb-3">
              <p className="text-xs font-semibold text-neutral-300 flex items-center gap-2">
                <Clock className="w-3.5 h-3.5 text-neutral-500" />
                Snapshot Health
              </p>
              <Badge variant="success">Live</Badge>
            </div>
            <div className="space-y-2">
              {[
                { key: 'Config Compiler', val: 'Atomic pointer swap' },
                { key: 'Sync Transport', val: 'Redis pub/sub + PG LISTEN' },
                { key: 'Cache Scope', val: 'User-isolated (Redis)' },
                { key: 'Secrets', val: 'AES-256-GCM envelope' },
              ].map((item) => (
                <div key={item.key} className="flex items-center justify-between text-[11px]">
                  <span className="text-neutral-600">{item.key}</span>
                  <span className="text-neutral-400 font-mono">{item.val}</span>
                </div>
              ))}
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
};
