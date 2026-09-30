import React, { useState } from 'react';
import { AdminUser, Project, Environment } from '../api/client';
import { LogOut, Layers, ChevronDown, GitBranch, User } from 'lucide-react';

interface NavbarProps {
  user: AdminUser | null;
  projects: Project[];
  currentProject: string;
  currentEnv: string;
  environments: Environment[];
  onChangeProject: (projectID: string) => void;
  onChangeEnv: (env: string) => void;
  onLogout: () => void;
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
}) => {
  const currentProjectData = projects.find((p) => p.id === currentProject);

  return (
    <header className="h-[60px] bg-neutral-950/90 backdrop-blur-md border-b border-neutral-800/80 flex items-center justify-between px-5 z-10 flex-shrink-0">
      {/* Left: Context selectors */}
      <div className="flex items-center gap-2">
        {/* Project selector */}
        <div className="flex items-center gap-1.5 bg-neutral-900 border border-neutral-800 rounded-lg px-2.5 py-1.5">
          <Layers className="w-3.5 h-3.5 text-neutral-500 flex-shrink-0" />
          <select
            value={currentProject}
            onChange={(e) => onChangeProject(e.target.value)}
            className="bg-transparent text-xs text-neutral-200 font-medium focus:outline-none cursor-pointer pr-4"
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
          </select>
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
    </header>
  );
};
