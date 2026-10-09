import React, { useEffect, useState } from 'react';
import { api } from '../api/client';
import {
  Coins,
  RefreshCw,
  Download,
  Calendar,
  Layers,
  Bot,
  Cpu,
  Zap,
  Activity,
  AlertTriangle,
} from 'lucide-react';
import { Card, CardHeader } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { EmptyState, Loading } from '../components/ui/Feedback';
import { formatNumber, formatCost } from '../lib/utils';

export const UsagePage: React.FC = () => {
  const [range, setRange] = useState<'24h' | '7d' | '30d'>('24h');
  const [loading, setLoading] = useState(true);
  const [usageData, setUsageData] = useState<any[]>([]);

  const loadUsage = async (isBackground = false) => {
    if (!isBackground) setLoading(true);
    try {
      let granularity = 'hour';
      const toDate = new Date();
      let fromDate = new Date();

      if (range === '24h') {
        fromDate.setHours(toDate.getHours() - 24);
        granularity = 'hour';
      } else if (range === '7d') {
        fromDate.setDate(toDate.getDate() - 7);
        granularity = 'day';
      } else {
        fromDate.setDate(toDate.getDate() - 30);
        granularity = 'day';
      }

      const data = await api.getUsage(granularity, fromDate.toISOString(), toDate.toISOString()).catch(() => []);
      const sorted = (data || []).slice().sort((a, b) => {
        const timeA = new Date(a.bucket_start?.Time || a.bucket_start || 0).getTime();
        const timeB = new Date(b.bucket_start?.Time || b.bucket_start || 0).getTime();
        return timeB - timeA;
      });
      setUsageData(sorted);
    } finally {
      if (!isBackground) setLoading(false);
    }
  };

  useEffect(() => {
    loadUsage();
    const interval = setInterval(() => {
      loadUsage(true);
    }, 30000);
    return () => clearInterval(interval);
  }, [range]);

  // Aggregate totals
  const totalRequests = usageData.reduce((acc, r) => acc + Number(r.request_count || r.total_requests || 0), 0);
  const totalInputTokens = usageData.reduce((acc, r) => acc + Number(r.input_tokens || 0), 0);
  const totalOutputTokens = usageData.reduce((acc, r) => acc + Number(r.output_tokens || 0), 0);
  const totalTokens = usageData.reduce((acc, r) => acc + Number(r.total_tokens || 0), 0);
  const totalErrors = usageData.reduce((acc, r) => acc + Number(r.error_count || 0), 0);
  const totalCost = usageData.reduce((acc, r) => {
    const c = typeof r.cost_usd === 'object' && r.cost_usd ? r.cost_usd.Float64 ?? Number(r.cost_usd) : Number(r.cost_usd || 0);
    return acc + (isNaN(c) ? 0 : c);
  }, 0);

  // Group by Model
  const byModel = usageData.reduce((acc: Record<string, any>, r) => {
    const key = r.logical_model || 'unknown';
    if (!acc[key]) {
      acc[key] = {
        model: key,
        provider: r.provider || 'default',
        requests: 0,
        inputTokens: 0,
        outputTokens: 0,
        totalTokens: 0,
        cost: 0,
        errors: 0,
      };
    }
    acc[key].requests += Number(r.request_count || 0);
    acc[key].inputTokens += Number(r.input_tokens || 0);
    acc[key].outputTokens += Number(r.output_tokens || 0);
    acc[key].totalTokens += Number(r.total_tokens || 0);
    acc[key].errors += Number(r.error_count || 0);
    const c = typeof r.cost_usd === 'object' && r.cost_usd ? r.cost_usd.Float64 ?? Number(r.cost_usd) : Number(r.cost_usd || 0);
    acc[key].cost += isNaN(c) ? 0 : c;
    return acc;
  }, {});

  // Group by App
  const byApp = usageData.reduce((acc: Record<string, any>, r) => {
    const key = r.ai_app_id || 'unknown';
    if (!acc[key]) {
      acc[key] = {
        appId: key,
        requests: 0,
        totalTokens: 0,
        cost: 0,
      };
    }
    acc[key].requests += Number(r.request_count || 0);
    acc[key].totalTokens += Number(r.total_tokens || 0);
    const c = typeof r.cost_usd === 'object' && r.cost_usd ? r.cost_usd.Float64 ?? Number(r.cost_usd) : Number(r.cost_usd || 0);
    acc[key].cost += isNaN(c) ? 0 : c;
    return acc;
  }, {});

  const exportCSV = () => {
    if (usageData.length === 0) return;
    const headers = ['bucket_start', 'ai_app_id', 'logical_model', 'provider', 'request_count', 'input_tokens', 'output_tokens', 'total_tokens', 'cost_usd', 'error_count'];
    const rows = usageData.map((r) => [
      r.bucket_start?.Time || r.bucket_start || '',
      r.ai_app_id || '',
      r.logical_model || '',
      r.provider || '',
      r.request_count || 0,
      r.input_tokens || 0,
      r.output_tokens || 0,
      r.total_tokens || 0,
      r.cost_usd || 0,
      r.error_count || 0,
    ]);
    const csvContent = 'data:text/csv;charset=utf-8,' + [headers.join(','), ...rows.map((e) => e.join(','))].join('\n');
    const encodedUri = encodeURI(csvContent);
    const link = document.createElement('a');
    link.setAttribute('href', encodedUri);
    link.setAttribute('download', `beavend_usage_${range}_${Date.now()}.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  return (
    <div className="space-y-6 max-w-[1200px]">
      {/* Header */}
      <div className="flex items-start justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
            <Coins className="w-5 h-5 text-amber-400" />
            Usage & Cost Analytics
          </h1>
          <p className="text-sm text-neutral-500 mt-0.5">
            Aggregated token consumption, model utilization, and cost statistics recorded asynchronously.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {/* Range filter buttons */}
          <div className="flex rounded-lg bg-neutral-900 border border-neutral-800 p-0.5">
            {(['24h', '7d', '30d'] as const).map((r) => (
              <button
                key={r}
                type="button"
                onClick={() => setRange(r)}
                className={`px-3 py-1.5 text-xs font-medium rounded-md transition-colors ${
                  range === r
                    ? 'bg-neutral-800 text-neutral-100 shadow-sm'
                    : 'text-neutral-500 hover:text-neutral-300'
                }`}
              >
                {r === '24h' && 'Last 24 Hours'}
                {r === '7d' && 'Last 7 Days'}
                {r === '30d' && 'Last 30 Days'}
              </button>
            ))}
          </div>

          <Button
            variant="secondary"
            size="sm"
            icon={<Download className="w-3.5 h-3.5" />}
            onClick={exportCSV}
            disabled={usageData.length === 0}
          >
            Export CSV
          </Button>

          <Button
            variant="secondary"
            size="sm"
            icon={<RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />}
            onClick={() => loadUsage(false)}
            disabled={loading}
          >
            Refresh
          </Button>
        </div>
      </div>

      {loading ? (
        <Loading text="Loading usage metrics and rollups..." />
      ) : (
        <>
          {/* Key Metric Summary Cards */}
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
            {[
              { label: 'Total Requests', value: formatNumber(totalRequests), icon: <Activity className="w-4 h-4 text-emerald-400" /> },
              { label: 'Input Tokens', value: formatNumber(totalInputTokens), icon: <Zap className="w-4 h-4 text-blue-400" /> },
              { label: 'Output Tokens', value: formatNumber(totalOutputTokens), icon: <Zap className="w-4 h-4 text-purple-400" /> },
              { label: 'Total Tokens', value: formatNumber(totalTokens), icon: <Layers className="w-4 h-4 text-indigo-400" /> },
              { label: 'Est. Cost', value: formatCost(totalCost), icon: <Coins className="w-4 h-4 text-amber-400" /> },
              {
                label: 'Errors',
                value: totalErrors,
                icon: <AlertTriangle className="w-4 h-4 text-red-400" />,
                badge: totalRequests > 0 ? `${((totalErrors / totalRequests) * 100).toFixed(1)}%` : undefined,
              },
            ].map((m) => (
              <Card key={m.label} padding="sm" className="bg-neutral-900 border-neutral-800">
                <div className="flex items-center justify-between mb-1.5">
                  <span className="text-[10px] text-neutral-500 font-medium uppercase tracking-wider">{m.label}</span>
                  {m.icon}
                </div>
                <div className="flex items-baseline gap-2">
                  <p className="text-xl font-bold text-neutral-100 tracking-tight font-mono">{m.value}</p>
                  {m.badge && (
                    <span className="text-[10px] font-mono text-neutral-500">({m.badge})</span>
                  )}
                </div>
              </Card>
            ))}
          </div>

          {usageData.length === 0 ? (
            <EmptyState
              icon={<Coins className="w-6 h-6 text-neutral-500" />}
              title="No usage recorded for this timeframe"
              description="Inference requests executed via chat or stream endpoints will populate usage statistics here."
            />
          ) : (
            <div className="space-y-6">
              {/* Breakdown Grid: Models & Apps */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                {/* Breakdown by Model */}
                <Card padding="lg">
                  <CardHeader
                    title="Utilization by Logical Model"
                    description="Tokens and estimated costs grouped by model alias"
                    icon={<Cpu className="w-4 h-4 text-blue-400" />}
                  />
                  <div className="overflow-x-auto mt-3">
                    <table className="w-full text-left">
                      <thead>
                        <tr className="border-b border-neutral-800 text-[10px] uppercase font-semibold text-neutral-500 tracking-wider">
                          <th className="pb-2">Model Alias</th>
                          <th className="pb-2 text-right">Requests</th>
                          <th className="pb-2 text-right">Tokens</th>
                          <th className="pb-2 text-right">Cost</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-neutral-800/40 text-xs font-mono">
                        {Object.values(byModel).map((m: any) => (
                          <tr key={m.model} className="hover:bg-neutral-800/30">
                            <td className="py-2.5">
                              <span className="text-emerald-400 font-medium">{m.model}</span>
                              <span className="text-[10px] text-neutral-600 block">{m.provider}</span>
                            </td>
                            <td className="py-2.5 text-right text-neutral-300">{formatNumber(m.requests)}</td>
                            <td className="py-2.5 text-right text-neutral-400">{formatNumber(m.totalTokens)}</td>
                            <td className="py-2.5 text-right text-amber-400">{formatCost(m.cost)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </Card>

                {/* Breakdown by App */}
                <Card padding="lg">
                  <CardHeader
                    title="Utilization by AI Application"
                    description="Token traffic distributed across client AI apps"
                    icon={<Bot className="w-4 h-4 text-emerald-400" />}
                  />
                  <div className="overflow-x-auto mt-3">
                    <table className="w-full text-left">
                      <thead>
                        <tr className="border-b border-neutral-800 text-[10px] uppercase font-semibold text-neutral-500 tracking-wider">
                          <th className="pb-2">App ID</th>
                          <th className="pb-2 text-right">Requests</th>
                          <th className="pb-2 text-right">Tokens</th>
                          <th className="pb-2 text-right">Cost</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-neutral-800/40 text-xs font-mono">
                        {Object.values(byApp).map((a: any) => (
                          <tr key={a.appId} className="hover:bg-neutral-800/30">
                            <td className="py-2.5 text-neutral-200 font-medium">{a.appId}</td>
                            <td className="py-2.5 text-right text-neutral-300">{formatNumber(a.requests)}</td>
                            <td className="py-2.5 text-right text-neutral-400">{formatNumber(a.totalTokens)}</td>
                            <td className="py-2.5 text-right text-amber-400">{formatCost(a.cost)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </Card>
              </div>

              {/* Detailed Rollups Table */}
              <Card padding="lg">
                <CardHeader
                  title="Historical Usage Rollups"
                  description="Time-bucketed aggregation records from background usage reconciler"
                  icon={<Calendar className="w-4 h-4 text-purple-400" />}
                />
                <div className="overflow-x-auto mt-4">
                  <table className="w-full text-left">
                    <thead>
                      <tr className="border-b border-neutral-800 text-[10px] uppercase font-semibold text-neutral-500 tracking-wider">
                        <th className="pb-2.5 px-3">Bucket Time</th>
                        <th className="pb-2.5 px-3">App ID</th>
                        <th className="pb-2.5 px-3">Model</th>
                        <th className="pb-2.5 px-3 text-right">Requests</th>
                        <th className="pb-2.5 px-3 text-right">In / Out Tokens</th>
                        <th className="pb-2.5 px-3 text-right">Total Tokens</th>
                        <th className="pb-2.5 px-3 text-right">Cost</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-neutral-800/50 text-xs font-mono">
                      {usageData.slice(0, 100).map((r, idx) => {
                        const timeStr = r.bucket_start?.Time || r.bucket_start;
                        const formattedTime = timeStr ? new Date(timeStr).toLocaleString() : '-';
                        const cost = typeof r.cost_usd === 'object' && r.cost_usd ? r.cost_usd.Float64 ?? Number(r.cost_usd) : Number(r.cost_usd || 0);
                        return (
                          <tr key={idx} className="hover:bg-neutral-800/25">
                            <td className="py-2 px-3 text-neutral-400 text-[11px] whitespace-nowrap">{formattedTime}</td>
                            <td className="py-2 px-3 text-neutral-200">{r.ai_app_id}</td>
                            <td className="py-2 px-3 text-emerald-400">{r.logical_model}</td>
                            <td className="py-2 px-3 text-right text-neutral-300">{formatNumber(r.request_count || 0)}</td>
                            <td className="py-2 px-3 text-right text-neutral-500 text-[11px]">
                              {formatNumber(r.input_tokens || 0)} / {formatNumber(r.output_tokens || 0)}
                            </td>
                            <td className="py-2 px-3 text-right text-neutral-300 font-semibold">{formatNumber(r.total_tokens || 0)}</td>
                            <td className="py-2 px-3 text-right text-amber-400">{formatCost(cost)}</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </Card>
            </div>
          )}
        </>
      )}
    </div>
  );
};
