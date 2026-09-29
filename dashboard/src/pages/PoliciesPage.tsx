import React, { useEffect, useState } from 'react';
import { api, Policy } from '../api/client';
import { ShieldCheck, Plus, Trash2, X, AlertCircle } from 'lucide-react';

export const PoliciesPage: React.FC = () => {
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [policyId, setPolicyId] = useState('');
  const [policyName, setPolicyName] = useState('');
  const [policyDesc, setPolicyDesc] = useState('');
  const [expression, setExpression] = useState("claims.plan in ['pro', 'team'] && claims.ai_access == true");

  const loadData = async () => {
    setLoading(true);
    try {
      const data = await api.listPolicies().catch(() => []);
      setPolicies(data);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await api.createPolicy({
        id: policyId,
        name: policyName,
        description: policyDesc,
        expression: expression,
      });
      setShowModal(false);
      setPolicyId('');
      setPolicyName('');
      setPolicyDesc('');
      loadData();
    } catch (err: any) {
      setError(err.message || 'Failed to create policy');
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(`Delete Policy "${id}"? This will invalidate active snapshots.`)) return;
    try {
      await api.deletePolicy(id);
      loadData();
    } catch (err: any) {
      alert(`Error deleting policy: ${err.message}`);
    }
  };

  return (
    <div className="space-y-6 max-w-6xl">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-100 flex items-center gap-2">
            <ShieldCheck className="w-6 h-6 text-emerald-400" />
            CEL Authorization Policies
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Google Common Expression Language (CEL) rules evaluated against caller JWT claims in microseconds.
          </p>
        </div>

        <button
          onClick={() => setShowModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-sm font-medium transition-colors shadow-lg shadow-emerald-900/20"
        >
          <Plus className="w-4 h-4" />
          <span>New Policy</span>
        </button>
      </div>

      {loading ? (
        <div className="text-center py-12 text-sm text-slate-500">Loading CEL policies...</div>
      ) : policies.length === 0 ? (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-12 text-center space-y-4">
          <ShieldCheck className="w-12 h-12 text-slate-600 mx-auto" />
          <div>
            <h3 className="text-base font-semibold text-slate-300">No Policies defined</h3>
            <p className="text-xs text-slate-500 mt-1">Create claim-based authorization policies for AI apps and tools.</p>
          </div>
          <button
            onClick={() => setShowModal(true)}
            className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 text-xs font-medium rounded-xl border border-slate-700 transition-colors"
          >
            Create Policy
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {policies.map((p) => (
            <div key={p.id} className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-3">
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="font-bold text-slate-100">{p.name}</h3>
                    <span className="text-[10px] bg-slate-800 text-slate-400 font-mono px-2 py-0.5 rounded-full border border-slate-700">
                      v{p.active_version}
                    </span>
                  </div>
                  <span className="text-xs font-mono text-emerald-400 mt-0.5 block">{p.id}</span>
                </div>
                <button
                  onClick={() => handleDelete(p.id)}
                  className="p-1.5 text-slate-500 hover:text-rose-400 hover:bg-slate-800 rounded-lg transition-colors"
                  title="Delete Policy"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>

              <p className="text-xs text-slate-400">{p.description || 'No description provided.'}</p>
            </div>
          ))}
        </div>
      )}

      {/* Modal */}
      {showModal && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4 z-50">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-lg w-full p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="font-bold text-slate-100 text-lg">Define CEL Policy</h3>
              <button onClick={() => setShowModal(false)} className="text-slate-400 hover:text-slate-200">
                <X className="w-5 h-5" />
              </button>
            </div>

            {error && (
              <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 flex-shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <form onSubmit={handleCreate} className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Policy Identifier (ID)</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. require_pro_tier"
                    value={policyId}
                    onChange={(e) => setPolicyId(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Policy Name</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. Require Pro Subscription"
                    value={policyName}
                    onChange={(e) => setPolicyName(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                  />
                </div>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Description</label>
                <input
                  type="text"
                  placeholder="Describes the authorization requirement..."
                  value={policyDesc}
                  onChange={(e) => setPolicyDesc(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">CEL Expression</label>
                <textarea
                  rows={3}
                  required
                  value={expression}
                  onChange={(e) => setExpression(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl p-3 text-xs font-mono text-emerald-400 focus:outline-none focus:border-emerald-500"
                />
                <p className="text-[11px] text-slate-500 mt-1">
                  Validated and compiled on submit with Google CEL compiler.
                </p>
              </div>

              <div className="pt-2 flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setShowModal(false)}
                  className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-xl text-sm transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-sm font-medium transition-colors shadow-lg shadow-emerald-900/20"
                >
                  Save Policy
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
