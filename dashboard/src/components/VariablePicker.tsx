import React, { useState } from 'react';
import { Braces, Copy, Check, Search, Info, Plus, Sparkles, User, Shield, Bot, Globe, Layers } from 'lucide-react';
import { Modal } from './ui/Modal';
import { Badge } from './ui/Badge';
import { Button } from './ui/Button';

export interface VariableDefinition {
  name: string;
  syntax: string;
  category: 'identity' | 'claims' | 'app' | 'project' | 'metadata';
  description: string;
  example: string;
  source: string;
}

export const SYSTEM_VARIABLES: VariableDefinition[] = [
  // User & Identity
  {
    name: 'User ID',
    syntax: '{{user_id}}',
    category: 'identity',
    description: 'Unique authenticated end-user subject identifier (sub claim)',
    example: 'usr_849204',
    source: 'Caller JWT (sub)',
  },
  {
    name: 'User Name',
    syntax: '{{user.name}}',
    category: 'identity',
    description: 'Full name or display name of the authenticated user',
    example: 'Alice Chen',
    source: 'Caller JWT (name)',
  },
  {
    name: 'User Email',
    syntax: '{{user.email}}',
    category: 'identity',
    description: 'Email address of the authenticated caller',
    example: 'alice@example.com',
    source: 'Caller JWT (email)',
  },
  {
    name: 'User Role',
    syntax: '{{user.role}}',
    category: 'identity',
    description: 'Security role or permission level assigned in user claims',
    example: 'admin',
    source: 'Caller JWT (role)',
  },
  {
    name: 'User Plan',
    syntax: '{{user.plan}}',
    category: 'identity',
    description: 'Subscription tier or billing plan associated with the user',
    example: 'enterprise',
    source: 'Caller JWT (plan)',
  },
  {
    name: 'User Locale',
    syntax: '{{user.locale}}',
    category: 'identity',
    description: 'User locale or language preference code',
    example: 'en-US',
    source: 'Caller JWT (locale)',
  },
  {
    name: 'Token Issuer',
    syntax: '{{issuer}}',
    category: 'identity',
    description: 'Identity provider issuer URL or identifier',
    example: 'https://securetoken.google.com/my-project',
    source: 'Caller JWT (iss)',
  },

  // Custom Claims
  {
    name: 'Custom JWT Claim',
    syntax: '{{claims.<claim_name>}}',
    category: 'claims',
    description: 'Access any custom claim present in caller JWT token payload',
    example: '{{claims.organization_id}}',
    source: 'Caller JWT Claims',
  },

  // AI Application
  {
    name: 'Application Name',
    syntax: '{{app_name}}',
    category: 'app',
    description: 'Display name of the currently executing AI Application',
    example: 'Support Assistant',
    source: 'App Settings',
  },
  {
    name: 'Application ID',
    syntax: '{{app_id}}',
    category: 'app',
    description: 'Identifier slug of the current AI Application',
    example: 'support_v1',
    source: 'App Settings',
  },
  {
    name: 'Application Description',
    syntax: '{{app.description}}',
    category: 'app',
    description: 'Configured description text of the AI Application',
    example: 'Customer support AI agent for billing questions',
    source: 'App Settings',
  },

  // Project & Environment
  {
    name: 'Environment',
    syntax: '{{environment}}',
    category: 'project',
    description: 'Runtime environment of the executing request',
    example: 'production',
    source: 'X-Environment header / Config',
  },
  {
    name: 'Project ID',
    syntax: '{{project_id}}',
    category: 'project',
    description: 'Project namespace ID for this Beavend deployment',
    example: 'default',
    source: 'X-Project-ID header / Config',
  },

  // Client Request Metadata
  {
    name: 'Custom Request Metadata',
    syntax: '{{metadata.<key>}}',
    category: 'metadata',
    description: 'Any dynamic key passed in the request body under metadata object',
    example: '{{metadata.current_screen}}',
    source: 'POST /chat request body',
  },
];

const categoryLabels: Record<VariableDefinition['category'], { label: string; icon: React.ReactNode; color: string }> = {
  identity: { label: 'User Identity', icon: <User className="w-3.5 h-3.5" />, color: 'emerald' },
  claims: { label: 'JWT Claims', icon: <Shield className="w-3.5 h-3.5" />, color: 'purple' },
  app: { label: 'Application', icon: <Bot className="w-3.5 h-3.5" />, color: 'blue' },
  project: { label: 'Project & Env', icon: <Globe className="w-3.5 h-3.5" />, color: 'amber' },
  metadata: { label: 'Request Metadata', icon: <Layers className="w-3.5 h-3.5" />, color: 'cyan' },
};

interface VariableReferenceModalProps {
  open: boolean;
  onClose: () => void;
  onInsert?: (syntax: string) => void;
}

export const VariableReferenceModal: React.FC<VariableReferenceModalProps> = ({
  open,
  onClose,
  onInsert,
}) => {
  const [search, setSearch] = useState('');
  const [selectedCategory, setSelectedCategory] = useState<string>('all');
  const [copiedVar, setCopiedVar] = useState<string | null>(null);

  const handleCopy = (syntax: string) => {
    navigator.clipboard.writeText(syntax);
    setCopiedVar(syntax);
    setTimeout(() => setCopiedVar(null), 2000);
  };

  const filtered = SYSTEM_VARIABLES.filter((v) => {
    const matchesCategory = selectedCategory === 'all' || v.category === selectedCategory;
    const query = search.toLowerCase().trim();
    const matchesSearch =
      !query ||
      v.name.toLowerCase().includes(query) ||
      v.syntax.toLowerCase().includes(query) ||
      v.description.toLowerCase().includes(query) ||
      v.source.toLowerCase().includes(query);
    return matchesCategory && matchesSearch;
  });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Template Variables Catalog"
      description="Dynamic variables resolved at request runtime from user identity, app settings, and client metadata."
      size="xl"
    >
      <div className="space-y-4">
        {/* Search & Category Filter */}
        <div className="flex flex-col sm:flex-row gap-2">
          <div className="relative flex-1">
            <Search className="w-4 h-4 text-neutral-500 absolute left-3 top-2.5" />
            <input
              type="text"
              placeholder="Search variables (e.g. user, email, metadata)..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full bg-neutral-950 border border-neutral-800 rounded-lg pl-9 pr-3 py-1.5 text-xs text-neutral-200 placeholder-neutral-600 focus:outline-none focus:border-neutral-700"
            />
          </div>
          <div className="flex items-center gap-1 overflow-x-auto pb-1 sm:pb-0">
            <button
              type="button"
              onClick={() => setSelectedCategory('all')}
              className={`px-2.5 py-1 rounded-lg text-xs font-medium whitespace-nowrap transition-colors ${
                selectedCategory === 'all'
                  ? 'bg-neutral-800 text-neutral-100'
                  : 'text-neutral-400 hover:text-neutral-200 hover:bg-neutral-900'
              }`}
            >
              All ({SYSTEM_VARIABLES.length})
            </button>
            {Object.entries(categoryLabels).map(([key, cat]) => (
              <button
                key={key}
                type="button"
                onClick={() => setSelectedCategory(key)}
                className={`px-2 py-1 rounded-lg text-xs font-medium whitespace-nowrap flex items-center gap-1.5 transition-colors ${
                  selectedCategory === key
                    ? 'bg-neutral-800 text-neutral-100'
                    : 'text-neutral-400 hover:text-neutral-200 hover:bg-neutral-900'
                }`}
              >
                {cat.icon}
                <span>{cat.label}</span>
              </button>
            ))}
          </div>
        </div>

        {/* Variables List */}
        <div className="space-y-2 max-h-[50vh] overflow-y-auto pr-1">
          {filtered.length === 0 ? (
            <div className="text-center py-8 text-neutral-500 text-xs">
              No template variables matched "{search}".
            </div>
          ) : (
            filtered.map((item) => {
              const cat = categoryLabels[item.category];
              const isCopied = copiedVar === item.syntax;
              return (
                <div
                  key={item.syntax}
                  className="bg-neutral-950/80 border border-neutral-800/80 hover:border-neutral-700/80 rounded-xl p-3 flex flex-col sm:flex-row sm:items-center justify-between gap-3 transition-colors"
                >
                  <div className="space-y-1 min-w-0 flex-1">
                    <div className="flex items-center gap-2 flex-wrap">
                      <code className="text-emerald-400 font-mono text-xs font-semibold bg-emerald-950/40 px-1.5 py-0.5 rounded border border-emerald-800/40">
                        {item.syntax}
                      </code>
                      <span className="text-xs font-medium text-neutral-300">{item.name}</span>
                      <span className="text-[10px] text-neutral-500 font-mono">
                        ({item.source})
                      </span>
                    </div>
                    <p className="text-[11px] text-neutral-400 leading-relaxed">
                      {item.description}
                    </p>
                    <div className="text-[10px] text-neutral-600 font-mono">
                      Example: <span className="text-neutral-400">{item.example}</span>
                    </div>
                  </div>

                  <div className="flex items-center gap-1.5 flex-shrink-0 self-end sm:self-center">
                    <Button
                      type="button"
                      variant="secondary"
                      size="xs"
                      onClick={() => handleCopy(item.syntax)}
                      icon={isCopied ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                    >
                      {isCopied ? 'Copied' : 'Copy'}
                    </Button>
                    {onInsert && (
                      <Button
                        type="button"
                        variant="primary"
                        size="xs"
                        onClick={() => {
                          onInsert(item.syntax);
                          onClose();
                        }}
                        icon={<Plus className="w-3 h-3" />}
                      >
                        Insert
                      </Button>
                    )}
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Runtime info note */}
        <div className="p-3 bg-neutral-950 border border-neutral-800/90 rounded-xl text-[11px] text-neutral-500 flex items-start gap-2">
          <Info className="w-4 h-4 text-neutral-400 flex-shrink-0 mt-0.5" />
          <div className="space-y-0.5">
            <span className="font-medium text-neutral-300">How Beavend resolves variables:</span>
            <p className="leading-relaxed">
              When an end user makes an AI request, Beavend validates their JWT token, parses request metadata, and substitutes matching double curly braces before prompt execution. Unmatched variables safely resolve to empty strings.
            </p>
          </div>
        </div>
      </div>
    </Modal>
  );
};

interface VariableHelperProps {
  detectedVars?: string[];
  onInsert?: (syntax: string) => void;
  className?: string;
}

export const VariableHelper: React.FC<VariableHelperProps> = ({
  detectedVars = [],
  onInsert,
  className = '',
}) => {
  const [showCatalog, setShowCatalog] = useState(false);

  // Common quick insert suggestions
  const quickVars = [
    { label: 'user.name', syntax: '{{user.name}}' },
    { label: 'user_id', syntax: '{{user_id}}' },
    { label: 'app_name', syntax: '{{app_name}}' },
    { label: 'metadata.key', syntax: '{{metadata.key}}' },
  ];

  return (
    <div className={`space-y-1.5 ${className}`}>
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div className="flex items-center gap-1.5 text-[11px] text-neutral-400">
          <span className="text-neutral-500 text-[10px] uppercase tracking-wider font-semibold">Quick Insert:</span>
          {quickVars.map((qv) => (
            <button
              key={qv.syntax}
              type="button"
              onClick={() => onInsert && onInsert(qv.syntax)}
              className="text-[10px] font-mono bg-neutral-900 hover:bg-neutral-800 text-neutral-300 hover:text-emerald-400 border border-neutral-800 px-1.5 py-0.5 rounded transition-colors flex items-center gap-0.5"
              title={`Click to insert ${qv.syntax}`}
            >
              <Plus className="w-2.5 h-2.5 opacity-60" />
              {qv.label}
            </button>
          ))}
        </div>

        <button
          type="button"
          onClick={() => setShowCatalog(true)}
          className="text-[11px] text-emerald-400 hover:text-emerald-300 font-medium flex items-center gap-1 transition-colors hover:underline"
        >
          <Braces className="w-3.5 h-3.5" />
          <span>All Variables Catalog ({SYSTEM_VARIABLES.length})</span>
        </button>
      </div>

      {detectedVars.length > 0 && (
        <div className="flex items-center gap-1.5 text-[11px] text-neutral-400 pt-0.5">
          <span className="text-neutral-500 text-[10px]">Detected in text:</span>
          {detectedVars.map((v) => (
            <span
              key={v}
              className="bg-neutral-800 text-emerald-400 px-1.5 py-0.5 rounded text-[10px] font-mono border border-neutral-700/60"
            >
              {`{{${v}}}`}
            </span>
          ))}
        </div>
      )}

      <VariableReferenceModal
        open={showCatalog}
        onClose={() => setShowCatalog(false)}
        onInsert={onInsert}
      />
    </div>
  );
};
