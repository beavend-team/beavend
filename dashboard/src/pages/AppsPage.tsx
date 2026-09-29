import React, { useEffect, useState } from 'react';
import { api, AIApp, Prompt, ModelRouting } from '../api/client';
import { Bot, Plus, Trash2, Edit3, Check, X, Shield, Sparkles } from 'lucide-react';

export const AppsPage: React.FC = () => {
  const [apps, setApps] = useState<AIApp[]>([]);
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [models, setModels] = useState<ModelRouting[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Form State
  const [appId, setAppId] = useState('');
  const [appName, setAppName] = useState('');
  const [appDesc, setAppDesc] = useState('');
  const [selectedPrompt, setSelectedPrompt] = useState('');
  const [selectedModel, setSelectedModel] = useState('');
  const [accessPolicy, setAccessPolicy] = useState('');

  const loadData = async () => {
    setLoading(true);
    try {
      const [appsData, promptsData, modelsData] = await Promise.all([
        api.listApps().catch(() => []),
        api.listPrompts().catch(() => []),
        api.listModels().catch(() => []),
      ]);
      setApps(appsData);
      setPrompts(promptsData);
      setModels(modelsData);
      if (promptsData.length > 0) setSelectedPrompt(promptsData[0].id);
      if (modelsData.length > 0) setSelectedModel(modelsData[0].logical_model);
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
      await api.createApp({
        id: appId,
        name: appName,
        description: appDesc,
        prompt_id: selectedPrompt,
        model_routing_id: selectedModel,
        access_policy: accessPolicy,
      });
      setShowModal(false);
      setAppId('');
      setAppName('');
      setAppDesc('');
      setAccessPolicy('');
      loadData();
    } catch (err: any) {
      setError(err.message || 'Failed to create AI App');
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(`Delete AI App "${id}"? This will invalidate active snapshots.`)) return;
    try {
      await api.deleteApp(id);
      loadData();
    } catch (err: any) {
      alert(`Error deleting app: ${err.message}`);
    }
  };

  return (
    <div className="space-y-6 max-w-6xl">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-100 flex items-center gap-2">
            <Bot className="w-6 h-6 text-emerald-400" />
            AI Applications
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Configure client-facing AI apps with assigned prompt templates, model routing, and policies.
          </p>
        </div>

        <button
          onClick={() => setShowModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-sm font-medium transition-colors shadow-lg shadow-emerald-900/20"
        >
          <Plus className="w-4 h-4" />
          <span>New AI App</span>
        </button>
      </div>

      {loading ? (
        <div className="text-center py-12 text-sm text-slate-500">Loading AI applications...</div>
      ) : apps.length === 0 ? (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-12 text-center space-y-4">
          <Bot className="w-12 h-12 text-slate-600 mx-auto" />
          <div>
            <h3 className="text-base font-semibold text-slate-300">No AI Applications configured</h3>
            <p className="text-xs text-slate-500 mt-1">Create your first AI App to enable runtime chat completions.</p>
          </div>
          <button
            onClick={() => setShowModal(true)}
            className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 text-xs font-medium rounded-xl border border-slate-700 transition-colors"
          >
            Create AI App
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {apps.map((app) => (
            <div key={app.id} className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="font-bold text-slate-100">{app.name}</h3>
                    <span className="text-[10px] bg-slate-800 text-slate-400 font-mono px-2 py-0.5 rounded-full border border-slate-700">
                      v{app.active_version}
                    </span>
                  </div>
                  <span className="text-xs font-mono text-emerald-400 mt-0.5 block">{app.id}</span>
                </div>
                <button
                  onClick={() => handleDelete(app.id)}
                  className="p-1.5 text-slate-500 hover:text-rose-400 hover:bg-slate-800 rounded-lg transition-colors"
                  title="Delete App"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>

              <p className="text-xs text-slate-400 line-clamp-2">
                {app.description || 'No description provided.'}
              </p>

              <div className="pt-3 border-t border-slate-800/80 flex items-center justify-between text-xs text-slate-500">
                <span className="font-mono">Endpoint: /v1/apps/{app.id}/chat</span>
                <span>Active</span>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Create Modal */}
      {showModal && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4 z-50">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-lg w-full p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="font-bold text-slate-100 text-lg">Create New AI App</h3>
              <button onClick={() => setShowModal(false)} className="text-slate-400 hover:text-slate-200">
                <X className="w-5 h-5" />
              </button>
            </div>

            {error && (
              <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs">
                {error}
              </div>
            )}

            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">App Identifier (ID)</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. customer_support_bot"
                  value={appId}
                  onChange={(e) => setAppId(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500 font-mono"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">App Name</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. Customer Support Assistant"
                  value={appName}
                  onChange={(e) => setAppName(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Description</label>
                <textarea
                  rows={2}
                  placeholder="Brief explanation of app role and capabilities..."
                  value={appDesc}
                  onChange={(e) => setAppDesc(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Assigned Prompt</label>
                  <select
                    value={selectedPrompt}
                    onChange={(e) => setSelectedPrompt(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                  >
                    <option value="">(None / Pass-through)</option>
                    {prompts.map((p) => (
                      <option key={p.id} value={p.id}>{p.name} ({p.id})</option>
                    ))}
                  </select>
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Logical Model</label>
                  <select
                    value={selectedModel}
                    onChange={(e) => setSelectedModel(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                  >
                    <option value="">(Default: fast)</option>
                    {models.map((m) => (
                      <option key={m.logical_model} value={m.logical_model}>{m.logical_model} ({m.deployment})</option>
                    ))}
                  </select>
                </div>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Access Policy (Optional CEL)</label>
                <input
                  type="text"
                  placeholder="e.g. claims.plan in ['pro', 'team']"
                  value={accessPolicy}
                  onChange={(e) => setAccessPolicy(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500 font-mono"
                />
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
                  Save Application
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
