import React, { useEffect, useState } from 'react';
import { api } from '../api/client';
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

// Simulated sparkline data for demo
const mockSparklines: Record<string, number[]> = {
  apps: [3, 3, 4, 4, 5, 5, 5],
  models: [1, 2, 2, 2, 3, 3, 3],
  tools: [0, 1, 2, 2, 3, 3, 4],
  policies: [0, 0, 1, 1, 2, 2, 2],
};

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
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [apps, models, tools, policies] = await Promise.all([
          api.listApps().catch(() => []),
          api.listModels().catch(() => []),
          api.listTools().catch(() => []),
          api.listPolicies().catch(() => []),
        ]);
        setAppsCount(apps.length);
        setModelsCount(models.length);
        setToolsCount(tools.length);
        setPoliciesCount(policies.length);
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, []);

  const curlExample = `POST /v1/apps/{app_id}/chat
Authorization: Bearer <user-jwt>
X-Project-ID: default

{ "messages": [{ "role": "user", "content": "Hello!" }] }`;

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
          sparkData={mockSparklines.apps}
          trend="Active deployments"
        />
        <StatCard
          label="Model Routings"
          value={modelsCount}
          loading={loading}
          icon={<Cpu className="w-4 h-4" />}
          color="bg-blue-500/10 text-blue-400"
          sparkData={mockSparklines.models}
          trend="Logical aliases"
        />
        <StatCard
          label="Registered Tools"
          value={toolsCount}
          loading={loading}
          icon={<Wrench className="w-4 h-4" />}
          color="bg-amber-500/10 text-amber-400"
          sparkData={mockSparklines.tools}
          trend="HTTP executors"
        />
        <StatCard
          label="CEL Policies"
          value={policiesCount}
          loading={loading}
          icon={<ShieldCheck className="w-4 h-4" />}
          color="bg-purple-500/10 text-purple-400"
          sparkData={mockSparklines.policies}
          trend="Authorization rules"
        />
      </div>

      {/* Main content grid */}
      <div className="grid grid-cols-1 lg:grid-cols-5 gap-4">
        {/* Runtime Invariants */}
        <div className="lg:col-span-3 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold text-neutral-300 flex items-center gap-2">
              <Activity className="w-4 h-4 text-neutral-500" />
              Runtime Architecture
            </h2>
            <Badge variant="success" dot>All systems operational</Badge>
          </div>

          <div className="space-y-2">
            <InvariantItem
              title="Zero DB Reads on the Hot Path"
              description="Runtime instances compile configuration into atomic in-memory snapshots. Config changes propagate via Redis pub/sub without interrupting active requests."
              icon={<Database className="w-3.5 h-3.5" />}
            />
            <InvariantItem
              title="SSRF Guard & Confused-Deputy Defense"
              description="All HTTP tool executor calls use DNS-rebinding-safe dialers that block loopback, private ranges, and cloud metadata endpoints. Identity fields are injected from verified JWT claims."
              icon={<Shield className="w-3.5 h-3.5" />}
            />
            <InvariantItem
              title="Bounded Agent Loop & Stateless Replication"
              description="Tool execution is capped at 5 iterations. Suspended confirmation state is persisted to Redis with 5-minute TTL, enabling any replica to resume requests."
              icon={<Network className="w-3.5 h-3.5" />}
            />
            <InvariantItem
              title="Async Usage Pipeline"
              description="Token usage and cost are written via a bounded channel pipeline with batched async writes — never in the request hot path."
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
            <p className="text-xs text-neutral-500 mb-3">
              Pass your authenticated end-user JWT in the Authorization header.
              Beavend validates the token, evaluates CEL policies, and routes to
              the configured provider.
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
