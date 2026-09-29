import React, { useEffect, useState } from 'react';
import { api, ModelRouting, ProviderConfig } from '../api/client';
import { Cpu, Plus, Trash2, KeyRound, Shield, Check, X, Server } from 'lucide-react';

export const ModelsPage: React.FC = () => {
  const [models, setModels] = useState<ModelRouting[]>([]);
  const [providers, setProviders] = useState<ProviderConfig[]>([]);
  const [loading, setLoading] = useState(true);

  // Model Modal
  const [showModelModal, setShowModelModal] = useState(false);
  const [logicalModel, setLogicalModel] = useState('fast');
  const [deployment, setDeployment] = useState('gpt-4o-mini');
  const [adapter, setAdapter] = useState('litellm');

  // Provider Modal
  const [showProviderModal, setShowProviderModal] = useState(false);
  const [providerName, setProviderName] = useState('litellm');
  const [baseUrl, setBaseUrl] = useState('http://localhost:4000');
  const [apiKey, setApiKey] = useState('');

  const loadData = async () => {
    setLoading(true);
    try {
      const [m, p] = await Promise.all([
        api.listModels().catch(() => []),
        api.listProviders().catch(() => []),
      ]);
      setModels(m);
      setProviders(p);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  const handleSaveModel = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.upsertModel(logicalModel, {
        deployment,
        adapter,
        fallback_deployments: [],
      });
      setShowModelModal(false);
      loadData();
    } catch (err: any) {
      alert(`Failed to save model routing: ${err.message}`);
    }
  };

  const handleDeleteModel = async (lm: string) => {
    if (!confirm(`Delete logical model routing "${lm}"?`)) return;
    try {
      await api.deleteModel(lm);
      loadData();
    } catch (err: any) {
      alert(`Error deleting model routing: ${err.message}`);
    }
  };

  const handleSaveProvider = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.upsertProvider(providerName, {
        base_url: baseUrl,
        api_key: apiKey,
      });
      setShowProviderModal(false);
      setApiKey('');
      loadData();
    } catch (err: any) {
      alert(`Failed to save provider config: ${err.message}`);
    }
  };

  return (
    <div className="space-y-8 max-w-6xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-slate-100 flex items-center gap-2">
          <Cpu className="w-6 h-6 text-emerald-400" />
          Models & Provider Routing
        </h1>
        <p className="text-sm text-slate-400 mt-1">
          Map client-facing logical model aliases (e.g. fast, reasoning) to provider deployments.
        </p>
      </div>

      {/* Logical Models Section */}
      <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold text-slate-200">Logical Model Routing</h2>
            <p className="text-xs text-slate-500">Clients request an alias; Beavend routes to the configured deployment.</p>
          </div>
          <button
            onClick={() => setShowModelModal(true)}
            className="flex items-center gap-2 px-3 py-1.5 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-xs font-medium transition-colors"
          >
            <Plus className="w-3.5 h-3.5" />
            <span>Map Model</span>
          </button>
        </div>

        {models.length === 0 ? (
          <div className="text-center py-8 text-xs text-slate-500">
            No logical model routings configured. Click "Map Model" to set up fast, reasoning, or vision aliases.
          </div>
        ) : (
          <div className="divide-y divide-slate-800/80">
            {models.map((m) => (
              <div key={m.logical_model} className="py-3 flex items-center justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-sm font-semibold text-emerald-400">{m.logical_model}</span>
                    <span className="text-[10px] bg-slate-800 text-slate-400 border border-slate-700 px-2 py-0.5 rounded-full">
                      Adapter: {m.adapter}
                    </span>
                  </div>
                  <span className="text-xs text-slate-400 mt-0.5 block font-mono">Deployment: {m.deployment}</span>
                </div>
                <button
                  onClick={() => handleDeleteModel(m.logical_model)}
                  className="p-1.5 text-slate-500 hover:text-rose-400 rounded-lg transition-colors"
                  title="Delete Routing"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Provider Configs Section */}
      <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold text-slate-200">Provider Connections & Credentials</h2>
            <p className="text-xs text-slate-500">Credentials are encrypted with AES-256-GCM envelope encryption at rest.</p>
          </div>
          <button
            onClick={() => setShowProviderModal(true)}
            className="flex items-center gap-2 px-3 py-1.5 bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 rounded-xl text-xs font-medium transition-colors"
          >
            <KeyRound className="w-3.5 h-3.5" />
            <span>Configure Provider</span>
          </button>
        </div>

        {providers.length === 0 ? (
          <div className="text-center py-8 text-xs text-slate-500">
            No provider credentials stored. Configure LiteLLM or direct OpenAI endpoint credentials.
          </div>
        ) : (
          <div className="divide-y divide-slate-800/80">
            {providers.map((p) => (
              <div key={p.provider_name} className="py-3 flex items-center justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="font-semibold text-slate-200 text-sm">{p.provider_name}</span>
                    <span className="text-[10px] bg-purple-500/10 text-purple-400 border border-purple-500/20 px-2 py-0.5 rounded-full">
                      AES-256-GCM Encrypted
                    </span>
                  </div>
                  <span className="text-xs text-slate-400 font-mono mt-0.5 block">{p.base_url || '(Default Endpoint)'}</span>
                </div>
                <div className="text-xs font-mono text-slate-500">
                  API Key: {p.masked_api_key}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Model Modal */}
      {showModelModal && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4 z-50">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-md w-full p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="font-bold text-slate-100 text-base">Map Logical Model</h3>
              <button onClick={() => setShowModelModal(false)} className="text-slate-400 hover:text-slate-200">
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={handleSaveModel} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Logical Model Alias</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. fast, reasoning, vision"
                  value={logicalModel}
                  onChange={(e) => setLogicalModel(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Provider Deployment</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. gpt-4o, claude-3-5-sonnet, gemini-1.5-pro"
                  value={deployment}
                  onChange={(e) => setDeployment(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Adapter</label>
                <select
                  value={adapter}
                  onChange={(e) => setAdapter(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                >
                  <option value="litellm">LiteLLM (Universal Gateway)</option>
                  <option value="openaicompat">Direct OpenAI-Compatible</option>
                </select>
              </div>

              <div className="pt-2 flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setShowModelModal(false)}
                  className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-xl text-sm"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-sm font-medium"
                >
                  Save Routing
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Provider Modal */}
      {showProviderModal && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4 z-50">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-md w-full p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="font-bold text-slate-100 text-base">Configure Provider Credentials</h3>
              <button onClick={() => setShowProviderModal(false)} className="text-slate-400 hover:text-slate-200">
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={handleSaveProvider} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Provider Name</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. litellm, openai, anthropic"
                  value={providerName}
                  onChange={(e) => setProviderName(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Base URL</label>
                <input
                  type="text"
                  placeholder="e.g. http://localhost:4000"
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">API Key / Master Key</label>
                <input
                  type="password"
                  required
                  placeholder="••••••••••••••••"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                />
              </div>

              <div className="pt-2 flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setShowProviderModal(false)}
                  className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-xl text-sm"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-sm font-medium"
                >
                  Save Credentials
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
