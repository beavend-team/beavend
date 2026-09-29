import React, { useEffect, useState } from 'react';
import { api, Prompt } from '../api/client';
import { MessageSquare, Plus, Trash2, X, Tag } from 'lucide-react';

export const PromptsPage: React.FC = () => {
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [promptId, setPromptId] = useState('');
  const [promptName, setPromptName] = useState('');
  const [promptDesc, setPromptDesc] = useState('');
  const [template, setTemplate] = useState('You are an AI assistant for {{app_name}}. Today is {{current_date}}.\nUser context: {{user_name}}');

  const loadData = async () => {
    setLoading(true);
    try {
      const data = await api.listPrompts().catch(() => []);
      setPrompts(data);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  const extractVariables = (tmpl: string): string[] => {
    const regex = /\{\{([^}]+)\}\}/g;
    const matches = new Set<string>();
    let m;
    while ((m = regex.exec(tmpl)) !== null) {
      matches.add(m[1].trim());
    }
    return Array.from(matches);
  };

  const detectedVars = extractVariables(template);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await api.createPrompt({
        id: promptId,
        name: promptName,
        description: promptDesc,
        template: template,
        variables: detectedVars,
      });
      setShowModal(false);
      setPromptId('');
      setPromptName('');
      setPromptDesc('');
      loadData();
    } catch (err: any) {
      setError(err.message || 'Failed to create prompt');
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(`Delete Prompt "${id}"? This will invalidate active snapshots.`)) return;
    try {
      await api.deletePrompt(id);
      loadData();
    } catch (err: any) {
      alert(`Error deleting prompt: ${err.message}`);
    }
  };

  return (
    <div className="space-y-6 max-w-6xl">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-100 flex items-center gap-2">
            <MessageSquare className="w-6 h-6 text-emerald-400" />
            Prompts Studio
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Author and version system prompt templates with automatic variable extraction.
          </p>
        </div>

        <button
          onClick={() => setShowModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-sm font-medium transition-colors shadow-lg shadow-emerald-900/20"
        >
          <Plus className="w-4 h-4" />
          <span>New Prompt</span>
        </button>
      </div>

      {loading ? (
        <div className="text-center py-12 text-sm text-slate-500">Loading prompt templates...</div>
      ) : prompts.length === 0 ? (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-12 text-center space-y-4">
          <MessageSquare className="w-12 h-12 text-slate-600 mx-auto" />
          <div>
            <h3 className="text-base font-semibold text-slate-300">No Prompts registered</h3>
            <p className="text-xs text-slate-500 mt-1">Create versioned prompt templates with dynamic variables.</p>
          </div>
          <button
            onClick={() => setShowModal(true)}
            className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 text-xs font-medium rounded-xl border border-slate-700 transition-colors"
          >
            Create Prompt
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {prompts.map((p) => (
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
                  title="Delete Prompt"
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
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-xl w-full p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="font-bold text-slate-100 text-lg">Create Prompt Template</h3>
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
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Prompt Identifier (ID)</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. system_support"
                    value={promptId}
                    onChange={(e) => setPromptId(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500 font-mono"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Prompt Name</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. Support Persona"
                    value={promptName}
                    onChange={(e) => setPromptName(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500"
                  />
                </div>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Description</label>
                <input
                  type="text"
                  placeholder="Purpose of this prompt template..."
                  value={promptDesc}
                  onChange={(e) => setPromptDesc(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500"
                />
              </div>

              <div>
                <div className="flex items-center justify-between mb-1">
                  <label className="block text-xs font-semibold text-slate-400">Template Text</label>
                  <span className="text-[11px] text-slate-500 font-mono">Use {'{{variable_name}}'} for dynamic inputs</span>
                </div>
                <textarea
                  rows={6}
                  required
                  value={template}
                  onChange={(e) => setTemplate(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl p-3 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                />
              </div>

              {detectedVars.length > 0 && (
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1 flex items-center gap-1.5">
                    <Tag className="w-3.5 h-3.5 text-emerald-400" />
                    Detected Variables ({detectedVars.length}):
                  </label>
                  <div className="flex flex-wrap gap-1.5">
                    {detectedVars.map((v) => (
                      <span key={v} className="bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 px-2 py-0.5 rounded text-xs font-mono">
                        {v}
                      </span>
                    ))}
                  </div>
                </div>
              )}

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
                  Save Prompt
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
