import React from 'react';
import { AdminUser, Project, Environment } from '../api/client';
import { LogOut, ShieldAlert, Layers } from 'lucide-react';

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
  const getRoleBadge = (role?: string) => {
    switch (role) {
      case 'owner':
        return 'bg-purple-500/20 text-purple-300 border-purple-500/30';
      case 'editor':
        return 'bg-blue-500/20 text-blue-300 border-blue-500/30';
      default:
        return 'bg-slate-500/20 text-slate-300 border-slate-500/30';
    }
  };

  return (
    <header className="h-16 bg-slate-900/80 backdrop-blur border-b border-slate-800 flex items-center justify-between px-6 z-10">
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-2 bg-slate-800/80 border border-slate-700/60 rounded-lg px-3 py-1.5 text-xs text-slate-300">
          <Layers className="w-4 h-4 text-emerald-400" />
          <span className="font-semibold text-slate-400">Project:</span>
          <select
            value={currentProject}
            onChange={(e) => onChangeProject(e.target.value)}
            className="bg-transparent text-slate-100 font-medium focus:outline-none cursor-pointer"
          >
            {projects.length === 0 ? (
              <option value="default" className="bg-slate-900">default</option>
            ) : (
              projects.map((p) => (
                <option key={p.id} value={p.id} className="bg-slate-900">
                  {p.name} ({p.id})
                </option>
              ))
            )}
          </select>
        </div>

        <div className="flex items-center gap-2 bg-slate-800/80 border border-slate-700/60 rounded-lg px-3 py-1.5 text-xs text-slate-300">
          <span className="font-semibold text-slate-400">Env:</span>
          <select
            value={currentEnv}
            onChange={(e) => onChangeEnv(e.target.value)}
            className="bg-transparent text-emerald-400 font-semibold focus:outline-none cursor-pointer"
          >
            {environments.length === 0 ? (
              <>
                <option value="development" className="bg-slate-900">development</option>
                <option value="staging" className="bg-slate-900">staging</option>
                <option value="production" className="bg-slate-900">production</option>
              </>
            ) : (
              environments.map((e) => (
                <option key={e.name} value={e.name} className="bg-slate-900">
                  {e.name}
                </option>
              ))
            )}
          </select>
        </div>
      </div>

      <div className="flex items-center gap-4">
        {user && (
          <div className="flex items-center gap-3">
            <div className="flex flex-col items-end">
              <span className="text-xs font-medium text-slate-200">{user.email}</span>
              <span
                className={`text-[10px] uppercase font-bold px-1.5 py-0.5 rounded border ${getRoleBadge(
                  user.role
                )}`}
              >
                {user.role}
              </span>
            </div>
            <button
              onClick={onLogout}
              title="Logout"
              className="p-2 text-slate-400 hover:text-rose-400 hover:bg-slate-800 rounded-lg transition-colors"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        )}
      </div>
    </header>
  );
};
