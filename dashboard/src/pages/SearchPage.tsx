import React, { useEffect, useState } from 'react';
import { api, SearchHealthStatus } from '../api/client';
import {
  Search,
  RefreshCw,
  ExternalLink,
  Shield,
  Play,
  Save,
  CheckCircle2,
  AlertCircle,
  Server,
  Zap,
  Bot,
  ArrowRight,
} from 'lucide-react';
import { Card, CardHeader } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Input, Select, Toggle } from '../components/ui/Form';
import { Alert, Loading } from '../components/ui/Feedback';

interface SearchPageProps {
  onNavigateApps?: () => void;
}

export const SearchPage: React.FC<SearchPageProps> = ({ onNavigateApps }) => {
  // Service configuration & health state
  const [searxngUrl, setSearxngUrl] = useState('http://searxng:8080');
  const [enabled, setEnabled] = useState(true);
  const [timeoutSeconds, setTimeoutSeconds] = useState(8);
  const [searchHealth, setSearchHealth] = useState<SearchHealthStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [savingConfig, setSavingConfig] = useState(false);
  const [checkingHealth, setCheckingHealth] = useState(false);
  const [configSuccess, setConfigSuccess] = useState<string | null>(null);
  const [configError, setConfigError] = useState<string | null>(null);

  // Search testing state
  const [searchQuery, setSearchQuery] = useState('beavend ai runtime architecture');
  const [searchCategory, setSearchCategory] = useState('general');
  const [searchMaxResults, setSearchMaxResults] = useState(5);
  const [searchLoading, setSearchLoading] = useState(false);
  const [searchResults, setSearchResults] = useState<any>(null);
  const [searchTestError, setSearchTestError] = useState<string | null>(null);
  const [showRawJson, setShowRawJson] = useState(false);

  const fetchHealth = async () => {
    setCheckingHealth(true);
    try {
      const status = await api.getSearchHealth();
      setSearchHealth(status);
      if (status.url) {
        setSearxngUrl(status.url);
      }
      setEnabled(status.enabled);
    } catch (err: any) {
      setSearchHealth({
        status: 'error',
        url: searxngUrl,
        configured: false,
        enabled: false,
        error: err.message || 'Failed to connect to search health endpoint',
      });
    } finally {
      setCheckingHealth(false);
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchHealth();
  }, []);

  const handleSaveConfig = async (e: React.FormEvent) => {
    e.preventDefault();
    setSavingConfig(true);
    setConfigSuccess(null);
    setConfigError(null);

    try {
      const cleanUrl = searxngUrl.trim().replace(/\/+$/, '');
      const status = await api.updateSearchConfig({
        url: cleanUrl,
        enabled,
        timeout_seconds: timeoutSeconds,
      });
      setSearchHealth(status);
      if (status.status === 'healthy') {
        setConfigSuccess('SearXNG configuration saved and connection verified successfully.');
      } else {
        setConfigError(status.error || `Connection test resulted in status: ${status.status}`);
      }
    } catch (err: any) {
      setConfigError(err.message || 'Failed to update search configuration');
    } finally {
      setSavingConfig(false);
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
      setSearchTestError(err.message || 'Search execution failed');
    } finally {
      setSearchLoading(false);
    }
  };

  return (
    <div className="space-y-6 max-w-[1100px]">
      {/* Page Header */}
      <div className="flex items-start justify-between">
        <div>
          <div className="flex items-center gap-2.5">
            <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
              <Search className="w-5 h-5 text-neutral-400" />
              Search Engine (SearXNG)
            </h1>
            {searchHealth?.status === 'healthy' ? (
              <Badge variant="success" dot>Online</Badge>
            ) : searchHealth?.status === 'disabled' ? (
              <Badge variant="default">Disabled</Badge>
            ) : searchHealth ? (
              <Badge variant="danger" dot>Offline / Unreachable</Badge>
            ) : null}
          </div>
          <p className="text-sm text-neutral-500 mt-0.5">
            Self-hosted meta-search engine. Configure endpoint connection, test queries, and enable web search per AI App.
          </p>
        </div>
        <Button
          variant="secondary"
          size="sm"
          icon={<RefreshCw className={`w-3.5 h-3.5 ${checkingHealth ? 'animate-spin' : ''}`} />}
          onClick={fetchHealth}
          disabled={checkingHealth}
        >
          {checkingHealth ? 'Checking...' : 'Refresh Health'}
        </Button>
      </div>

      {loading ? (
        <Loading text="Inspecting SearXNG connection..." />
      ) : (
        <div className="space-y-6">
          {/* Top Row: Setup & Live Health */}
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-4">
            {/* Setup Form */}
            <Card padding="md" className="lg:col-span-7">
              <div className="flex items-center gap-2 mb-3">
                <Server className="w-4 h-4 text-emerald-400" />
                <h2 className="text-sm font-semibold text-neutral-200">Connection Setup &amp; Parameters</h2>
              </div>
              <p className="text-xs text-neutral-500 mb-4">
                Define the HTTP endpoint of your SearXNG container or cluster. Beavend queries this URL via standard JSON REST API.
              </p>

              {configSuccess && (
                <Alert variant="success" className="mb-4" onDismiss={() => setConfigSuccess(null)}>
                  {configSuccess}
                </Alert>
              )}
              {configError && (
                <Alert variant="error" className="mb-4" onDismiss={() => setConfigError(null)}>
                  {configError}
                </Alert>
              )}

              <form onSubmit={handleSaveConfig} className="space-y-4">
                <div className="space-y-3">
                  <Input
                    label="SearXNG Base URL"
                    required
                    placeholder="e.g. http://searxng:8080 or http://localhost:8081"
                    value={searxngUrl}
                    onChange={(e) => setSearxngUrl(e.target.value)}
                    className="font-mono text-xs"
                    hint="Internal Docker hostname or reverse proxy URL reachable by the Beavend binary"
                  />

                  <div className="grid grid-cols-2 gap-3">
                    <div className="flex items-end pb-1">
                      <Toggle
                        checked={enabled}
                        onChange={setEnabled}
                        label="Enable Search Globally"
                        description="Global master switch for web search"
                      />
                    </div>
                    <Input
                      label="Request Timeout (Seconds)"
                      type="number"
                      min="1"
                      max="60"
                      value={timeoutSeconds}
                      onChange={(e) => setTimeoutSeconds(Number(e.target.value) || 8)}
                      className="font-mono"
                      hint="Max wait before graceful fallback"
                    />
                  </div>
                </div>

                <div className="flex justify-end pt-2 border-t border-neutral-800/80">
                  <Button
                    type="submit"
                    variant="primary"
                    size="sm"
                    loading={savingConfig}
                    icon={<Save className="w-3.5 h-3.5" />}
                  >
                    Save &amp; Test Connection
                  </Button>
                </div>
              </form>
            </Card>

            {/* Live Health Overview */}
            <Card padding="md" className="lg:col-span-5 flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-3">
                  <span className="text-xs font-semibold uppercase tracking-wider text-neutral-500">Service Status</span>
                  {searchHealth?.status === 'healthy' ? (
                    <Badge variant="success" dot>Online &amp; Healthy</Badge>
                  ) : searchHealth?.status === 'disabled' ? (
                    <Badge variant="default">Disabled</Badge>
                  ) : (
                    <Badge variant="danger" dot>Connection Failed</Badge>
                  )}
                </div>

                <div className="space-y-2.5 text-xs">
                  <div>
                    <span className="text-neutral-500 block text-[11px]">Active Backend URL</span>
                    <code className="text-neutral-200 font-mono text-[11px] bg-neutral-900 px-2 py-1 rounded border border-neutral-800 block truncate mt-0.5">
                      {searchHealth?.url || searxngUrl}
                    </code>
                  </div>
                  <div className="flex justify-between items-center pt-1.5 border-t border-neutral-800/60">
                    <span className="text-neutral-500">Roundtrip Latency</span>
                    <span className="font-mono text-neutral-200">
                      {searchHealth?.latency_ms !== undefined ? `${searchHealth.latency_ms} ms` : '-'}
                    </span>
                  </div>
                  <div className="flex justify-between items-center">
                    <span className="text-neutral-500">Format Requirement</span>
                    <span className="font-mono text-neutral-200">JSON Enabled</span>
                  </div>
                  <div className="flex justify-between items-center">
                    <span className="text-neutral-500">Architecture Isolation</span>
                    <span className="font-mono text-emerald-400">Network REST (Clean)</span>
                  </div>

                  {searchHealth?.error && (
                    <div className="p-2.5 rounded-lg bg-red-950/40 border border-red-800/50 text-red-300 text-[11px] mt-2 space-y-1">
                      <div className="flex items-center gap-1.5 font-semibold text-red-200">
                        <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                        <span>Connection Issue Detected</span>
                      </div>
                      <p className="leading-relaxed text-red-300/90">{searchHealth.error}</p>
                      <p className="text-[10px] text-red-400/80 pt-1">
                        Tip: Verify the SearXNG container is running (<code className="text-red-200 font-mono">docker compose ps</code>) and configured with <code className="text-red-200 font-mono">formats: [html, json]</code> in <code className="text-red-200 font-mono">deploy/docker/searxng/settings.yml</code>.
                      </p>
                    </div>
                  )}
                </div>
              </div>

              {searchHealth?.status === 'healthy' && (
                <div className="mt-4 pt-3 border-t border-neutral-800/60 flex items-center gap-2 text-xs text-emerald-400">
                  <CheckCircle2 className="w-4 h-4 shrink-0" />
                  <span>SearXNG is responding normally and ready for agent queries.</span>
                </div>
              )}
            </Card>
          </div>

          {/* Interactive Live Search Console */}
          <Card padding="md">
            <div className="flex items-center justify-between mb-4">
              <div>
                <h2 className="text-sm font-semibold text-neutral-200 flex items-center gap-2">
                  <Play className="w-4 h-4 text-blue-400" />
                  Live Search Tester
                </h2>
                <p className="text-xs text-neutral-500 mt-0.5">
                  Test SearXNG query execution, category filtering, and JSON result parsing in real time.
                </p>
              </div>
              {searchResults && (
                <button
                  type="button"
                  onClick={() => setShowRawJson(!showRawJson)}
                  className="text-xs text-neutral-400 hover:text-neutral-200 transition-colors"
                >
                  {showRawJson ? 'Hide Raw JSON' : 'View Raw JSON'}
                </button>
              )}
            </div>

            <form onSubmit={handleTestSearch} className="space-y-4">
              <div className="grid grid-cols-1 md:grid-cols-12 gap-3">
                <div className="md:col-span-6">
                  <Input
                    label="Search Query"
                    required
                    placeholder="e.g. quantum computing latest developments"
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
                      { value: 'general', label: 'General Web' },
                      { value: 'science', label: 'Science / Academic' },
                      { value: 'it', label: 'IT & Code' },
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

            {searchResults && showRawJson && (
              <div className="mt-4 p-3 bg-neutral-950 border border-neutral-800 rounded-lg">
                <pre className="text-[11px] font-mono text-neutral-400 overflow-x-auto max-h-[300px]">
                  {JSON.stringify(searchResults, null, 2)}
                </pre>
              </div>
            )}

            {searchResults && !showRawJson && (
              <div className="mt-5 pt-4 border-t border-neutral-800 space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-neutral-300">
                    Results ({searchResults.total_results || searchResults.number_of_results || searchResults.results?.length || 0})
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
                  <div className="space-y-2 max-h-[460px] overflow-y-auto pr-1">
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

          {/* Bottom Row: AI App Guide & AGPL Isolation */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Card padding="md">
              <div className="flex items-center gap-2 mb-2">
                <Bot className="w-4 h-4 text-purple-400" />
                <h3 className="text-sm font-semibold text-neutral-200">How to Enable in AI Apps</h3>
              </div>
              <p className="text-xs text-neutral-400 leading-relaxed mb-3">
                Web search is activated on a per-app basis. When active, Beavend automatically injects the <code className="text-amber-400 font-mono">web_search</code> function into the model&apos;s agent loop.
              </p>
              <div className="p-3 bg-neutral-950 border border-neutral-800 rounded-lg text-xs space-y-1.5 mb-3 text-neutral-400">
                <p>1. Open <b>AI Apps</b> in the navigation.</p>
                <p>2. Select your AI application to open its workspace.</p>
                <p>3. Go to the <b>Settings</b> tab &rarr; scroll to <b>Web Search (SearXNG)</b>.</p>
                <p>4. Toggle <b>Enable Live Web Search</b> and click Save.</p>
              </div>
              {onNavigateApps && (
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={onNavigateApps}
                  icon={<ArrowRight className="w-3.5 h-3.5" />}
                >
                  Manage AI Apps
                </Button>
              )}
            </Card>

            <Card padding="md">
              <div className="flex items-center gap-2 mb-2">
                <Shield className="w-4 h-4 text-emerald-400" />
                <h3 className="text-sm font-semibold text-neutral-200">License Isolation &amp; Zero Copyleft</h3>
              </div>
              <p className="text-xs text-neutral-400 leading-relaxed mb-3">
                SearXNG is licensed under GNU AGPL-3.0. Beavend is released under permissive Apache/MIT licenses.
              </p>
              <div className="space-y-2 text-xs text-neutral-400">
                <div className="flex items-start gap-2">
                  <div className="w-1.5 h-1.5 rounded-full bg-emerald-400 mt-1.5 shrink-0" />
                  <p><b>Network Isolation:</b> SearXNG runs as a detached Docker container. Beavend communicates strictly over private HTTP REST JSON.</p>
                </div>
                <div className="flex items-start gap-2">
                  <div className="w-1.5 h-1.5 rounded-full bg-emerald-400 mt-1.5 shrink-0" />
                  <p><b>Zero Binary Linking:</b> No SearXNG code or dependencies are bundled or imported into Beavend&apos;s compiled binary.</p>
                </div>
                <div className="flex items-start gap-2">
                  <div className="w-1.5 h-1.5 rounded-full bg-emerald-400 mt-1.5 shrink-0" />
                  <p><b>Graceful Degradation:</b> If SearXNG is ever down, the agent receives an error notification rather than crashing the chat stream.</p>
                </div>
              </div>
            </Card>
          </div>
        </div>
      )}
    </div>
  );
};
