import React, { useEffect, useState } from 'react';
import { api, AdminUser, Project, Environment } from './api/client';
import { Sidebar, NavTab } from './components/Sidebar';
import { Navbar } from './components/Navbar';
import { LoginPage } from './pages/LoginPage';
import { OverviewPage } from './pages/OverviewPage';
import { AppsPage } from './pages/AppsPage';
import { PromptsPage } from './pages/PromptsPage';
import { ModelsPage } from './pages/ModelsPage';
import { ToolsPage } from './pages/ToolsPage';
import { PoliciesPage } from './pages/PoliciesPage';
import { LogsPage } from './pages/LogsPage';
import { AuditPage } from './pages/AuditPage';
import { SettingsPage } from './pages/SettingsPage';

export const App: React.FC = () => {
  const [currentUser, setCurrentUser] = useState<AdminUser | null>(null);
  const [currentTab, setCurrentTab] = useState<NavTab>('overview');
  const [loading, setLoading] = useState(true);

  // Projects & Environments Context
  const [projects, setProjects] = useState<Project[]>([]);
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [currentProject, setCurrentProject] = useState('default');
  const [currentEnv, setCurrentEnv] = useState('development');

  const checkAuth = async () => {
    if (!api.getToken()) {
      setLoading(false);
      return;
    }

    try {
      const user = await api.getMe();
      setCurrentUser(user);
      await loadProjects();
    } catch (_) {
      api.setToken(null);
      setCurrentUser(null);
    } finally {
      setLoading(false);
    }
  };

  const loadProjects = async () => {
    try {
      const projs = await api.listProjects();
      setProjects(projs);
      if (projs.length > 0 && !projs.find((p) => p.id === currentProject)) {
        setCurrentProject(projs[0].id);
        loadEnvironments(projs[0].id);
      } else {
        loadEnvironments(currentProject);
      }
    } catch (_) {}
  };

  const loadEnvironments = async (projId: string) => {
    try {
      const envs = await api.listEnvironments(projId);
      setEnvironments(envs);
      if (envs.length > 0 && !envs.find((e) => e.name === currentEnv)) {
        setCurrentEnv(envs[0].name);
        api.setContext(projId, envs[0].name);
      } else {
        api.setContext(projId, currentEnv);
      }
    } catch (_) {}
  };

  useEffect(() => {
    checkAuth();

    const handleUnauthorized = () => {
      setCurrentUser(null);
    };
    window.addEventListener('beavend:unauthorized', handleUnauthorized);
    return () => window.removeEventListener('beavend:unauthorized', handleUnauthorized);
  }, []);

  const handleProjectChange = (projId: string) => {
    setCurrentProject(projId);
    api.setContext(projId, currentEnv);
    loadEnvironments(projId);
  };

  const handleEnvChange = (env: string) => {
    setCurrentEnv(env);
    api.setContext(currentProject, env);
  };

  const handleLogout = async () => {
    await api.logout();
    setCurrentUser(null);
  };

  if (loading) {
    return (
      <div className="min-h-screen bg-slate-950 flex items-center justify-center text-slate-400 text-sm">
        Initializing Beavend Control Plane...
      </div>
    );
  }

  if (!currentUser) {
    return (
      <LoginPage
        onLoginSuccess={(user) => {
          setCurrentUser(user);
          loadProjects();
        }}
      />
    );
  }

  return (
    <div className="h-screen flex overflow-hidden bg-slate-950 text-slate-100">
      <Sidebar currentTab={currentTab} onSelectTab={setCurrentTab} />

      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        <Navbar
          user={currentUser}
          projects={projects}
          currentProject={currentProject}
          currentEnv={currentEnv}
          environments={environments}
          onChangeProject={handleProjectChange}
          onChangeEnv={handleEnvChange}
          onLogout={handleLogout}
        />

        <main key={`${currentProject}-${currentEnv}`} className="flex-1 overflow-y-auto p-8">
          {currentTab === 'overview' && <OverviewPage />}
          {currentTab === 'apps' && <AppsPage />}
          {currentTab === 'prompts' && <PromptsPage />}
          {currentTab === 'models' && <ModelsPage />}
          {currentTab === 'tools' && <ToolsPage />}
          {currentTab === 'policies' && <PoliciesPage />}
          {currentTab === 'logs' && <LogsPage />}
          {currentTab === 'audit' && <AuditPage />}
          {currentTab === 'settings' && (
            <SettingsPage
              currentUser={currentUser}
              projects={projects}
              currentProject={currentProject}
              onRefreshProjects={loadProjects}
            />
          )}
        </main>
      </div>
    </div>
  );
};
