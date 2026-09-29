import React, { useEffect, useState } from 'react';
import { api, RequestLog } from '../api/client';
import { Activity, RefreshCw, AlertCircle, CheckCircle2, Clock } from 'lucide-react';

export const LogsPage: React.FC = () => {
  const [logs, setLogs] = useState<RequestLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null);

  const loadLogs = async () => {
    setLoading(true);
    try {
      const data = await api.getLogs(50, 0).catch(() => []);
      setLogs(data);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadLogs();
  }, []);

  const getStatusBadge = (status: string, code: number) => {
    if (code >= 200 && code < 300) {
      return (
        <span className="flex items-center gap-1 text-[11px] font-semibold text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 px-2 py-0.5 rounded-full">
          <CheckCircle2 className="w-3 h-3" />
          {code}
        </span>
      );
    }
    if (code === 429) {
      return (
        <span className="flex items-center gap-1 text-[11px] font-semibold text-amber-400 bg-amber-500/10 border border-amber-500/20 px-2 py-0.5 rounded-full">
          <Clock className="w-3 h-3" />
          429
        </span>
      );
    }
    return (
      <span className="flex items-center gap-1 text-[11px] font-semibold text-rose-400 bg-rose-500/10 border border-rose-500/20 px-2 py-0.5 rounded-full">
        <AlertCircle className="w-3 h-3" />
        {code || status}
      </span>
    );
  };

  return (
    <div className="space-y-6 max-w-7xl">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-100 flex items-center gap-2">
            <Activity className="w-6 h-6 text-emerald-400" />
            Request Logs & Telemetry
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Operational request metadata and token usage recorded asynchronously.
          </p>
        </div>

        <button
          onClick={loadLogs}
          disabled={loading}
          className="flex items-center gap-2 px-3 py-1.5 bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 rounded-xl text-xs font-medium transition-colors"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          <span>Refresh</span>
        </button>
      </div>

      <div className="bg-slate-900 border border-slate-800 rounded-2xl overflow-hidden shadow-xl">
        {loading && logs.length === 0 ? (
          <div className="p-12 text-center text-sm text-slate-500">Loading request logs...</div>
        ) : logs.length === 0 ? (
          <div className="p-12 text-center text-sm text-slate-500">
            No request logs recorded yet for this project and environment.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse text-xs">
              <thead>
                <tr className="border-b border-slate-800 bg-slate-950/50 text-slate-400 font-semibold uppercase tracking-wider">
                  <th className="py-3 px-4">Time</th>
                  <th className="py-3 px-4">Status</th>
                  <th className="py-3 px-4">App ID</th>
                  <th className="py-3 px-4">Model</th>
                  <th className="py-3 px-4">Latency</th>
                  <th className="py-3 px-4">Tokens (In / Out)</th>
                  <th className="py-3 px-4">Cost</th>
                  <th className="py-3 px-4">Request ID</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/60 font-mono">
                {logs.map((log) => (
                  <tr
                    key={log.id}
                    onClick={() => setSelectedLog(log)}
                    className="hover:bg-slate-800/40 cursor-pointer transition-colors"
                  >
                    <td className="py-3 px-4 text-slate-400 whitespace-nowrap">
                      {new Date(log.created_at).toLocaleTimeString()}
                    </td>
                    <td className="py-3 px-4">{getStatusBadge(log.status, log.status_code)}</td>
                    <td className="py-3 px-4 text-slate-200 font-medium">{log.ai_app_id}</td>
                    <td className="py-3 px-4 text-slate-300">
                      <span className="text-emerald-400">{log.logical_model}</span>
                      <span className="text-slate-500 text-[10px] block font-sans">{log.model_deployment}</span>
                    </td>
                    <td className="py-3 px-4 text-slate-300">{log.latency_ms}ms</td>
                    <td className="py-3 px-4 text-slate-300">
                      {log.input_tokens} / {log.output_tokens}
                    </td>
                    <td className="py-3 px-4 text-slate-300">${parseFloat(log.cost_usd || '0').toFixed(5)}</td>
                    <td className="py-3 px-4 text-slate-500 text-[11px] truncate max-w-[120px]">
                      {log.request_id}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Log Detail Modal */}
      {selectedLog && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4 z-50">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-2xl w-full p-6 shadow-2xl space-y-4 max-h-[85vh] overflow-y-auto">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="font-bold text-slate-100 text-base">Request Inspection</h3>
                <span className="text-xs font-mono text-slate-400">{selectedLog.request_id}</span>
              </div>
              <button onClick={() => setSelectedLog(null)} className="text-slate-400 hover:text-slate-200 text-sm">
                Close
              </button>
            </div>

            <div className="bg-slate-950 border border-slate-800 rounded-xl p-4 font-mono text-xs text-slate-300 overflow-x-auto">
              <pre>{JSON.stringify(selectedLog, null, 2)}</pre>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
