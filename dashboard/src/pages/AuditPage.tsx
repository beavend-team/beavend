import React, { useEffect, useState } from 'react';
import { api, AuditLog } from '../api/client';
import { History, RefreshCw, UserCheck, Search, X } from 'lucide-react';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Loading } from '../components/ui/Feedback';
import { formatRelativeTime } from '../lib/utils';

const actionVariants: Record<string, 'success' | 'danger' | 'warning' | 'info' | 'default'> = {
  create: 'success',
  created: 'success',
  update: 'info',
  updated: 'info',
  delete: 'danger',
  deleted: 'danger',
  login: 'default',
  logout: 'default',
};

const getActionVariant = (action: string) =>
  actionVariants[action.toLowerCase()] ?? 'default';

export const AuditPage: React.FC = () => {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState('');

  const loadLogs = async () => {
    setLoading(true);
    try {
      setLogs(await api.getAuditLogs(100, 0).catch(() => []));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadLogs(); }, []);

  const filteredLogs = filter
    ? logs.filter(
        (l) =>
          l.action.includes(filter) ||
          l.resource_type.includes(filter) ||
          l.resource_id.includes(filter) ||
          l.user_id.includes(filter)
      )
    : logs;

  return (
    <div className="space-y-5 max-w-[1100px]">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
            <History className="w-5 h-5 text-neutral-400" />
            Audit Trail
          </h1>
          <p className="text-sm text-neutral-500 mt-0.5">
            Immutable log of all configuration mutations and administrative actions.
          </p>
        </div>
        <Button variant="secondary" size="sm" icon={<RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />} onClick={loadLogs} disabled={loading}>
          Refresh
        </Button>
      </div>

      {!loading && logs.length > 0 && (
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-neutral-600" />
          <input
            type="text"
            placeholder="Filter by action, resource type, resource ID, or admin user..."
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

      {loading ? (
        <Loading text="Loading audit events..." />
      ) : filteredLogs.length === 0 ? (
        <div className="bg-neutral-900 border border-neutral-800 rounded-2xl p-12 text-center">
          <p className="text-sm text-neutral-600">
            {filter ? 'No audit events matching your filter.' : 'No audit log entries recorded yet.'}
          </p>
        </div>
      ) : (
        <div className="bg-neutral-900 border border-neutral-800 rounded-2xl overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="border-b border-neutral-800/80 bg-neutral-950/50">
                  {['Time', 'Admin User', 'Action', 'Resource', 'Identifier', 'Project / Env'].map((h) => (
                    <th key={h} className="py-2.5 px-4 text-[10px] font-semibold uppercase tracking-wider text-neutral-600">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-neutral-800/50">
                {filteredLogs.map((log) => (
                  <tr key={log.id} className="hover:bg-neutral-800/40 transition-colors">
                    <td className="py-2.5 px-4">
                      <p className="text-[11px] font-mono text-neutral-600 whitespace-nowrap">
                        {new Date(log.created_at).toLocaleTimeString()}
                      </p>
                      <p className="text-[10px] text-neutral-700">
                        {new Date(log.created_at).toLocaleDateString()}
                      </p>
                    </td>
                    <td className="py-2.5 px-4">
                      <div className="flex items-center gap-1.5">
                        <UserCheck className="w-3 h-3 text-neutral-600 flex-shrink-0" />
                        <span className="text-[11px] font-mono text-neutral-300 truncate max-w-[140px]">{log.user_id}</span>
                      </div>
                    </td>
                    <td className="py-2.5 px-4">
                      <Badge variant={getActionVariant(log.action)}>{log.action}</Badge>
                    </td>
                    <td className="py-2.5 px-4 text-xs text-neutral-400">{log.resource_type}</td>
                    <td className="py-2.5 px-4 text-[11px] font-mono text-neutral-500 truncate max-w-[160px]">
                      {log.resource_id}
                    </td>
                    <td className="py-2.5 px-4 text-[11px] font-mono text-neutral-600">
                      {log.project_id} / {log.environment}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="px-4 py-2.5 border-t border-neutral-800/80 text-[11px] text-neutral-600">
            {filteredLogs.length} of {logs.length} audit events
          </div>
        </div>
      )}
    </div>
  );
};
