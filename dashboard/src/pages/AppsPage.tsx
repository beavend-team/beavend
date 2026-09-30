import React, { useEffect, useState } from 'react';
import { api, AIApp, Prompt, ModelRouting } from '../api/client';
import { AppWorkspace } from './AppWorkspace';
import { Bot, Plus, Trash2, ChevronRight, Sparkles, Clock, Cpu } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Input, Textarea, Select } from '../components/ui/Form';
import { Alert, EmptyState, Loading } from '../components/ui/Feedback';
import { formatRelativeTime } from '../lib/utils';

export const AppsPage: React.FC = () => {
  const [apps, setApps] = useState<AIApp[]>([]);
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [models, setModels] = useState<ModelRouting[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedAppId, setSelectedAppId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  // Form state
  const [appId, setAppId] = useState('');
  const [appName, setAppName] = useState('');
  const [appDesc, setAppDesc] = useState('');
  const [selectedPrompt, setSelectedPrompt] = useState('');
  const [selectedModel, setSelectedModel] = useState('');
  const [accessPolicy, setAccessPolicy] = useState('');
  const [creating, setCreating] = useState(false);

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

  useEffect(() => { loadData(); }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setCreating(true);
    try {
      await api.createApp({
        id: appId, name: appName, description: appDesc,
        prompt_id: selectedPrompt, model_routing_id: selectedModel, access_policy: accessPolicy,
      });
      const createdId = appId;
      setShowModal(false);
      resetForm();
      await loadData();
      setSelectedAppId(createdId);
    } catch (err: any) {
      setError(err.message || 'Failed to create AI App');
    } finally {
      setCreating(false);
    }
  };

  const resetForm = () => {
    setAppId(''); setAppName(''); setAppDesc(''); setAccessPolicy('');
  };

  const handleDelete = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (!confirm(`Delete AI App "${id}"? This will invalidate active snapshots.`)) return;
    setDeleting(id);
    try {
      await api.deleteApp(id);
      if (selectedAppId === id) setSelectedAppId(null);
      loadData();
    } catch (err: any) {
      alert(`Error deleting app: ${err.message}`);
    } finally {
      setDeleting(null);
    }
  };

  if (selectedAppId) {
    return (
      <AppWorkspace
        appId={selectedAppId}
        onBack={() => setSelectedAppId(null)}
        onRefreshApps={loadData}
      />
    );
  }

  return (
    <div className="space-y-5 max-w-[1100px]">
      {/* Page Header */}
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
            <Bot className="w-5 h-5 text-neutral-400" />
            AI Applications
          </h1>
          <p className="text-sm text-neutral-500 mt-0.5">
            Configure per-app prompts, models, tools, rate limits, and safety guardrails.
          </p>
        </div>
        <Button
          variant="primary"
          size="sm"
          icon={<Plus className="w-3.5 h-3.5" />}
          onClick={() => setShowModal(true)}
        >
          New App
        </Button>
      </div>

      {/* Content */}
      {loading ? (
        <Loading text="Loading AI applications..." />
      ) : apps.length === 0 ? (
        <EmptyState
          icon={<Bot className="w-6 h-6" />}
          title="No AI Applications configured"
          description="Create your first AI App to enable chat completions and streaming for your users."
          action={
            <Button variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />} onClick={() => setShowModal(true)}>
              Create AI App
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          {apps.map((app) => (
            <Card
              key={app.id}
              hover
              onClick={() => setSelectedAppId(app.id)}
              padding="md"
              className="group relative"
            >
              <div className="flex items-start justify-between mb-3">
                <div className="w-9 h-9 rounded-lg bg-emerald-500/10 border border-emerald-500/15 flex items-center justify-center">
                  <Bot className="w-4 h-4 text-emerald-400" />
                </div>
                <div className="flex items-center gap-1.5">
                  <Badge variant="mono">v{app.active_version}</Badge>
                  <button
                    onClick={(e) => handleDelete(app.id, e)}
                    disabled={deleting === app.id}
                    className="p-1 text-neutral-600 hover:text-red-400 hover:bg-neutral-800 rounded transition-colors opacity-0 group-hover:opacity-100"
                    title="Delete app"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>

              <div className="space-y-1 mb-3">
                <h3 className="text-sm font-semibold text-neutral-100 group-hover:text-emerald-400 transition-colors">
                  {app.name}
                </h3>
                <p className="text-[11px] font-mono text-neutral-600">{app.id}</p>
              </div>

              <p className="text-xs text-neutral-500 line-clamp-2 mb-4">
                {app.description || 'No description. Click to configure this app.'}
              </p>

              <div className="pt-3 border-t border-neutral-800/80 flex items-center justify-between">
                <div className="flex items-center gap-1 text-[10px] text-neutral-700">
                  <Clock className="w-3 h-3" />
                  {formatRelativeTime(app.updated_at)}
                </div>
                <div className="flex items-center gap-1 text-[11px] text-neutral-500 group-hover:text-emerald-400 transition-colors font-medium">
                  Configure
                  <ChevronRight className="w-3 h-3 group-hover:translate-x-0.5 transition-transform" />
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Create Modal */}
      <Modal
        open={showModal}
        onClose={() => { setShowModal(false); setError(null); resetForm(); }}
        title="Create New AI App"
        description="Configure the application ID, model routing, and optional access policy."
        size="md"
      >
        {error && <Alert variant="error" className="mb-4" onDismiss={() => setError(null)}>{error}</Alert>}

        <form onSubmit={handleCreate} className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Input
              label="App Identifier (ID)"
              required
              placeholder="e.g. customer_support"
              value={appId}
              onChange={(e) => setAppId(e.target.value)}
              className="font-mono"
            />
            <Input
              label="Display Name"
              required
              placeholder="e.g. Customer Support Bot"
              value={appName}
              onChange={(e) => setAppName(e.target.value)}
            />
          </div>

          <Textarea
            label="Description"
            rows={2}
            placeholder="Brief description of this app's role and capabilities..."
            value={appDesc}
            onChange={(e) => setAppDesc(e.target.value)}
          />

          <div className="grid grid-cols-2 gap-3">
            <Select
              label="Assigned Prompt"
              value={selectedPrompt}
              onChange={(e) => setSelectedPrompt(e.target.value)}
              options={[
                { value: '', label: '(None / Pass-through)' },
                ...prompts.map((p) => ({ value: p.id, label: `${p.name}` })),
              ]}
            />
            <Select
              label="Logical Model"
              value={selectedModel}
              onChange={(e) => setSelectedModel(e.target.value)}
              options={[
                { value: '', label: '(Default: fast)' },
                ...models.map((m) => ({ value: m.logical_model, label: `${m.logical_model} → ${m.deployment}` })),
              ]}
            />
          </div>

          <Input
            label="Access Policy (CEL, optional)"
            placeholder="e.g. claims.plan in ['pro', 'team']"
            value={accessPolicy}
            onChange={(e) => setAccessPolicy(e.target.value)}
            className="font-mono text-xs"
            hint="Leave empty to allow all authenticated users"
          />

          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button type="submit" variant="primary" size="sm" loading={creating} icon={<Sparkles className="w-3.5 h-3.5" />}>
              Create App
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
};
