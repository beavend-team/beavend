import React, { useEffect, useState } from 'react';
import { api, Prompt } from '../api/client';
import { MessageSquare, Plus, Trash2, Tag, Eye, Edit3 } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Input, Textarea } from '../components/ui/Form';
import { Alert, EmptyState, Loading } from '../components/ui/Feedback';
import { formatRelativeTime } from '../lib/utils';

export const PromptsPage: React.FC = () => {
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const [promptId, setPromptId] = useState('');
  const [promptName, setPromptName] = useState('');
  const [promptDesc, setPromptDesc] = useState('');
  const [template, setTemplate] = useState(
    'You are an AI assistant for {{app_name}}. Today is {{current_date}}.\nUser context: {{user_name}}'
  );

  const loadData = async () => {
    setLoading(true);
    try {
      setPrompts(await api.listPrompts().catch(() => []));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadData(); }, []);

  const extractVariables = (tmpl: string): string[] => {
    const regex = /\{\{([^}]+)\}\}/g;
    const matches = new Set<string>();
    let m;
    while ((m = regex.exec(tmpl)) !== null) matches.add(m[1].trim());
    return Array.from(matches);
  };

  const detectedVars = extractVariables(template);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setCreating(true);
    try {
      await api.createPrompt({ id: promptId, name: promptName, description: promptDesc, template, variables: detectedVars });
      setShowModal(false);
      setPromptId(''); setPromptName(''); setPromptDesc('');
      loadData();
    } catch (err: any) {
      setError(err.message || 'Failed to create prompt');
    } finally {
      setCreating(false);
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
    <div className="space-y-5 max-w-[1100px]">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
            <MessageSquare className="w-5 h-5 text-neutral-400" />
            Prompts Studio
          </h1>
          <p className="text-sm text-neutral-500 mt-0.5">
            Author and version system prompt templates with dynamic variable substitution.
          </p>
        </div>
        <Button variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />} onClick={() => setShowModal(true)}>
          New Prompt
        </Button>
      </div>

      {loading ? (
        <Loading text="Loading prompt templates..." />
      ) : prompts.length === 0 ? (
        <EmptyState
          icon={<MessageSquare className="w-6 h-6" />}
          title="No Prompts defined"
          description="Create versioned prompt templates with dynamic variables like {{user_name}} and {{current_date}}."
          action={
            <Button variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />} onClick={() => setShowModal(true)}>
              Create Prompt
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          {prompts.map((p) => (
            <Card key={p.id} padding="md" className="group">
              <div className="flex items-start justify-between mb-3">
                <div className="w-8 h-8 rounded-lg bg-purple-500/10 border border-purple-500/15 flex items-center justify-center">
                  <MessageSquare className="w-4 h-4 text-purple-400" />
                </div>
                <div className="flex items-center gap-1.5">
                  <Badge variant="mono">v{p.active_version}</Badge>
                  <button
                    onClick={() => handleDelete(p.id)}
                    className="p-1 text-neutral-600 hover:text-red-400 hover:bg-neutral-800 rounded transition-colors opacity-0 group-hover:opacity-100"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>
              <h3 className="text-sm font-semibold text-neutral-100 mb-0.5">{p.name}</h3>
              <p className="text-[11px] font-mono text-neutral-600 mb-2">{p.id}</p>
              <p className="text-xs text-neutral-500 line-clamp-2 mb-3">
                {p.description || 'No description provided.'}
              </p>
              <div className="pt-2.5 border-t border-neutral-800/80 flex items-center justify-between text-[10px] text-neutral-700">
                <span className="flex items-center gap-1">
                  <Tag className="w-3 h-3" />
                  Template with variables
                </span>
                <span>{formatRelativeTime(p.updated_at)}</span>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal
        open={showModal}
        onClose={() => { setShowModal(false); setError(null); }}
        title="Create Prompt Template"
        description="Use {{variable_name}} syntax for dynamic template variables"
        size="lg"
      >
        {error && <Alert variant="error" className="mb-4" onDismiss={() => setError(null)}>{error}</Alert>}
        <form onSubmit={handleCreate} className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Input label="Prompt Identifier (ID)" required placeholder="e.g. system_support" value={promptId} onChange={(e) => setPromptId(e.target.value)} className="font-mono" />
            <Input label="Display Name" required placeholder="e.g. Support Persona" value={promptName} onChange={(e) => setPromptName(e.target.value)} />
          </div>
          <Input label="Description" placeholder="Purpose of this prompt template..." value={promptDesc} onChange={(e) => setPromptDesc(e.target.value)} />
          <div className="space-y-1">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-neutral-400">Template Text</label>
              <span className="text-[10px] text-neutral-600 font-mono">{'{{variable_name}}'} for dynamic inputs</span>
            </div>
            <Textarea
              rows={7}
              required
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              mono
            />
          </div>

          {detectedVars.length > 0 && (
            <div className="space-y-1">
              <p className="text-xs font-medium text-neutral-400 flex items-center gap-1.5">
                <Tag className="w-3.5 h-3.5 text-emerald-400" />
                Detected Variables ({detectedVars.length})
              </p>
              <div className="flex flex-wrap gap-1.5">
                {detectedVars.map((v) => (
                  <Badge key={v} variant="success">{v}</Badge>
                ))}
              </div>
            </div>
          )}

          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button type="submit" variant="primary" size="sm" loading={creating}>Save Prompt</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
};
