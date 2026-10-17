import React, { useEffect, useState } from 'react';
import { api, Skill, SkillDetails, ImportSkillResponse } from '../api/client';
import { Sparkles, Plus, Download, Trash2, Pencil, ExternalLink, Zap, Layers, RefreshCw, Check, Code } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Input, Textarea } from '../components/ui/Form';
import { Alert, EmptyState, Loading } from '../components/ui/Feedback';
import { formatRelativeTime } from '../lib/utils';

export const SkillsPage: React.FC = () => {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(true);

  // Create modal state
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [skillId, setSkillId] = useState('');
  const [skillName, setSkillName] = useState('');
  const [skillDesc, setSkillDesc] = useState('');
  const [skillMode, setSkillMode] = useState<'always' | 'on_demand'>('always');
  const [skillContent, setSkillContent] = useState(
    '# Role & Instructions\nYou have specialized expertise in code analysis and debugging.\n\nContext: {{context}}'
  );

  // Edit modal state
  const [showEditModal, setShowEditModal] = useState(false);
  const [editSkillId, setEditSkillId] = useState('');
  const [editName, setEditName] = useState('');
  const [editDesc, setEditDesc] = useState('');
  const [editMode, setEditMode] = useState<'always' | 'on_demand'>('always');
  const [editContent, setEditContent] = useState('');
  const [loadingEdit, setLoadingEdit] = useState(false);
  const [savingEdit, setSavingEdit] = useState(false);
  const [editError, setEditError] = useState<string | null>(null);

  // Import modal state
  const [showImportModal, setShowImportModal] = useState(false);
  const [importSource, setImportSource] = useState('');
  const [importMode, setImportMode] = useState<'always' | 'on_demand'>('always');
  const [previewLoading, setPreviewLoading] = useState(false);
  const [savingImport, setSavingImport] = useState(false);
  const [importPreview, setImportPreview] = useState<ImportSkillResponse | null>(null);
  const [importError, setImportError] = useState<string | null>(null);

  const loadData = async () => {
    setLoading(true);
    try {
      setSkills(await api.listSkills().catch(() => []));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  const extractVariables = (text: string): string[] => {
    const regex = /\{\{([^}]+)\}\}/g;
    const matches = new Set<string>();
    let m;
    while ((m = regex.exec(text)) !== null) {
      matches.add(m[1].trim());
    }
    return Array.from(matches);
  };

  const detectedVars = extractVariables(skillContent);
  const editDetectedVars = extractVariables(editContent);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreateError(null);
    setCreating(true);
    try {
      await api.createSkill({
        id: skillId.trim(),
        name: skillName.trim(),
        description: skillDesc.trim(),
        source: 'manual',
        injection_mode: skillMode,
        content: skillContent,
        variables: detectedVars,
      });
      setShowCreateModal(false);
      setSkillId('');
      setSkillName('');
      setSkillDesc('');
      setSkillContent('');
      loadData();
    } catch (err: any) {
      setCreateError(err.message || 'Failed to create skill');
    } finally {
      setCreating(false);
    }
  };

  const openEditModal = async (id: string, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    setEditSkillId(id);
    setEditError(null);
    setShowEditModal(true);
    setLoadingEdit(true);
    try {
      const data: SkillDetails = await api.getSkill(id);
      setEditName(data.skill?.name || '');
      setEditDesc(data.skill?.description || '');
      setEditMode((data.skill?.injection_mode as any) || 'always');
      const activeContent =
        data.active_version?.content ||
        data.versions?.find((v) => v.version === data.skill?.active_version)?.content ||
        data.versions?.[0]?.content ||
        '';
      setEditContent(activeContent);
    } catch (err: any) {
      setEditError(err.message || 'Failed to load skill details');
    } finally {
      setLoadingEdit(false);
    }
  };

  const handleSaveEdit = async (e: React.FormEvent) => {
    e.preventDefault();
    setEditError(null);
    setSavingEdit(true);
    try {
      await api.updateSkill(editSkillId, {
        name: editName.trim(),
        description: editDesc.trim(),
        injection_mode: editMode,
        content: editContent,
        variables: editDetectedVars,
      });
      setShowEditModal(false);
      loadData();
    } catch (err: any) {
      setEditError(err.message || 'Failed to update skill');
    } finally {
      setSavingEdit(false);
    }
  };

  const handleDelete = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (!window.confirm(`Are you sure you want to delete skill "${id}"?`)) return;
    try {
      await api.deleteSkill(id);
      loadData();
    } catch (err: any) {
      alert(err.message || 'Failed to delete skill');
    }
  };

  const handlePreviewImport = async () => {
    if (!importSource.trim()) return;
    setPreviewLoading(true);
    setImportError(null);
    setImportPreview(null);
    try {
      const preview = await api.importSkill({
        source: importSource.trim(),
        injection_mode: importMode,
        save: false,
      });
      setImportPreview(preview);
    } catch (err: any) {
      setImportError(err.message || 'Failed to preview skill from source');
    } finally {
      setPreviewLoading(false);
    }
  };

  const handleSaveImport = async () => {
    if (!importSource.trim()) return;
    setSavingImport(true);
    setImportError(null);
    try {
      await api.importSkill({
        source: importSource.trim(),
        injection_mode: importMode,
        id: importPreview?.id,
        save: true,
      });
      setShowImportModal(false);
      setImportSource('');
      setImportPreview(null);
      loadData();
    } catch (err: any) {
      setImportError(err.message || 'Failed to import and save skill');
    } finally {
      setSavingImport(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 flex items-center gap-2">
            <Sparkles className="w-5 h-5 text-emerald-400" />
            Skills Studio
          </h1>
          <p className="text-xs text-neutral-400 mt-1">
            Modular agent capability instruction sets. Inject directly into system prompts or expose dynamically on-demand via the <code className="text-neutral-300">load_skill</code> tool.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setImportSource('');
              setImportPreview(null);
              setImportError(null);
              setShowImportModal(true);
            }}
          >
            <Download className="w-3.5 h-3.5 mr-1.5" />
            Import from skills.sh / GitHub
          </Button>
          <Button
            size="sm"
            onClick={() => {
              setSkillId('');
              setSkillName('');
              setSkillDesc('');
              setSkillMode('always');
              setCreateError(null);
              setShowCreateModal(true);
            }}
          >
            <Plus className="w-3.5 h-3.5 mr-1.5" />
            New Skill
          </Button>
        </div>
      </div>

      {/* Content */}
      {loading ? (
        <Loading text="Loading skills..." />
      ) : skills.length === 0 ? (
        <EmptyState
          icon={<Sparkles className="w-6 h-6 text-neutral-400" />}
          title="No skills found"
          description="Create your first agent skill or import one directly from skills.sh or GitHub."
          action={
            <Button size="sm" onClick={() => setShowCreateModal(true)}>
              <Plus className="w-3.5 h-3.5 mr-1.5" />
              New Skill
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {skills.map((s) => {
            const isOnDemand = s.injection_mode === 'on_demand';
            return (
              <Card
                key={s.id}
                className="hover:border-neutral-700/80 transition-all cursor-pointer flex flex-col justify-between group"
                onClick={() => openEditModal(s.id)}
              >
                <div>
                  <div className="flex items-start justify-between gap-2 mb-2">
                    <div className="flex items-center gap-1.5 flex-wrap">
                      <span className="font-semibold text-sm text-neutral-100 group-hover:text-emerald-400 transition-colors">
                        {s.name}
                      </span>
                      <Badge variant="mono" className="text-[10px] font-mono">
                        v{s.active_version}
                      </Badge>
                    </div>
                    <Badge
                      variant={isOnDemand ? 'purple' : 'success'}
                      className="text-[10px]"
                    >
                      {isOnDemand ? (
                        <span className="flex items-center gap-1">
                          <Layers className="w-2.5 h-2.5" /> On-Demand
                        </span>
                      ) : (
                        <span className="flex items-center gap-1">
                          <Zap className="w-2.5 h-2.5" /> Always
                        </span>
                      )}
                    </Badge>
                  </div>

                  <p className="text-xs text-neutral-400 line-clamp-2 mb-3">
                    {s.description || 'No description provided.'}
                  </p>

                  <div className="text-[11px] font-mono text-neutral-500 bg-neutral-900/80 px-2 py-1 rounded border border-neutral-800/80 mb-3 truncate">
                    ID: {s.id}
                  </div>
                </div>

                <div className="pt-3 border-t border-neutral-800/60 flex items-center justify-between text-[11px] text-neutral-500">
                  <span title={s.updated_at}>
                    Updated {formatRelativeTime(s.updated_at)}
                  </span>
                  <div className="flex items-center gap-1">
                    <button
                      type="button"
                      onClick={(e) => openEditModal(s.id, e)}
                      className="p-1 rounded hover:bg-neutral-800 text-neutral-400 hover:text-neutral-200 transition-colors"
                      title="Edit Skill"
                    >
                      <Pencil className="w-3.5 h-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={(e) => handleDelete(s.id, e)}
                      className="p-1 rounded hover:bg-red-500/10 text-neutral-400 hover:text-red-400 transition-colors"
                      title="Delete Skill"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                </div>
              </Card>
            );
          })}
        </div>
      )}

      {/* Create Modal */}
      <Modal
        open={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        title="Create New Agent Skill"
      >
        <form onSubmit={handleCreate} className="space-y-4">
          {createError && <Alert variant="error">{createError}</Alert>}
          <div className="grid grid-cols-2 gap-3">
            <Input
              label="Skill Name"
              placeholder="e.g. Code Reviewer"
              value={skillName}
              onChange={(e) => {
                setSkillName(e.target.value);
                if (!skillId) {
                  setSkillId(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, '-'));
                }
              }}
              required
            />
            <Input
              label="Skill ID"
              placeholder="e.g. code-reviewer"
              value={skillId}
              onChange={(e) => setSkillId(e.target.value)}
              required
            />
          </div>

          <Input
            label="Description"
            placeholder="Brief description of what this skill does"
            value={skillDesc}
            onChange={(e) => setSkillDesc(e.target.value)}
          />

          <div>
            <label className="block text-xs font-medium text-neutral-300 mb-1.5">
              Injection / Delivery Mode
            </label>
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => setSkillMode('always')}
                className={`p-2.5 rounded-lg border text-left transition-all ${
                  skillMode === 'always'
                    ? 'border-emerald-500/50 bg-emerald-500/10 text-neutral-100'
                    : 'border-neutral-800 bg-neutral-900/60 text-neutral-400 hover:border-neutral-700'
                }`}
              >
                <div className="flex items-center gap-1.5 font-medium text-xs text-emerald-400 mb-0.5">
                  <Zap className="w-3.5 h-3.5" />
                  Always Injected
                </div>
                <div className="text-[11px] text-neutral-400">
                  Full skill prompt appended directly to active app system prompt.
                </div>
              </button>

              <button
                type="button"
                onClick={() => setSkillMode('on_demand')}
                className={`p-2.5 rounded-lg border text-left transition-all ${
                  skillMode === 'on_demand'
                    ? 'border-purple-500/50 bg-purple-500/10 text-neutral-100'
                    : 'border-neutral-800 bg-neutral-900/60 text-neutral-400 hover:border-neutral-700'
                }`}
              >
                <div className="flex items-center gap-1.5 font-medium text-xs text-purple-400 mb-0.5">
                  <Layers className="w-3.5 h-3.5" />
                  On-Demand Tool
                </div>
                <div className="text-[11px] text-neutral-400">
                  Lists in available catalog; loaded via built-in <code className="text-neutral-300">load_skill</code>.
                </div>
              </button>
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="block text-xs font-medium text-neutral-300">
                Skill Instructions / Markdown
              </label>
              {detectedVars.length > 0 && (
                <div className="flex items-center gap-1 text-[11px] text-neutral-400">
                  <span>Variables:</span>
                  {detectedVars.map((v) => (
                    <span key={v} className="bg-neutral-800 text-emerald-400 px-1 py-0.5 rounded text-[10px]">
                      {`{{${v}}}`}
                    </span>
                  ))}
                </div>
              )}
            </div>
            <Textarea
              rows={8}
              value={skillContent}
              onChange={(e) => setSkillContent(e.target.value)}
              placeholder="Paste Markdown instructions or full SKILL.md with frontmatter..."
              required
            />
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" type="button" onClick={() => setShowCreateModal(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={creating}>
              {creating ? 'Creating...' : 'Create Skill'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Edit Modal */}
      <Modal
        open={showEditModal}
        onClose={() => setShowEditModal(false)}
        title={`Edit Skill: ${editSkillId}`}
      >
        {loadingEdit ? (
          <Loading text="Loading skill details..." />
        ) : (
          <form onSubmit={handleSaveEdit} className="space-y-4">
            {editError && <Alert variant="error">{editError}</Alert>}
            <Input
              label="Skill Name"
              value={editName}
              onChange={(e) => setEditName(e.target.value)}
              required
            />

            <Input
              label="Description"
              value={editDesc}
              onChange={(e) => setEditDesc(e.target.value)}
            />

            <div>
              <label className="block text-xs font-medium text-neutral-300 mb-1.5">
                Injection / Delivery Mode
              </label>
              <div className="grid grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => setEditMode('always')}
                  className={`p-2.5 rounded-lg border text-left transition-all ${
                    editMode === 'always'
                      ? 'border-emerald-500/50 bg-emerald-500/10 text-neutral-100'
                      : 'border-neutral-800 bg-neutral-900/60 text-neutral-400 hover:border-neutral-700'
                  }`}
                >
                  <div className="flex items-center gap-1.5 font-medium text-xs text-emerald-400 mb-0.5">
                    <Zap className="w-3.5 h-3.5" />
                    Always Injected
                  </div>
                  <div className="text-[11px] text-neutral-400">
                    Directly into system prompt.
                  </div>
                </button>

                <button
                  type="button"
                  onClick={() => setEditMode('on_demand')}
                  className={`p-2.5 rounded-lg border text-left transition-all ${
                    editMode === 'on_demand'
                      ? 'border-purple-500/50 bg-purple-500/10 text-neutral-100'
                      : 'border-neutral-800 bg-neutral-900/60 text-neutral-400 hover:border-neutral-700'
                  }`}
                >
                  <div className="flex items-center gap-1.5 font-medium text-xs text-purple-400 mb-0.5">
                    <Layers className="w-3.5 h-3.5" />
                    On-Demand Tool
                  </div>
                  <div className="text-[11px] text-neutral-400">
                    Loaded via <code className="text-neutral-300">load_skill</code>.
                  </div>
                </button>
              </div>
            </div>

            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="block text-xs font-medium text-neutral-300">
                  Instructions / Markdown
                </label>
                {editDetectedVars.length > 0 && (
                  <div className="flex items-center gap-1 text-[11px] text-neutral-400">
                    <span>Variables:</span>
                    {editDetectedVars.map((v) => (
                      <span key={v} className="bg-neutral-800 text-emerald-400 px-1 py-0.5 rounded text-[10px]">
                        {`{{${v}}}`}
                      </span>
                    ))}
                  </div>
                )}
              </div>
              <Textarea
                rows={9}
                value={editContent}
                onChange={(e) => setEditContent(e.target.value)}
                required
              />
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <Button variant="outline" type="button" onClick={() => setShowEditModal(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={savingEdit}>
                {savingEdit ? 'Saving...' : 'Save New Version'}
              </Button>
            </div>
          </form>
        )}
      </Modal>

      {/* Import Modal */}
      <Modal
        open={showImportModal}
        onClose={() => setShowImportModal(false)}
        title="Import Skill from skills.sh or GitHub"
      >
        <div className="space-y-4">
          {importError && <Alert variant="error">{importError}</Alert>}

          <div>
            <label className="block text-xs font-medium text-neutral-300 mb-1">
              Source URL or CLI Command
            </label>
            <div className="flex gap-2">
              <Input
                placeholder="e.g. npx skills add owner/repo --skill code-review OR skills.sh/owner/repo"
                value={importSource}
                onChange={(e) => setImportSource(e.target.value)}
                className="flex-1 font-mono text-xs"
              />
              <Button
                variant="outline"
                onClick={handlePreviewImport}
                disabled={previewLoading || !importSource.trim()}
              >
                {previewLoading ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : 'Fetch & Preview'}
              </Button>
            </div>
            <p className="text-[11px] text-neutral-500 mt-1">
              Supported formats: <code className="text-neutral-400">npx skills add ...</code>, <code className="text-neutral-400">skills.sh/owner/repo</code>, GitHub tree/blob URLs, and direct raw <code className="text-neutral-400">SKILL.md</code> URLs.
            </p>
          </div>

          <div>
            <label className="block text-xs font-medium text-neutral-300 mb-1.5">
              Default Delivery Mode
            </label>
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => setImportMode('always')}
                className={`p-2.5 rounded-lg border text-left transition-all ${
                  importMode === 'always'
                    ? 'border-emerald-500/50 bg-emerald-500/10 text-neutral-100'
                    : 'border-neutral-800 bg-neutral-900/60 text-neutral-400 hover:border-neutral-700'
                }`}
              >
                <div className="flex items-center gap-1.5 font-medium text-xs text-emerald-400 mb-0.5">
                  <Zap className="w-3.5 h-3.5" />
                  Always Injected
                </div>
              </button>

              <button
                type="button"
                onClick={() => setImportMode('on_demand')}
                className={`p-2.5 rounded-lg border text-left transition-all ${
                  importMode === 'on_demand'
                    ? 'border-purple-500/50 bg-purple-500/10 text-neutral-100'
                    : 'border-neutral-800 bg-neutral-900/60 text-neutral-400 hover:border-neutral-700'
                }`}
              >
                <div className="flex items-center gap-1.5 font-medium text-xs text-purple-400 mb-0.5">
                  <Layers className="w-3.5 h-3.5" />
                  On-Demand Tool
                </div>
              </button>
            </div>
          </div>

          {importPreview && (
            <div className="rounded-lg border border-neutral-800 bg-neutral-900/80 p-3 space-y-2">
              <div className="flex items-center justify-between">
                <span className="font-semibold text-xs text-emerald-400 flex items-center gap-1">
                  <Check className="w-3.5 h-3.5" /> Resolved Skill: {importPreview.name}
                </span>
                <Badge variant="mono" className="text-[10px] font-mono">
                  {importPreview.id}
                </Badge>
              </div>

              {importPreview.description && (
                <p className="text-xs text-neutral-300">
                  {importPreview.description}
                </p>
              )}

              <div className="text-[10px] font-mono text-neutral-500 truncate">
                Source: {importPreview.resolved_url}
              </div>

              <div className="max-h-40 overflow-y-auto rounded bg-neutral-950 p-2 border border-neutral-800 text-[11px] font-mono text-neutral-400 whitespace-pre-wrap">
                {importPreview.content.slice(0, 500)}
                {importPreview.content.length > 500 ? '...' : ''}
              </div>
            </div>
          )}

          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" type="button" onClick={() => setShowImportModal(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleSaveImport}
              disabled={savingImport || !importSource.trim()}
            >
              {savingImport ? 'Saving...' : 'Import & Save'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};
