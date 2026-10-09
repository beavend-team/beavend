import React, { useEffect, useState } from 'react';
import {
  api,
  AdminUser,
  Project,
  Environment,
  DiffReport,
} from '../api/client';
import {
  Settings,
  Users,
  Plus,
  Trash2,
  Layers,
  KeyRound,
  ShieldAlert,
  Save,
  Search,
  FileCode,
  Download,
  Upload,
  Copy,
  Check,
  RefreshCw,
  Shield,
  GitBranch,
} from 'lucide-react';
import { Card, CardHeader } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Alert } from '../components/ui/Feedback';
import { Input, Textarea, Select, Toggle } from '../components/ui/Form';

interface SettingsPageProps {
  currentUser: AdminUser | null;
  projects: Project[];
  currentProject: string;
  currentEnv: string;
  onRefreshProjects: () => void;
  onNavigateTab?: (tab: any) => void;
}

type SettingsTab = 'projects' | 'configcode' | 'users' | 'compliance';

const TabBtn: React.FC<{
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
}> = ({ active, onClick, icon, label }) => (
  <button
    onClick={onClick}
    className={`
      flex items-center gap-1.5 px-3 py-2 text-xs font-medium transition-all border-b-2 -mb-px
      ${active ? 'border-emerald-500 text-emerald-400' : 'border-transparent text-neutral-500 hover:text-neutral-300'}
    `}
  >
    <span className="flex-shrink-0">{icon}</span>
    {label}
  </button>
);

export const SettingsPage: React.FC<SettingsPageProps> = ({
  currentUser,
  projects,
  currentProject,
  currentEnv,
  onRefreshProjects,
  onNavigateTab,
}) => {
  const [activeTab, setActiveTab] = useState<SettingsTab>('projects');
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [loadingUsers, setLoadingUsers] = useState(false);
  const [environments, setEnvironments] = useState<Environment[]>([]);

  // Load environments for the current project
  useEffect(() => {
    if (currentProject) {
      api.listEnvironments(currentProject).then(setEnvironments).catch(() => setEnvironments([]));
    }
  }, [currentProject]);

  // Projects
  const [newProjId, setNewProjId] = useState('');
  const [newProjName, setNewProjName] = useState('');
  const [newProjDesc, setNewProjDesc] = useState('');
  const [newEnvName, setNewEnvName] = useState('');

  // Users
  const [newUserEmail, setNewUserEmail] = useState('');
  const [newUserPassword, setNewUserPassword] = useState('');
  const [newUserRole, setNewUserRole] = useState<'owner' | 'editor' | 'viewer'>('editor');

  // Compliance
  const [purgeIssuer, setPurgeIssuer] = useState('https://securetoken.google.com/my-firebase-app');
  const [purgeSubject, setPurgeSubject] = useState('');
  const [purgeSuccess, setPurgeSuccess] = useState<string | null>(null);

  // Config as Code
  const [exportedYAML, setExportedYAML] = useState('');
  const [loadingExport, setLoadingExport] = useState(false);
  const [importYAML, setImportYAML] = useState('');
  const [importing, setImporting] = useState(false);
  const [diffReport, setDiffReport] = useState<DiffReport | null>(null);
  const [importSuccess, setImportSuccess] = useState<string | null>(null);
  const [importError, setImportError] = useState<string | null>(null);
  const [copiedYAML, setCopiedYAML] = useState(false);

  const isOwner = currentUser?.role === 'owner';

  const loadUsers = async () => {
    if (!isOwner) return;
    setLoadingUsers(true);
    try {
      setUsers(await api.listUsers().catch(() => []));
    } finally {
      setLoadingUsers(false);
    }
  };

  useEffect(() => {
    loadUsers();
  }, [currentUser]);

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.createProject({ id: newProjId, name: newProjName, description: newProjDesc });
      setNewProjId(''); setNewProjName(''); setNewProjDesc('');
      onRefreshProjects();
    } catch (err: any) {
      alert(`Error: ${err.message}`);
    }
  };

  const handleCreateEnv = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.createEnvironment(currentProject, { name: newEnvName, description: `${newEnvName} environment` });
      setNewEnvName('');
      onRefreshProjects();
    } catch (err: any) {
      alert(`Error: ${err.message}`);
    }
  };

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.createUser({ email: newUserEmail, password: newUserPassword, role: newUserRole });
      setNewUserEmail(''); setNewUserPassword('');
      loadUsers();
    } catch (err: any) {
      alert(`Error: ${err.message}`);
    }
  };

  const handleDeleteUser = async (id: string) => {
    if (!confirm(`Delete admin user ${id}?`)) return;
    try { await api.deleteUser(id); loadUsers(); } catch (err: any) { alert(err.message); }
  };

  const handlePrivacyPurge = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!confirm(`Permanently purge all data for subject "${purgeSubject}"? This cannot be undone.`)) return;
    try {
      await api.privacyPurge(purgeIssuer, purgeSubject);
      setPurgeSuccess(`Successfully purged all records for subject: ${purgeSubject}`);
      setPurgeSubject('');
    } catch (err: any) {
      alert(`Privacy purge failed: ${err.message}`);
    }
  };

  const handleExport = async () => {
    setLoadingExport(true);
    try {
      setExportedYAML(await api.exportConfig(currentProject, currentEnv));
    } catch (err: any) {
      setImportError(err.message || 'Export failed');
    } finally {
      setLoadingExport(false);
    }
  };

  const handleDownloadYAML = () => {
    if (!exportedYAML) return;
    const blob = new Blob([exportedYAML], { type: 'application/x-yaml' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `${currentProject}-${currentEnv}.yaml`; a.click();
    URL.revokeObjectURL(url);
  };

  const handleCopyYAML = async () => {
    if (!exportedYAML) return;
    await navigator.clipboard.writeText(exportedYAML);
    setCopiedYAML(true);
    setTimeout(() => setCopiedYAML(false), 2000);
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (event) => {
      setImportYAML(event.target?.result as string);
      setDiffReport(null); setImportSuccess(null); setImportError(null);
    };
    reader.readAsText(file);
  };

  const handlePreviewDiff = async () => {
    if (!importYAML.trim()) { setImportError('Provide YAML to preview'); return; }
    setImporting(true); setImportError(null); setImportSuccess(null);
    try { setDiffReport(await api.importConfig(currentProject, currentEnv, importYAML, true)); }
    catch (err: any) { setImportError(err.message); }
    finally { setImporting(false); }
  };

  const handleApplyImport = async () => {
    if (!importYAML.trim()) return;
    if (!confirm(`Apply configuration to ${currentProject}/${currentEnv}?`)) return;
    setImporting(true); setImportError(null); setImportSuccess(null);
    try {
      const report = await api.importConfig(currentProject, currentEnv, importYAML, false);
      setDiffReport(report);
      setImportSuccess(`Applied! Created: ${report.summary.total_created}, Updated: ${report.summary.total_updated}, Unchanged: ${report.summary.total_unchanged}.`);
      onRefreshProjects();
    } catch (err: any) { setImportError(err.message); }
    finally { setImporting(false); }
  };

  return (
    <div className="space-y-5 max-w-[1000px]">
      {/* Page header */}
      <div>
        <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
          <Settings className="w-5 h-5 text-neutral-400" />
          Settings
        </h1>
        <p className="text-sm text-neutral-500 mt-0.5">
          Manage projects, environments, admin users, declarative config, and data compliance.
        </p>
      </div>

      {/* Tab bar */}
      <div className="flex items-center border-b border-neutral-800 gap-1">
        <TabBtn active={activeTab === 'projects'} onClick={() => setActiveTab('projects')} icon={<Layers className="w-3.5 h-3.5" />} label="Projects & Envs" />
        <TabBtn active={activeTab === 'configcode'} onClick={() => setActiveTab('configcode')} icon={<FileCode className="w-3.5 h-3.5" />} label="Config as Code" />
        {isOwner && (
          <TabBtn active={activeTab === 'users'} onClick={() => setActiveTab('users')} icon={<Users className="w-3.5 h-3.5" />} label="Admin Users" />
        )}
        <TabBtn active={activeTab === 'compliance'} onClick={() => setActiveTab('compliance')} icon={<ShieldAlert className="w-3.5 h-3.5" />} label="Privacy & GDPR" />
      </div>

      {/* ─── TAB: Projects & Envs ─── */}
      {activeTab === 'projects' && (
        <div className="space-y-4">
          <Card padding="lg">
            <CardHeader title="Projects" icon={<Layers className="w-4 h-4 text-blue-400" />} />

            {isOwner && (
              <form onSubmit={handleCreateProject} className="flex gap-2 mb-5">
                <input type="text" required placeholder="Project ID" value={newProjId} onChange={(e) => setNewProjId(e.target.value)} className="flex-1 bg-neutral-950 border border-neutral-800 rounded-lg px-3 py-2 text-xs font-mono text-neutral-100 focus:outline-none focus:border-neutral-600" />
                <input type="text" required placeholder="Project Name" value={newProjName} onChange={(e) => setNewProjName(e.target.value)} className="flex-1 bg-neutral-950 border border-neutral-800 rounded-lg px-3 py-2 text-xs text-neutral-100 focus:outline-none focus:border-neutral-600" />
                <input type="text" placeholder="Description" value={newProjDesc} onChange={(e) => setNewProjDesc(e.target.value)} className="flex-1 bg-neutral-950 border border-neutral-800 rounded-lg px-3 py-2 text-xs text-neutral-100 focus:outline-none focus:border-neutral-600" />
                <Button type="submit" variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />}>Add Project</Button>
              </form>
            )}

            <div className="space-y-1">
              {projects.map((p) => (
                <div key={p.id} className="flex items-center justify-between py-2.5 px-3 bg-neutral-950 border border-neutral-800 rounded-lg group hover:border-neutral-700 transition-colors">
                  <div>
                    <span className="text-sm font-semibold text-neutral-200">{p.name}</span>
                    <span className="text-[11px] font-mono text-neutral-600 ml-2">{p.id}</span>
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="text-[11px] text-neutral-700">{new Date(p.created_at).toLocaleDateString()}</span>
                    {isOwner && (
                      <button
                        onClick={async () => {
                          if (!confirm(`Delete project "${p.name}" (${p.id})? This will remove ALL resources in the project. This cannot be undone.`)) return;
                          try { await api.deleteProject(p.id); onRefreshProjects(); }
                          catch (err: any) { alert(`Failed: ${err.message}`); }
                        }}
                        className="p-1 text-neutral-700 hover:text-red-400 hover:bg-neutral-800 rounded opacity-0 group-hover:opacity-100 transition-all"
                        title="Delete project"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </Card>

          <Card padding="lg">
            <CardHeader title={`Environments in "${currentProject}"`} icon={<GitBranch className="w-4 h-4 text-emerald-400" />} />
            {isOwner && (
              <form onSubmit={handleCreateEnv} className="flex gap-2 mb-4">
                <input type="text" required placeholder="New environment name (e.g. staging)" value={newEnvName} onChange={(e) => setNewEnvName(e.target.value)} className="flex-1 bg-neutral-950 border border-neutral-800 rounded-lg px-3 py-2 text-xs font-mono text-neutral-100 focus:outline-none focus:border-neutral-600" />
                <Button type="submit" variant="secondary" size="sm" icon={<Plus className="w-3.5 h-3.5" />}>Add Environment</Button>
              </form>
            )}
            <p className="text-xs text-neutral-600 italic mb-3">Switch environments using the selector in the top navigation bar.</p>
            <div className="space-y-1">
              {environments.map((env) => (
                <div key={env.name} className="flex items-center justify-between py-2 px-3 bg-neutral-950 border border-neutral-800 rounded-lg group hover:border-neutral-700 transition-colors">
                  <span className="text-xs font-mono text-neutral-400">{env.name}</span>
                  {isOwner && (
                    <button
                      onClick={async () => {
                        if (!confirm(`Delete environment "${env.name}"? This removes all its configuration.`)) return;
                        try { await api.deleteEnvironment(currentProject, env.name); onRefreshProjects(); }
                        catch (err: any) { alert(`Failed: ${err.message}`); }
                      }}
                      className="p-1 text-neutral-700 hover:text-red-400 hover:bg-neutral-800 rounded opacity-0 group-hover:opacity-100 transition-all"
                      title="Delete environment"
                    >
                      <Trash2 className="w-3 h-3" />
                    </button>
                  )}
                </div>
              ))}
            </div>
          </Card>
        </div>
      )}

      {/* ─── TAB: Config as Code ─── */}
      {activeTab === 'configcode' && (
        <div className="space-y-4">
          {importSuccess && <Alert variant="success" onDismiss={() => setImportSuccess(null)}>{importSuccess}</Alert>}
          {importError && <Alert variant="error" onDismiss={() => setImportError(null)}>{importError}</Alert>}

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            {/* Export */}
            <Card padding="lg">
              <div className="flex items-center justify-between mb-4">
                <div className="flex items-center gap-2">
                  <Download className="w-4 h-4 text-emerald-400" />
                  <h3 className="text-sm font-semibold text-neutral-200">Export Configuration</h3>
                </div>
                <Button variant="primary" size="xs" loading={loadingExport} icon={<Download className="w-3.5 h-3.5" />} onClick={handleExport}>
                  Generate YAML
                </Button>
              </div>

              <p className="text-xs text-neutral-500 mb-3">
                Exports all AI Apps, Prompts, Models, Tools, Policies, and Auth settings as declarative YAML.
                Secrets are safely masked.
              </p>

              {exportedYAML && (
                <div className="relative">
                  <pre className="bg-neutral-950 border border-neutral-800 rounded-xl p-3 text-[11px] font-mono text-neutral-400 max-h-80 overflow-y-auto leading-relaxed">
                    {exportedYAML}
                  </pre>
                  <div className="absolute top-2 right-2 flex gap-1">
                    <button onClick={handleCopyYAML} className="px-2 py-1 bg-neutral-800/80 hover:bg-neutral-700 text-neutral-200 rounded text-[10px] flex items-center gap-1 backdrop-blur-sm transition-colors">
                      {copiedYAML ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                      {copiedYAML ? 'Copied' : 'Copy'}
                    </button>
                    <button onClick={handleDownloadYAML} className="px-2 py-1 bg-emerald-600/80 hover:bg-emerald-500 text-white rounded text-[10px] flex items-center gap-1 backdrop-blur-sm transition-colors">
                      <Download className="w-3 h-3" />
                      .yaml
                    </button>
                  </div>
                </div>
              )}
            </Card>

            {/* Import */}
            <Card padding="lg">
              <div className="flex items-center justify-between mb-4">
                <div className="flex items-center gap-2">
                  <Upload className="w-4 h-4 text-blue-400" />
                  <h3 className="text-sm font-semibold text-neutral-200">Import & Reconcile</h3>
                </div>
                <label className="cursor-pointer">
                  <Button variant="outline" size="xs" icon={<Upload className="w-3.5 h-3.5" />}>
                    Upload .yaml
                  </Button>
                  <input type="file" accept=".yaml,.yml,.json" onChange={handleFileUpload} className="hidden" />
                </label>
              </div>

              <textarea
                value={importYAML}
                onChange={(e) => setImportYAML(e.target.value)}
                placeholder="Paste YAML configuration document here..."
                rows={9}
                className="w-full bg-neutral-950 border border-neutral-800 rounded-xl p-3 text-xs font-mono text-neutral-100 focus:outline-none focus:border-neutral-600 transition-colors resize-none"
              />

              <div className="flex gap-2 mt-3">
                <Button variant="secondary" size="sm" disabled={importing || !importYAML.trim()} onClick={handlePreviewDiff} icon={<Search className="w-3.5 h-3.5" />}>
                  Preview Diff
                </Button>
                <Button variant="primary" size="sm" disabled={importing || !importYAML.trim()} loading={importing} onClick={handleApplyImport} icon={<Save className="w-3.5 h-3.5" />}>
                  Apply Config
                </Button>
              </div>

              {diffReport && (
                <div className="mt-4 bg-neutral-950 border border-neutral-800 rounded-xl p-3">
                  <div className="flex items-center justify-between mb-2">
                    <p className="text-xs font-semibold text-neutral-300">Diff Plan</p>
                    <div className="flex gap-1.5 text-[10px]">
                      <Badge variant="success">{diffReport.summary.total_created} create</Badge>
                      <Badge variant="warning">{diffReport.summary.total_updated} update</Badge>
                      <Badge variant="default">{diffReport.summary.total_unchanged} unchanged</Badge>
                    </div>
                  </div>
                  <div className="divide-y divide-neutral-800/60 max-h-48 overflow-y-auto">
                    {diffReport.resources.map((res, idx) => (
                      <div key={idx} className="py-2 flex items-center justify-between text-xs">
                        <span className="font-mono text-neutral-400">
                          {res.type}: <strong className="text-neutral-200">{res.id}</strong>
                        </span>
                        <Badge variant={res.action === 'create' ? 'success' : res.action === 'update' ? 'warning' : 'default'}>
                          {res.action}
                        </Badge>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </Card>
          </div>
        </div>
      )}

      {/* ─── TAB: Admin Users ─── */}
      {activeTab === 'users' && isOwner && (
        <Card padding="lg">
          <CardHeader title="Admin Users & RBAC" icon={<Users className="w-4 h-4 text-purple-400" />} description="Manage who has access to this Beavend control plane" />

          <form onSubmit={handleCreateUser} className="flex gap-2 mb-5">
            <input type="email" required placeholder="Admin email" value={newUserEmail} onChange={(e) => setNewUserEmail(e.target.value)} className="flex-1 bg-neutral-950 border border-neutral-800 rounded-lg px-3 py-2 text-xs text-neutral-100 focus:outline-none focus:border-neutral-600" />
            <input type="password" required placeholder="Password" value={newUserPassword} onChange={(e) => setNewUserPassword(e.target.value)} className="w-36 bg-neutral-950 border border-neutral-800 rounded-lg px-3 py-2 text-xs text-neutral-100 focus:outline-none focus:border-neutral-600" />
            <select value={newUserRole} onChange={(e) => setNewUserRole(e.target.value as any)} className="w-36 bg-neutral-950 border border-neutral-800 rounded-lg px-3 py-2 text-xs text-neutral-100 focus:outline-none focus:border-neutral-600">
              <option value="viewer">Viewer</option>
              <option value="editor">Editor</option>
              <option value="owner">Owner</option>
            </select>
            <Button type="submit" variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />}>Add User</Button>
          </form>

          <div className="space-y-1">
            {loadingUsers ? (
              <p className="text-xs text-neutral-600 py-4 text-center">Loading users...</p>
            ) : users.length === 0 ? (
              <p className="text-xs text-neutral-600 py-4 text-center italic">No admin users found.</p>
            ) : (
              users.map((u) => (
                <div key={u.id} className="flex items-center justify-between py-2.5 px-3 bg-neutral-950 border border-neutral-800 rounded-lg group">
                  <div className="flex items-center gap-3">
                    <div className="w-7 h-7 rounded-full bg-gradient-to-br from-emerald-500 to-teal-600 flex items-center justify-center text-white text-[10px] font-bold">
                      {u.email[0].toUpperCase()}
                    </div>
                    <div>
                      <p className="text-xs font-medium text-neutral-200">{u.email}</p>
                      {u.id === currentUser?.id && <span className="text-[10px] text-neutral-600">(you)</span>}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant={u.role === 'owner' ? 'purple' : u.role === 'editor' ? 'info' : 'default'}>
                      {u.role}
                    </Badge>
                    {u.id !== currentUser?.id && (
                      <button
                        onClick={() => handleDeleteUser(u.id)}
                        className="p-1 text-neutral-600 hover:text-red-400 rounded transition-colors opacity-0 group-hover:opacity-100"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    )}
                  </div>
                </div>
              ))
            )}
          </div>
        </Card>
      )}

      {/* ─── TAB: Privacy & GDPR ─── */}
      {activeTab === 'compliance' && (
        <Card padding="lg">
          <CardHeader
            title="GDPR Privacy Purge"
            description="Permanently purge all data for an authenticated end-user subject"
            icon={<ShieldAlert className="w-4 h-4 text-red-400" />}
          />

          <div className="bg-red-500/5 border border-red-500/10 rounded-xl p-3 mb-5">
            <p className="text-xs text-red-300">
              ⚠️ This operation permanently deletes all cached responses, tool execution logs, and usage records
              for the specified user subject across all environments. This cannot be undone.
            </p>
          </div>

          {purgeSuccess && <Alert variant="success" className="mb-4" onDismiss={() => setPurgeSuccess(null)}>{purgeSuccess}</Alert>}

          <form onSubmit={handlePrivacyPurge} className="space-y-4">
            <Input
              label="Identity Issuer URL"
              required
              value={purgeIssuer}
              onChange={(e) => setPurgeIssuer(e.target.value)}
              className="font-mono"
            />
            <Input
              label="Subject Identifier (sub)"
              required
              placeholder="e.g. usr_9981247"
              value={purgeSubject}
              onChange={(e) => setPurgeSubject(e.target.value)}
              className="font-mono"
            />
            <div className="flex justify-end">
              <Button
                type="submit"
                variant="danger"
                size="sm"
                icon={<ShieldAlert className="w-3.5 h-3.5" />}
              >
                Execute Privacy Purge
              </Button>
            </div>
          </form>
        </Card>
      )}
    </div>
  );
};
