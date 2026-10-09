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
import { SearchPage } from './pages/SearchPage';
import { UsagePage } from './pages/UsagePage';
import { LogsPage } from './pages/LogsPage';
import { AuditPage } from './pages/AuditPage';
import { SettingsPage } from './pages/SettingsPage';

interface ErrorBoundaryProps {
  children: React.ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('Unhandled render error caught by ErrorBoundary:', error, errorInfo);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="min-h-screen bg-neutral-950 flex flex-col items-center justify-center p-6 text-center">
          <div className="w-12 h-12 rounded-xl bg-red-500/10 border border-red-500/20 flex items-center justify-center text-red-400 mb-4">
            <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
          </div>
          <h2 className="text-base font-bold text-neutral-100 mb-1">Rendering Error</h2>
          <p className="text-xs text-neutral-400 max-w-md mb-4 font-mono bg-neutral-900 border border-neutral-800 p-3 rounded-lg text-left overflow-auto max-h-32">
            {this.state.error?.message || 'An unexpected rendering error occurred.'}
          </p>
          <button
            onClick={() => {
              this.setState({ hasError: false, error: null });
              window.location.reload();
            }}
            className="px-4 py-2 text-xs font-medium rounded-lg bg-neutral-800 text-neutral-100 hover:bg-neutral-700 transition-colors"
          >
            Reload Page
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

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
    if (!api.getToken()) { setLoading(false); return; }
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
      let projs = await api.listProjects();
      if (!projs || projs.length === 0) {
        try {
          const def = await api.createProject({
            id: 'default',
            name: 'Default Project',
            description: 'Default project initialized automatically',
          });
          projs = [def];
        } catch (_) {}
      }
      setProjects(projs || []);
      if (projs && projs.length > 0) {
        const active = projs.find((p) => p.id === currentProject) || projs[0];
        setCurrentProject(active.id);
        loadEnvironments(active.id);
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
    const handleUnauthorized = () => setCurrentUser(null);
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
      <div className="min-h-screen bg-neutral-950 flex flex-col items-center justify-center gap-3 text-neutral-500">
        <div className="w-8 h-8 rounded-lg bg-neutral-900 border border-neutral-800 flex items-center justify-center">
          <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
        <span className="text-sm">Initializing Beavend Control Plane...</span>
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
    <div className="h-screen flex overflow-hidden bg-neutral-950 text-neutral-100">
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
          onRefreshProjects={loadProjects}
        />

        <main
          key={`${currentProject}-${currentEnv}`}
          className="flex-1 overflow-y-auto px-7 py-6"
        >
          <ErrorBoundary>
            {currentTab === 'overview' && <OverviewPage />}
            {currentTab === 'apps' && <AppsPage />}
            {currentTab === 'prompts' && <PromptsPage />}
            {currentTab === 'models' && <ModelsPage />}
            {currentTab === 'tools' && <ToolsPage />}
            {currentTab === 'search' && <SearchPage onNavigateApps={() => setCurrentTab('apps')} />}
            {currentTab === 'usage' && <UsagePage />}
            {currentTab === 'logs' && <LogsPage />}
            {currentTab === 'audit' && <AuditPage />}
            {currentTab === 'settings' && (
              <SettingsPage
                currentUser={currentUser}
                projects={projects}
                currentProject={currentProject}
                currentEnv={currentEnv}
                onRefreshProjects={loadProjects}
                onNavigateTab={setCurrentTab}
              />
            )}
          </ErrorBoundary>
        </main>
      </div>
    </div>
  );
};
