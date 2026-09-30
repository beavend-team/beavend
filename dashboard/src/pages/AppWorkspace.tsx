import React, { useEffect, useState } from 'react';
import {
  api,
  AIApp,
  AIAppDetails,
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
  Shield,
  Zap,
  Clock,
  Wrench,
  FileCode,
  RefreshCw,
  Trash2,
  X,
} from 'lucide-react';
import { Card, CardHeader } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Alert } from '../components/ui/Feedback';
import { Input, Textarea, Select, Toggle } from '../components/ui/Form';

interface AppWorkspaceProps {
  appId: string;
  onBack: () => void;
  onRefreshApps: () => void;
}

type WorkspaceTab = 'overview' | 'settings' | 'playground';

// Tab button
const TabButton: React.FC<{
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
}> = ({ active, onClick, icon, label }) => (
  <button
    onClick={onClick}
    className={`
      flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-all
      ${active ? 'bg-neutral-800 text-neutral-100' : 'text-neutral-500 hover:text-neutral-300 hover:bg-neutral-800/60'}
    `}
  >
    <span className="flex-shrink-0">{icon}</span>
    {label}
  </button>
);

// Code snippet with copy
const CodeBlock: React.FC<{ code: string; label: string }> = ({ code, label }) => {
  const [copied, setCopied] = useState(false);
  const handleCopy = () => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-medium text-neutral-500">{label}</span>
        <button
          onClick={handleCopy}
          className="flex items-center gap-1 text-[11px] text-neutral-500 hover:text-neutral-300 transition-colors"
        >
          {copied ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre className="bg-neutral-950 border border-neutral-800 rounded-lg p-3 text-[11px] font-mono text-neutral-400 overflow-x-auto whitespace-pre">
        {code}
      </pre>
    </div>
  );
};

// Section header used within settings
const SectionTitle: React.FC<{ icon: React.ReactNode; title: string; description?: string }> = ({
  icon, title, description,
}) => (
  <div className="flex items-center gap-2 mb-4">
    <div className="w-6 h-6 rounded flex items-center justify-center text-neutral-500">
      {icon}
    </div>
    <div>
      <p className="text-sm font-semibold text-neutral-200">{title}</p>
      {description && <p className="text-[11px] text-neutral-600">{description}</p>}
    </div>
  </div>
);

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

  // Settings form state
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [promptId, setPromptId] = useState('');
  const [promptVersion, setPromptVersion] = useState(1);
  const [modelRoutingId, setModelRoutingId] = useState('');
  const [accessPolicy, setAccessPolicy] = useState('');
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const [rpmLimit, setRpmLimit] = useState(60);
  const [dailyTokenLimit, setDailyTokenLimit] = useState(100000);
  const [proTierRpm, setProTierRpm] = useState(500);
  const [enableTierCondition, setEnableTierCondition] = useState(false);
  const [tierExpression, setTierExpression] = useState("claims.plan == 'pro'");
  const [guardrails, setGuardrails] = useState<GuardrailConfig>({
    pii_detection: true, pii_redaction: true,
    prompt_injection_defense: true, max_input_length: 8000,
    max_output_tokens: 4096, streaming_mode: 'buffered',
  });
  const [cacheConfig, setCacheConfig] = useState<CacheConfig>({
    enabled: true, ttl_seconds: 300, tool_caching: true, scope: 'user',
  });
  const [responseSchema, setResponseSchema] = useState('');

  // Playground
  const [chatMessages, setChatMessages] = useState<{ role: string; content: string }[]>([
    { role: 'assistant', content: 'Ready to test. Type a message below.' },
  ]);
  const [userInput, setUserInput] = useState('');
  const [simulatedToken, setSimulatedToken] = useState('');
  const [chatLoading, setChatLoading] = useState(false);
  const chatEndRef = React.useRef<HTMLDivElement>(null);

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
      setAppDetails(details as AIAppDetails);
      setPrompts(pList);
      setModels(mList);
      setAvailableTools(tList);
      if (details.app) {
        setName(details.app.name || '');
        setDescription(details.app.description || '');
      }
      const activeVer = details.versions?.[0];
      if (activeVer) {
        setPromptId(activeVer.prompt_id || '');
        setPromptVersion(activeVer.prompt_version || 1);
        setModelRoutingId(activeVer.model_routing_id || '');
        setAccessPolicy(activeVer.access_policy || '');
        setSelectedTools(activeVer.tool_ids || []);
        if (activeVer.guardrails) setGuardrails(activeVer.guardrails);
        if (activeVer.cache_config) setCacheConfig(activeVer.cache_config);
        if (activeVer.response_schema) setResponseSchema(activeVer.response_schema);
        if (activeVer.limits && Array.isArray(activeVer.limits)) {
          for (const rule of activeVer.limits) {
            if (rule.resource === 'inference' && (rule.window === '1m' || rule.window === '60s')) {
              if (rule.tier_condition) {
                setEnableTierCondition(true);
                setTierExpression(rule.tier_condition);
                setProTierRpm(rule.max);
              } else { setRpmLimit(rule.max); }
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

  useEffect(() => { loadAll(); }, [appId]);
  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [chatMessages]);

  const handleToolToggle = (tId: string) =>
    setSelectedTools((prev) => prev.includes(tId) ? prev.filter((id) => id !== tId) : [...prev, tId]);

  const handleSaveSettings = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setSuccessMsg(null);
    setErrorMsg(null);
    const compiledLimits: LimitRule[] = [
      { resource: 'inference', window: '1m', max: Number(rpmLimit) },
      { resource: 'tokens', window: '24h', max: Number(dailyTokenLimit) },
    ];
    if (enableTierCondition && tierExpression.trim()) {
      compiledLimits.push({ resource: 'inference', window: '1m', max: Number(proTierRpm), tier_condition: tierExpression.trim() });
    }
    try {
      await api.updateApp(appId, {
        id: appId, name, description,
        prompt_id: promptId, prompt_version: Number(promptVersion),
        model_routing_id: modelRoutingId, access_policy: accessPolicy,
        tool_ids: selectedTools, limits: compiledLimits,
        guardrails, cache_config: cacheConfig, response_schema: responseSchema,
      });
      setSuccessMsg('Configuration saved. Active snapshot invalidated.');
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
    const updated = [...chatMessages, userMsg];
    setChatMessages(updated);
    setUserInput('');
    setChatLoading(true);
    try {
      const resp = await api.chatWithApp(appId, updated.map((m) => ({ role: m.role, content: m.content })), simulatedToken);
      const reply = resp.choices?.[0]?.message?.content || resp.message?.content || JSON.stringify(resp, null, 2);
      setChatMessages((prev) => [...prev, { role: 'assistant', content: reply }]);
    } catch (err: any) {
      setChatMessages((prev) => [...prev, { role: 'assistant', content: `[Error: ${err.message}]` }]);
    } finally {
      setChatLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20 text-neutral-600 text-sm gap-2">
        <RefreshCw className="w-4 h-4 animate-spin" />
        Loading workspace...
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
  -d '{"messages":[{"role":"user","content":"Hello!"}]}'`;

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
    <div className="space-y-4 max-w-[1100px]">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <button
            onClick={onBack}
            className="p-1.5 text-neutral-500 hover:text-neutral-200 hover:bg-neutral-800 rounded-lg transition-colors border border-neutral-800"
          >
            <ArrowLeft className="w-4 h-4" />
          </button>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-base font-bold text-neutral-100">{name || appId}</h1>
              <Badge variant="success">active v{currentVersion}</Badge>
            </div>
            <p className="text-[11px] font-mono text-neutral-600 mt-0.5">/v1/apps/{appId}/chat</p>
          </div>
        </div>

        {/* Sub-tabs */}
        <div className="flex items-center bg-neutral-900 border border-neutral-800 rounded-xl p-1">
          <TabButton active={activeTab === 'overview'} onClick={() => setActiveTab('overview')} icon={<Code2 className="w-3.5 h-3.5" />} label="Overview & SDK" />
          <TabButton active={activeTab === 'settings'} onClick={() => setActiveTab('settings')} icon={<Sliders className="w-3.5 h-3.5" />} label="Settings" />
          <TabButton active={activeTab === 'playground'} onClick={() => setActiveTab('playground')} icon={<Terminal className="w-3.5 h-3.5" />} label="Playground" />
        </div>
      </div>

      {/* Global alerts */}
      {successMsg && <Alert variant="success" onDismiss={() => setSuccessMsg(null)}>{successMsg}</Alert>}
      {errorMsg && <Alert variant="error" onDismiss={() => setErrorMsg(null)}>{errorMsg}</Alert>}

      {/* ─── TAB 1: Overview ─── */}
      {activeTab === 'overview' && (
        <div className="space-y-4">
          {/* App summary */}
          <Card padding="lg">
            <CardHeader
              title="Application Summary"
              icon={<Bot className="w-4 h-4 text-emerald-400" />}
            />
            <p className="text-xs text-neutral-500 mb-4">{description || 'No description configured.'}</p>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              {[
                { label: 'Logical Model', value: modelRoutingId || 'fast (default)', mono: true },
                { label: 'System Prompt', value: promptId ? `${promptId} (v${promptVersion})` : 'Pass-through', mono: true },
                { label: 'Active Tools', value: `${selectedTools.length} attached`, mono: false },
                { label: 'Rate Limit', value: `${rpmLimit} RPM`, mono: true },
              ].map((item) => (
                <div key={item.label} className="bg-neutral-950 border border-neutral-800/80 p-3 rounded-lg">
                  <p className="text-[10px] uppercase tracking-wider text-neutral-600 font-semibold">{item.label}</p>
                  <p className={`text-sm text-neutral-200 mt-1 ${item.mono ? 'font-mono' : 'font-medium'}`}>{item.value}</p>
                </div>
              ))}
            </div>
          </Card>

          {/* Integration Snippets */}
          <Card padding="lg">
            <CardHeader
              title="Client Integration Snippets"
              icon={<Code2 className="w-4 h-4 text-blue-400" />}
              description="Pass the authenticated user's JWT in the Authorization header"
            />
            <div className="space-y-4">
              <CodeBlock code={curlSnippet} label="cURL / HTTP Request" />
              <CodeBlock code={tsSnippet} label="TypeScript / Web SDK" />
            </div>
          </Card>
        </div>
      )}

      {/* ─── TAB 2: Settings ─── */}
      {activeTab === 'settings' && (
        <form onSubmit={handleSaveSettings} className="space-y-4">
          {/* General */}
          <Card padding="lg">
            <SectionTitle icon={<Bot className="w-4 h-4" />} title="General & Model Assignment" />
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <Input label="Display Name" required value={name} onChange={(e) => setName(e.target.value)} />
              <Select
                label="Logical Model Routing"
                value={modelRoutingId}
                onChange={(e) => setModelRoutingId(e.target.value)}
                options={[
                  { value: '', label: '(Default: fast)' },
                  ...models.map((m) => ({ value: m.logical_model, label: `${m.logical_model} → ${m.deployment}` })),
                ]}
              />
            </div>
            <div className="mt-4">
              <Textarea label="Description" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mt-4">
              <Select
                label="System Prompt"
                value={promptId}
                onChange={(e) => setPromptId(e.target.value)}
                options={[
                  { value: '', label: '(None / Pass-through)' },
                  ...prompts.map((p) => ({ value: p.id, label: p.name })),
                ]}
              />
              <Input
                label="CEL Access Policy"
                placeholder="e.g. claims.ai_access == true && claims.plan in ['pro']"
                value={accessPolicy}
                onChange={(e) => setAccessPolicy(e.target.value)}
                className="font-mono text-xs"
              />
            </div>
          </Card>

          {/* Tools */}
          <Card padding="lg">
            <SectionTitle
              icon={<Wrench className="w-4 h-4" />}
              title="Attached Tools"
              description="Select which HTTP tools this app can call on behalf of users"
            />
            {availableTools.length === 0 ? (
              <p className="text-xs text-neutral-600 italic">No tools registered. Add tools in Tools Registry.</p>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
                {availableTools.map((tool) => {
                  const isChecked = selectedTools.includes(tool.id);
                  return (
                    <div
                      key={tool.id}
                      onClick={() => handleToolToggle(tool.id)}
                      className={`
                        cursor-pointer rounded-lg border p-3 flex items-start gap-2.5 transition-all
                        ${isChecked
                          ? 'bg-amber-500/8 border-amber-500/25 text-amber-300'
                          : 'bg-neutral-950 border-neutral-800 hover:border-neutral-700 text-neutral-500'
                        }
                      `}
                    >
                      <div className={`w-4 h-4 mt-0.5 rounded border flex items-center justify-center flex-shrink-0 ${isChecked ? 'bg-amber-500 border-amber-500' : 'border-neutral-700'}`}>
                        {isChecked && <Check className="w-2.5 h-2.5 text-white" />}
                      </div>
                      <div className="min-w-0">
                        <p className="text-xs font-semibold text-neutral-200">{tool.name}</p>
                        <p className="text-[10px] font-mono text-neutral-600 truncate">{tool.id}</p>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </Card>

          {/* Rate Limits */}
          <Card padding="lg">
            <SectionTitle
              icon={<Zap className="w-4 h-4" />}
              title="Rate Limiting & CEL Tiers"
              description="Evaluated per caller keyed by project:issuer:subject using atomic sliding windows in Redis"
            />
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
              <Input
                label="Default Inference Rate (RPM)"
                type="number"
                min="1"
                value={rpmLimit}
                onChange={(e) => setRpmLimit(Number(e.target.value))}
                hint="Requests per minute sliding window"
                className="font-mono"
              />
              <Input
                label="Daily Token Quota"
                type="number"
                min="1000"
                step="5000"
                value={dailyTokenLimit}
                onChange={(e) => setDailyTokenLimit(Number(e.target.value))}
                hint="Pre-allocated tokens per 24h period"
                className="font-mono"
              />
            </div>
            <div className="border-t border-neutral-800/80 pt-4">
              <Toggle
                checked={enableTierCondition}
                onChange={setEnableTierCondition}
                label="Enable CEL Plan Tier Override"
                description="Apply different limits to users matching a CEL condition"
              />
              {enableTierCondition && (
                <div className="grid grid-cols-3 gap-3 mt-3 p-3 bg-neutral-950 border border-neutral-800 rounded-lg">
                  <div className="col-span-2">
                    <Input
                      label="Tier Match Condition (CEL)"
                      value={tierExpression}
                      onChange={(e) => setTierExpression(e.target.value)}
                      placeholder="claims.plan == 'pro'"
                      className="font-mono text-xs text-emerald-400"
                    />
                  </div>
                  <Input
                    label="Tier Rate Limit (RPM)"
                    type="number"
                    value={proTierRpm}
                    onChange={(e) => setProTierRpm(Number(e.target.value))}
                    className="font-mono"
                  />
                </div>
              )}
            </div>
          </Card>

          {/* Guardrails */}
          <Card padding="lg">
            <SectionTitle icon={<Shield className="w-4 h-4" />} title="Safety & Guardrails" />
            <div className="space-y-3 mb-4">
              <Toggle
                checked={guardrails.pii_redaction ?? true}
                onChange={(v) => setGuardrails((prev) => ({ ...prev, pii_redaction: v, pii_detection: v }))}
                label="PII Detection & Redaction"
                description="Luhn credit cards, SSNs, emails, phone numbers"
              />
              <Toggle
                checked={guardrails.prompt_injection_defense ?? true}
                onChange={(v) => setGuardrails((prev) => ({ ...prev, prompt_injection_defense: v }))}
                label="Prompt Injection Defense"
                description="Heuristic jailbreak & system override guard"
              />
            </div>
            <div className="grid grid-cols-3 gap-3">
              <Input
                label="Max Input Length (chars)"
                type="number"
                value={guardrails.max_input_length || 8000}
                onChange={(e) => setGuardrails((prev) => ({ ...prev, max_input_length: Number(e.target.value) }))}
                className="font-mono"
              />
              <Input
                label="Max Output Tokens"
                type="number"
                value={guardrails.max_output_tokens || 4096}
                onChange={(e) => setGuardrails((prev) => ({ ...prev, max_output_tokens: Number(e.target.value) }))}
                className="font-mono"
              />
              <Select
                label="Streaming Mode"
                value={guardrails.streaming_mode || 'buffered'}
                onChange={(e) => setGuardrails((prev) => ({ ...prev, streaming_mode: e.target.value as any }))}
                options={[
                  { value: 'buffered', label: 'Buffered (Strict Safety)' },
                  { value: 'passthrough_with_post_check', label: 'Passthrough (Low Latency)' },
                ]}
              />
            </div>
          </Card>

          {/* Caching */}
          <Card padding="lg">
            <SectionTitle
              icon={<Clock className="w-4 h-4" />}
              title="Response Caching"
              description="Ephemeral Redis cache for repeat completions"
            />
            <div className="mb-4">
              <Toggle
                checked={cacheConfig.enabled ?? true}
                onChange={(v) => setCacheConfig((prev) => ({ ...prev, enabled: v }))}
                label="Enable Response Cache"
                description="Sub-millisecond repeat completions via Redis"
              />
            </div>
            <div className="grid grid-cols-3 gap-3">
              <Input
                label="Cache TTL (seconds)"
                type="number"
                min="0"
                value={cacheConfig.ttl_seconds || 300}
                onChange={(e) => setCacheConfig((prev) => ({ ...prev, ttl_seconds: Number(e.target.value) }))}
                className="font-mono"
              />
              <Select
                label="Cache Scope"
                value={cacheConfig.scope || 'user'}
                onChange={(e) => setCacheConfig((prev) => ({ ...prev, scope: e.target.value as any }))}
                options={[
                  { value: 'user', label: 'User (Caller-isolated)' },
                  { value: 'shared', label: 'Shared (Project-wide)' },
                ]}
              />
              <div className="flex items-end pb-1">
                <Toggle
                  checked={cacheConfig.tool_caching ?? true}
                  onChange={(v) => setCacheConfig((prev) => ({ ...prev, tool_caching: v }))}
                  label="Tool Caching"
                  description="Cache tool call results"
                />
              </div>
            </div>
          </Card>

          {/* Structured Output */}
          <Card padding="lg">
            <SectionTitle
              icon={<FileCode className="w-4 h-4" />}
              title="Structured Output Schema (Optional)"
              description="JSON Schema constraining model output format"
            />
            <Textarea
              rows={5}
              placeholder={'{\n  "type": "object",\n  "properties": {\n    "summary": { "type": "string" }\n  }\n}'}
              value={responseSchema}
              onChange={(e) => setResponseSchema(e.target.value)}
              mono
            />
          </Card>

          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="ghost" size="sm" onClick={onBack}>Cancel</Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              loading={saving}
              icon={<Save className="w-3.5 h-3.5" />}
            >
              Save Settings
            </Button>
          </div>
        </form>
      )}

      {/* ─── TAB 3: Playground ─── */}
      {activeTab === 'playground' && (
        <div className="space-y-3">
          {/* JWT input */}
          <Card padding="sm">
            <Input
              label="Simulated Bearer JWT (optional)"
              placeholder="Paste end-user JWT token to test identity, CEL policies, and rate limits"
              value={simulatedToken}
              onChange={(e) => setSimulatedToken(e.target.value)}
              className="font-mono text-xs"
            />
          </Card>

          {/* Chat area */}
          <Card padding="none" className="flex flex-col" style={{ height: '480px' } as React.CSSProperties}>
            <div className="flex items-center justify-between px-4 py-3 border-b border-neutral-800">
              <div className="flex items-center gap-2">
                <Terminal className="w-4 h-4 text-neutral-500" />
                <span className="text-xs font-semibold text-neutral-300">Interactive Test Playground</span>
                {chatLoading && (
                  <span className="text-[10px] text-neutral-600 animate-pulse">Generating...</span>
                )}
              </div>
              <Button
                size="xs"
                variant="ghost"
                onClick={() => setChatMessages([{ role: 'assistant', content: 'Chat reset.' }])}
                icon={<Trash2 className="w-3 h-3" />}
              >
                Reset
              </Button>
            </div>

            {/* Messages */}
            <div className="flex-1 overflow-y-auto px-4 py-3 space-y-3">
              {chatMessages.map((msg, idx) => {
                const isUser = msg.role === 'user';
                return (
                  <div key={idx} className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
                    <div
                      className={`
                        max-w-[75%] rounded-2xl px-3.5 py-2.5 text-xs leading-relaxed whitespace-pre-wrap
                        ${isUser
                          ? 'bg-emerald-600 text-white rounded-br-none'
                          : 'bg-neutral-800 text-neutral-200 rounded-bl-none border border-neutral-700/60'
                        }
                      `}
                    >
                      {msg.content}
                    </div>
                  </div>
                );
              })}
              {chatLoading && (
                <div className="flex justify-start">
                  <div className="bg-neutral-800 border border-neutral-700/60 text-neutral-500 rounded-2xl rounded-bl-none px-3.5 py-2.5 text-xs flex items-center gap-1.5">
                    <span className="w-1.5 h-1.5 bg-neutral-500 rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
                    <span className="w-1.5 h-1.5 bg-neutral-500 rounded-full animate-bounce" style={{ animationDelay: '100ms' }} />
                    <span className="w-1.5 h-1.5 bg-neutral-500 rounded-full animate-bounce" style={{ animationDelay: '200ms' }} />
                  </div>
                </div>
              )}
              <div ref={chatEndRef} />
            </div>

            {/* Input */}
            <div className="border-t border-neutral-800 p-3">
              <form onSubmit={handleSendMessage} className="flex gap-2">
                <input
                  type="text"
                  placeholder="Ask anything..."
                  value={userInput}
                  onChange={(e) => setUserInput(e.target.value)}
                  className="flex-1 bg-neutral-950 border border-neutral-800 rounded-xl px-4 py-2.5 text-xs text-neutral-100 placeholder-neutral-600 focus:outline-none focus:border-neutral-600"
                />
                <Button
                  type="submit"
                  variant="primary"
                  size="xs"
                  disabled={chatLoading || !userInput.trim()}
                  icon={<Send className="w-3.5 h-3.5" />}
                >
                  Send
                </Button>
              </form>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
};
