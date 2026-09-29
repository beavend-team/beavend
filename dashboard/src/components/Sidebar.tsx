import React from 'react';
import { Logo } from './Logo';
import {
  LayoutDashboard,
  Bot,
  MessageSquare,
  Cpu,
  Wrench,
  ShieldCheck,
  Activity,
  History,
  Settings,
} from 'lucide-react';

export type NavTab =
  | 'overview'
  | 'apps'
  | 'prompts'
  | 'models'
  | 'tools'
  | 'policies'
  | 'logs'
  | 'audit'
  | 'settings';

interface SidebarProps {
  currentTab: NavTab;
  onSelectTab: (tab: NavTab) => void;
}

export const Sidebar: React.FC<SidebarProps> = ({ currentTab, onSelectTab }) => {
  const navItems = [
    { id: 'overview', label: 'Overview', icon: LayoutDashboard },
    { id: 'apps', label: 'AI Apps', icon: Bot },
    { id: 'prompts', label: 'Prompts Studio', icon: MessageSquare },
    { id: 'models', label: 'Models & Routing', icon: Cpu },
    { id: 'tools', label: 'Tools Registry', icon: Wrench },
    { id: 'policies', label: 'CEL Policies', icon: ShieldCheck },
    { id: 'logs', label: 'Logs & Analytics', icon: Activity },
    { id: 'audit', label: 'Audit Trail', icon: History },
    { id: 'settings', label: 'Settings', icon: Settings },
  ];

  return (
    <aside className="w-64 bg-slate-900 border-r border-slate-800 flex flex-col flex-shrink-0">
      <div className="h-16 flex items-center gap-3 px-6 border-b border-slate-800">
        <Logo theme="dark" className="h-8 w-auto flex-shrink-0" />
        <div className="flex flex-col">
          <span className="font-bold text-slate-100 tracking-tight text-lg leading-tight">Beavend</span>
          <span className="text-xs text-slate-400 font-mono">v0.1.0-alpha</span>
        </div>
      </div>

      <nav className="flex-1 px-3 py-4 space-y-1">
        {navItems.map((item) => {
          const Icon = item.icon;
          const isActive = currentTab === item.id;
          return (
            <button
              key={item.id}
              onClick={() => onSelectTab(item.id as NavTab)}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-emerald-500/10 text-emerald-400 font-semibold'
                  : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/60'
              }`}
            >
              <Icon className={`w-5 h-5 ${isActive ? 'text-emerald-400' : 'text-slate-500'}`} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </nav>

      <div className="p-4 border-t border-slate-800 text-xs text-slate-500">
        <p className="font-medium text-slate-400">Zero-DB Hot Path</p>
        <p>Serving in-memory compiled snapshot</p>
      </div>
    </aside>
  );
};
