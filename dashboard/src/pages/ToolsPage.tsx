import React, { useEffect, useState } from 'react';
import { api, Tool } from '../api/client';
import { Wrench, Plus, Trash2, X, Play, Database, Globe, Cpu, CheckCircle2, AlertCircle } from 'lucide-react';

export const ToolsPage: React.FC = () => {
  const [tools, setTools] = useState<Tool[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Form State
  const [toolId, setToolId] = useState('');
  const [toolName, setToolName] = useState('');
  const [toolDesc, setToolDesc] = useState('');
  const [sideEffect, setSideEffect] = useState<'read' | 'write'>('read');
  const [confirmation, setConfirmation] = useState<'none' | 'required'>('none');
  const [inputSchema, setInputSchema] = useState('{\n  "type": "object",\n  "properties": {\n    "query": { "type": "string" }\n  },\n  "required": ["query"]\n}');
  const [identityBindings, setIdentityBindings] = useState('{\n  "user_id": "claims.sub"\n}');
  const [executorType, setExecutorType] = useState<'http' | 'mcp' | 'sql_template'>('http');
  const [executorUrl, setExecutorUrl] = useState('https://api.example.com/search');
  const [sqlTemplate, setSqlTemplate] = useState('SELECT id, status, total FROM orders WHERE user_id = :user_id LIMIT :limit');
  const [sqlDriver, setSqlDriver] = useState('pgx');
  const [sqlDsn, setSqlDsn] = useState('postgres://user:pass@localhost:5432/mydb');

  // Test Modal State
  const [testingTool, setTestingTool] = useState<Tool | null>(null);
  const [testArgs, setTestArgs] = useState('{\n  "query": "laptop"\n}');
  const [testSubject, setTestSubject] = useState('usr_alice_123');
  const [testLoading, setTestLoading] = useState(false);
  const [testResult, setTestResult] = useState<any>(null);
  const [testError, setTestError] = useState<string | null>(null);

  const loadData = async () => {
    setLoading(true);
    try {
      const data = await api.listTools().catch(() => []);
      setTools(data);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      let parsedSchema = {};
      let parsedBindings = {};
      try {
        parsedSchema = JSON.parse(inputSchema);
      } catch (_) {
        throw new Error('Input Schema is not valid JSON');
      }
      try {
        parsedBindings = JSON.parse(identityBindings);
      } catch (_) {
        throw new Error('Identity Bindings is not valid JSON');
      }

      let executorConfig: any = {
        type: executorType,
      };

      if (executorType === 'http') {
        executorConfig.url = executorUrl;
        executorConfig.method = 'POST';
      } else if (executorType === 'mcp') {
        executorConfig.url = executorUrl;
      } else if (executorType === 'sql_template') {
        executorConfig.method = sqlDriver;
        executorConfig.url = sqlDsn;
        executorConfig.template = sqlTemplate;
      }

      await api.createTool({
        id: toolId,
        name: toolName,
        description: toolDesc,
        side_effect: sideEffect,
        confirmation: confirmation,
        input_schema: parsedSchema,
        identity_bindings: parsedBindings,
        executor: executorConfig,
      });

      setShowModal(false);
      setToolId('');
      setToolName('');
      setToolDesc('');
      loadData();
    } catch (err: any) {
      setError(err.message || 'Failed to create tool');
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

  const handleOpenTest = (tool: Tool) => {
    setTestingTool(tool);
    setTestArgs('{\n  \n}');
    setTestResult(null);
    setTestError(null);
  };

  const handleRunTest = async () => {
    if (!testingTool) return;
    setTestLoading(true);
    setTestError(null);
    setTestResult(null);

    try {
      let argsObj = {};
      try {
        if (testArgs.trim()) {
          argsObj = JSON.parse(testArgs);
        }
      } catch (_) {
        throw new Error('Test arguments must be valid JSON');
      }

      const res = await api.testTool(testingTool.id, {
        arguments: argsObj,
        identity: {
          subject: testSubject,
          issuer: 'https://auth.beavend.local',
        },
      });
      setTestResult(res);
    } catch (err: any) {
      setTestError(err.message || 'Test execution failed');
    } finally {
      setTestLoading(false);
    }
  };

  return (
    <div className="space-y-6 max-w-6xl">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-100 flex items-center gap-2">
            <Wrench className="w-6 h-6 text-emerald-400" />
            Tools Registry
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            SSRF-hardened HTTP endpoints, Model Context Protocol (MCP) servers, and parameterized read-only SQL templates.
          </p>
        </div>

        <button
          onClick={() => setShowModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-sm font-medium transition-colors shadow-lg shadow-emerald-900/20"
        >
          <Plus className="w-4 h-4" />
          <span>Register Tool</span>
        </button>
      </div>

      {loading ? (
        <div className="text-center py-12 text-sm text-slate-500">Loading registered tools...</div>
      ) : tools.length === 0 ? (
        <div className="bg-slate-900 border border-slate-800 rounded-2xl p-12 text-center space-y-4">
          <Wrench className="w-12 h-12 text-slate-600 mx-auto" />
          <div>
            <h3 className="text-base font-semibold text-slate-300">No Custom Tools registered</h3>
            <p className="text-xs text-slate-500 mt-1">Register HTTP APIs, MCP servers, or parameterized SQL queries for model agent calling.</p>
          </div>
          <button
            onClick={() => setShowModal(true)}
            className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 text-xs font-medium rounded-xl border border-slate-700 transition-colors"
          >
            Register Tool
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {tools.map((t) => (
            <div key={t.id} className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-3">
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="font-bold text-slate-100">{t.name}</h3>
                    <span className="text-[10px] bg-slate-800 text-slate-400 font-mono px-2 py-0.5 rounded-full border border-slate-700">
                      v{t.active_version}
                    </span>
                  </div>
                  <span className="text-xs font-mono text-emerald-400 mt-0.5 block">{t.id}</span>
                </div>
                <div className="flex items-center gap-1">
                  <button
                    onClick={() => handleOpenTest(t)}
                    className="p-1.5 text-slate-400 hover:text-emerald-400 hover:bg-slate-800 rounded-lg transition-colors"
                    title="Test Tool"
                  >
                    <Play className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleDelete(t.id)}
                    className="p-1.5 text-slate-500 hover:text-rose-400 hover:bg-slate-800 rounded-lg transition-colors"
                    title="Delete Tool"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>

              <p className="text-xs text-slate-400 line-clamp-2">{t.description || 'No description provided.'}</p>
            </div>
          ))}
        </div>
      )}

      {/* Register Modal */}
      {showModal && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4 z-50">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-xl w-full p-6 shadow-2xl space-y-4 max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between">
              <h3 className="font-bold text-slate-100 text-lg">Register Custom Tool</h3>
              <button onClick={() => setShowModal(false)} className="text-slate-400 hover:text-slate-200">
                <X className="w-5 h-5" />
              </button>
            </div>

            {error && (
              <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs">
                {error}
              </div>
            )}

            <form onSubmit={handleCreate} className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Tool Identifier (ID)</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. search_orders"
                    value={toolId}
                    onChange={(e) => setToolId(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500 font-mono"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Tool Name</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. Search Customer Orders"
                    value={toolName}
                    onChange={(e) => setToolName(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                  />
                </div>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Description</label>
                <textarea
                  rows={2}
                  placeholder="Describes tool purpose to the LLM agent..."
                  value={toolDesc}
                  onChange={(e) => setToolDesc(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                />
              </div>

              {/* Executor Selector */}
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Executor Engine</label>
                <div className="grid grid-cols-3 gap-2">
                  <button
                    type="button"
                    onClick={() => setExecutorType('http')}
                    className={`flex items-center justify-center gap-2 p-2.5 rounded-xl border text-xs font-medium transition-all ${
                      executorType === 'http'
                        ? 'bg-emerald-950/40 border-emerald-500 text-emerald-300'
                        : 'bg-slate-950 border-slate-800 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <Globe className="w-3.5 h-3.5" />
                    <span>HTTP / REST</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setExecutorType('mcp')}
                    className={`flex items-center justify-center gap-2 p-2.5 rounded-xl border text-xs font-medium transition-all ${
                      executorType === 'mcp'
                        ? 'bg-emerald-950/40 border-emerald-500 text-emerald-300'
                        : 'bg-slate-950 border-slate-800 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <Cpu className="w-3.5 h-3.5" />
                    <span>MCP Server</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setExecutorType('sql_template')}
                    className={`flex items-center justify-center gap-2 p-2.5 rounded-xl border text-xs font-medium transition-all ${
                      executorType === 'sql_template'
                        ? 'bg-emerald-950/40 border-emerald-500 text-emerald-300'
                        : 'bg-slate-950 border-slate-800 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <Database className="w-3.5 h-3.5" />
                    <span>SQL Template</span>
                  </button>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Side Effect</label>
                  <select
                    value={sideEffect}
                    onChange={(e) => setSideEffect(e.target.value as 'read' | 'write')}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                  >
                    <option value="read">read (Safe / Cacheable)</option>
                    <option value="write">write (Mutating)</option>
                  </select>
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Confirmation</label>
                  <select
                    value={confirmation}
                    onChange={(e) => setConfirmation(e.target.value as 'none' | 'required')}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-sm text-slate-100 focus:outline-none focus:border-emerald-500"
                  >
                    <option value="none">none (Auto-execute)</option>
                    <option value="required">required (Pause for human approval)</option>
                  </select>
                </div>
              </div>

              {/* Dynamic Executor Config Fields */}
              {executorType === 'http' && (
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Downstream HTTP Endpoint URL</label>
                  <input
                    type="url"
                    required
                    placeholder="https://api.internal.com/v1/orders"
                    value={executorUrl}
                    onChange={(e) => setExecutorUrl(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                  />
                </div>
              )}

              {executorType === 'mcp' && (
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">MCP Server Endpoint (HTTP / SSE)</label>
                  <input
                    type="url"
                    required
                    placeholder="http://localhost:8000/sse"
                    value={executorUrl}
                    onChange={(e) => setExecutorUrl(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                  />
                  <p className="text-[11px] text-slate-500 mt-1">Dispatches JSON-RPC 2.0 <code>tools/call</code> with downstream auth headers and caller identity.</p>
                </div>
              )}

              {executorType === 'sql_template' && (
                <div className="space-y-3 p-3 bg-slate-950/60 border border-slate-800 rounded-xl">
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="block text-xs font-semibold text-slate-400 mb-1">SQL Driver</label>
                      <select
                        value={sqlDriver}
                        onChange={(e) => setSqlDriver(e.target.value)}
                        className="w-full bg-slate-900 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-emerald-500"
                      >
                        <option value="pgx">PostgreSQL (pgx)</option>
                        <option value="postgres">PostgreSQL (lib/pq)</option>
                      </select>
                    </div>
                    <div>
                      <label className="block text-xs font-semibold text-slate-400 mb-1">Database DSN / Connection URL</label>
                      <input
                        type="text"
                        required
                        placeholder="postgres://user:pass@host:5432/db"
                        value={sqlDsn}
                        onChange={(e) => setSqlDsn(e.target.value)}
                        className="w-full bg-slate-900 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                      />
                    </div>
                  </div>

                  <div>
                    <label className="block text-xs font-semibold text-slate-400 mb-1">Read-Only SQL Template</label>
                    <textarea
                      rows={3}
                      required
                      value={sqlTemplate}
                      onChange={(e) => setSqlTemplate(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-800 rounded-xl p-3 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                    />
                    <p className="text-[11px] text-slate-500 mt-1">Use <code>:param</code> placeholders (e.g. <code>:user_id</code>, <code>:limit</code>). Mutating keywords are blocked.</p>
                  </div>
                </div>
              )}

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Input JSON Schema</label>
                <textarea
                  rows={3}
                  required
                  value={inputSchema}
                  onChange={(e) => setInputSchema(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl p-3 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Identity Bindings (Confused-Deputy Defense)</label>
                <textarea
                  rows={2}
                  required
                  value={identityBindings}
                  onChange={(e) => setIdentityBindings(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl p-3 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                />
              </div>

              <div className="pt-2 flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setShowModal(false)}
                  className="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-xl text-sm transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-xl text-sm font-medium transition-colors shadow-lg shadow-emerald-900/20"
                >
                  Register Tool
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Test Tool Modal */}
      {testingTool && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4 z-50">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-xl w-full p-6 shadow-2xl space-y-4 max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Play className="w-5 h-5 text-emerald-400" />
                <h3 className="font-bold text-slate-100 text-lg">Test Tool: {testingTool.name}</h3>
              </div>
              <button onClick={() => setTestingTool(null)} className="text-slate-400 hover:text-slate-200">
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="space-y-3">
              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Mock Caller Identity Subject</label>
                <input
                  type="text"
                  value={testSubject}
                  onChange={(e) => setTestSubject(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                  placeholder="usr_123"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-400 mb-1">Tool Arguments (JSON)</label>
                <textarea
                  rows={4}
                  value={testArgs}
                  onChange={(e) => setTestArgs(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl p-3 text-xs font-mono text-slate-100 focus:outline-none focus:border-emerald-500"
                />
              </div>

              <div className="flex justify-end">
                <button
                  type="button"
                  disabled={testLoading}
                  onClick={handleRunTest}
                  className="flex items-center gap-2 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white rounded-xl text-sm font-medium transition-colors"
                >
                  <Play className="w-4 h-4" />
                  <span>{testLoading ? 'Executing...' : 'Run Test'}</span>
                </button>
              </div>

              {testError && (
                <div className="p-3 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs flex items-start gap-2">
                  <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
                  <span>{testError}</span>
                </div>
              )}

              {testResult && (
                <div className="space-y-2 pt-2 border-t border-slate-800">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-semibold text-slate-300">Execution Result</span>
                    {testResult.is_error ? (
                      <span className="text-[10px] bg-rose-950/60 text-rose-400 border border-rose-800 px-2 py-0.5 rounded-full flex items-center gap-1">
                        <AlertCircle className="w-3 h-3" /> Error
                      </span>
                    ) : (
                      <span className="text-[10px] bg-emerald-950/60 text-emerald-400 border border-emerald-800 px-2 py-0.5 rounded-full flex items-center gap-1">
                        <CheckCircle2 className="w-3 h-3" /> Success
                      </span>
                    )}
                  </div>
                  <pre className="p-3 bg-slate-950 border border-slate-800 rounded-xl text-xs font-mono text-slate-300 overflow-x-auto max-h-48">
                    {testResult.content || JSON.stringify(testResult, null, 2)}
                  </pre>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
