import React, { useEffect, useState } from 'react';
import { api, Tool, Policy, SearchHealthStatus } from '../api/client';
import {
  Wrench,
  Plus,
  Trash2,
  Shield,
  KeyRound,
  Globe,
  Play,
  CheckCircle2,
  XCircle,
  Database,
  Server,
  Lock,
  Layers,
  Pencil,
  GitBranch,
  Search,
  RefreshCw,
  ExternalLink,
  Cpu,
  AlertCircle,
} from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Input, Textarea, Select, Toggle } from '../components/ui/Form';
import { Alert, EmptyState, Loading } from '../components/ui/Feedback';
import { formatRelativeTime } from '../lib/utils';

export const ToolsPage: React.FC = () => {
  const [activeTab, setActiveTab] = useState<'registry' | 'searxng'>('registry');
  const [tools, setTools] = useState<Tool[]>([]);
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [loading, setLoading] = useState(true);

  // SearXNG Search Engine State
  const [searchHealth, setSearchHealth] = useState<SearchHealthStatus | null>(null);
  const [checkingHealth, setCheckingHealth] = useState(false);
  const [searchQuery, setSearchQuery] = useState('beavend ai runtime');
  const [searchCategory, setSearchCategory] = useState('general');
  const [searchMaxResults, setSearchMaxResults] = useState(5);
  const [searchLoading, setSearchLoading] = useState(false);
  const [searchResults, setSearchResults] = useState<any>(null);
  const [searchTestError, setSearchTestError] = useState<string | null>(null);
  const [showModal, setShowModal] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  // History state
  const [selectedToolForHistory, setSelectedToolForHistory] = useState<string | null>(null);
  const [toolDetail, setToolDetail] = useState<{ tool: Tool; versions: any[] } | null>(null);
  const [loadingHistory, setLoadingHistory] = useState(false);

  // Secret management
  const [showSecretModal, setShowSecretModal] = useState(false);
  const [selectedToolForSecret, setSelectedToolForSecret] = useState<string>('');
  const [secretRef, setSecretRef] = useState('');
  const [secretValue, setSecretValue] = useState('');
  const [savingSecret, setSavingSecret] = useState(false);

  // Test tool
  const [showTestModal, setShowTestModal] = useState(false);
  const [selectedToolForTest, setSelectedToolForTest] = useState<string>('');
  const [testArgs, setTestArgs] = useState('{\n  "query": "test search term"\n}');
  const [testSubject, setTestSubject] = useState('usr_123');
  const [testResult, setTestResult] = useState<any>(null);
  const [testError, setTestError] = useState<string | null>(null);
  const [testRunning, setTestRunning] = useState(false);

  // Form state
  const [toolId, setToolId] = useState('');
  const [toolName, setToolName] = useState('');
  const [toolDesc, setToolDesc] = useState('');
  const [sideEffect, setSideEffect] = useState<'read' | 'write'>('read');
  const [confirmation, setConfirmation] = useState<'none' | 'required'>('none');
  const [toolAuthPolicy, setToolAuthPolicy] = useState('');
  const [cacheEnabled, setCacheEnabled] = useState(false);
  const [cacheTtlSeconds, setCacheTtlSeconds] = useState(300);
  const [inputSchema, setInputSchema] = useState(
    '{\n  "type": "object",\n  "properties": {\n    "query": { "type": "string" }\n  },\n  "required": ["query"]\n}'
  );
  const [identityBindings, setIdentityBindings] = useState('{\n  "user_id": "claims.sub"\n}');

  // Executor settings
  const [executorType, setExecutorType] = useState<'http' | 'webhook' | 'mcp' | 'sql_template'>('http');
  const [executorUrl, setExecutorUrl] = useState('https://api.example.com/search');
  const [executorMethod, setExecutorMethod] = useState('POST');
  const [timeoutMs, setTimeoutMs] = useState(5000);
  const [retries, setRetries] = useState(0);

  // MCP specific
  const [mcpUrl, setMcpUrl] = useState('http://localhost:8000/sse');

  // SQL Template specific
  const [sqlDriver, setSqlDriver] = useState<'pgx' | 'postgres'>('pgx');
  const [sqlDsn, setSqlDsn] = useState('postgres://postgres:postgres@localhost:5432/app_db?sslmode=disable');
  const [sqlTemplate, setSqlTemplate] = useState('SELECT id, status, total_amount FROM orders WHERE customer_id = :user_id LIMIT 10;');

  // Downstream Auth
  const [authMode, setAuthMode] = useState<'none' | 'forward_user_token' | 'service_token' | 'signed_headers'>('forward_user_token');
  const [authSecretRef, setAuthSecretRef] = useState('');
  const [authHeaderName, setAuthHeaderName] = useState('');

  // Result config
  const [maxBytes, setMaxBytes] = useState(50000);
  const [redactFields, setRedactFields] = useState('');

  const loadData = async () => {
    setLoading(true);
    try {
      const [tList, pList] = await Promise.all([
        api.listTools().catch(() => []),
        api.listPolicies().catch(() => []),
      ]);
      setTools(tList);
      setPolicies(pList);
      api.getSearchHealth().then(setSearchHealth).catch(() => {});
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadData(); }, []);

  const checkSearxngHealth = async () => {
    setCheckingHealth(true);
    try {
      const status = await api.getSearchHealth();
      setSearchHealth(status);
    } catch (err: any) {
      setSearchHealth({
        status: 'error',
        url: '',
        configured: false,
        enabled: false,
        error: err.message || 'Failed to connect to search health endpoint',
      });
    } finally {
      setCheckingHealth(false);
    }
  };

  const handleTestSearch = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!searchQuery.trim()) return;
    setSearchLoading(true);
    setSearchTestError(null);
    setSearchResults(null);
    try {
      const cats = searchCategory && searchCategory !== 'all' ? [searchCategory] : undefined;
      const res = await api.testSearch(searchQuery.trim(), cats, searchMaxResults);
      setSearchResults(res);
    } catch (err: any) {
      setSearchTestError(err.message || 'Search test failed');
    } finally {
      setSearchLoading(false);
    }
  };

  const handleSideEffectChange = (val: 'read' | 'write') => {
    setSideEffect(val);
    if (val === 'write') {
      setConfirmation('required');
      setCacheEnabled(false);
    }
  };

  const openCreateModal = () => {
    setIsEditing(false);
    setError(null);
    setToolId('');
    setToolName('');
    setToolDesc('');
    setSideEffect('read');
    setConfirmation('none');
    setToolAuthPolicy('');
    setCacheEnabled(false);
    setCacheTtlSeconds(300);
    setInputSchema('{\n  "type": "object",\n  "properties": {\n    "query": { "type": "string" }\n  },\n  "required": ["query"]\n}');
    setIdentityBindings('{\n  "user_id": "claims.sub"\n}');
    setExecutorType('http');
    setExecutorUrl('https://api.example.com/search');
    setExecutorMethod('POST');
    setTimeoutMs(5000);
    setRetries(0);
    setMcpUrl('http://localhost:8000/sse');
    setSqlDriver('pgx');
    setSqlDsn('postgres://postgres:postgres@localhost:5432/app_db?sslmode=disable');
    setSqlTemplate('SELECT id, status, total_amount FROM orders WHERE customer_id = :user_id LIMIT 10;');
    setAuthMode('forward_user_token');
    setAuthSecretRef('');
    setAuthHeaderName('');
    setMaxBytes(50000);
    setRedactFields('');
    setShowModal(true);
  };

  const openEditModal = async (id: string, versionData?: any) => {
    setError(null);
    try {
      let activeVersion = versionData;
      let toolNameVal = toolName;
      let toolDescVal = toolDesc;

      if (!activeVersion) {
        const data = await api.getTool(id);
        const tool = data.tool;
        toolNameVal = tool.name;
        toolDescVal = tool.description || '';
        activeVersion = data.versions?.find((v: any) => v.version === tool.active_version) || data.versions?.[0];
      }

      setIsEditing(true);
      setToolId(id);
      setToolName(toolNameVal);
      setToolDesc(toolDescVal);

      if (activeVersion) {
        setSideEffect(activeVersion.side_effect || 'read');
        setConfirmation(activeVersion.confirmation || 'none');
        setToolAuthPolicy(activeVersion.authorization || '');
        if (activeVersion.cache_config) {
          setCacheEnabled(Boolean(activeVersion.cache_config.enabled));
          setCacheTtlSeconds(activeVersion.cache_config.ttl_seconds || 300);
        } else {
          setCacheEnabled(false);
          setCacheTtlSeconds(300);
        }
        setInputSchema(JSON.stringify(activeVersion.input_schema || {}, null, 2));
        setIdentityBindings(JSON.stringify(activeVersion.identity_bindings || {}, null, 2));

        const exec = activeVersion.executor || {};
        setExecutorType(exec.type || 'http');
        if (exec.url) {
          setExecutorUrl(exec.url);
          setMcpUrl(exec.url);
        }
        if (exec.method) setExecutorMethod(exec.method);
        if (exec.timeout_ms) setTimeoutMs(exec.timeout_ms);
        if (exec.retries !== undefined) setRetries(exec.retries);
        if (exec.template) setSqlTemplate(exec.template);
        if (exec.headers?.driver) setSqlDriver(exec.headers.driver);
        if (exec.headers?.dsn) setSqlDsn(exec.headers.dsn);

        const auth = activeVersion.downstream_auth;
        if (auth && auth.mode) {
          setAuthMode(auth.mode);
          setAuthSecretRef(auth.secret_ref || '');
          setAuthHeaderName(auth.header_name || '');
        } else {
          setAuthMode('none');
          setAuthSecretRef('');
          setAuthHeaderName('');
        }

        const resCfg = activeVersion.result_config;
        if (resCfg) {
          if (resCfg.max_bytes) setMaxBytes(resCfg.max_bytes);
          if (resCfg.redact) setRedactFields(resCfg.redact.join(', '));
        }
      }

      setShowModal(true);
    } catch (err: any) {
      alert(`Failed to load tool details: ${err.message}`);
    }
  };

  const openHistory = async (id: string) => {
    setSelectedToolForHistory(id);
    setLoadingHistory(true);
    try {
      const data = await api.getTool(id);
      setToolDetail(data);
    } catch (_) {
      setToolDetail(null);
    } finally {
      setLoadingHistory(false);
    }
  };

  const handleSetActiveToolVersion = async (version: number) => {
    if (!selectedToolForHistory) return;
    try {
      await api.setActiveToolVersion(selectedToolForHistory, version);
      await openHistory(selectedToolForHistory);
      loadData();
    } catch (err: any) {
      alert(`Failed to set active version: ${err.message}`);
    }
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setCreating(true);
    try {
      let parsedSchema = {}, parsedBindings = {};
      try { parsedSchema = JSON.parse(inputSchema); } catch { throw new Error('Input Schema is not valid JSON'); }
      try { parsedBindings = JSON.parse(identityBindings); } catch { throw new Error('Identity Bindings is not valid JSON'); }

      // Build executor configuration
      let executorConfig: any = { type: executorType, timeout_ms: Number(timeoutMs) };
      if (executorType === 'http' || executorType === 'webhook') {
        executorConfig.url = executorUrl;
        executorConfig.method = executorMethod;
        executorConfig.retries = Number(retries);
      } else if (executorType === 'mcp') {
        executorConfig.url = mcpUrl;
      } else if (executorType === 'sql_template') {
        executorConfig.template = sqlTemplate;
        executorConfig.headers = { driver: sqlDriver, dsn: sqlDsn };
      }

      // Build downstream auth configuration
      let downstreamAuth: any = undefined;
      if (authMode !== 'none') {
        downstreamAuth = { mode: authMode };
        if (authSecretRef.trim()) downstreamAuth.secret_ref = authSecretRef.trim();
        if (authHeaderName.trim()) downstreamAuth.header_name = authHeaderName.trim();
      }

      // Build result config
      let resultConfig: any = undefined;
      if (maxBytes > 0 || redactFields.trim()) {
        resultConfig = {
          max_bytes: Number(maxBytes),
          redact: redactFields.split(',').map((s) => s.trim()).filter(Boolean),
        };
      }

      // Build cache config
      let cacheConfig: any = undefined;
      if (cacheEnabled) {
        cacheConfig = {
          enabled: true,
          ttl_seconds: Number(cacheTtlSeconds) || 300,
          strategy: 'exact',
        };
      } else {
        cacheConfig = { enabled: false };
      }

      const payload = {
        id: toolId,
        name: toolName,
        description: toolDesc,
        side_effect: sideEffect,
        confirmation,
        input_schema: parsedSchema,
        identity_bindings: parsedBindings,
        executor: executorConfig,
        downstream_auth: downstreamAuth,
        result_config: resultConfig,
        authorization: toolAuthPolicy.trim() || undefined,
        cache_config: cacheConfig,
      };

      if (isEditing) {
        await api.updateTool(toolId, payload);
      } else {
        await api.createTool(payload);
      }

      setShowModal(false);
      if (selectedToolForHistory === toolId) {
        openHistory(toolId);
      }
      loadData();
    } catch (err: any) {
      setError(err.message || 'Failed to save tool');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(`Delete Tool "${id}"? This will invalidate active snapshots.`)) return;
    try {
      await api.deleteTool(id);
      loadData();
    } catch (err: any) {
      alert(`Error deleting tool: ${err.message}`);
    }
  };

  const handleSaveSecret = async (e: React.FormEvent) => {
    e.preventDefault();
    setSavingSecret(true);
    try {
      await api.upsertToolSecret(selectedToolForSecret, secretRef, secretValue);
      setShowSecretModal(false);
      setSecretRef(''); setSecretValue('');
    } catch (err: any) {
      alert(`Failed to save secret: ${err.message}`);
    } finally {
      setSavingSecret(false);
    }
  };

  const handleTestTool = async (e: React.FormEvent) => {
    e.preventDefault();
    setTestRunning(true);
    setTestResult(null);
    setTestError(null);
    try {
      let parsedArgs = {};
      try { parsedArgs = JSON.parse(testArgs); } catch { throw new Error('Arguments must be valid JSON'); }

      let issuerUrl = 'https://auth.example.com';
      try {
        const authCfg = await api.getEnvironmentAuthConfig(api.currentProject, api.currentEnv);
        if (authCfg?.issuer) issuerUrl = authCfg.issuer;
      } catch (_) {}

      const payload: any = { arguments: parsedArgs };
      if (testSubject.trim()) {
        payload.identity = {
          subject: testSubject.trim(),
          issuer: issuerUrl,
          claims: { sub: testSubject.trim(), user_id: testSubject.trim(), iss: issuerUrl },
        };
      }

      const result = await api.testTool(selectedToolForTest, payload);
      setTestResult(result);
    } catch (err: any) {
      setTestError(err.message);
    } finally {
      setTestRunning(false);
    }
  };

  return (
    <div className="space-y-5 max-w-[1100px]">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
            <Wrench className="w-5 h-5 text-neutral-400" />
            Tools & Search
          </h1>
          <p className="text-sm text-neutral-500 mt-0.5">
            SSRF-hardened tool executors, parameterized SQL templates, and self-hosted SearXNG meta-search engine.
          </p>
        </div>
        {activeTab === 'registry' ? (
          <Button variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />} onClick={openCreateModal}>
            Register Tool
          </Button>
        ) : (
          <Button
            variant="secondary"
            size="sm"
            icon={<RefreshCw className={`w-3.5 h-3.5 ${checkingHealth ? 'animate-spin' : ''}`} />}
            onClick={checkSearxngHealth}
            disabled={checkingHealth}
          >
            Check Health
          </Button>
        )}
      </div>

      {/* Navigation Tabs */}
      <div className="flex border-b border-neutral-800 gap-6">
        <button
          onClick={() => setActiveTab('registry')}
          className={`pb-3 text-sm font-medium transition-colors flex items-center gap-2 border-b-2 ${
            activeTab === 'registry'
              ? 'border-white text-white'
              : 'border-transparent text-neutral-400 hover:text-neutral-200'
          }`}
        >
          <Wrench className="w-4 h-4" />
          Custom Tools Registry
          <Badge variant="mono">{tools.length}</Badge>
        </button>
        <button
          onClick={() => {
            setActiveTab('searxng');
            if (!searchHealth) checkSearxngHealth();
          }}
          className={`pb-3 text-sm font-medium transition-colors flex items-center gap-2 border-b-2 ${
            activeTab === 'searxng'
              ? 'border-white text-white'
              : 'border-transparent text-neutral-400 hover:text-neutral-200'
          }`}
        >
          <Search className="w-4 h-4" />
          Search Engine (SearXNG)
          {searchHealth?.status === 'healthy' ? (
            <Badge variant="success" dot>Online</Badge>
          ) : searchHealth?.status === 'disabled' ? (
            <Badge variant="default">Disabled</Badge>
          ) : searchHealth?.status ? (
            <Badge variant="danger" dot>Offline</Badge>
          ) : null}
        </button>
      </div>

      {activeTab === 'registry' ? (
        <>
          {loading ? (
            <Loading text="Loading registered tools..." />
          ) : tools.length === 0 ? (
            <EmptyState
              icon={<Wrench className="w-6 h-6" />}
              title="No Custom Tools registered"
              description="Register external API endpoints, MCP servers, or parameterized SQL queries for LLM function-calling with SSRF protection and identity binding."
              action={
                <Button variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />} onClick={openCreateModal}>
                  Register Tool
                </Button>
              }
            />
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
              {tools.map((t) => (
                <Card key={t.id} padding="md" className="group">
                  <div className="flex items-start justify-between mb-3">
                    <div className="w-8 h-8 rounded-lg bg-amber-500/10 border border-amber-500/15 flex items-center justify-center">
                      <Wrench className="w-4 h-4 text-amber-400" />
                    </div>
                    <div className="flex items-center gap-1.5 opacity-0 group-hover:opacity-100 transition-opacity">
                      <button
                        onClick={() => openEditModal(t.id)}
                        className="p-1 text-neutral-600 hover:text-blue-400 hover:bg-neutral-800 rounded transition-colors"
                        title="Edit tool & create new version"
                      >
                        <Pencil className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => { setSelectedToolForTest(t.id); setTestResult(null); setTestError(null); setShowTestModal(true); }}
                        className="p-1 text-neutral-600 hover:text-emerald-400 hover:bg-neutral-800 rounded transition-colors"
                        title="Test tool execution"
                      >
                        <Play className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => { setSelectedToolForSecret(t.id); setShowSecretModal(true); }}
                        className="p-1 text-neutral-600 hover:text-amber-400 hover:bg-neutral-800 rounded transition-colors"
                        title="Manage secrets"
                      >
                        <KeyRound className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => handleDelete(t.id)}
                        className="p-1 text-neutral-600 hover:text-red-400 hover:bg-neutral-800 rounded transition-colors"
                        title="Delete tool"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  </div>

                  <h3 className="text-sm font-semibold text-neutral-100 mb-0.5">{t.name}</h3>
                  <p className="text-[11px] font-mono text-neutral-600 mb-2">{t.id}</p>
                  <p className="text-xs text-neutral-500 line-clamp-2 mb-3">
                    {t.description || 'No description provided.'}
                  </p>

                  <div className="flex items-center gap-1.5 flex-wrap">
                    <Badge variant="mono">v{t.active_version}</Badge>
                    <Badge variant="success">
                      <Shield className="w-2.5 h-2.5" />
                      SSRF Guard
                    </Badge>
                  </div>

                  <div className="pt-2.5 mt-2.5 border-t border-neutral-800/80 flex items-center justify-between text-[10px] text-neutral-700">
                    <button
                      type="button"
                      onClick={() => openHistory(t.id)}
                      className="flex items-center gap-1 text-emerald-600 hover:text-emerald-400 transition-colors"
                    >
                      <GitBranch className="w-3 h-3" />
                      View versions
                    </button>
                  </div>
                </Card>
              ))}
            </div>
          )}
        </>
      ) : (
        /* SearXNG Tab */
        <div className="space-y-5">
          {/* Health & Status Cards */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
            <Card padding="md" className="lg:col-span-1">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-semibold uppercase tracking-wider text-neutral-500">Service Status</span>
                {searchHealth?.status === 'healthy' ? (
                  <Badge variant="success" dot>Online</Badge>
                ) : searchHealth?.status === 'disabled' ? (
                  <Badge variant="default">Disabled</Badge>
                ) : (
                  <Badge variant="danger" dot>Connection Failed</Badge>
                )}
              </div>
              <div className="space-y-2.5 text-xs">
                <div>
                  <span className="text-neutral-500 block text-[11px]">Backend URL</span>
                  <code className="text-neutral-200 font-mono text-[11px] bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800 block truncate">
                    {searchHealth?.url || 'http://searxng:8080'}
                  </code>
                </div>
                <div className="flex justify-between items-center pt-1 border-t border-neutral-800/60">
                  <span className="text-neutral-500">Roundtrip Latency</span>
                  <span className="font-mono text-neutral-200">
                    {searchHealth?.latency_ms !== undefined ? `${searchHealth.latency_ms} ms` : '-'}
                  </span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-neutral-500">Engine Output</span>
                  <span className="font-mono text-neutral-200">JSON Enabled</span>
                </div>
                {searchHealth?.error && (
                  <div className="p-2 rounded bg-red-950/40 border border-red-800/50 text-red-300 text-[11px] mt-2 flex items-start gap-1.5">
                    <AlertCircle className="w-3.5 h-3.5 mt-0.5 shrink-0" />
                    <span>{searchHealth.error}</span>
                  </div>
                )}
              </div>
            </Card>

            <Card padding="md" className="lg:col-span-2">
              <div className="flex items-center gap-2 mb-2">
                <Shield className="w-4 h-4 text-emerald-400" />
                <h3 className="text-sm font-semibold text-neutral-200">Clean AGPL-3.0 License Isolation & Architecture</h3>
              </div>
              <p className="text-xs text-neutral-400 leading-relaxed mb-3">
                SearXNG runs as an autonomous, self-hosted meta-search microservice in the Docker Compose network. Beavend interacts strictly via standard HTTP REST API endpoints without linking any GPL/AGPL libraries, keeping Beavend's Apache/MIT codebase unpolluted.
              </p>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs">
                <div className="p-2.5 rounded-lg bg-neutral-900/80 border border-neutral-800">
                  <span className="font-medium text-neutral-300 block mb-0.5">Per-App Opt-In</span>
                  <p className="text-neutral-500 text-[11px]">
                    Web search is enabled per AI App. When active, <code className="text-neutral-400">web_search</code> is automatically injected into the agent loop.
                  </p>
                </div>
                <div className="p-2.5 rounded-lg bg-neutral-900/80 border border-neutral-800">
                  <span className="font-medium text-neutral-300 block mb-0.5">Graceful Degradation</span>
                  <p className="text-neutral-500 text-[11px]">
                    Network timeouts, upstream 5xx, or empty results notify the LLM cleanly rather than crashing the chat stream.
                  </p>
                </div>
              </div>
            </Card>
          </div>

          {/* Interactive Search Console */}
          <Card padding="md">
            <div className="flex items-center justify-between mb-4">
              <div>
                <h2 className="text-sm font-semibold text-neutral-200 flex items-center gap-2">
                  <Play className="w-4 h-4 text-blue-400" />
                  Live Search Tester
                </h2>
                <p className="text-xs text-neutral-500 mt-0.5">
                  Test SearXNG query execution, engine aggregation, and JSON payload parsing in real time.
                </p>
              </div>
            </div>

            <form onSubmit={handleTestSearch} className="space-y-4">
              <div className="grid grid-cols-1 md:grid-cols-12 gap-3">
                <div className="md:col-span-6">
                  <Input
                    label="Search Query"
                    required
                    placeholder="e.g. latest advancements in quantum computing"
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                  />
                </div>
                <div className="md:col-span-3">
                  <Select
                    label="Category"
                    value={searchCategory}
                    onChange={(e) => setSearchCategory(e.target.value)}
                    options={[
                      { value: 'all', label: 'All Categories' },
                      { value: 'general', label: 'General' },
                      { value: 'science', label: 'Science' },
                      { value: 'it', label: 'IT / Code' },
                      { value: 'news', label: 'News' },
                    ]}
                  />
                </div>
                <div className="md:col-span-3">
                  <Input
                    label="Max Results"
                    type="number"
                    min="1"
                    max="20"
                    value={searchMaxResults}
                    onChange={(e) => setSearchMaxResults(parseInt(e.target.value) || 5)}
                  />
                </div>
              </div>

              <div className="flex justify-end">
                <Button
                  type="submit"
                  variant="primary"
                  size="sm"
                  icon={<Search className="w-3.5 h-3.5" />}
                  disabled={searchLoading || !searchQuery.trim()}
                >
                  {searchLoading ? 'Executing Search...' : 'Run Search Query'}
                </Button>
              </div>
            </form>

            {searchTestError && (
              <Alert variant="error" className="mt-4" onDismiss={() => setSearchTestError(null)}>
                {searchTestError}
              </Alert>
            )}

            {searchResults && (
              <div className="mt-5 pt-4 border-t border-neutral-800 space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-neutral-300">
                    Results ({searchResults.number_of_results || searchResults.results?.length || 0})
                  </span>
                  {searchResults.latency_ms !== undefined && (
                    <span className="text-[11px] font-mono text-neutral-500">
                      Elapsed: {searchResults.latency_ms} ms
                    </span>
                  )}
                </div>

                {(!searchResults.results || searchResults.results.length === 0) ? (
                  <p className="text-xs text-neutral-500 italic py-2">No results returned for this query.</p>
                ) : (
                  <div className="space-y-2 max-h-[420px] overflow-y-auto pr-1">
                    {searchResults.results.map((r: any, idx: number) => (
                      <div
                        key={idx}
                        className="p-3 rounded-lg bg-neutral-900/60 border border-neutral-800/80 hover:border-neutral-700 transition-colors"
                      >
                        <div className="flex items-start justify-between gap-3 mb-1">
                          <a
                            href={r.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-xs font-medium text-blue-400 hover:underline flex items-center gap-1.5"
                          >
                            <span>{r.title || r.url}</span>
                            <ExternalLink className="w-3 h-3 shrink-0" />
                          </a>
                          {r.engine && (
                            <Badge variant="mono">{r.engine}</Badge>
                          )}
                        </div>
                        <span className="text-[11px] font-mono text-emerald-500/80 block truncate mb-1.5">
                          {r.url}
                        </span>
                        {r.content && (
                          <p className="text-xs text-neutral-400 line-clamp-3 leading-relaxed">
                            {r.content}
                          </p>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </Card>
        </div>
      )}

      {/* Create / Edit Tool Modal */}
      <Modal
        open={showModal}
        onClose={() => { setShowModal(false); setError(null); }}
        title={isEditing ? `Edit Tool: ${toolName || toolId}` : "Register Custom Tool"}
        description={isEditing ? "Modify configuration to publish a new active version" : "Configure an execution engine with identity bindings and SSRF protection"}
        size="xl"
      >
        {error && <Alert variant="error" className="mb-4" onDismiss={() => setError(null)}>{error}</Alert>}
        <form onSubmit={handleSave} className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Input
              label="Tool Identifier (ID)"
              required
              disabled={isEditing}
              placeholder="e.g. search_orders"
              value={toolId}
              onChange={(e) => setToolId(e.target.value)}
              className="font-mono disabled:opacity-60"
            />
            <Input label="Display Name" required placeholder="e.g. Search Customer Orders" value={toolName} onChange={(e) => setToolName(e.target.value)} />
          </div>
          <Textarea label="Description (shown to LLM agent)" rows={2} placeholder="Describes the tool's purpose and when to use it..." value={toolDesc} onChange={(e) => setToolDesc(e.target.value)} />

          {/* Executor Engine Selector */}
          <div className="p-3 bg-neutral-900 border border-neutral-800 rounded-xl space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-neutral-300 flex items-center gap-1.5">
                <Server className="w-3.5 h-3.5 text-emerald-400" />
                Executor Type
              </span>
              <div className="flex rounded-lg bg-neutral-950 p-0.5 border border-neutral-800">
                {(['http', 'webhook', 'mcp', 'sql_template'] as const).map((type) => (
                  <button
                    key={type}
                    type="button"
                    onClick={() => setExecutorType(type)}
                    className={`px-2.5 py-1 text-xs rounded-md font-medium transition-all ${
                      executorType === type
                        ? 'bg-neutral-800 text-neutral-100 shadow-sm'
                        : 'text-neutral-500 hover:text-neutral-300'
                    }`}
                  >
                    {type === 'http' && 'HTTP'}
                    {type === 'webhook' && 'Webhook'}
                    {type === 'mcp' && 'MCP (JSON-RPC)'}
                    {type === 'sql_template' && 'SQL Template'}
                  </button>
                ))}
              </div>
            </div>

            {/* HTTP / Webhook Fields */}
            {(executorType === 'http' || executorType === 'webhook') && (
              <div className="space-y-3">
                <div className="grid grid-cols-3 gap-3">
                  <Input label="Downstream URL" required type="url" placeholder="https://api.internal.com/v1/search" value={executorUrl} onChange={(e) => setExecutorUrl(e.target.value)} className="font-mono text-xs col-span-2" />
                  <Select
                    label="HTTP Method"
                    value={executorMethod}
                    onChange={(e) => setExecutorMethod(e.target.value)}
                    options={[
                      { value: 'POST', label: 'POST' },
                      { value: 'GET', label: 'GET' },
                      { value: 'PUT', label: 'PUT' },
                      { value: 'PATCH', label: 'PATCH' },
                      { value: 'DELETE', label: 'DELETE' },
                    ]}
                  />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <Input label="Timeout (ms)" type="number" value={timeoutMs} onChange={(e) => setTimeoutMs(Number(e.target.value))} />
                  <Input label="Retries" type="number" min={0} max={3} value={retries} onChange={(e) => setRetries(Number(e.target.value))} />
                </div>
              </div>
            )}

            {/* MCP Fields */}
            {executorType === 'mcp' && (
              <div className="space-y-2">
                <Input
                  label="MCP Server Endpoint URL (HTTP / SSE)"
                  required
                  type="url"
                  placeholder="http://localhost:8000/sse"
                  value={mcpUrl}
                  onChange={(e) => setMcpUrl(e.target.value)}
                  className="font-mono text-xs"
                  hint="Dispatches JSON-RPC 2.0 tools/call with downstream auth headers and caller identity bindings."
                />
              </div>
            )}

            {/* SQL Template Fields */}
            {executorType === 'sql_template' && (
              <div className="space-y-3">
                <div className="grid grid-cols-3 gap-3">
                  <Select
                    label="SQL Driver"
                    value={sqlDriver}
                    onChange={(e) => setSqlDriver(e.target.value as any)}
                    options={[
                      { value: 'pgx', label: 'PostgreSQL (pgx)' },
                      { value: 'postgres', label: 'PostgreSQL (lib/pq)' },
                    ]}
                  />
                  <Input
                    label="Database DSN / Connection URL"
                    required
                    placeholder="postgres://user:pass@host:5432/db"
                    value={sqlDsn}
                    onChange={(e) => setSqlDsn(e.target.value)}
                    className="font-mono text-xs col-span-2"
                  />
                </div>
                <Textarea
                  label="Read-Only SQL Query Template"
                  rows={3}
                  required
                  mono
                  value={sqlTemplate}
                  onChange={(e) => setSqlTemplate(e.target.value)}
                  hint="Use :param placeholders (e.g. :user_id, :query). Mutating statements (INSERT, UPDATE, DELETE, DROP) are strictly blocked."
                />
              </div>
            )}
          </div>

          {/* Side effect & Confirmation */}
          <div className="grid grid-cols-2 gap-3">
            <Select
              label="Side Effect"
              value={sideEffect}
              onChange={(e) => handleSideEffectChange(e.target.value as 'read' | 'write')}
              options={[
                { value: 'read', label: 'read (Safe / Cacheable)' },
                { value: 'write', label: 'write (Mutating / Destructive)' },
              ]}
            />
            <Select
              label="Confirmation"
              value={confirmation}
              onChange={(e) => setConfirmation(e.target.value as 'none' | 'required')}
              options={[
                { value: 'none', label: 'none (Auto-execute)' },
                { value: 'required', label: 'required (Pause for approval)' },
              ]}
              hint={sideEffect === 'write' ? '⚠️ Security default: Write tools require confirmation' : undefined}
            />
          </div>

          {/* Authorization Policy & Caching */}
          <div className="p-3 bg-neutral-900 border border-neutral-800 rounded-xl space-y-3">
            <span className="text-xs font-semibold text-neutral-300 flex items-center gap-1.5">
              <Shield className="w-3.5 h-3.5 text-purple-400" />
              Authorization Policy & Result Caching
            </span>
            <div className="grid grid-cols-2 gap-3">
              <Select
                label="Authorization Policy (CEL)"
                value={toolAuthPolicy}
                onChange={(e) => setToolAuthPolicy(e.target.value)}
                options={[
                  { value: '', label: '(None / Inherit from App)' },
                  ...policies.map((p) => ({ value: p.name, label: `${p.name}` })),
                ]}
                hint="Enforces caller token policy before tool executes"
              />
              <div className="space-y-2">
                <Toggle
                  label="Cache Tool Results"
                  description="Cache idempotent responses in Redis"
                  checked={cacheEnabled}
                  onChange={setCacheEnabled}
                />
                {cacheEnabled && (
                  <Input
                    label="Cache TTL (seconds)"
                    type="number"
                    min={1}
                    value={cacheTtlSeconds}
                    onChange={(e) => setCacheTtlSeconds(Number(e.target.value))}
                    className="font-mono text-xs"
                  />
                )}
              </div>
            </div>
          </div>

          {/* Downstream Authentication */}
          <div className="p-3 bg-neutral-900 border border-neutral-800 rounded-xl space-y-3">
            <span className="text-xs font-semibold text-neutral-300 flex items-center gap-1.5">
              <Lock className="w-3.5 h-3.5 text-blue-400" />
              Downstream Authentication Strategy
            </span>
            <div className="grid grid-cols-3 gap-3">
              <Select
                label="Auth Mode"
                value={authMode}
                onChange={(e) => setAuthMode(e.target.value as any)}
                options={[
                  { value: 'forward_user_token', label: 'Forward User JWT' },
                  { value: 'service_token', label: 'Service Token (Secret)' },
                  { value: 'signed_headers', label: 'Signed HMAC Headers' },
                  { value: 'none', label: 'None (Unauthenticated)' },
                ]}
              />
              <Input
                label="Secret Reference"
                placeholder="e.g. downstream_api_key"
                value={authSecretRef}
                onChange={(e) => setAuthSecretRef(e.target.value)}
                className="font-mono text-xs"
                hint="Key used in Secrets Vault"
              />
              <Input
                label="Custom Header Name"
                placeholder="Authorization"
                value={authHeaderName}
                onChange={(e) => setAuthHeaderName(e.target.value)}
                className="font-mono text-xs"
                hint="Default: Authorization"
              />
            </div>
          </div>

          <Textarea
            label="Input JSON Schema"
            rows={4}
            required
            value={inputSchema}
            onChange={(e) => setInputSchema(e.target.value)}
            mono
          />
          <Textarea
            label="Identity Bindings (Confused-Deputy Defense)"
            rows={2}
            required
            value={identityBindings}
            onChange={(e) => setIdentityBindings(e.target.value)}
            mono
            hint="Map tool parameters to validated JWT claims, e.g. user_id → claims.sub"
          />

          <div className="grid grid-cols-2 gap-3">
            <Input
              label="Max Result Bytes"
              type="number"
              value={maxBytes}
              onChange={(e) => setMaxBytes(Number(e.target.value))}
              hint="Cap downstream response payload size"
            />
            <Input
              label="Redact Response Fields"
              placeholder="e.g. ssn, credit_card, token"
              value={redactFields}
              onChange={(e) => setRedactFields(e.target.value)}
              hint="Comma-separated keys to redact from results"
            />
          </div>

          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button type="submit" variant="primary" size="sm" loading={creating}>
              {isEditing ? 'Update Tool' : 'Register Tool'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Secret Modal */}
      <Modal
        open={showSecretModal}
        onClose={() => setShowSecretModal(false)}
        title="Store Tool Secret"
        description={`Encrypted secret for tool: ${selectedToolForSecret}`}
        size="sm"
      >
        <form onSubmit={handleSaveSecret} className="space-y-4">
          <Input label="Secret Reference Name" required placeholder="e.g. api_key, bearer_token" value={secretRef} onChange={(e) => setSecretRef(e.target.value)} className="font-mono" />
          <Input label="Secret Value" type="password" required placeholder="••••••••••••••••" value={secretValue} onChange={(e) => setSecretValue(e.target.value)} hint="Encrypted with AES-256-GCM before storage" />
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowSecretModal(false)}>Cancel</Button>
            <Button type="submit" variant="primary" size="sm" loading={savingSecret} icon={<KeyRound className="w-3.5 h-3.5" />}>Save Secret</Button>
          </div>
        </form>
      </Modal>

      {/* Test Tool Modal */}
      <Modal
        open={showTestModal}
        onClose={() => { setShowTestModal(false); setTestResult(null); setTestError(null); }}
        title="Test Tool Execution"
        description={`Execute live test for: ${selectedToolForTest}`}
        size="md"
      >
        <form onSubmit={handleTestTool} className="space-y-4">
          <Input
            label="Mock Caller Identity Subject"
            placeholder="usr_123"
            value={testSubject}
            onChange={(e) => setTestSubject(e.target.value)}
            className="font-mono text-xs"
            hint="Caller subject used by Identity Bindings (e.g. claims.sub)"
          />
          <Textarea
            label="Arguments (JSON)"
            rows={4}
            required
            mono
            value={testArgs}
            onChange={(e) => setTestArgs(e.target.value)}
            hint="Tool arguments as JSON. Identity bindings will overwrite matching fields."
          />
          <div className="flex justify-end">
            <Button type="submit" variant="primary" size="sm" loading={testRunning} icon={<Play className="w-3.5 h-3.5" />}>
              Execute Test
            </Button>
          </div>
        </form>

        {testError && (
          <div className="mt-4 p-3 bg-red-500/10 border border-red-500/20 rounded-xl">
            <div className="flex items-center gap-2 mb-1">
              <XCircle className="w-4 h-4 text-red-400" />
              <span className="text-xs font-semibold text-red-400">Execution Failed</span>
            </div>
            <pre className="text-[11px] font-mono text-red-300">{testError}</pre>
          </div>
        )}

        {testResult && (
          <div className="mt-4 p-3 bg-emerald-500/5 border border-emerald-500/20 rounded-xl">
            <div className="flex items-center gap-2 mb-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400" />
              <span className="text-xs font-semibold text-emerald-400">Tool Execution Result</span>
            </div>
            <pre className="text-[11px] font-mono text-neutral-400 overflow-x-auto max-h-48">
              {JSON.stringify(testResult, null, 2)}
            </pre>
          </div>
        )}
      </Modal>

      {/* Tool Version History Modal */}
      {selectedToolForHistory && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" onClick={() => { setSelectedToolForHistory(null); setToolDetail(null); }} />
          <div className="relative w-full max-w-xl bg-neutral-900 border border-neutral-800 rounded-2xl shadow-2xl max-h-[80vh] flex flex-col">
            <div className="flex items-center justify-between p-5 border-b border-neutral-800">
              <div>
                <h3 className="text-sm font-semibold text-neutral-100">Tool Version History</h3>
                <p className="text-[11px] font-mono text-neutral-600 mt-0.5">{selectedToolForHistory}</p>
              </div>
              <Button
                variant="secondary"
                size="xs"
                icon={<Pencil className="w-3.5 h-3.5" />}
                onClick={() => {
                  const id = selectedToolForHistory;
                  setSelectedToolForHistory(null);
                  openEditModal(id);
                }}
              >
                Edit & Publish
              </Button>
            </div>

            <div className="overflow-y-auto p-5 space-y-3">
              {loadingHistory ? (
                <Loading text="Loading versions..." />
              ) : !toolDetail?.versions?.length ? (
                <p className="text-xs text-neutral-600 text-center py-4">No version history available.</p>
              ) : (
                toolDetail.versions.map((v: any) => {
                  const isActive = v.version === toolDetail.tool?.active_version || v.is_active;
                  return (
                    <div key={v.id} className="bg-neutral-950 border border-neutral-800 rounded-xl p-3.5 space-y-2">
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2 flex-wrap">
                          <Badge variant="mono">v{v.version}</Badge>
                          {isActive && <Badge variant="success" dot>active</Badge>}
                          <Badge variant="default">{v.side_effect}</Badge>
                          <Badge variant="mono">{v.executor?.type || 'http'}</Badge>
                          {v.confirmation === 'required' && <Badge variant="warning">confirmation</Badge>}
                        </div>
                        <div className="flex items-center gap-2">
                          {!isActive && (
                            <Button
                              variant="secondary"
                              size="xs"
                              onClick={() => handleSetActiveToolVersion(v.version)}
                            >
                              Set Active
                            </Button>
                          )}
                          <button
                            onClick={() => {
                              const id = selectedToolForHistory;
                              setSelectedToolForHistory(null);
                              openEditModal(id, v);
                            }}
                            className="text-xs text-neutral-500 hover:text-blue-400 flex items-center gap-1 transition-colors"
                            title="Edit from this version"
                          >
                            <Pencil className="w-3 h-3" /> Fork
                          </button>
                        </div>
                      </div>

                      <div className="text-[11px] text-neutral-400 font-mono flex items-center justify-between">
                        <span className="truncate max-w-[320px]">
                          {v.executor?.url || v.executor?.template || '(custom executor)'}
                        </span>
                        <span className="text-[10px] text-neutral-600">{formatRelativeTime(v.created_at)}</span>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
