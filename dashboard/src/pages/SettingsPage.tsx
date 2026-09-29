import React, { useEffect, useState } from 'react';
import { api, AdminUser, Project, Environment } from '../api/client';
import { Settings, Users, Plus, Trash2, ShieldAlert, Layers, ShieldCheck, UserPlus } from 'lucide-react';

interface SettingsPageProps {
  currentUser: AdminUser | null;
  projects: Project[];
  currentProject: string;
  onRefreshProjects: () => void;
}

export const SettingsPage: React.FC<SettingsPageProps> = ({
  currentUser,
  projects,
  currentProject,
  onRefreshProjects,
}) => {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [loadingUsers, setLoadingUsers] = useState(false);

  // New Project State
  const [newProjId, setNewProjId] = useState('');
  const [newProjName, setNewProjName] = useState('');
  const [newProjDesc, setNewProjDesc] = useState('');

  // New Environment State
  const [newEnvName, setNewEnvName] = useState('');

  // New User State
  const [newUserEmail, setNewUserEmail] = useState('');
  const [newUserPassword, setNewUserPassword] = useState('');
  const [newUserRole, setNewUserRole] = useState<'owner' | 'editor' | 'viewer'>('editor');

  // Privacy Purge State
  const [purgeIssuer, setPurgeIssuer] = useState('https://securetoken.google.com/my-firebase-app');
  const [purgeSubject, setPurgeSubject] = useState('');
  const [purgeSuccess, setPurgeSuccess] = useState<string | null>(null);

  const isOwner = currentUser?.role === 'owner';

  const loadUsers = async () => {
    if (!isOwner) return;
    setLoadingUsers(true);
    try {
      const data = await api.listUsers().catch(() => []);
      setUsers(data);
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
      await api.createProject({
        id: newProjId,
        name: newProjName,
        description: newProjDesc,
      });
      setNewProjId('');
      setNewProjName('');
      setNewProjDesc('');
      onRefreshProjects();
      alert('Project created successfully!');
    } catch (err: any) {
      alert(`Error creating project: ${err.message}`);
    }
  };

  const handleCreateEnv = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.createEnvironment(currentProject, {
        name: newEnvName,
        description: `${newEnvName} environment`,
      });
      setNewEnvName('');
      onRefreshProjects();
      alert('Environment created successfully!');
    } catch (err: any) {
      alert(`Error creating environment: ${err.message}`);
    }
  };

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.createUser({
        email: newUserEmail,
        password: newUserPassword,
        role: newUserRole,
      });
      setNewUserEmail('');
      setNewUserPassword('');
      loadUsers();
      alert('Admin user created successfully!');
    } catch (err: any) {
      alert(`Error creating user: ${err.message}`);
    }
  };

  const handleDeleteUser = async (id: string) => {
    if (!confirm(`Delete admin user ${id}?`)) return;
    try {
      await api.deleteUser(id);
      loadUsers();
    } catch (err: any) {
      alert(`Error deleting user: ${err.message}`);
    }
  };

  const handlePrivacyPurge = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!confirm(`Permanently purge all logs and data for subject "${purgeSubject}"? This cannot be undone.`)) return;
    try {
      await api.privacyPurge(purgeIssuer, purgeSubject);
      setPurgeSuccess(`Successfully purged all records for subject: ${purgeSubject}`);
      setPurgeSubject('');
    } catch (err: any) {
      alert(`Privacy purge failed: ${err.message}`);
    }
  };

  return (
    <div className="space-y-8 max-w-5xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-slate-100 flex items-center gap-2">
          <Settings className="w-6 h-6 text-emerald-400" />
          Settings & Management
        </h1>
        <p className="text-sm text-slate-400 mt-1">
          Tenant projects, environments, administrative accounts, and compliance operations.
        </p>
      </div>

      {/* Projects Section */}
      <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
        <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
          <Layers className="w-5 h-5 text-blue-400" />
          Projects & Environments
        </h2>

        {isOwner && (
          <form onSubmit={handleCreateProject} className="grid grid-cols-1 md:grid-cols-4 gap-3 pt-2">
            <input
              type="text"
              required
              placeholder="Project ID (e.g. proj_prod)"
              value={newProjId}
              onChange={(e) => setNewProjId(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 font-mono focus:outline-none focus:border-emerald-500"
            />
            <input
              type="text"
              required
              placeholder="Project Name"
              value={newProjName}
              onChange={(e) => setNewProjName(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
            />
            <input
              type="text"
              placeholder="Description"
              value={newProjDesc}
              onChange={(e) => setNewProjDesc(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
            />
            <button
              type="submit"
              className="bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-xs font-medium py-2 px-4 transition-colors"
            >
              Add Project
            </button>
          </form>
        )}

        <div className="divide-y divide-slate-800/80 pt-2">
          {projects.map((p) => (
            <div key={p.id} className="py-2.5 flex items-center justify-between text-xs font-mono">
              <span className="font-semibold text-slate-200">{p.name} ({p.id})</span>
              <span className="text-slate-500">{new Date(p.created_at).toLocaleDateString()}</span>
            </div>
          ))}
        </div>
      </div>

      {/* Admin Users Management (Owner Only) */}
      {isOwner && (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
          <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
            <Users className="w-5 h-5 text-purple-400" />
            Admin Users & RBAC
          </h2>

          <form onSubmit={handleCreateUser} className="grid grid-cols-1 md:grid-cols-4 gap-3 pt-2">
            <input
              type="email"
              required
              placeholder="Admin Email"
              value={newUserEmail}
              onChange={(e) => setNewUserEmail(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
            />
            <input
              type="password"
              required
              placeholder="Password (min 8 chars)"
              value={newUserPassword}
              onChange={(e) => setNewUserPassword(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
            />
            <select
              value={newUserRole}
              onChange={(e) => setNewUserRole(e.target.value as any)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
            >
              <option value="viewer">Viewer (Read-only)</option>
              <option value="editor">Editor (Config write)</option>
              <option value="owner">Owner (Full admin)</option>
            </select>
            <button
              type="submit"
              className="bg-purple-600 hover:bg-purple-500 text-white rounded-xl text-xs font-medium py-2 px-4 transition-colors"
            >
              Create Admin
            </button>
          </form>

          <div className="divide-y divide-slate-800/80 pt-2">
            {users.map((u) => (
              <div key={u.id} className="py-2.5 flex items-center justify-between text-xs">
                <div>
                  <span className="font-semibold text-slate-200 mr-2">{u.email}</span>
                  <span className="font-mono text-[10px] uppercase font-bold px-1.5 py-0.5 rounded bg-slate-800 text-slate-400 border border-slate-700">
                    {u.role}
                  </span>
                </div>
                {u.id !== currentUser?.id && (
                  <button
                    onClick={() => handleDeleteUser(u.id)}
                    className="p-1 text-slate-500 hover:text-rose-400"
                    title="Delete User"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* GDPR / Privacy Purge (Owner Only) */}
      {isOwner && (
        <div className="bg-slate-900 border border-rose-500/20 rounded-2xl p-6 space-y-4">
          <h2 className="text-base font-semibold text-rose-400 flex items-center gap-2">
            <ShieldAlert className="w-5 h-5 text-rose-400" />
            GDPR / CCPA Privacy Purge
          </h2>
          <p className="text-xs text-slate-400">
            Right-to-be-forgotten compliance operation: permanently purges all operational request logs, usage accounting events, and aggregated rollups for a subject identifier across the active tenant.
          </p>

          {purgeSuccess && (
            <div className="p-3 bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 rounded-xl text-xs">
              {purgeSuccess}
            </div>
          )}

          <form onSubmit={handlePrivacyPurge} className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <input
              type="text"
              required
              placeholder="Issuer (e.g. Firebase / Supabase URL)"
              value={purgeIssuer}
              onChange={(e) => setPurgeIssuer(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 focus:outline-none focus:border-rose-500"
            />
            <input
              type="text"
              required
              placeholder="Subject (sub claim / user UUID)"
              value={purgeSubject}
              onChange={(e) => setPurgeSubject(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 focus:outline-none focus:border-rose-500"
            />
            <button
              type="submit"
              className="bg-rose-600 hover:bg-rose-500 text-white rounded-xl text-xs font-medium py-2 px-4 transition-colors"
            >
              Purge Subject Data
            </button>
          </form>
        </div>
      )}
    </div>
  );
};
