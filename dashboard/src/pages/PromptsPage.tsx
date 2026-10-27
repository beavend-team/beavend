import React, { useEffect, useState } from 'react';
import { api, Prompt } from '../api/client';
import { MessageSquare, Plus, Trash2, Tag, Pencil } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Input, Textarea } from '../components/ui/Form';
import { Alert, EmptyState, Loading } from '../components/ui/Feedback';
import { VariableHelper } from '../components/VariablePicker';
import { formatRelativeTime } from '../lib/utils';

export const PromptsPage: React.FC = () => {
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [loading, setLoading] = useState(true);

  // Create form modal state
  const [showModal, setShowModal] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [promptId, setPromptId] = useState('');
  const [promptName, setPromptName] = useState('');
  const [promptDesc, setPromptDesc] = useState('');
  const [template, setTemplate] = useState(
    'You are an AI assistant for {{app_name}}. Today is {{current_date}}.\nUser context: {{user_name}}'
  );

  // Edit modal state
  const [showEditModal, setShowEditModal] = useState(false);
  const [editPromptId, setEditPromptId] = useState('');
  const [editName, setEditName] = useState('');
  const [editDesc, setEditDesc] = useState('');
  const [editTemplate, setEditTemplate] = useState('');
  const [loadingEdit, setLoadingEdit] = useState(false);
  const [savingEdit, setSavingEdit] = useState(false);
  const [editError, setEditError] = useState<string | null>(null);

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
  const editDetectedVars = extractVariables(editTemplate);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setCreating(true);
    try {
      await api.createPrompt({
        id: promptId,
        name: promptName,
        description: promptDesc,
        template,
        variables: detectedVars,
      });
      setShowModal(false);
      setPromptId('');
      setPromptName('');
      setPromptDesc('');
      loadData();
    } catch (err: any) {
      setError(err.message || 'Failed to create prompt');
    } finally {
      setCreating(false);
    }
  };

  const openEditModal = async (id: string, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    setEditPromptId(id);
    setEditError(null);
    setShowEditModal(true);
    setLoadingEdit(true);
    try {
      const data = await api.getPrompt(id);
      setEditName(data.prompt?.name || '');
      setEditDesc(data.prompt?.description || '');
      const activeTmpl = (data as any).active_version?.template
        || data.versions?.find((v: any) => v.version === data.prompt?.active_version)?.template
        || data.versions?.[0]?.template
        || '';
      setEditTemplate(activeTmpl);
    } catch (err: any) {
      setEditError(err.message || 'Failed to load prompt details');
    } finally {
      setLoadingEdit(false);
    }
  };

  const handleSaveEdit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editPromptId) return;
    setSavingEdit(true);
    setEditError(null);
    try {
      await api.updatePrompt(editPromptId, {
        name: editName,
        description: editDesc,
        template: editTemplate,
        variables: editDetectedVars,
      });
      setShowEditModal(false);
      loadData();
    } catch (err: any) {
      setEditError(err.message || 'Failed to update prompt');
    } finally {
      setSavingEdit(false);
    }
  };

  const handleDelete = async (id: string, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
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
            Author and configure system prompt templates with dynamic variable substitution.
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
          description="Create system prompt templates with dynamic variables like {{user_name}} and {{current_date}}."
          action={
            <Button variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />} onClick={() => setShowModal(true)}>
              Create Prompt
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          {prompts.map((p) => (
            <Card
              key={p.id}
              padding="md"
              hover
              className="group cursor-pointer"
              onClick={() => openEditModal(p.id)}
            >
              <div className="flex items-start justify-between mb-3">
                <div className="w-8 h-8 rounded-lg bg-purple-500/10 border border-purple-500/15 flex items-center justify-center">
                  <MessageSquare className="w-4 h-4 text-purple-400" />
                </div>
                <button
                  onClick={(e) => handleDelete(p.id, e)}
                  className="p-1 text-neutral-600 hover:text-red-400 hover:bg-neutral-800 rounded transition-colors opacity-0 group-hover:opacity-100"
                  title="Delete prompt"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
              <h3 className="text-sm font-semibold text-neutral-100 group-hover:text-emerald-400 transition-colors mb-0.5">
                {p.name}
              </h3>
              <p className="text-[11px] font-mono text-neutral-600 mb-2">{p.id}</p>
              <p className="text-xs text-neutral-500 line-clamp-2 mb-3">
                {p.description || 'No description provided.'}
              </p>
              <div className="pt-2.5 border-t border-neutral-800/80 flex items-center justify-between text-[10px] text-neutral-600">
                <span className="flex items-center gap-1">
                  <Tag className="w-3 h-3 text-purple-400/80" />
                  Template
                </span>
                <span>{formatRelativeTime(p.updated_at)}</span>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Create Modal */}
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
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-neutral-400">Template Text</label>
            </div>
            <VariableHelper
              detectedVars={detectedVars}
              onInsert={(syntax) => setTemplate((prev) => (prev ? `${prev}\n${syntax}` : syntax))}
            />
            <Textarea rows={7} required value={template} onChange={(e) => setTemplate(e.target.value)} mono />
          </div>

          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button type="submit" variant="primary" size="sm" loading={creating}>Save Prompt</Button>
          </div>
        </form>
      </Modal>

      {/* Edit Prompt Modal */}
      <Modal
        open={showEditModal}
        onClose={() => { setShowEditModal(false); setEditError(null); }}
        title="Edit System Prompt"
        description="Update prompt template and instructions"
        size="lg"
      >
        {editError && <Alert variant="error" className="mb-4" onDismiss={() => setEditError(null)}>{editError}</Alert>}
        {loadingEdit ? (
          <div className="py-12">
            <Loading text="Loading prompt..." />
          </div>
        ) : (
          <form onSubmit={handleSaveEdit} className="space-y-4">
            <div className="grid grid-cols-2 gap-3">
              <Input
                label="Prompt Identifier (ID)"
                value={editPromptId}
                disabled
                className="font-mono opacity-60 cursor-not-allowed"
              />
              <Input
                label="Display Name"
                required
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
              />
            </div>
            <Input
              label="Description"
              placeholder="Purpose of this prompt template..."
              value={editDesc}
              onChange={(e) => setEditDesc(e.target.value)}
            />
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <label className="text-xs font-medium text-neutral-400">Template Text</label>
              </div>
              <VariableHelper
                detectedVars={editDetectedVars}
                onInsert={(syntax) => setEditTemplate((prev) => (prev ? `${prev}\n${syntax}` : syntax))}
              />
              <Textarea
                rows={8}
                required
                value={editTemplate}
                onChange={(e) => setEditTemplate(e.target.value)}
                mono
              />
            </div>

            <div className="flex justify-end gap-2 pt-1">
              <Button type="button" variant="ghost" size="sm" onClick={() => setShowEditModal(false)}>Cancel</Button>
              <Button type="submit" variant="primary" size="sm" loading={savingEdit}>Save Changes</Button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  );
};
