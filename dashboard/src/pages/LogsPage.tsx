import React, { useEffect, useState } from 'react';
import { api, RequestLog } from '../api/client';
import { BarChart3, RefreshCw, AlertCircle, CheckCircle2, Clock, Search, X, Calendar, Wrench, Zap, Radio } from 'lucide-react';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Loading } from '../components/ui/Feedback';
import { formatCost, formatRelativeTime } from '../lib/utils';

const StatusBadge: React.FC<{ status: string; code: number }> = ({ status, code }) => {
  if (code >= 200 && code < 300) return <Badge variant="success" dot>{code}</Badge>;
  if (code === 429) return <Badge variant="warning" dot>429</Badge>;
  if (code >= 400) return <Badge variant="danger" dot>{code || status}</Badge>;
  return <Badge variant="default">{code || status}</Badge>;
};

export const LogsPage: React.FC = () => {
  const [logs, setLogs] = useState<RequestLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null);
  const [filter, setFilter] = useState('');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');

  const loadLogs = async () => {
    setLoading(true);
    try {
      const from = dateFrom ? new Date(dateFrom).toISOString() : undefined;
      const to = dateTo ? new Date(dateTo).toISOString() : undefined;
      setLogs(await api.getLogs(200, 0, from, to).catch(() => []));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadLogs(); }, []);

  const filteredLogs = filter
    ? logs.filter(
        (l) =>
          l.ai_app_id.includes(filter) ||
          l.user_subject?.includes(filter) ||
          l.logical_model?.includes(filter) ||
          l.request_id?.includes(filter) ||
          String(l.status_code).includes(filter)
      )
    : logs;

  const stats = {
    total: logs.length,
    success: logs.filter((l) => l.status_code >= 200 && l.status_code < 300).length,
    errors: logs.filter((l) => l.status_code >= 400).length,
    avgLatency: logs.length > 0 ? Math.round(logs.reduce((acc, l) => acc + (l.latency_ms || 0), 0) / logs.length) : 0,
    totalTokens: logs.reduce((acc, l) => acc + (l.total_tokens || 0), 0),
  };

  return (
    <div className="space-y-5 max-w-[1200px]">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
            <BarChart3 className="w-5 h-5 text-neutral-400" />
            Request Logs
          </h1>
          <p className="text-sm text-neutral-500 mt-0.5">
            Operational request metadata and token usage recorded asynchronously.
          </p>
        </div>
        <Button variant="secondary" size="sm" icon={<RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />} onClick={loadLogs} disabled={loading}>
          Refresh
        </Button>
      </div>

      {/* Stats mini-bar */}
      {!loading && logs.length > 0 && (
        <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
          {[
            { label: 'Total Requests', value: stats.total, color: 'text-neutral-200' },
            { label: 'Successful', value: stats.success, color: 'text-emerald-400' },
            { label: 'Errors', value: stats.errors, color: stats.errors > 0 ? 'text-red-400' : 'text-neutral-400' },
            { label: 'Avg Latency', value: `${stats.avgLatency}ms`, color: 'text-blue-400' },
            { label: 'Total Tokens', value: stats.totalTokens.toLocaleString(), color: 'text-purple-400' },
          ].map((s) => (
            <div key={s.label} className="bg-neutral-900 border border-neutral-800 rounded-xl p-3.5">
              <p className="text-[10px] text-neutral-600 font-medium uppercase tracking-wider">{s.label}</p>
              <p className={`text-lg font-bold tracking-tight mt-0.5 ${s.color}`}>{s.value}</p>
            </div>
          ))}
        </div>
      )}

      {/* Date range filter */}
      <div className="flex items-center gap-2">
        <Calendar className="w-3.5 h-3.5 text-neutral-600" />
        <label className="text-xs text-neutral-500">From:</label>
        <input
          type="datetime-local"
          value={dateFrom}
          onChange={(e) => setDateFrom(e.target.value)}
          className="bg-neutral-900 border border-neutral-800 rounded-lg px-2.5 py-1.5 text-xs text-neutral-200 focus:outline-none focus:border-neutral-600 transition-colors"
        />
        <label className="text-xs text-neutral-500">To:</label>
        <input
          type="datetime-local"
          value={dateTo}
          onChange={(e) => setDateTo(e.target.value)}
          className="bg-neutral-900 border border-neutral-800 rounded-lg px-2.5 py-1.5 text-xs text-neutral-200 focus:outline-none focus:border-neutral-600 transition-colors"
        />
        {(dateFrom || dateTo) && (
          <button onClick={() => { setDateFrom(''); setDateTo(''); }} className="text-neutral-500 hover:text-neutral-300">
            <X className="w-3.5 h-3.5" />
          </button>
        )}
        <Button variant="secondary" size="xs" icon={<RefreshCw className="w-3 h-3" />} onClick={loadLogs} disabled={loading}>
          Apply
        </Button>
      </div>

      {/* Text Filter */}
      {!loading && logs.length > 0 && (
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-neutral-600" />
          <input
            type="text"
            placeholder="Filter by app ID, model, user subject, request ID, or status code..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="w-full bg-neutral-900 border border-neutral-800 rounded-xl pl-9 pr-4 py-2.5 text-sm text-neutral-200 placeholder-neutral-600 focus:outline-none focus:border-neutral-600 transition-colors"
          />
          {filter && (
            <button onClick={() => setFilter('')} className="absolute right-3 top-1/2 -translate-y-1/2 text-neutral-500 hover:text-neutral-300">
              <X className="w-3.5 h-3.5" />
            </button>
          )}
        </div>
      )}

      {/* Table */}
      {loading ? (
        <Loading text="Loading request logs..." />
      ) : filteredLogs.length === 0 ? (
        <div className="bg-neutral-900 border border-neutral-800 rounded-2xl p-12 text-center">
          <p className="text-sm text-neutral-600">
            {filter ? 'No logs matching your filter.' : 'No request logs recorded yet for this project and environment.'}
          </p>
        </div>
      ) : (
        <div className="bg-neutral-900 border border-neutral-800 rounded-2xl overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="border-b border-neutral-800/80 bg-neutral-950/50">
                  {['Time', 'Status', 'App', 'Model', 'Latency', 'Tokens In/Out', 'Cost', 'User'].map((h) => (
                    <th key={h} className="py-2.5 px-4 text-[10px] font-semibold uppercase tracking-wider text-neutral-600">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-neutral-800/50">
                {filteredLogs.map((log) => (
                  <tr
                    key={log.id}
                    onClick={() => setSelectedLog(log)}
                    className="hover:bg-neutral-800/40 cursor-pointer transition-colors group"
                  >
                    <td className="py-2.5 px-4 text-[11px] font-mono text-neutral-600 whitespace-nowrap">
                      {new Date(log.created_at).toLocaleTimeString()}
                    </td>
                    <td className="py-2.5 px-4">
                      <StatusBadge status={log.status} code={log.status_code} />
                    </td>
                    <td className="py-2.5 px-4 text-xs font-medium text-neutral-200">{log.ai_app_id}</td>
                    <td className="py-2.5 px-4">
                      <span className="text-xs font-mono text-emerald-400">{log.logical_model}</span>
                      {log.model_deployment && (
                        <span className="text-[10px] text-neutral-600 block">{log.model_deployment}</span>
                      )}
                    </td>
                    <td className="py-2.5 px-4 text-xs font-mono text-neutral-400">
                      {log.latency_ms ? `${log.latency_ms}ms` : '-'}
                    </td>
                    <td className="py-2.5 px-4 text-xs font-mono text-neutral-400">
                      {log.input_tokens ?? 0} / {log.output_tokens ?? 0}
                    </td>
                    <td className="py-2.5 px-4 text-xs font-mono text-neutral-400">
                      {formatCost(log.cost_usd)}
                    </td>
                    <td className="py-2.5 px-4 text-[11px] font-mono text-neutral-600 truncate max-w-[120px]">
                      {log.user_subject || '-'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {filteredLogs.length > 0 && (
            <div className="px-4 py-2.5 border-t border-neutral-800/80 flex items-center justify-between text-[11px] text-neutral-600">
              <span>Showing {filteredLogs.length} of {logs.length} requests</span>
              {filter && <span>{logs.length - filteredLogs.length} filtered out</span>}
            </div>
          )}
        </div>
      )}

      {/* Log Detail Panel */}
      {selectedLog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" onClick={() => setSelectedLog(null)} />
          <div className="relative w-full max-w-2xl bg-neutral-900 border border-neutral-800 rounded-2xl shadow-2xl max-h-[85vh] overflow-hidden flex flex-col">
            <div className="flex items-center justify-between p-5 border-b border-neutral-800">
              <div>
                <h3 className="text-sm font-semibold text-neutral-100">Request Detail</h3>
                <p className="text-[11px] font-mono text-neutral-600 mt-0.5">{selectedLog.request_id}</p>
              </div>
              <div className="flex items-center gap-2">
                <StatusBadge status={selectedLog.status} code={selectedLog.status_code} />
                <button onClick={() => setSelectedLog(null)} className="p-1 text-neutral-500 hover:text-neutral-200 rounded-lg hover:bg-neutral-800 transition-colors">
                  <X className="w-4 h-4" />
                </button>
              </div>
            </div>

            <div className="overflow-y-auto p-5 space-y-4">
              {/* Error Banner if error occurred */}
              {(selectedLog.error_code || selectedLog.error_message) && (
                <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-xl space-y-1">
                  <div className="flex items-center gap-2">
                    <AlertCircle className="w-4 h-4 text-red-400 shrink-0" />
                    <span className="text-xs font-semibold text-red-400">
                      {selectedLog.error_code || 'Request Failed'}
                    </span>
                  </div>
                  {selectedLog.error_message && (
                    <p className="text-xs text-red-300 font-mono pl-6">{selectedLog.error_message}</p>
                  )}
                </div>
              )}

              {/* Key metrics */}
              <div className="grid grid-cols-3 gap-3">
                {[
                  { label: 'App ID', value: selectedLog.ai_app_id },
                  { label: 'Model', value: selectedLog.logical_model },
                  { label: 'Latency', value: `${selectedLog.latency_ms}ms` },
                  { label: 'Input Tokens', value: selectedLog.input_tokens },
                  { label: 'Output Tokens', value: selectedLog.output_tokens },
                  { label: 'Cost', value: formatCost(selectedLog.cost_usd) },
                ].map((item) => (
                  <div key={item.label} className="bg-neutral-950 border border-neutral-800 rounded-lg p-2.5">
                    <p className="text-[10px] text-neutral-600 uppercase tracking-wider">{item.label}</p>
                    <p className="text-xs font-mono text-neutral-200 mt-0.5">{item.value}</p>
                  </div>
                ))}
              </div>

              {/* Protocol & Identity */}
              <div className="grid grid-cols-3 gap-3">
                <div className="bg-neutral-950 border border-neutral-800 rounded-lg p-2.5">
                  <p className="text-[10px] text-neutral-600 uppercase tracking-wider">Protocol</p>
                  <div className="flex items-center gap-1.5 mt-1">
                    {selectedLog.is_streaming ? (
                      <Badge variant="info">
                        <Radio className="w-2.5 h-2.5 animate-pulse" />
                        SSE Stream
                      </Badge>
                    ) : (
                      <Badge variant="default">Unary HTTP</Badge>
                    )}
                  </div>
                </div>
                <div className="bg-neutral-950 border border-neutral-800 rounded-lg p-2.5">
                  <p className="text-[10px] text-neutral-600 uppercase tracking-wider">Provider / Deployment</p>
                  <p className="text-xs font-mono text-neutral-300 mt-0.5 truncate">
                    {selectedLog.provider || 'default'} / {selectedLog.model_deployment || '-'}
                  </p>
                </div>
                <div className="bg-neutral-950 border border-neutral-800 rounded-lg p-2.5">
                  <p className="text-[10px] text-neutral-600 uppercase tracking-wider">User Subject</p>
                  <p className="text-xs font-mono text-neutral-300 mt-0.5 truncate">
                    {selectedLog.user_subject || 'anonymous'}
                  </p>
                </div>
              </div>

              {/* Tool Execution Traces */}
              {selectedLog.tool_calls && Array.isArray(selectedLog.tool_calls) && selectedLog.tool_calls.length > 0 && (
                <div>
                  <div className="flex items-center gap-1.5 mb-2">
                    <Wrench className="w-3.5 h-3.5 text-amber-400" />
                    <span className="text-xs font-semibold text-neutral-200">
                      Tool Invocations ({selectedLog.tool_calls.length})
                    </span>
                  </div>
                  <div className="space-y-2">
                    {selectedLog.tool_calls.map((tc: any, idx: number) => (
                      <div key={idx} className="bg-neutral-950 border border-neutral-800 rounded-xl p-3 space-y-2">
                        <div className="flex items-center justify-between">
                          <span className="text-xs font-mono font-semibold text-amber-400">
                            {tc.name || tc.tool_name || `tool_${idx + 1}`}
                          </span>
                          <span className="text-[10px] font-mono text-neutral-500">
                            {tc.call_id || tc.id || ''}
                          </span>
                        </div>
                        {tc.arguments && (
                          <div>
                            <span className="text-[10px] text-neutral-500 block mb-0.5">Arguments</span>
                            <pre className="p-2 bg-neutral-900 border border-neutral-800/80 rounded-lg text-[10px] font-mono text-neutral-300 overflow-x-auto">
                              {typeof tc.arguments === 'string' ? tc.arguments : JSON.stringify(tc.arguments, null, 2)}
                            </pre>
                          </div>
                        )}
                        {(tc.result !== undefined || tc.content !== undefined) && (
                          <div>
                            <span className="text-[10px] text-neutral-500 block mb-0.5">Result</span>
                            <pre className="p-2 bg-neutral-900 border border-neutral-800/80 rounded-lg text-[10px] font-mono text-neutral-300 overflow-x-auto max-h-32">
                              {typeof (tc.result || tc.content) === 'string' ? (tc.result || tc.content) : JSON.stringify(tc.result || tc.content, null, 2)}
                            </pre>
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Raw JSON */}
              <div>
                <p className="text-[11px] font-medium text-neutral-500 mb-1.5">Raw Log Metadata</p>
                <pre className="bg-neutral-950 border border-neutral-800 rounded-xl p-4 text-[11px] font-mono text-neutral-400 overflow-x-auto max-h-48">
                  {JSON.stringify(selectedLog, null, 2)}
                </pre>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
