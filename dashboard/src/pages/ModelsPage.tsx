import React, { useEffect, useState } from 'react';
import { api, ModelRouting, ProviderConfig, Modality, ALL_MODALITIES } from '../api/client';
import { Cpu, Plus, Trash2, KeyRound, ArrowRight, Activity, CheckCircle, AlertTriangle, Pencil, RefreshCw, ChevronUp, ChevronDown, Eye, EyeOff, ExternalLink, Check, Type, Image, Mic, Video, FileText, Sparkles, Layers } from 'lucide-react';
import { Card, CardHeader } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Input, Select, SearchableSelect, SearchableOption } from '../components/ui/Form';
import { EmptyState, Loading, Spinner } from '../components/ui/Feedback';
import { OPENAI_COMPATIBLE_PROVIDERS, POPULAR_PRESET_IDS, OpenAICompatibleProvider } from '../data/openAiProviders';

interface FallbackItem {
  id: string;
  provider: string;
  deployment: string;
}

const normalizeFallbacks = (raw: any): string[] => {
  if (!raw) return [];
  if (Array.isArray(raw)) return raw.filter((s) => typeof s === 'string' && s.trim());
  if (typeof raw === 'string') {
    try {
      const decoded = atob(raw);
      const parsed = JSON.parse(decoded);
      if (Array.isArray(parsed)) return parsed.filter((s) => typeof s === 'string' && s.trim());
    } catch (_) {}
    try {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed)) return parsed.filter((s) => typeof s === 'string' && s.trim());
    } catch (_) {}
    return raw.split(',').map((s) => s.trim()).filter(Boolean);
  }
  return [];
};

const getModalityIcon = (m: Modality) => {
  switch (m) {
    case 'text':
      return <Type className="w-3.5 h-3.5" />;
    case 'image':
      return <Image className="w-3.5 h-3.5" />;
    case 'audio':
      return <Mic className="w-3.5 h-3.5" />;
    case 'video':
      return <Video className="w-3.5 h-3.5" />;
    case 'document':
      return <FileText className="w-3.5 h-3.5" />;
    default:
      return null;
  }
};

export const ModelsPage: React.FC = () => {
  const [models, setModels] = useState<ModelRouting[]>([]);
  const [providers, setProviders] = useState<ProviderConfig[]>([]);
  const [loading, setLoading] = useState(true);

  const [showModelModal, setShowModelModal] = useState(false);
  const [logicalModel, setLogicalModel] = useState('fast');
  const [deployment, setDeployment] = useState('');
  const [selectedProvider, setSelectedProvider] = useState('');
  const [fallbackChain, setFallbackChain] = useState<FallbackItem[]>([]);
  const [inputCost, setInputCost] = useState('');
  const [outputCost, setOutputCost] = useState('');
  const [inputModalities, setInputModalities] = useState<Modality[]>(['text']);
  const [outputModalities, setOutputModalities] = useState<Modality[]>(['text']);
  const [savingModel, setSavingModel] = useState(false);

  const [availableModels, setAvailableModels] = useState<string[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [fetchModelsError, setFetchModelsError] = useState<string | null>(null);

  const [providerModelsCache, setProviderModelsCache] = useState<Record<string, string[]>>({});
  const [loadingProviderModels, setLoadingProviderModels] = useState<Record<string, boolean>>({});

  const [showProviderModal, setShowProviderModal] = useState(false);
  const [selectedPreset, setSelectedPreset] = useState('openrouter');
  const [providerName, setProviderName] = useState('openrouter');
  const [baseUrl, setBaseUrl] = useState('https://openrouter.ai/api/v1');
  const [apiKey, setApiKey] = useState('');
  const [showApiKey, setShowApiKey] = useState(false);
  const [isEditingProvider, setIsEditingProvider] = useState(false);
  const [savingProvider, setSavingProvider] = useState(false);

  const [testingProvider, setTestingProvider] = useState<string | null>(null);
  const [providerTestStatus, setProviderTestStatus] = useState<Record<string, { success: boolean; message: string; latency_ms?: number }>>({});
  const [modalTestStatus, setModalTestStatus] = useState<{ success: boolean; message: string; latency_ms?: number } | null>(null);
  const [testingModal, setTestingModal] = useState(false);

  const loadData = async () => {
    setLoading(true);
    try {
      const [m, p] = await Promise.all([
        api.listModels().catch(() => []),
        api.listProviders().catch(() => []),
      ]);
      const safeModels = (m || []).map((model) => ({
        ...model,
        fallback_deployments: normalizeFallbacks(model.fallback_deployments),
      }));
      setModels(safeModels);
      setProviders(p || []);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadData(); }, []);

  const handleTestProvider = async (name: string) => {
    setTestingProvider(name);
    try {
      const res = await api.testProviderConnection({ provider_name: name });
      setProviderTestStatus((prev) => ({ ...prev, [name]: res }));
    } catch (err: any) {
      setProviderTestStatus((prev) => ({ ...prev, [name]: { success: false, message: err.message || 'Connection test failed' } }));
    } finally {
      setTestingProvider(null);
    }
  };

  const handleTestModal = async () => {
    setTestingModal(true);
    setModalTestStatus(null);
    try {
      const res = await api.testProviderConnection({
        provider_name: providerName,
        base_url: baseUrl,
        api_key: apiKey,
      });
      setModalTestStatus(res);
    } catch (err: any) {
      setModalTestStatus({ success: false, message: err.message || 'Connection test failed' });
    } finally {
      setTestingModal(false);
    }
  };

  const loadModelsForProvider = async (provName: string) => {
    if (!provName || providerModelsCache[provName]) return;
    setLoadingProviderModels((prev) => ({ ...prev, [provName]: true }));
    try {
      const res = await api.fetchProviderModels(provName);
      const list = res.models || [];
      setProviderModelsCache((prev) => ({ ...prev, [provName]: list }));
    } catch (_) {
      setProviderModelsCache((prev) => ({ ...prev, [provName]: [] }));
    } finally {
      setLoadingProviderModels((prev) => ({ ...prev, [provName]: false }));
    }
  };

  const loadProviderModels = async (provName: string, preserveDeployment?: string) => {
    if (!provName) {
      setAvailableModels([]);
      return;
    }
    setLoadingModels(true);
    setFetchModelsError(null);
    try {
      const res = await api.fetchProviderModels(provName);
      const list = res.models || [];
      setAvailableModels(list);
      setProviderModelsCache((prev) => ({ ...prev, [provName]: list }));
      if (list.length > 0) {
        if (preserveDeployment && (list.includes(preserveDeployment) || preserveDeployment.trim())) {
          setDeployment(preserveDeployment);
        } else {
          setDeployment((prev) => (prev && list.includes(prev) ? prev : list[0]));
        }
      } else if (preserveDeployment) {
        setDeployment(preserveDeployment);
      }
    } catch (err: any) {
      setFetchModelsError(err.message || 'Failed to fetch models from provider');
      setAvailableModels([]);
      if (preserveDeployment) {
        setDeployment(preserveDeployment);
      }
    } finally {
      setLoadingModels(false);
    }
  };

  const openCreateModelModal = () => {
    if (providers.length === 0) return;
    const initialProvider = providers[0].provider_name;
    setLogicalModel('fast');
    setSelectedProvider(initialProvider);
    setDeployment('');
    setFallbackChain([]);
    setInputCost('');
    setOutputCost('');
    setInputModalities(['text']);
    setOutputModalities(['text']);
    setFetchModelsError(null);
    setShowModelModal(true);
    loadProviderModels(initialProvider);
  };

  const openEditModelModal = (m: ModelRouting) => {
    setLogicalModel(m.logical_model);
    const matchedProvider = providers.find((p) => p.provider_name === m.adapter)?.provider_name || providers[0]?.provider_name || m.adapter;
    setSelectedProvider(matchedProvider);
    setDeployment(m.deployment);
    setInputCost(m.input_cost_per_1m !== undefined && m.input_cost_per_1m !== null ? String(m.input_cost_per_1m) : '');
    setOutputCost(m.output_cost_per_1m !== undefined && m.output_cost_per_1m !== null ? String(m.output_cost_per_1m) : '');
    setInputModalities(m.input_modalities && m.input_modalities.length > 0 ? m.input_modalities : ['text']);
    setOutputModalities(m.output_modalities && m.output_modalities.length > 0 ? m.output_modalities : ['text']);

    const rawFbs = normalizeFallbacks(m.fallback_deployments);
    const parsedFbs: FallbackItem[] = rawFbs.map((fb, idx) => {
      let prov = matchedProvider;
      let dep = fb;
      if (fb.includes('/')) {
        const slashIdx = fb.indexOf('/');
        prov = fb.slice(0, slashIdx);
        dep = fb.slice(slashIdx + 1);
      } else if (fb.includes(':')) {
        const colonIdx = fb.indexOf(':');
        prov = fb.slice(0, colonIdx);
        dep = fb.slice(colonIdx + 1);
      }
      return {
        id: `fb-${idx}-${Date.now()}`,
        provider: prov,
        deployment: dep,
      };
    });
    setFallbackChain(parsedFbs);
    parsedFbs.forEach((fb) => loadModelsForProvider(fb.provider));

    setFetchModelsError(null);
    setShowModelModal(true);
    loadProviderModels(matchedProvider, m.deployment);
  };

  const handleProviderChange = (provName: string) => {
    setSelectedProvider(provName);
    loadProviderModels(provName);
  };

  const handleAddFallback = () => {
    // Default to another provider if available, or current provider
    const otherProvider = providers.find((p) => p.provider_name !== selectedProvider)?.provider_name || selectedProvider || providers[0]?.provider_name || 'litellm';
    setFallbackChain((prev) => [
      ...prev,
      {
        id: `fb-${Date.now()}-${Math.random().toString(36).substring(2, 7)}`,
        provider: otherProvider,
        deployment: '',
      },
    ]);
    loadModelsForProvider(otherProvider);
  };

  const handleFallbackProviderChange = (index: number, newProv: string) => {
    setFallbackChain((prev) => {
      const next = [...prev];
      next[index] = { ...next[index], provider: newProv, deployment: '' };
      return next;
    });
    loadModelsForProvider(newProv);
  };

  const handleFallbackDeploymentChange = (index: number, newDep: string) => {
    setFallbackChain((prev) => {
      const next = [...prev];
      next[index] = { ...next[index], deployment: newDep };
      return next;
    });
  };

  const handleMoveFallback = (index: number, direction: 'up' | 'down') => {
    setFallbackChain((prev) => {
      const targetIdx = direction === 'up' ? index - 1 : index + 1;
      if (targetIdx < 0 || targetIdx >= prev.length) return prev;
      const next = [...prev];
      const temp = next[index];
      next[index] = next[targetIdx];
      next[targetIdx] = temp;
      return next;
    });
  };

  const handleRemoveFallback = (index: number) => {
    setFallbackChain((prev) => prev.filter((_, idx) => idx !== index));
  };

  const toggleModality = (
    setter: React.Dispatch<React.SetStateAction<Modality[]>>,
    m: Modality,
  ) => setter((prev) => (prev.includes(m) ? prev.filter((x) => x !== m) : [...prev, m]));

  const handleSaveModel = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedProvider) {
      alert('Please select a provider');
      return;
    }
    if (!deployment.trim()) {
      alert('Please select or enter a model deployment');
      return;
    }
    setSavingModel(true);
    try {
      const formattedFallbacks = fallbackChain
        .filter((item) => item.deployment.trim())
        .map((item) => `${item.provider}/${item.deployment.trim()}`);

      const payload: any = {
        deployment: deployment.trim(),
        adapter: selectedProvider,
        fallback_deployments: formattedFallbacks,
        input_modalities: inputModalities,
        output_modalities: outputModalities,
      };
      if (inputCost.trim() !== '') {
        const val = parseFloat(inputCost);
        if (!isNaN(val)) payload.input_cost_per_1m = val;
      }
      if (outputCost.trim() !== '') {
        const val = parseFloat(outputCost);
        if (!isNaN(val)) payload.output_cost_per_1m = val;
      }

      await api.upsertModel(logicalModel, payload);
      setShowModelModal(false);
      loadData();
    } catch (err: any) {
      alert(`Failed to save model routing: ${err.message}`);
    } finally {
      setSavingModel(false);
    }
  };

  const handleDeleteModel = async (lm: string) => {
    if (!confirm(`Delete logical model routing "${lm}"?`)) return;
    try {
      await api.deleteModel(lm);
      loadData();
    } catch (err: any) {
      alert(`Error: ${err.message}`);
    }
  };

  const findProviderPreset = (nameOrId: string, url?: string): OpenAICompatibleProvider | undefined => {
    const normName = (nameOrId || '').trim().toLowerCase();
    const direct = OPENAI_COMPATIBLE_PROVIDERS.find(
      (p) => p.id.toLowerCase() === normName || p.name.toLowerCase() === normName
    );
    if (direct) return direct;
    if (url) {
      const normUrl = url.trim().toLowerCase().replace(/\/+$/, '');
      const byUrl = OPENAI_COMPATIBLE_PROVIDERS.find(
        (p) => p.baseUrl && p.baseUrl.toLowerCase().replace(/\/+$/, '') === normUrl
      );
      if (byUrl) return byUrl;
    }
    return undefined;
  };

  const openAddProviderModal = () => {
    setSelectedPreset('openrouter');
    setProviderName('openrouter');
    setBaseUrl('https://openrouter.ai/api/v1');
    setApiKey('');
    setShowApiKey(false);
    setIsEditingProvider(false);
    setModalTestStatus(null);
    setShowProviderModal(true);
  };

  const openEditProviderModal = (p: ProviderConfig) => {
    const matched = findProviderPreset(p.provider_name, p.base_url);
    setSelectedPreset(matched ? matched.id : 'custom');
    setProviderName(p.provider_name);
    setBaseUrl(p.base_url);
    setApiKey('');
    setShowApiKey(false);
    setIsEditingProvider(true);
    setModalTestStatus(null);
    setShowProviderModal(true);
  };

  const handleSelectPreset = (presetId: string) => {
    const preset = OPENAI_COMPATIBLE_PROVIDERS.find((p) => p.id === presetId);
    if (preset) {
      setSelectedPreset(preset.id);
      if (preset.id === 'custom') {
        if (!isEditingProvider && (providerName === 'openrouter' || providerName === 'litellm')) {
          setProviderName('custom-openai');
        }
        const wasPresetUrl = OPENAI_COMPATIBLE_PROVIDERS.some((p) => p.id !== 'custom' && p.baseUrl === baseUrl);
        if (wasPresetUrl) {
          setBaseUrl('');
        }
      } else {
        if (!isEditingProvider) {
          setProviderName(preset.id);
        }
        setBaseUrl(preset.baseUrl);
      }
    } else {
      setSelectedPreset('custom');
      if (!isEditingProvider) {
        setProviderName(presetId.toLowerCase().replace(/[^a-z0-9_-]/g, '-'));
      }
    }
    setModalTestStatus(null);
  };

  const providerOptions: SearchableOption[] = OPENAI_COMPATIBLE_PROVIDERS.map((p) => ({
    value: p.id,
    label: p.name,
    description: p.baseUrl || 'Custom base URL & credentials',
    badge: p.category,
  }));

  const currentPreset = OPENAI_COMPATIBLE_PROVIDERS.find((p) => p.id === selectedPreset);

  const handleSaveProvider = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!providerName.trim()) {
      alert('Provider identifier is required');
      return;
    }
    if (!baseUrl.trim() && selectedPreset !== 'litellm') {
      alert('Base URL is required');
      return;
    }
    if (!apiKey.trim()) {
      alert('API Key / Secret Token is required');
      return;
    }
    setSavingProvider(true);
    try {
      await api.upsertProvider(providerName.trim(), { base_url: baseUrl.trim(), api_key: apiKey.trim() });
      setShowProviderModal(false);
      setApiKey('');
      setModalTestStatus(null);
      loadData();
    } catch (err: any) {
      alert(`Failed to save provider: ${err.message}`);
    } finally {
      setSavingProvider(false);
    }
  };

  const handleDeleteProvider = async (name: string) => {
    if (!confirm(`Delete provider "${name}"?`)) return;
    try {
      await api.deleteProvider(name);
      loadData();
    } catch (err: any) {
      alert(`Error: ${err.message}`);
    }
  };

  return (
    <div className="space-y-5 max-w-[1100px]">
      <div>
        <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
          <Cpu className="w-5 h-5 text-neutral-400" />
          Models & Provider Routing
        </h1>
        <p className="text-sm text-neutral-500 mt-0.5">
          First configure upstream provider credentials, then map logical aliases for your client applications.
        </p>
      </div>

      {loading ? (
        <Loading text="Loading model configurations..." />
      ) : (
        <div className="space-y-4">
          {/* STEP 1: Providers (Shown First) */}
          <Card padding="lg">
            <div className="flex items-center justify-between mb-5">
              <div className="flex items-start gap-2.5">
                <span className="text-[11px] font-bold font-mono px-2 py-0.5 rounded bg-amber-500/10 text-amber-400 border border-amber-500/20 mt-0.5">
                  Step 1
                </span>
                <CardHeader
                  title="Provider Credentials"
                  description="Upstream gateways, OpenAI-compatible APIs & direct endpoints. API keys are encrypted at rest with AES-256-GCM."
                  icon={<KeyRound className="w-4 h-4 text-amber-400" />}
                />
              </div>
              <Button
                variant="primary"
                size="xs"
                icon={<KeyRound className="w-3.5 h-3.5" />}
                onClick={openAddProviderModal}
              >
                Add Provider
              </Button>
            </div>

            {providers.length === 0 ? (
              <EmptyState
                icon={<KeyRound className="w-5 h-5" />}
                title="No provider credentials stored"
                description="Configure OpenRouter, Groq, OpenAI, Ollama or custom OpenAI-compatible endpoint credentials first."
                action={
                  <Button variant="primary" size="xs" icon={<KeyRound className="w-3.5 h-3.5" />} onClick={openAddProviderModal}>
                    Configure Provider
                  </Button>
                }
              />
            ) : (
              <div className="space-y-2">
                {providers.map((p) => {
                  const testStatus = providerTestStatus[p.provider_name];
                  const matchedPreset = findProviderPreset(p.provider_name, p.base_url);
                  return (
                    <div key={p.provider_name} className="flex items-center justify-between py-2.5 px-3.5 bg-neutral-950 border border-neutral-800 rounded-lg group">
                      <div className="flex items-center gap-3">
                        <div className="w-8 h-8 rounded-lg bg-amber-500/10 border border-amber-500/15 flex items-center justify-center shrink-0">
                          <KeyRound className="w-4 h-4 text-amber-400" />
                        </div>
                        <div>
                          <div className="flex items-center gap-2 flex-wrap">
                            <span className="text-sm font-semibold text-neutral-200">{p.provider_name}</span>
                            {matchedPreset && (
                              <Badge variant={
                                matchedPreset.category === 'Gateway' ? 'purple' :
                                matchedPreset.category === 'Fast Inference' ? 'warning' :
                                'info'
                              }>
                                {matchedPreset.name}
                              </Badge>
                            )}
                            <Badge variant="purple">AES-256-GCM</Badge>
                            {testStatus && (
                              <Badge variant={testStatus.success ? 'success' : 'danger'} dot>
                                {testStatus.message}
                              </Badge>
                            )}
                          </div>
                          <p className="text-[11px] font-mono text-neutral-500 mt-0.5">{p.base_url || '(Default endpoint)'}</p>
                        </div>
                      </div>
                      <div className="flex items-center gap-2.5">
                        <span className="text-[11px] font-mono text-neutral-600 hidden sm:inline">Key: {p.masked_api_key}</span>
                        <Button
                          size="xs"
                          variant="secondary"
                          icon={<Activity className="w-3 h-3 text-neutral-400" />}
                          loading={testingProvider === p.provider_name}
                          onClick={() => handleTestProvider(p.provider_name)}
                        >
                          Test Connection
                        </Button>
                        <button
                          onClick={() => openEditProviderModal(p)}
                          className="p-1 text-neutral-500 hover:text-neutral-200 hover:bg-neutral-800 rounded transition-colors opacity-0 group-hover:opacity-100"
                          title="Edit provider credentials"
                        >
                          <Pencil className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => handleDeleteProvider(p.provider_name)}
                          className="p-1 text-neutral-500 hover:text-red-400 hover:bg-neutral-800 rounded transition-colors opacity-0 group-hover:opacity-100"
                          title="Delete provider"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </Card>

          {/* STEP 2: Logical Models */}
          <Card padding="lg">
            <div className="flex items-center justify-between mb-5">
              <div className="flex items-start gap-2.5">
                <span className="text-[11px] font-bold font-mono px-2 py-0.5 rounded bg-blue-500/10 text-blue-400 border border-blue-500/20 mt-0.5">
                  Step 2
                </span>
                <CardHeader
                  title="Logical Model Routing"
                  description="Map logical aliases (fast, reasoning, vision) to configured provider deployments with failover."
                  icon={<Cpu className="w-4 h-4 text-blue-400" />}
                />
              </div>
              <Button
                variant="secondary"
                size="xs"
                icon={<Plus className="w-3.5 h-3.5" />}
                disabled={providers.length === 0}
                title={providers.length === 0 ? 'Configure at least one provider in Step 1 first' : undefined}
                onClick={openCreateModelModal}
              >
                Map Model
              </Button>
            </div>

            {providers.length === 0 ? (
              <div className="py-8 px-4 text-center border border-dashed border-amber-500/20 bg-amber-500/5 rounded-xl">
                <KeyRound className="w-8 h-8 text-amber-400/70 mx-auto mb-2.5" />
                <h3 className="text-sm font-semibold text-neutral-200">No Providers Configured</h3>
                <p className="text-xs text-neutral-500 max-w-md mx-auto mt-1 mb-4">
                  You must add and configure upstream provider credentials in Step 1 before you can map logical model routings.
                </p>
                <Button
                  variant="primary"
                  size="xs"
                  icon={<KeyRound className="w-3.5 h-3.5" />}
                  onClick={() => {
                    setModalTestStatus(null);
                    setShowProviderModal(true);
                  }}
                >
                  Configure Provider First
                </Button>
              </div>
            ) : models.length === 0 ? (
              <EmptyState
                icon={<Cpu className="w-5 h-5" />}
                title="No model routings configured"
                description="Create aliases like fast, reasoning, or vision that route requests to provider deployments."
                action={
                  <Button variant="secondary" size="xs" icon={<Plus className="w-3.5 h-3.5" />} onClick={openCreateModelModal}>
                    Map Model
                  </Button>
                }
              />
            ) : (
              <div className="space-y-2">
                {models.map((m) => (
                  <div key={m.logical_model} className="flex items-center justify-between py-2.5 px-3.5 bg-neutral-950 border border-neutral-800 rounded-lg group">
                    <div className="flex items-center gap-3 flex-1 min-w-0">
                      <div className="w-2 h-2 rounded-full bg-emerald-500 flex-shrink-0" />
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-mono font-semibold text-emerald-400">{m.logical_model}</span>
                          <Badge variant="mono">{m.adapter}</Badge>
                        </div>
                        {(() => {
                          const fbs = normalizeFallbacks(m.fallback_deployments);
                          return fbs.length > 0 ? (
                            <div className="flex items-center gap-1.5 mt-1 flex-wrap">
                              <span className="text-[10px] text-neutral-600 font-medium">Fallbacks:</span>
                              {fbs.map((fb, idx) => {
                                let prov = m.adapter;
                                let dep = fb;
                                if (fb.includes('/')) {
                                  const slashIdx = fb.indexOf('/');
                                  prov = fb.slice(0, slashIdx);
                                  dep = fb.slice(slashIdx + 1);
                                } else if (fb.includes(':')) {
                                  const colonIdx = fb.indexOf(':');
                                  prov = fb.slice(0, colonIdx);
                                  dep = fb.slice(colonIdx + 1);
                                }
                                return (
                                  <span
                                    key={idx}
                                    className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-800 text-neutral-300 flex items-center gap-1"
                                  >
                                    <span className="text-blue-400 font-semibold">{prov}</span>
                                    <span className="text-neutral-600">/</span>
                                    <span>{dep}</span>
                                  </span>
                                );
                              })}
                            </div>
                          ) : null;
                        })()}
                      </div>
                    </div>
                    <div className="flex items-center gap-3">
                      <div className="flex items-center gap-1.5 text-xs text-neutral-500">
                        <ArrowRight className="w-3.5 h-3.5 text-neutral-700" />
                        <span className="font-mono text-neutral-400">{m.deployment}</span>
                      </div>
                      {m.input_cost_per_1m !== undefined && m.input_cost_per_1m !== null && (
                        <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-amber-500/10 border border-amber-500/20 text-amber-300" title="Custom token rate per 1M (Input / Output)">
                          ${m.input_cost_per_1m} / ${m.output_cost_per_1m ?? 0} / 1M
                        </span>
                      )}
                      <div className="flex items-center gap-1">
                        <button
                          onClick={() => openEditModelModal(m)}
                          className="p-1 text-neutral-600 hover:text-neutral-200 hover:bg-neutral-800 rounded transition-colors opacity-0 group-hover:opacity-100"
                          title="Edit model routing"
                        >
                          <Pencil className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => handleDeleteModel(m.logical_model)}
                          className="p-1 text-neutral-600 hover:text-red-400 hover:bg-neutral-800 rounded transition-colors opacity-0 group-hover:opacity-100"
                          title="Delete model routing"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>
      )}

      {/* Model Modal */}
      <Modal
        open={showModelModal}
        onClose={() => setShowModelModal(false)}
        title="Map Logical Model"
        description="Create an alias that routes to a specific provider deployment with cross-provider failover, capabilities, and pricing."
        size="2xl"
      >
        <form onSubmit={handleSaveModel} className="space-y-5">
          {/* Top 2-Column Grid: Primary Routing & Model Capabilities */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-5 items-start">
            {/* Left Column: Primary Route & Pricing */}
            <div className="space-y-4 bg-neutral-950/60 border border-neutral-800/80 rounded-xl p-4">
              <div className="flex items-center gap-2 pb-2 border-b border-neutral-800/60">
                <Cpu className="w-4 h-4 text-emerald-400" />
                <h3 className="text-xs font-semibold text-neutral-200 uppercase tracking-wider">Primary Route</h3>
              </div>

              <div>
                <Input
                  label="Logical Model Alias"
                  required
                  placeholder="e.g. fast, reasoning, vision, chat"
                  value={logicalModel}
                  onChange={(e) => setLogicalModel(e.target.value)}
                  className="font-mono text-xs"
                />
                <div className="flex items-center gap-1.5 mt-2 flex-wrap">
                  <span className="text-[10px] text-neutral-500 font-medium">Suggestions:</span>
                  {['fast', 'reasoning', 'vision', 'chat'].map((alias) => (
                    <button
                      key={alias}
                      type="button"
                      onClick={() => setLogicalModel(alias)}
                      className={`text-[10px] font-mono px-2 py-0.5 rounded transition-all ${
                        logicalModel === alias
                          ? 'bg-emerald-500/20 text-emerald-300 border border-emerald-500/40 font-semibold'
                          : 'bg-neutral-900 border border-neutral-800 text-neutral-400 hover:text-neutral-200 hover:border-neutral-700'
                      }`}
                    >
                      {alias}
                    </button>
                  ))}
                </div>
              </div>

              <Select
                label="Primary Provider"
                value={selectedProvider}
                onChange={(e) => handleProviderChange(e.target.value)}
                options={providers.map((p) => ({
                  value: p.provider_name,
                  label: `${p.provider_name}${p.base_url ? ` (${p.base_url})` : ''}`,
                }))}
                hint="Select the upstream provider for this logical model."
              />

              {/* Provider Deployment with SearchableSelect */}
              <SearchableSelect
                label={
                  <div className="flex items-center justify-between w-full">
                    <span>
                      Primary Deployment (Model) <span className="text-red-400">*</span>
                    </span>
                    <button
                      type="button"
                      onClick={() => loadProviderModels(selectedProvider, deployment)}
                      disabled={loadingModels || !selectedProvider}
                      className="text-[11px] text-blue-400 hover:text-blue-300 flex items-center gap-1 transition-colors disabled:opacity-50"
                      title="Re-fetch models from selected provider"
                    >
                      <RefreshCw className={`w-3 h-3 ${loadingModels ? 'animate-spin' : ''}`} />
                      Refresh
                    </button>
                  </div>
                }
                value={deployment}
                onChange={(val) => setDeployment(val)}
                options={availableModels.map((m) => ({ value: m, label: m }))}
                placeholder={
                  loadingModels
                    ? `Fetching models from ${selectedProvider}...`
                    : 'Search or select a model deployment...'
                }
                searchPlaceholder="Search model deployment or enter custom..."
                allowCustom={true}
                disabled={loadingModels}
                hint={
                  fetchModelsError
                    ? `Could not auto-fetch models (${fetchModelsError}). Type custom model name.`
                    : availableModels.length > 0
                    ? `${availableModels.length} models fetched from ${selectedProvider}.`
                    : 'No models returned by endpoint. Type custom model name.'
                }
              />

              {/* Custom Token Pricing inside left column */}
              <div className="pt-2 border-t border-neutral-800/60 space-y-2">
                <div>
                  <label className="block text-xs font-medium text-neutral-300">
                    Custom Token Pricing ($ / 1M - Optional)
                  </label>
                  <p className="text-[10px] text-neutral-500">
                    Overrides catalog rate for enterprise or custom endpoints.
                  </p>
                </div>
                <div className="grid grid-cols-2 gap-2.5">
                  <Input
                    label="Input ($ / 1M)"
                    placeholder="e.g. 2.50"
                    value={inputCost}
                    onChange={(e) => setInputCost(e.target.value)}
                    type="number"
                    step="any"
                  />
                  <Input
                    label="Output ($ / 1M)"
                    placeholder="e.g. 10.00"
                    value={outputCost}
                    onChange={(e) => setOutputCost(e.target.value)}
                    type="number"
                    step="any"
                  />
                </div>
              </div>
            </div>

            {/* Right Column: Model Capabilities (Modalities) */}
            <div className="space-y-4 bg-neutral-950/60 border border-neutral-800/80 rounded-xl p-4">
              <div className="flex items-center justify-between pb-2 border-b border-neutral-800/60">
                <div className="flex items-center gap-2">
                  <Sparkles className="w-4 h-4 text-purple-400" />
                  <h3 className="text-xs font-semibold text-neutral-200 uppercase tracking-wider">Model Capabilities</h3>
                </div>
                <span className="text-[10px] text-neutral-500 font-mono">Routing Metadata</span>
              </div>
              <p className="text-[11px] text-neutral-400 leading-relaxed">
                Declare the modalities this model supports. Used to validate routing rules and filter model pickers.
              </p>

              {/* Input Modalities */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium text-neutral-300 flex items-center gap-1.5">
                    Input Modalities
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-neutral-800 text-neutral-400 font-mono">
                      {inputModalities.length} selected
                    </span>
                  </span>
                  <div className="flex items-center gap-1 text-[10px]">
                    <button
                      type="button"
                      onClick={() => setInputModalities(['text'])}
                      className="text-neutral-500 hover:text-neutral-300 px-1.5 py-0.5 rounded hover:bg-neutral-800 transition-colors"
                    >
                      Text only
                    </button>
                    <span className="text-neutral-700">•</span>
                    <button
                      type="button"
                      onClick={() => setInputModalities([...ALL_MODALITIES])}
                      className="text-neutral-500 hover:text-neutral-300 px-1.5 py-0.5 rounded hover:bg-neutral-800 transition-colors"
                    >
                      All
                    </button>
                  </div>
                </div>
                <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                  {ALL_MODALITIES.map((m) => {
                    const active = inputModalities.includes(m);
                    return (
                      <button
                        key={m}
                        type="button"
                        onClick={() => toggleModality(setInputModalities, m)}
                        className={`flex items-center justify-between px-3 py-2 rounded-lg text-xs font-medium border transition-all ${
                          active
                            ? 'bg-emerald-500/10 border-emerald-500/40 text-emerald-300 shadow-sm'
                            : 'bg-neutral-900/60 border-neutral-800/80 text-neutral-400 hover:text-neutral-200 hover:border-neutral-700'
                        }`}
                      >
                        <div className="flex items-center gap-2">
                          <span className={active ? 'text-emerald-400' : 'text-neutral-500'}>
                            {getModalityIcon(m)}
                          </span>
                          <span className="capitalize">{m}</span>
                        </div>
                        {active && <Check className="w-3.5 h-3.5 text-emerald-400 shrink-0" />}
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* Output Modalities */}
              <div className="space-y-2 pt-2 border-t border-neutral-800/40">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium text-neutral-300 flex items-center gap-1.5">
                    Output Modalities
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-neutral-800 text-neutral-400 font-mono">
                      {outputModalities.length} selected
                    </span>
                  </span>
                  <div className="flex items-center gap-1 text-[10px]">
                    <button
                      type="button"
                      onClick={() => setOutputModalities(['text'])}
                      className="text-neutral-500 hover:text-neutral-300 px-1.5 py-0.5 rounded hover:bg-neutral-800 transition-colors"
                    >
                      Text only
                    </button>
                    <span className="text-neutral-700">•</span>
                    <button
                      type="button"
                      onClick={() => setOutputModalities([...ALL_MODALITIES])}
                      className="text-neutral-500 hover:text-neutral-300 px-1.5 py-0.5 rounded hover:bg-neutral-800 transition-colors"
                    >
                      All
                    </button>
                  </div>
                </div>
                <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                  {ALL_MODALITIES.map((m) => {
                    const active = outputModalities.includes(m);
                    return (
                      <button
                        key={m}
                        type="button"
                        onClick={() => toggleModality(setOutputModalities, m)}
                        className={`flex items-center justify-between px-3 py-2 rounded-lg text-xs font-medium border transition-all ${
                          active
                            ? 'bg-purple-500/10 border-purple-500/40 text-purple-300 shadow-sm'
                            : 'bg-neutral-900/60 border-neutral-800/80 text-neutral-400 hover:text-neutral-200 hover:border-neutral-700'
                        }`}
                      >
                        <div className="flex items-center gap-2">
                          <span className={active ? 'text-purple-400' : 'text-neutral-500'}>
                            {getModalityIcon(m)}
                          </span>
                          <span className="capitalize">{m}</span>
                        </div>
                        {active && <Check className="w-3.5 h-3.5 text-purple-400 shrink-0" />}
                      </button>
                    );
                  })}
                </div>
              </div>
            </div>
          </div>

          {/* Full-Width Bottom Section: Fallback Deployments (Failover Chain) */}
          <div className="space-y-3 pt-3 border-t border-neutral-800/80">
            <div className="flex items-center justify-between gap-4">
              <div>
                <div className="flex items-center gap-2">
                  <Layers className="w-4 h-4 text-blue-400" />
                  <label className="text-xs font-semibold text-neutral-200 uppercase tracking-wider">
                    Fallback Deployments (Failover Chain)
                  </label>
                  {fallbackChain.length > 0 && (
                    <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-blue-500/10 border border-blue-500/20 text-blue-400">
                      {fallbackChain.length} configured
                    </span>
                  )}
                </div>
                <p className="text-[11px] text-neutral-500 mt-0.5 leading-relaxed">
                  Sequential failover across providers if the primary deployment fails.
                </p>
              </div>
              <Button
                type="button"
                variant="secondary"
                size="xs"
                className="shrink-0"
                icon={<Plus className="w-3.5 h-3.5" />}
                onClick={handleAddFallback}
              >
                Add Fallback
              </Button>
            </div>

            {fallbackChain.length === 0 ? (
              <div className="p-4 rounded-xl border border-dashed border-neutral-800 bg-neutral-950/40 text-center">
                <p className="text-xs text-neutral-500">
                  No fallback deployments configured. Click &quot;Add Fallback&quot; above to set up a failover chain across providers.
                </p>
              </div>
            ) : (
              <div className="space-y-2">
                {fallbackChain.map((fb, idx) => {
                  const modelsForProv = providerModelsCache[fb.provider] || [];
                  const isLoadingModels = loadingProviderModels[fb.provider] || false;

                  return (
                    <div
                      key={fb.id}
                      className="p-3 rounded-xl border border-neutral-800 bg-neutral-950/80 space-y-2"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="text-[10px] font-mono font-semibold px-2 py-0.5 rounded bg-blue-500/10 border border-blue-500/20 text-blue-400">
                            #{idx + 1} Failover
                          </span>
                          <span className="text-xs text-neutral-400">Provider:</span>
                          <div className="relative inline-flex items-center">
                            <select
                              value={fb.provider}
                              onChange={(e) => handleFallbackProviderChange(idx, e.target.value)}
                              className="appearance-none bg-neutral-900 border border-neutral-800 hover:border-neutral-700 rounded-md pl-2.5 pr-7 py-1 text-xs text-neutral-200 font-mono focus:border-blue-500 focus:outline-none cursor-pointer transition-colors"
                            >
                              {providers.map((p) => (
                                <option key={p.provider_name} value={p.provider_name} className="bg-neutral-900">
                                  {p.provider_name}
                                </option>
                              ))}
                            </select>
                            <ChevronDown className="w-3.5 h-3.5 text-neutral-400 absolute right-2 pointer-events-none" />
                          </div>
                        </div>
                        <div className="flex items-center gap-1 shrink-0">
                          <button
                            type="button"
                            onClick={() => handleMoveFallback(idx, 'up')}
                            disabled={idx === 0}
                            className="p-1 text-neutral-500 hover:text-neutral-200 disabled:opacity-30 rounded hover:bg-neutral-900 transition-colors"
                            title="Move Up"
                          >
                            <ChevronUp className="w-3.5 h-3.5" />
                          </button>
                          <button
                            type="button"
                            onClick={() => handleMoveFallback(idx, 'down')}
                            disabled={idx === fallbackChain.length - 1}
                            className="p-1 text-neutral-500 hover:text-neutral-200 disabled:opacity-30 rounded hover:bg-neutral-900 transition-colors"
                            title="Move Down"
                          >
                            <ChevronDown className="w-3.5 h-3.5" />
                          </button>
                          <button
                            type="button"
                            onClick={() => handleRemoveFallback(idx)}
                            className="p-1 text-neutral-500 hover:text-red-400 rounded hover:bg-neutral-900 transition-colors"
                            title="Remove Fallback"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </div>

                      <SearchableSelect
                        value={fb.deployment}
                        onChange={(val) => handleFallbackDeploymentChange(idx, val)}
                        options={modelsForProv.map((m) => ({ value: m, label: m }))}
                        placeholder={
                          isLoadingModels
                            ? `Fetching models for ${fb.provider}...`
                            : `Search or enter model deployment on ${fb.provider}...`
                        }
                        searchPlaceholder={`Search model or enter custom for ${fb.provider}...`}
                        allowCustom={true}
                        disabled={isLoadingModels}
                        hint={
                          modelsForProv.length > 0
                            ? `${modelsForProv.length} models available on ${fb.provider}`
                            : `Select or type custom model name for ${fb.provider}`
                        }
                      />
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Footer Actions */}
          <div className="flex items-center justify-end gap-2 pt-3 border-t border-neutral-800/80">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowModelModal(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" size="sm" loading={savingModel}>
              Save Routing
            </Button>
          </div>
        </form>
      </Modal>

      {/* Provider Modal */}
      <Modal
        open={showProviderModal}
        onClose={() => { setShowProviderModal(false); setModalTestStatus(null); }}
        title={isEditingProvider ? `Edit Provider (${providerName})` : 'Add Upstream AI Provider'}
        description="Select from 25+ OpenAI-compatible providers (auto-fills base URL) or connect a custom endpoint. Credentials are encrypted at rest with AES-256-GCM."
      >
        <form onSubmit={handleSaveProvider} className="space-y-4">
          {/* Provider Preset / Type Picker */}
          <div className="space-y-2">
            <SearchableSelect
              label={
                <div className="flex items-center justify-between w-full">
                  <span>OpenAI-Compatible Provider Preset</span>
                  {selectedPreset === 'custom' && (
                    <span className="text-[10px] text-amber-400 font-mono">Custom Endpoint Mode</span>
                  )}
                </div>
              }
              value={selectedPreset}
              onChange={handleSelectPreset}
              options={providerOptions}
              placeholder="Search OpenRouter, Groq, DeepSeek, Together, Ollama, custom..."
              searchPlaceholder="Search 25+ providers, base URLs, or enter custom..."
              allowCustom={true}
              customLabel={(val) => (
                <span>Use custom provider: &quot;<strong className="text-emerald-300">{val}</strong>&quot;</span>
              )}
              hint="Selecting a provider automatically fills its official OpenAI-compatible Base URL."
            />

            {/* Quick 1-click popular pills */}
            <div className="flex items-center gap-1.5 flex-wrap pt-0.5">
              <span className="text-[10px] text-neutral-500 font-medium shrink-0">Popular:</span>
              {POPULAR_PRESET_IDS.map((pid) => {
                const p = OPENAI_COMPATIBLE_PROVIDERS.find((item) => item.id === pid);
                if (!p) return null;
                const isSelected = selectedPreset === p.id;
                return (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => handleSelectPreset(p.id)}
                    className={`text-[10px] font-mono px-2 py-0.5 rounded border transition-colors ${
                      isSelected
                        ? 'bg-blue-500/20 border-blue-500/50 text-blue-300 font-semibold'
                        : 'bg-neutral-900 border-neutral-800 text-neutral-400 hover:text-neutral-200 hover:border-neutral-700'
                    }`}
                  >
                    {p.name.replace(' (Local)', '').replace(' (Local / Server)', '').replace(' Endpoint', '')}
                  </button>
                );
              })}
            </div>
          </div>

          {/* Active Preset Info Callout */}
          {currentPreset && (
            <div className="p-2.5 rounded-lg border border-neutral-800/80 bg-neutral-950/70 flex items-center justify-between gap-3 text-xs">
              <div className="flex items-center gap-2 flex-wrap min-w-0">
                <span className="font-semibold text-neutral-200">{currentPreset.name}</span>
                <Badge variant={
                  currentPreset.category === 'Gateway' ? 'purple' :
                  currentPreset.category === 'Fast Inference' ? 'warning' :
                  currentPreset.category === 'Custom' ? 'mono' : 'info'
                }>
                  {currentPreset.category}
                </Badge>
                {providerName && (
                  <span className="text-[10px] font-mono text-neutral-500">
                    ID: <span className="text-neutral-300 font-semibold">{providerName}</span>
                  </span>
                )}
              </div>
              {currentPreset.apiKeyHelpUrl && (
                <a
                  href={currentPreset.apiKeyHelpUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-[11px] text-blue-400 hover:text-blue-300 flex items-center gap-1 shrink-0 px-2 py-0.5 rounded bg-blue-500/10 border border-blue-500/20 hover:border-blue-500/40 transition-colors"
                >
                  <span>Get Key</span>
                  <ExternalLink className="w-3 h-3" />
                </a>
              )}
            </div>
          )}

          {/* Custom Provider Identifier (Only shown if custom endpoint) */}
          {selectedPreset === 'custom' && (
            <Input
              label="Custom Provider Identifier"
              required
              placeholder="e.g. custom-llm, private-cluster, proxy"
              value={providerName}
              onChange={(e) => setProviderName(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, '-'))}
              className="font-mono text-xs"
              hint="Unique identifier referenced when configuring model routings"
            />
          )}

          {/* Full-width Base URL (OpenAI-Compatible) */}
          <Input
            label={
              <div className="flex items-center justify-between w-full">
                <span>Base URL (OpenAI-Compatible) <span className="text-red-400">*</span></span>
                {selectedPreset !== 'custom' && providerName && (
                  <span className="text-[10px] text-neutral-500 font-mono">
                    Provider ID: <span className="text-neutral-300 font-semibold">{providerName}</span>
                  </span>
                )}
              </div>
            }
            required
            placeholder={currentPreset?.baseUrl || "https://your-custom-endpoint.com/v1"}
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            className="font-mono text-xs"
            hint="Root OpenAI-compatible endpoint supporting /chat/completions and /models"
          />

          {/* API Key */}
          <div className="space-y-1">
            <label className="block text-xs font-medium text-neutral-400">
              API Key / Secret Token <span className="text-red-400">*</span>
            </label>
            <div className="relative">
              <input
                type={showApiKey ? 'text' : 'password'}
                required
                placeholder={currentPreset?.placeholderKey || '••••••••••••••••'}
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                className="w-full bg-neutral-950 border border-neutral-800 focus:border-neutral-600 rounded-lg px-3 py-2 pr-10 text-sm text-neutral-100 placeholder-neutral-600 focus:outline-none transition-colors font-mono"
              />
              <button
                type="button"
                onClick={() => setShowApiKey(!showApiKey)}
                className="absolute inset-y-0 right-0 flex items-center pr-3 text-neutral-500 hover:text-neutral-300 transition-colors"
                title={showApiKey ? 'Hide key' : 'Show key'}
              >
                {showApiKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
              </button>
            </div>
            <p className="text-[11px] text-neutral-600">
              Keys are envelope-encrypted with AES-256-GCM before storage in the database.
            </p>
          </div>

          {modalTestStatus && (
            <div className={`p-3 rounded-lg border text-xs flex items-start gap-2 ${
              modalTestStatus.success ? 'bg-emerald-500/10 border-emerald-500/20 text-emerald-300' : 'bg-red-500/10 border-red-500/20 text-red-300'
            }`}>
              {modalTestStatus.success ? <CheckCircle className="w-4 h-4 shrink-0 text-emerald-400 mt-0.5" /> : <AlertTriangle className="w-4 h-4 shrink-0 text-red-400 mt-0.5" />}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <p className="font-semibold">{modalTestStatus.success ? 'Connection Successful' : 'Connection Failed'}</p>
                  {modalTestStatus.latency_ms !== undefined && (
                    <span className="font-mono text-[10px] px-1 rounded bg-neutral-900 border border-neutral-800 text-neutral-400">
                      {modalTestStatus.latency_ms}ms
                    </span>
                  )}
                </div>
                <p className="text-[11px] mt-0.5 opacity-90 break-words">{modalTestStatus.message}</p>
              </div>
            </div>
          )}

          <div className="flex items-center justify-between pt-1">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              icon={<Activity className="w-3.5 h-3.5" />}
              loading={testingModal}
              disabled={!baseUrl.trim() && selectedPreset !== 'litellm'}
              onClick={handleTestModal}
            >
              Test Connection
            </Button>
            <div className="flex gap-2">
              <Button type="button" variant="ghost" size="sm" onClick={() => setShowProviderModal(false)}>Cancel</Button>
              <Button type="submit" variant="primary" size="sm" loading={savingProvider} icon={<KeyRound className="w-3.5 h-3.5" />}>
                {isEditingProvider ? 'Update Credentials' : 'Save Provider'}
              </Button>
            </div>
          </div>
        </form>
      </Modal>
    </div>
  );
};
