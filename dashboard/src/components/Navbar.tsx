import React, { useState } from 'react';
import { api, AdminUser, Project, Environment } from '../api/client';
import { LogOut, Layers, ChevronDown, GitBranch, User, Plus } from 'lucide-react';
import { Modal } from './ui/Modal';
import { Input } from './ui/Form';
import { Button } from './ui/Button';
import { Alert } from './ui/Feedback';

interface NavbarProps {
  user: AdminUser | null;
  projects: Project[];
  currentProject: string;
  currentEnv: string;
  environments: Environment[];
  onChangeProject: (projectID: string) => void;
  onChangeEnv: (env: string) => void;
  onLogout: () => void;
  onRefreshProjects?: () => void;
}

const envColors: Record<string, string> = {
  production: 'text-red-400 bg-red-500/10 border-red-500/20',
  prod: 'text-red-400 bg-red-500/10 border-red-500/20',
  staging: 'text-amber-400 bg-amber-500/10 border-amber-500/20',
  development: 'text-emerald-400 bg-emerald-500/10 border-emerald-500/20',
  dev: 'text-emerald-400 bg-emerald-500/10 border-emerald-500/20',
};

const getEnvStyle = (env: string) =>
  envColors[env.toLowerCase()] || 'text-blue-400 bg-blue-500/10 border-blue-500/20';

const roleStyles: Record<string, string> = {
  owner: 'text-purple-400 bg-purple-500/10 border-purple-500/20',
  editor: 'text-blue-400 bg-blue-500/10 border-blue-500/20',
  viewer: 'text-neutral-400 bg-neutral-500/10 border-neutral-500/20',
};

export const Navbar: React.FC<NavbarProps> = ({
  user,
  projects,
  currentProject,
  currentEnv,
  environments,
  onChangeProject,
  onChangeEnv,
  onLogout,
  onRefreshProjects,
}) => {
  const [showNewProjModal, setShowNewProjModal] = useState(false);
  const [newProjId, setNewProjId] = useState('');
  const [newProjName, setNewProjName] = useState('');
  const [newProjDesc, setNewProjDesc] = useState('');
  const [creatingProj, setCreatingProj] = useState(false);
  const [createProjError, setCreateProjError] = useState<string | null>(null);

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreatingProj(true);
    setCreateProjError(null);
    try {
      const cleanId = newProjId.trim().toLowerCase().replace(/[^a-z0-9_-]/g, '-');
      const created = await api.createProject({
        id: cleanId,
        name: newProjName.trim(),
        description: newProjDesc.trim(),
      });
      setShowNewProjModal(false);
      setNewProjId('');
      setNewProjName('');
      setNewProjDesc('');
      if (onRefreshProjects) {
        await onRefreshProjects();
      }
      onChangeProject(created.id);
    } catch (err: any) {
      setCreateProjError(err.message || 'Failed to create project');
    } finally {
      setCreatingProj(false);
    }
  };

  const handleSelectProject = (val: string) => {
    if (val === '__new__') {
      setShowNewProjModal(true);
      return;
    }
    onChangeProject(val);
  };

  return (
    <header className="h-[60px] bg-neutral-950/90 backdrop-blur-md border-b border-neutral-800/80 flex items-center justify-between px-5 z-10 flex-shrink-0">
      {/* Left: Context selectors */}
      <div className="flex items-center gap-2">
        {/* Project selector */}
        <div className="flex items-center gap-1.5 bg-neutral-900 border border-neutral-800 rounded-lg pl-2.5 pr-1.5 py-1.5">
          <Layers className="w-3.5 h-3.5 text-neutral-500 flex-shrink-0" />
          <select
            value={currentProject}
            onChange={(e) => handleSelectProject(e.target.value)}
            className="bg-transparent text-xs text-neutral-200 font-medium focus:outline-none cursor-pointer pr-2"
          >
            {projects.length === 0 ? (
              <option value="default" className="bg-neutral-900">default</option>
            ) : (
              projects.map((p) => (
                <option key={p.id} value={p.id} className="bg-neutral-900">
                  {p.name}
                </option>
              ))
            )}
            <option disabled className="bg-neutral-900 text-neutral-600">──────────</option>
            <option value="__new__" className="bg-neutral-900 text-emerald-400 font-medium">
              + New Project...
            </option>
          </select>
          <button
            type="button"
            onClick={() => setShowNewProjModal(true)}
            className="p-1 hover:bg-neutral-800 text-neutral-400 hover:text-emerald-400 rounded transition-colors"
            title="Create New Project"
          >
            <Plus className="w-3.5 h-3.5" />
          </button>
        </div>

        <ChevronDown className="w-3.5 h-3.5 text-neutral-700 rotate-[-90deg]" />

        {/* Environment selector */}
        <div className={`flex items-center gap-1.5 border rounded-lg px-2.5 py-1.5 ${getEnvStyle(currentEnv)}`}>
          <GitBranch className="w-3.5 h-3.5 flex-shrink-0" />
          <select
            value={currentEnv}
            onChange={(e) => onChangeEnv(e.target.value)}
            className="bg-transparent text-xs font-semibold focus:outline-none cursor-pointer pr-4"
            style={{ color: 'inherit' }}
          >
            {environments.length === 0 ? (
              <>
                <option value="development" className="bg-neutral-900 text-neutral-200">development</option>
                <option value="staging" className="bg-neutral-900 text-neutral-200">staging</option>
                <option value="production" className="bg-neutral-900 text-neutral-200">production</option>
              </>
            ) : (
              environments.map((e) => (
                <option key={e.name} value={e.name} className="bg-neutral-900 text-neutral-200">
                  {e.name}
                </option>
              ))
            )}
          </select>
        </div>
      </div>

      {/* Right: User area */}
      {user && (
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <div className="w-6 h-6 rounded-full bg-gradient-to-br from-emerald-500 to-teal-600 flex items-center justify-center">
              <User className="w-3.5 h-3.5 text-white" />
            </div>
            <div className="hidden sm:block">
              <p className="text-xs font-medium text-neutral-200">{user.email}</p>
            </div>
            <span className={`text-[10px] font-semibold px-1.5 py-0.5 rounded border ${roleStyles[user.role] || roleStyles.viewer}`}>
              {user.role}
            </span>
          </div>

          <div className="w-px h-5 bg-neutral-800" />

          <button
            onClick={onLogout}
            title="Sign out"
            className="p-1.5 text-neutral-500 hover:text-red-400 hover:bg-neutral-800 rounded-lg transition-colors"
          >
            <LogOut className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* Quick New Project Modal */}
      <Modal
        open={showNewProjModal}
        onClose={() => {
          setShowNewProjModal(false);
          setCreateProjError(null);
        }}
        title="Create New Project"
        description="A project is an isolated tenant with its own AI apps, auth config, and environments."
        size="sm"
      >
        {createProjError && (
          <Alert variant="error" className="mb-4" onDismiss={() => setCreateProjError(null)}>
            {createProjError}
          </Alert>
        )}
        <form onSubmit={handleCreateProject} className="space-y-4">
          <Input
            label="Project Name"
            required
            placeholder="e.g. Mobile App"
            value={newProjName}
            onChange={(e) => {
              setNewProjName(e.target.value);
              if (!newProjId || newProjId === newProjName.toLowerCase().replace(/[^a-z0-9_-]/g, '-').slice(0, -1)) {
                setNewProjId(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, '-'));
              }
            }}
          />
          <Input
            label="Project ID"
            required
            placeholder="e.g. mobile-app"
            value={newProjId}
            onChange={(e) => setNewProjId(e.target.value)}
            className="font-mono text-xs"
            hint="Unique identifier used in API headers (X-Project-ID)"
          />
          <Input
            label="Description (Optional)"
            placeholder="e.g. AI backend for iOS and Android"
            value={newProjDesc}
            onChange={(e) => setNewProjDesc(e.target.value)}
          />
          <div className="flex justify-end gap-2 pt-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setShowNewProjModal(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              loading={creatingProj}
              icon={<Plus className="w-3.5 h-3.5" />}
            >
              Create Project
            </Button>
          </div>
        </form>
      </Modal>
    </header>
  );
};
