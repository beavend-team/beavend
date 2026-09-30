import React from 'react';
import { Logo } from './Logo';
import {
  LayoutDashboard,
  Bot,
  MessageSquare,
  Cpu,
  Wrench,
  ShieldCheck,
  BarChart3,
  History,
  Settings,
  ChevronRight,
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

const navGroups = [
  {
    label: 'Dashboard',
    items: [
      { id: 'overview', label: 'Overview', icon: LayoutDashboard },
    ],
  },
  {
    label: 'Configuration',
    items: [
      { id: 'apps', label: 'AI Apps', icon: Bot },
      { id: 'prompts', label: 'Prompts Studio', icon: MessageSquare },
      { id: 'models', label: 'Models & Routing', icon: Cpu },
      { id: 'tools', label: 'Tools Registry', icon: Wrench },
      { id: 'policies', label: 'CEL Policies', icon: ShieldCheck },
    ],
  },
  {
    label: 'Observability',
    items: [
      { id: 'logs', label: 'Request Logs', icon: BarChart3 },
      { id: 'audit', label: 'Audit Trail', icon: History },
    ],
  },
];

export const Sidebar: React.FC<SidebarProps> = ({ currentTab, onSelectTab }) => {
  return (
    <aside className="w-[220px] bg-neutral-950 border-r border-neutral-800/80 flex flex-col flex-shrink-0">
      {/* Brand */}
      <div className="h-[60px] flex items-center gap-3 px-5 border-b border-neutral-800/80">
        <Logo theme="dark" className="h-7 w-auto flex-shrink-0" />
        <div>
          <span className="font-semibold text-neutral-100 text-sm tracking-tight">Beavend</span>
          <span className="text-[10px] text-neutral-600 font-mono ml-1.5">v0.1</span>
        </div>
      </div>

      {/* Nav */}
      <nav className="flex-1 px-3 py-3 space-y-4 overflow-y-auto">
        {navGroups.map((group) => (
          <div key={group.label}>
            <p className="px-2 mb-1.5 text-[10px] font-semibold text-neutral-600 uppercase tracking-widest">
              {group.label}
            </p>
            <div className="space-y-0.5">
              {group.items.map((item) => {
                const Icon = item.icon;
                const isActive = currentTab === item.id;
                return (
                  <button
                    key={item.id}
                    onClick={() => onSelectTab(item.id as NavTab)}
                    className={`
                      w-full flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-xs font-medium
                      transition-all duration-100 group
                      ${isActive
                        ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/15'
                        : 'text-neutral-500 hover:text-neutral-200 hover:bg-neutral-800/70 border border-transparent'
                      }
                    `}
                  >
                    <Icon
                      className={`w-4 h-4 flex-shrink-0 ${isActive ? 'text-emerald-400' : 'text-neutral-600 group-hover:text-neutral-400'}`}
                    />
                    <span className="flex-1 text-left">{item.label}</span>
                    {isActive && (
                      <ChevronRight className="w-3 h-3 text-emerald-500/60" />
                    )}
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </nav>

      {/* Settings */}
      <div className="px-3 pb-3 border-t border-neutral-800/80 pt-3">
        <button
          onClick={() => onSelectTab('settings')}
          className={`
            w-full flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-xs font-medium
            transition-all duration-100
            ${currentTab === 'settings'
              ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/15'
              : 'text-neutral-500 hover:text-neutral-200 hover:bg-neutral-800/70 border border-transparent'
            }
          `}
        >
          <Settings className={`w-4 h-4 flex-shrink-0 ${currentTab === 'settings' ? 'text-emerald-400' : 'text-neutral-600'}`} />
          Settings
        </button>

        {/* Status indicator */}
        <div className="mt-3 px-2.5 py-2 rounded-lg bg-neutral-900 border border-neutral-800">
          <div className="flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 flex-shrink-0" />
            <span className="text-[10px] text-neutral-500 font-medium">Runtime active</span>
          </div>
          <p className="text-[10px] text-neutral-700 mt-0.5">Zero-DB hot path</p>
        </div>
      </div>
    </aside>
  );
};
