import React, { useEffect, useState } from 'react';
import {
  api,
  AIApp,
  AIAppDetails,
  AIAppVersion,
  Prompt,
  ModelRouting,
  Tool,
  LimitRule,
  GuardrailConfig,
  CacheConfig,
} from '../api/client';
import {
  ArrowLeft,
  Bot,
  Sliders,
  Terminal,
  Code2,
  Save,
  Check,
  Copy,
  Send,
  Sparkles,
  Shield,
  Zap,
  Clock,
  Wrench,
  AlertCircle,
  FileCode,
} from 'lucide-react';

interface AppWorkspaceProps {
  appId: string;
  onBack: () => void;
  onRefreshApps: () => void;
}

type WorkspaceTab = 'overview' | 'settings' | 'playground';

export const AppWorkspace: React.FC<AppWorkspaceProps> = ({ appId, onBack, onRefreshApps }) => {
  const [activeTab, setActiveTab] = useState<WorkspaceTab>('overview');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const [appDetails, setAppDetails] = useState<AIAppDetails | null>(null);
  const [prompts, setPrompts] = useState<Prompt[]>([]);
  const [models, setModels] = useState<ModelRouting[]>([]);
  const [availableTools, setAvailableTools] = useState<Tool[]>([]);

  // Per-App Settings Form State
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [promptId, setPromptId] = useState('');
  const [promptVersion, setPromptVersion] = useState(1);
  const [modelRoutingId, setModelRoutingId] = useState('');
  const [accessPolicy, setAccessPolicy] = useState('');
  const [selectedTools, setSelectedTools] = useState<string[]>([]);

  // Rate Limits & Tiers
  const [rpmLimit, setRpmLimit] = useState(60);
  const [dailyTokenLimit, setDailyTokenLimit] = useState(100000);
  const [proTierRpm, setProTierRpm] = useState(500);
  const [enableTierCondition, setEnableTierCondition] = useState(false);
  const [tierExpression, setTierExpression] = useState("claims.plan == 'pro'");

  // Guardrails
  const [guardrails, setGuardrails] = useState<GuardrailConfig>({
    pii_detection: true,
    pii_redaction: true,
    prompt_injection_defense: true,
    max_input_length: 8000,
    max_output_tokens: 4096,
    streaming_mode: 'buffered',
  });

  // Response Caching
  const [cacheConfig, setCacheConfig] = useState<CacheConfig>({
    enabled: true,
    ttl_seconds: 300,
    tool_caching: true,
    scope: 'user',
  });

  // Response Schema
  const [responseSchema, setResponseSchema] = useState('');

  // Playground State
  const [chatMessages, setChatMessages] = useState<{ role: string; content: string }[]>([
    { role: 'assistant', content: 'Hello! I am ready to test. Type a prompt below.' },
  ]);
  const [userInput, setUserInput] = useState('');
  const [simulatedToken, setSimulatedToken] = useState('');
  const [chatLoading, setChatLoading] = useState(false);
  const [copiedCode, setCopiedCode] = useState<string | null>(null);

  const loadAll = async () => {
    setLoading(true);
    setErrorMsg(null);
    try {
      const [details, pList, mList, tList] = await Promise.all([
        api.getApp(appId),
        api.listPrompts().catch(() => []),
        api.listModels().catch(() => []),
        api.listTools().catch(() => []),
      ]);

      setAppDetails(details);
      setPrompts(pList);
      setModels(mList);
      setAvailableTools(tList);

      if (details.app) {
        setName(details.app.name || '');
        setDescription(details.app.description || '');
      }

      // Populate latest active version fields
      const activeVer = details.versions && details.versions.length > 0 ? details.versions[0] : null;
      if (activeVer) {
        setPromptId(activeVer.prompt_id || '');
        setPromptVersion(activeVer.prompt_version || 1);
        setModelRoutingId(activeVer.model_routing_id || '');
        setAccessPolicy(activeVer.access_policy || '');
        setSelectedTools(activeVer.tool_ids || []);
        if (activeVer.guardrails) {
          setGuardrails(activeVer.guardrails);
        }
        if (activeVer.cache_config) {
          setCacheConfig(activeVer.cache_config);
        }
        if (activeVer.response_schema) {
          setResponseSchema(activeVer.response_schema);
        }

        // Parse existing limits
        if (activeVer.limits && Array.isArray(activeVer.limits)) {
          for (const rule of activeVer.limits) {
            if (rule.resource === 'inference' && (rule.window === '1m' || rule.window === '60s')) {
              if (rule.tier_condition) {
                setEnableTierCondition(true);
                setTierExpression(rule.tier_condition);
                setProTierRpm(rule.max);
              } else {
                setRpmLimit(rule.max);
              }
            } else if (rule.resource === 'tokens' && (rule.window === '24h' || rule.window === '86400s')) {
              setDailyTokenLimit(rule.max);
            }
          }
        }
      }
    } catch (err: any) {
      setErrorMsg(err.message || 'Failed to load AI App configuration');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadAll();
  }, [appId]);

  const handleToolToggle = (tId: string) => {
    setSelectedTools((prev) =>
      prev.includes(tId) ? prev.filter((id) => id !== tId) : [...prev, tId]
    );
  };

  const handleSaveSettings = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setSuccessMsg(null);
    setErrorMsg(null);

    // Build LimitRule array
    const compiledLimits: LimitRule[] = [
      {
        resource: 'inference',
        window: '1m',
        max: Number(rpmLimit),
      },
      {
        resource: 'tokens',
        window: '24h',
        max: Number(dailyTokenLimit),
      },
    ];

    if (enableTierCondition && tierExpression.trim()) {
      compiledLimits.push({
        resource: 'inference',
        window: '1m',
        max: Number(proTierRpm),
        tier_condition: tierExpression.trim(),
      });
    }

    try {
      await api.updateApp(appId, {
        id: appId,
        name,
        description,
        prompt_id: promptId,
        prompt_version: Number(promptVersion),
        model_routing_id: modelRoutingId,
        access_policy: accessPolicy,
        tool_ids: selectedTools,
        limits: compiledLimits,
        guardrails,
        cache_config: cacheConfig,
        response_schema: responseSchema,
      });

      setSuccessMsg('App configuration updated successfully! Active snapshot invalidated.');
      onRefreshApps();
      loadAll();
    } catch (err: any) {
      setErrorMsg(err.message || 'Failed to save configuration');
    } finally {
      setSaving(false);
    }
  };

  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!userInput.trim() || chatLoading) return;

    const userMsg = { role: 'user', content: userInput.trim() };
    const updatedHistory = [...chatMessages, userMsg];
    setChatMessages(updatedHistory);
    setUserInput('');
    setChatLoading(true);

    try {
      const resp = await api.chatWithApp(
        appId,
        updatedHistory.map((m) => ({ role: m.role, content: m.content })),
        simulatedToken
      );

      const assistantReply =
        resp.choices?.[0]?.message?.content ||
        resp.message?.content ||
        JSON.stringify(resp, null, 2);

      setChatMessages((prev) => [...prev, { role: 'assistant', content: assistantReply }]);
    } catch (err: any) {
      setChatMessages((prev) => [
        ...prev,
        { role: 'assistant', content: `[Error: ${err.message}]` },
      ]);
    } finally {
      setChatLoading(false);
    }
  };

  const copyToClipboard = (text: string, label: string) => {
    navigator.clipboard.writeText(text);
    setCopiedCode(label);
    setTimeout(() => setCopiedCode(null), 2000);
  };

  if (loading) {
    return (
      <div className="text-center py-20 text-slate-500 text-sm">
        Loading AI App workspace...
      </div>
    );
  }

  const app = appDetails?.app;
  const currentVersion = app?.active_version || 1;

  const curlSnippet = `curl -X POST "http://localhost:8080/v1/apps/${appId}/chat" \\
  -H "Content-Type: application/json" \\
  -H "X-Project-ID: ${api.currentProject}" \\
  -H "X-Environment: ${api.currentEnv}" \\
  -H "Authorization: Bearer <USER_JWT>" \\
  -d '{
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'`;

  const tsSnippet = `import { BeavendClient } from '@beavend/sdk-web';

const client = new BeavendClient({
  baseUrl: 'https://api.yourdomain.com',
  projectId: '${api.currentProject}',
  environment: '${api.currentEnv}',
  getAuthToken: async () => await auth.currentUser?.getIdToken(),
});

const response = await client.chat('${appId}', [
  { role: 'user', content: 'Hello assistant!' }
]);
console.log(response.content);`;

  return (
    <div className="space-y-6 max-w-6xl">
      {/* Top Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <button
            onClick={onBack}
            className="p-2 bg-slate-900 hover:bg-slate-800 text-slate-400 hover:text-slate-100 rounded-xl border border-slate-800 transition-colors"
            title="Back to AI Applications"
          >
            <ArrowLeft className="w-4 h-4" />
          </button>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl font-bold tracking-tight text-slate-100">{name || appId}</h1>
              <span className="text-xs bg-emerald-500/10 text-emerald-400 font-mono px-2 py-0.5 rounded-full border border-emerald-500/20">
                active v{currentVersion}
              </span>
            </div>
            <p className="text-xs font-mono text-slate-500 mt-0.5">/v1/apps/{appId}/chat</p>
          </div>
        </div>

        {/* Sub-nav Tabs */}
        <div className="flex items-center bg-slate-900 border border-slate-800 rounded-xl p-1 text-xs font-medium">
          <button
            onClick={() => setActiveTab('overview')}
            className={`px-3 py-1.5 rounded-lg transition-colors flex items-center gap-1.5 ${
              activeTab === 'overview'
                ? 'bg-slate-800 text-slate-100 font-semibold shadow-sm'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <Code2 className="w-3.5 h-3.5" />
            Overview & SDK
          </button>
          <button
            onClick={() => setActiveTab('settings')}
            className={`px-3 py-1.5 rounded-lg transition-colors flex items-center gap-1.5 ${
              activeTab === 'settings'
                ? 'bg-slate-800 text-slate-100 font-semibold shadow-sm'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <Sliders className="w-3.5 h-3.5 text-emerald-400" />
            App Settings
          </button>
          <button
            onClick={() => setActiveTab('playground')}
            className={`px-3 py-1.5 rounded-lg transition-colors flex items-center gap-1.5 ${
              activeTab === 'playground'
                ? 'bg-slate-800 text-slate-100 font-semibold shadow-sm'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <Terminal className="w-3.5 h-3.5 text-blue-400" />
            Test Playground
          </button>
        </div>
      </div>

      {successMsg && (
        <div className="p-3 bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 rounded-xl text-xs flex items-center gap-2">
          <Check className="w-4 h-4 flex-shrink-0" />
          <span>{successMsg}</span>
        </div>
      )}

      {errorMsg && (
        <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs flex items-center gap-2">
          <AlertCircle className="w-4 h-4 flex-shrink-0" />
          <span>{errorMsg}</span>
        </div>
      )}

      {/* Tab 1: Overview & Integration */}
      {activeTab === 'overview' && (
        <div className="space-y-6">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
              <Bot className="w-5 h-5 text-emerald-400" />
              Application Overview
            </h2>
            <p className="text-xs text-slate-400 leading-relaxed">
              {description || 'No description configured for this AI App.'}
            </p>

            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 pt-2">
              <div className="bg-slate-950 border border-slate-800/80 p-3 rounded-xl">
                <span className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
                  Logical Model
                </span>
                <p className="text-sm font-mono text-slate-200 mt-1">{modelRoutingId || 'fast (default)'}</p>
              </div>
              <div className="bg-slate-950 border border-slate-800/80 p-3 rounded-xl">
                <span className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
                  Assigned Prompt
                </span>
                <p className="text-sm font-mono text-slate-200 mt-1">
                  {promptId ? `${promptId} (v${promptVersion})` : 'Pass-through'}
                </p>
              </div>
              <div className="bg-slate-950 border border-slate-800/80 p-3 rounded-xl">
                <span className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
                  Active Tools
                </span>
                <p className="text-sm font-mono text-emerald-400 mt-1">
                  {selectedTools.length} tool{selectedTools.length === 1 ? '' : 's'} attached
                </p>
              </div>
              <div className="bg-slate-950 border border-slate-800/80 p-3 rounded-xl">
                <span className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
                  User Rate Limit
                </span>
                <p className="text-sm font-mono text-slate-200 mt-1">{rpmLimit} RPM</p>
              </div>
            </div>
          </div>

          {/* Integration Snippets */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
              <Code2 className="w-5 h-5 text-blue-400" />
              Client Integration Snippets
            </h2>
            <p className="text-xs text-slate-400">
              Pass your authenticated user's JWT in the Authorization header. Beavend automatically validates the token and applies rate limits, policies, and tool bindings.
            </p>

            <div className="space-y-4 pt-2">
              <div>
                <div className="flex items-center justify-between pb-1.5">
                  <span className="text-xs font-semibold text-slate-400">cURL / HTTP Request</span>
                  <button
                    onClick={() => copyToClipboard(curlSnippet, 'curl')}
                    className="text-xs text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
                  >
                    {copiedCode === 'curl' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                    <span>{copiedCode === 'curl' ? 'Copied' : 'Copy'}</span>
                  </button>
                </div>
                <pre className="bg-slate-950 border border-slate-800 p-4 rounded-xl text-xs font-mono text-slate-300 overflow-x-auto">
                  {curlSnippet}
                </pre>
              </div>

              <div>
                <div className="flex items-center justify-between pb-1.5">
                  <span className="text-xs font-semibold text-slate-400">TypeScript / Web SDK</span>
                  <button
                    onClick={() => copyToClipboard(tsSnippet, 'ts')}
                    className="text-xs text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
                  >
                    {copiedCode === 'ts' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                    <span>{copiedCode === 'ts' ? 'Copied' : 'Copy'}</span>
                  </button>
                </div>
                <pre className="bg-slate-950 border border-slate-800 p-4 rounded-xl text-xs font-mono text-slate-300 overflow-x-auto">
                  {tsSnippet}
                </pre>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Tab 2: Per-App Settings */}
      {activeTab === 'settings' && (
        <form onSubmit={handleSaveSettings} className="space-y-6">
          {/* General & Model Routing */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
              <Bot className="w-5 h-5 text-emerald-400" />
              General & Model Assignment
            </h2>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">App Display Name</label>
                <input
                  type="text"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Logical Model Routing</label>
                <select
                  value={modelRoutingId}
                  onChange={(e) => setModelRoutingId(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                >
                  <option value="">(Default: fast)</option>
                  {models.map((m) => (
                    <option key={m.logical_model} value={m.logical_model}>
                      {m.logical_model} ({m.deployment})
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div>
              <label className="block text-xs font-semibold text-slate-400 mb-1">Description</label>
              <textarea
                rows={2}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
              />
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Assigned System Prompt</label>
                <select
                  value={promptId}
                  onChange={(e) => setPromptId(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                >
                  <option value="">(None / Direct Pass-through)</option>
                  {prompts.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} ({p.id})
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">CEL Access Policy</label>
                <input
                  type="text"
                  placeholder="e.g. claims.ai_access == true && claims.plan in ['pro', 'team']"
                  value={accessPolicy}
                  onChange={(e) => setAccessPolicy(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500"
                />
              </div>
            </div>
          </div>

          {/* Tools Attachment */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
              <Wrench className="w-5 h-5 text-amber-400" />
              Attached Tools (Function Calling)
            </h2>
            <p className="text-xs text-slate-400">
              Select which custom HTTP tools this AI App is authorized to call on behalf of authenticated users.
            </p>

            {availableTools.length === 0 ? (
              <p className="text-xs text-slate-500 italic">No tools registered in project. Add tools in Tools Registry.</p>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3 pt-1">
                {availableTools.map((tool) => {
                  const isChecked = selectedTools.includes(tool.id);
                  return (
                    <div
                      key={tool.id}
                      onClick={() => handleToolToggle(tool.id)}
                      className={`cursor-pointer border rounded-xl p-3 flex items-start gap-3 transition-colors ${
                        isChecked
                          ? 'bg-amber-500/10 border-amber-500/30 text-amber-300'
                          : 'bg-slate-950 border-slate-800 text-slate-400 hover:border-slate-700'
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={isChecked}
                        onChange={() => {}}
                        className="mt-1 rounded text-amber-500 focus:ring-0 bg-slate-900 border-slate-700"
                      />
                      <div className="min-w-0">
                        <div className="font-semibold text-xs text-slate-200">{tool.name}</div>
                        <div className="text-[11px] font-mono text-slate-500 truncate">{tool.id}</div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Rate Limiting & User Quotas */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
              <Zap className="w-5 h-5 text-emerald-400" />
              User-Level Rate Limiting & CEL Tiers
            </h2>
            <p className="text-xs text-slate-400">
              Evaluated per authenticated caller keyed by <code className="font-mono text-emerald-400">project:issuer:subject</code> using atomic sliding windows in Redis.
            </p>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">
                  Default Inference Rate Limit (RPM)
                </label>
                <input
                  type="number"
                  min="1"
                  value={rpmLimit}
                  onChange={(e) => setRpmLimit(Number(e.target.value))}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                />
                <span className="text-[10px] text-slate-500 mt-1 block">Requests per minute sliding window</span>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">
                  Daily Token Quota (Tokens / 24h)
                </label>
                <input
                  type="number"
                  min="1000"
                  step="5000"
                  value={dailyTokenLimit}
                  onChange={(e) => setDailyTokenLimit(Number(e.target.value))}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                />
                <span className="text-[10px] text-slate-500 mt-1 block">Pre-allocated estimate with async reconciliation</span>
              </div>
            </div>

            {/* CEL Tier Override */}
            <div className="border-t border-slate-800/80 pt-4 space-y-3">
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="tier-toggle"
                  checked={enableTierCondition}
                  onChange={(e) => setEnableTierCondition(e.target.checked)}
                  className="rounded text-emerald-500 bg-slate-950 border-slate-800"
                />
                <label htmlFor="tier-toggle" className="text-xs font-semibold text-slate-300 cursor-pointer">
                  Enable CEL Plan Tier Override (e.g. Pro Users)
                </label>
              </div>

              {enableTierCondition && (
                <div className="grid grid-cols-1 md:grid-cols-3 gap-3 bg-slate-950 border border-slate-800/80 p-3 rounded-xl">
                  <div className="md:col-span-2">
                    <label className="block text-[11px] font-semibold text-slate-400 mb-1">
                      Tier Match Condition (CEL)
                    </label>
                    <input
                      type="text"
                      value={tierExpression}
                      onChange={(e) => setTierExpression(e.target.value)}
                      placeholder="claims.plan == 'pro'"
                      className="w-full bg-slate-900 border border-slate-800 rounded-lg px-2.5 py-1.5 text-xs font-mono text-emerald-400 focus:outline-none focus:border-emerald-500"
                    />
                  </div>
                  <div>
                    <label className="block text-[11px] font-semibold text-slate-400 mb-1">
                      Tier Rate Limit (RPM)
                    </label>
                    <input
                      type="number"
                      value={proTierRpm}
                      onChange={(e) => setProTierRpm(Number(e.target.value))}
                      className="w-full bg-slate-900 border border-slate-800 rounded-lg px-2.5 py-1.5 text-xs font-mono text-slate-200 focus:outline-none focus:border-emerald-500"
                    />
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Safety & Guardrails */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
              <Shield className="w-5 h-5 text-indigo-400" />
              Safety, PII Redaction & Guardrails
            </h2>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="flex items-center justify-between p-3 bg-slate-950 border border-slate-800 rounded-xl">
                <div>
                  <span className="text-xs font-semibold text-slate-200">PII Detection & Redaction</span>
                  <p className="text-[11px] text-slate-500">Luhn credit cards, SSNs, emails, phone numbers</p>
                </div>
                <input
                  type="checkbox"
                  checked={guardrails.pii_redaction}
                  onChange={(e) =>
                    setGuardrails((prev) => ({
                      ...prev,
                      pii_redaction: e.target.checked,
                      pii_detection: e.target.checked,
                    }))
                  }
                  className="rounded text-indigo-500 bg-slate-900 border-slate-700"
                />
              </div>

              <div className="flex items-center justify-between p-3 bg-slate-950 border border-slate-800 rounded-xl">
                <div>
                  <span className="text-xs font-semibold text-slate-200">Prompt Injection Defense</span>
                  <p className="text-[11px] text-slate-500">Heuristic jailbreak & system override guard</p>
                </div>
                <input
                  type="checkbox"
                  checked={guardrails.prompt_injection_defense}
                  onChange={(e) =>
                    setGuardrails((prev) => ({ ...prev, prompt_injection_defense: e.target.checked }))
                  }
                  className="rounded text-indigo-500 bg-slate-900 border-slate-700"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Max Input Length</label>
                <input
                  type="number"
                  value={guardrails.max_input_length || 8000}
                  onChange={(e) =>
                    setGuardrails((prev) => ({ ...prev, max_input_length: Number(e.target.value) }))
                  }
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-200"
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Max Output Tokens</label>
                <input
                  type="number"
                  value={guardrails.max_output_tokens || 4096}
                  onChange={(e) =>
                    setGuardrails((prev) => ({ ...prev, max_output_tokens: Number(e.target.value) }))
                  }
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-200"
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Streaming Delivery Mode</label>
                <select
                  value={guardrails.streaming_mode || 'buffered'}
                  onChange={(e) =>
                    setGuardrails((prev) => ({ ...prev, streaming_mode: e.target.value as any }))
                  }
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-200"
                >
                  <option value="buffered">Buffered (Strict Safety)</option>
                  <option value="passthrough_with_post_check">Passthrough (Low Latency)</option>
                </select>
              </div>
            </div>
          </div>

          {/* Response Caching */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
              <Clock className="w-5 h-5 text-cyan-400" />
              Response Acceleration & Ephemeral Caching
            </h2>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="flex items-center justify-between p-3 bg-slate-950 border border-slate-800 rounded-xl">
                <div>
                  <span className="text-xs font-semibold text-slate-200">Enable Response Cache</span>
                  <p className="text-[11px] text-slate-500">Sub-millisecond repeat completions</p>
                </div>
                <input
                  type="checkbox"
                  checked={cacheConfig.enabled}
                  onChange={(e) =>
                    setCacheConfig((prev) => ({ ...prev, enabled: e.target.checked }))
                  }
                  className="rounded text-cyan-500 bg-slate-900 border-slate-700"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Cache TTL (Seconds)</label>
                <input
                  type="number"
                  min="0"
                  value={cacheConfig.ttl_seconds || 300}
                  onChange={(e) =>
                    setCacheConfig((prev) => ({ ...prev, ttl_seconds: Number(e.target.value) }))
                  }
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-200"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Cache Scope</label>
                <select
                  value={cacheConfig.scope || 'user'}
                  onChange={(e) =>
                    setCacheConfig((prev) => ({ ...prev, scope: e.target.value as any }))
                  }
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-200"
                >
                  <option value="user">User (Caller-isolated)</option>
                  <option value="shared">Shared (Project-wide)</option>
                </select>
              </div>
            </div>
          </div>

          {/* Structured Output Schema */}
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
            <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
              <FileCode className="w-5 h-5 text-purple-400" />
              Structured Output (JSON Schema)
            </h2>
            <p className="text-xs text-slate-400">
              Optional JSON Schema defining the exact structure the model must return. Enforced via runtime JSON schema validation.
            </p>
            <textarea
              rows={4}
              placeholder={'{\n  "type": "object",\n  "properties": {\n    "summary": { "type": "string" }\n  }\n}'}
              value={responseSchema}
              onChange={(e) => setResponseSchema(e.target.value)}
              className="w-full bg-slate-950 border border-slate-800 rounded-xl p-3 text-xs font-mono text-slate-200 focus:outline-none focus:border-emerald-500"
            />
          </div>

          {/* Submit Actions */}
          <div className="flex items-center justify-end gap-3 pt-2">
            <button
              type="button"
              onClick={onBack}
              className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium rounded-xl transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={saving}
              className="px-5 py-2.5 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white text-xs font-semibold rounded-xl flex items-center gap-2 transition-colors shadow-lg shadow-emerald-900/20"
            >
              <Save className="w-4 h-4" />
              <span>{saving ? 'Saving...' : 'Save Application Settings'}</span>
            </button>
          </div>
        </form>
      )}

      {/* Tab 3: Interactive Playground */}
      {activeTab === 'playground' && (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 space-y-4">
          <div className="flex items-center justify-between border-b border-slate-800 pb-3">
            <div>
              <h2 className="text-base font-semibold text-slate-200 flex items-center gap-2">
                <Terminal className="w-5 h-5 text-blue-400" />
                Interactive Test Playground
              </h2>
              <p className="text-xs text-slate-400">
                Send test chat messages to verify prompts, rate limits, guardrails, and tool executions.
              </p>
            </div>
            <button
              onClick={() =>
                setChatMessages([
                  { role: 'assistant', content: 'Chat reset. Type a prompt to begin.' },
                ])
              }
              className="px-2.5 py-1 text-xs text-slate-400 hover:text-slate-200 hover:bg-slate-800 rounded-lg transition-colors"
            >
              Reset Chat
            </button>
          </div>

          {/* Simulated User Token Input */}
          <div className="bg-slate-950 border border-slate-800/80 p-3 rounded-xl space-y-1">
            <label className="block text-[11px] font-semibold text-slate-400">
              Simulated Bearer JWT (Optional)
            </label>
            <input
              type="text"
              placeholder="Paste end-user JWT token to test identity extraction, CEL policies, and user rate limits"
              value={simulatedToken}
              onChange={(e) => setSimulatedToken(e.target.value)}
              className="w-full bg-slate-900 border border-slate-800 rounded-lg px-2.5 py-1.5 text-xs font-mono text-slate-200 focus:outline-none focus:border-blue-500"
            />
          </div>

          {/* Chat Messages Log */}
          <div className="h-96 overflow-y-auto bg-slate-950 border border-slate-800 rounded-xl p-4 space-y-3">
            {chatMessages.map((msg, idx) => {
              const isUser = msg.role === 'user';
              return (
                <div key={idx} className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
                  <div
                    className={`max-w-xl rounded-2xl px-4 py-2.5 text-xs leading-relaxed whitespace-pre-wrap ${
                      isUser
                        ? 'bg-emerald-600 text-white rounded-br-none'
                        : 'bg-slate-800 text-slate-200 rounded-bl-none border border-slate-700/60'
                    }`}
                  >
                    {msg.content}
                  </div>
                </div>
              );
            })}
            {chatLoading && (
              <div className="flex justify-start">
                <div className="bg-slate-800 text-slate-400 rounded-2xl rounded-bl-none px-4 py-2 text-xs italic">
                  Generating completion...
                </div>
              </div>
            )}
          </div>

          {/* Chat Input */}
          <form onSubmit={handleSendMessage} className="flex gap-2">
            <input
              type="text"
              placeholder="Ask anything..."
              value={userInput}
              onChange={(e) => setUserInput(e.target.value)}
              className="flex-1 bg-slate-950 border border-slate-800 rounded-xl px-4 py-2.5 text-xs text-slate-100 placeholder-slate-600 focus:outline-none focus:border-emerald-500"
            />
            <button
              type="submit"
              disabled={chatLoading || !userInput.trim()}
              className="px-5 py-2.5 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white rounded-xl text-xs font-semibold flex items-center gap-1.5 transition-colors shadow-lg shadow-emerald-900/20"
            >
              <Send className="w-3.5 h-3.5" />
              <span>Send</span>
            </button>
          </form>
        </div>
      )}
    </div>
  );
};
