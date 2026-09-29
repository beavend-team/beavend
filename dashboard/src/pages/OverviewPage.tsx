import React, { useEffect, useState } from 'react';
import { api } from '../api/client';
import { Bot, Cpu, Wrench, ShieldCheck, Zap, Server, Shield, CheckCircle2 } from 'lucide-react';

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

  const stats = [
    { label: 'Active AI Apps', value: appsCount, icon: Bot, color: 'text-emerald-400 bg-emerald-500/10 border-emerald-500/20' },
    { label: 'Model Routings', value: modelsCount, icon: Cpu, color: 'text-blue-400 bg-blue-500/10 border-blue-500/20' },
    { label: 'Tools Configured', value: toolsCount, icon: Wrench, color: 'text-amber-400 bg-amber-500/10 border-amber-500/20' },
    { label: 'CEL Policies', value: policiesCount, icon: ShieldCheck, color: 'text-purple-400 bg-purple-500/10 border-purple-500/20' },
  ];

  return (
    <div className="space-y-8 max-w-6xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-slate-100">Architecture Overview</h1>
        <p className="text-sm text-slate-400 mt-1">
          High-performance, stateless AI runtime backed by atomic in-memory snapshots.
        </p>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {stats.map((s, idx) => {
          const Icon = s.icon;
          return (
            <div key={idx} className="bg-slate-900 border border-slate-800 rounded-xl p-5 flex items-center gap-4">
              <div className={`w-12 h-12 rounded-xl flex items-center justify-center border ${s.color}`}>
                <Icon className="w-6 h-6" />
              </div>
              <div>
                <span className="text-xs font-medium text-slate-400 block">{s.label}</span>
                <span className="text-2xl font-bold text-slate-100 tracking-tight">
                  {loading ? '...' : s.value}
                </span>
              </div>
            </div>
          );
        })}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 bg-slate-900 border border-slate-800 rounded-xl p-6 space-y-4">
          <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
            <Zap className="w-5 h-5 text-amber-400" />
            Runtime Invariants & Capabilities
          </h2>
          <div className="space-y-3 text-sm text-slate-400">
            <div className="flex items-start gap-3 p-3 rounded-lg bg-slate-950/60 border border-slate-800">
              <CheckCircle2 className="w-5 h-5 text-emerald-400 flex-shrink-0 mt-0.5" />
              <div>
                <p className="font-medium text-slate-200">Zero DB Reads on the Hot Path</p>
                <p className="text-xs text-slate-400 mt-0.5">
                  Runtime instances compile snapshot configuration into memory and swap atomically on revision events.
                </p>
              </div>
            </div>

            <div className="flex items-start gap-3 p-3 rounded-lg bg-slate-950/60 border border-slate-800">
              <CheckCircle2 className="w-5 h-5 text-emerald-400 flex-shrink-0 mt-0.5" />
              <div>
                <p className="font-medium text-slate-200">Identity & Confused-Deputy Defense</p>
                <p className="text-xs text-slate-400 mt-0.5">
                  Downstream tool parameters (e.g. user_id) are strictly bound to verified caller JWT claims.
                </p>
              </div>
            </div>

            <div className="flex items-start gap-3 p-3 rounded-lg bg-slate-950/60 border border-slate-800">
              <CheckCircle2 className="w-5 h-5 text-emerald-400 flex-shrink-0 mt-0.5" />
              <div>
                <p className="font-medium text-slate-200">Bounded Tool Agent Loop & SSRF Guard</p>
                <p className="text-xs text-slate-400 mt-0.5">
                  External HTTP requests undergo DNS-rebinding verification with private network blocking and 5-iteration limits.
                </p>
              </div>
            </div>
          </div>
        </div>

        <div className="bg-slate-900 border border-slate-800 rounded-xl p-6 space-y-4">
          <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
            <Server className="w-5 h-5 text-blue-400" />
            Client SDK Quickstart
          </h2>
          <p className="text-xs text-slate-400">
            To connect your client mobile or web app, invoke the runtime chat or streaming endpoint:
          </p>
          <div className="bg-slate-950 border border-slate-800 rounded-lg p-3 font-mono text-xs text-slate-300 overflow-x-auto">
            <span className="text-emerald-400">POST</span> /v1/apps/{'{app_id}'}/chat<br />
            <span className="text-slate-500">Authorization:</span> Bearer &lt;user-jwt&gt;<br />
            <span className="text-slate-500">X-Project-ID:</span> default<br />
            <br />
            {JSON.stringify({ message: "Hello assistant!" }, null, 2)}
          </div>
        </div>
      </div>
    </div>
  );
};
