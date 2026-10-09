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
  MemoryConfig,
  ProviderConfig,
  AuthConfig,
  SearchConfig,
  AppAPIKey,
  TestTokenResult,
  ModelRoutes,
  ChatAttachment,
  MediaRef,
  Modality,
  ALL_MODALITIES,
  parseAuthConfig,
} from '../api/client';
import { RoutingEditor, parseModelRoutes, validateRoutes } from '../components/RoutingEditor';
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
  ShieldCheck,
  Zap,
  Clock,
  Wrench,
  FileCode,
  RefreshCw,
  Trash2,
  X,
  Brain,
  Radio,
  AlertTriangle,
  AlertCircle,
  Play,
  Search,
  KeyRound,
  Pencil,
  Plus,
  ChevronDown,
  ChevronRight,
  CheckCircle2,
  XCircle,
  Info,
  Paperclip,
  Image as ImageIcon,
  Link as LinkIcon,
  Sparkles,
  Music,
  Video as VideoIcon,
} from 'lucide-react';
import { Card, CardHeader } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Markdown } from '../components/Markdown';
import { Alert } from '../components/ui/Feedback';
import { MediaAttachmentPreview } from '../components/playground/MediaAttachmentPreview';
import { MediaMessageContent } from '../components/playground/MediaMessageContent';
import { Input, Textarea, Select, Toggle } from '../components/ui/Form';
import { formatRelativeTime } from '../lib/utils';

interface AppWorkspaceProps {
  appId: string;
  onBack: () => void;
  onRefreshApps: () => void;
}

type WorkspaceTab = 'overview' | 'policies' | 'identity' | 'routing' | 'settings' | 'keys' | 'playground';

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
      flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-all whitespace-nowrap shrink-0
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

export interface ChatMessageItem {
  role: string;
  content: string;
  thinking?: string;
  isStreaming?: boolean;
  attachments?: ChatAttachment[];
  parts?: MediaRef[];
  routedModel?: string;
}

function separateThinkingAndContent(content: string, rawThinking?: string): { thinking: string; content: string; isThinkingOpen: boolean } {
  let thinking = rawThinking || '';
  let cleanContent = content;

  if (cleanContent.includes('<think>')) {
    const parts = cleanContent.split('<think>');
    const before = parts[0];
    const after = parts.slice(1).join('<think>');
    if (after.includes('</think>')) {
      const [tPart, ...rest] = after.split('</think>');
      thinking = (thinking ? thinking + '\n' : '') + tPart.trim();
      cleanContent = (before + rest.join('</think>')).trim();
      return { thinking, content: cleanContent, isThinkingOpen: false };
    } else {
      thinking = (thinking ? thinking + '\n' : '') + after;
      cleanContent = before.trim();
      return { thinking, content: cleanContent, isThinkingOpen: true };
    }
  }

  return { thinking, content: cleanContent, isThinkingOpen: false };
}

const ThinkingBlock: React.FC<{
  thinking: string;
  isStreaming?: boolean;
  hasContent?: boolean;
}> = ({ thinking, isStreaming, hasContent }) => {
  const [isOpen, setIsOpen] = useState(Boolean(isStreaming && !hasContent));
  const wasStreamingRef = React.useRef(isStreaming);
  const hadContentRef = React.useRef(hasContent);
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);

  // When answer content starts arriving or stream ends, auto-collapse so the response takes focus
  React.useEffect(() => {
    if (!hadContentRef.current && hasContent) {
      setIsOpen(false);
    }
    if (wasStreamingRef.current && !isStreaming) {
      setIsOpen(false);
    }
    wasStreamingRef.current = isStreaming;
    hadContentRef.current = hasContent;
  }, [isStreaming, hasContent]);

  // Keep the thinking container pinned to the bottom as new tokens arrive (if autoScroll is active)
  React.useEffect(() => {
    if (isStreaming && autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [thinking, isStreaming, autoScroll]);

  if (!thinking && !isStreaming) return null;

  // Extract the latest thought line for the live ticker preview
  const lastLine = thinking.trim().split('\n').filter(Boolean).pop() || '';
  const latestThoughtPreview = lastLine.length > 70 ? '...' + lastLine.slice(-70) : lastLine;

  const scrollToBottom = () => {
    setAutoScroll(true);
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  };

  return (
    <div className="mb-2.5 border border-purple-500/25 bg-purple-950/20 rounded-xl overflow-hidden text-xs">
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center justify-between px-3 py-1.5 bg-purple-950/40 hover:bg-purple-950/60 transition-colors text-purple-300 font-medium text-[11px] gap-2"
      >
        <div className="flex items-center gap-1.5 min-w-0 flex-1">
          <Brain className={`w-3.5 h-3.5 text-purple-400 shrink-0 ${isStreaming ? 'animate-pulse' : ''}`} />
          <span className="shrink-0">{isStreaming ? 'Thinking...' : 'Thought Process'}</span>
          {isStreaming ? (
            <span className="flex gap-1 items-center ml-0.5 shrink-0">
              <span className="w-1 h-1 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
              <span className="w-1 h-1 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
              <span className="w-1 h-1 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
            </span>
          ) : null}

          {/* Live single-line thought ticker when collapsed or running */}
          {!isOpen && isStreaming && latestThoughtPreview && (
            <span className="text-neutral-400 font-mono text-[10px] truncate max-w-[260px] italic ml-1 opacity-80">
              &quot;{latestThoughtPreview}&quot;
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 text-purple-400/80 text-[10px] shrink-0">
          {thinking.length > 0 && <span>{thinking.length} chars</span>}
          {isOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
        </div>
      </button>
      {isOpen && (
        <div className="relative">
          <div
            ref={scrollRef}
            onScroll={(e) => {
              const target = e.currentTarget;
              const isAtBottom = target.scrollHeight - target.scrollTop - target.clientHeight < 35;
              setAutoScroll(isAtBottom);
            }}
            className="p-3 border-t border-purple-500/10 bg-neutral-950/80 text-[11px] font-mono text-neutral-300 max-h-52 overflow-y-auto leading-relaxed whitespace-pre-wrap select-text scroll-smooth"
          >
            {thinking || <span className="italic text-neutral-500">Formulating response...</span>}
            {isStreaming && (
              <span className="inline-block w-1.5 h-3.5 bg-purple-400 animate-pulse ml-1 align-middle" />
            )}
          </div>
          {isStreaming && !autoScroll && (
            <button
              type="button"
              onClick={scrollToBottom}
              className="absolute bottom-2 right-3 px-2 py-0.5 rounded-full bg-purple-600/90 text-white text-[10px] font-medium shadow-md hover:bg-purple-500 transition-colors flex items-center gap-1"
            >
              ↓ Latest thoughts
            </button>
          )}
        </div>
      )}
    </div>
  );
};

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
  const [providers, setProviders] = useState<ProviderConfig[]>([]);

  // Settings form state
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [promptId, setPromptId] = useState('');
  const [promptVersion, setPromptVersion] = useState(1);
  const [modelRoutingId, setModelRoutingId] = useState('');
  const [modelRoutes, setModelRoutes] = useState<ModelRoutes>({ default: '', rules: [] });
  const defaultModel = modelRoutes.default || modelRoutingId;
  const [accessPolicy, setAccessPolicy] = useState('');
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const [rpmLimit, setRpmLimit] = useState(60);
  const [dailyTokenLimit, setDailyTokenLimit] = useState(100000);
  const [proTierRpm, setProTierRpm] = useState(500);
  const [enableTierCondition, setEnableTierCondition] = useState(false);
  const [tierExpression, setTierExpression] = useState("claims.plan == 'pro'");
  const [guardrails, setGuardrails] = useState<GuardrailConfig>({
    pii_detection: true, pii_redaction: true,
    prompt_injection_defense: true, content_safety: true, max_input_length: 8000,
    max_output_tokens: 4096, streaming_mode: 'buffered',
  });
  const [cacheConfig, setCacheConfig] = useState<CacheConfig>({
    enabled: true, ttl_seconds: 300, tool_caching: true, scope: 'user',
  });
  const [responseSchema, setResponseSchema] = useState('');
  const [memoryConfig, setMemoryConfig] = useState<MemoryConfig>({
    enabled: false,
    max_history: 10,
    vector_recall: false,
    recall_limit: 5,
    provider_type: 'postgres',
    http_url: '',
    http_auth_token: '',
    thread_ttl_sec: 86400,
  });
  const [searchConfig, setSearchConfig] = useState<SearchConfig>({
    enabled: false,
    max_results: 5,
    categories: ['general'],
    safe_search: 1,
  });

  // App-Level Authentication
  const [appAuthProvider, setAppAuthProvider] = useState<string>('anonymous');
  const [appAuthIssuer, setAppAuthIssuer] = useState<string>('');
  const [appAuthAudience, setAppAuthAudience] = useState<string>('');
  const [appAuthJwksUrl, setAppAuthJwksUrl] = useState<string>('');
  const [appAuthProjectId, setAppAuthProjectId] = useState<string>('');
  const [appAuthAllowAnonymous, setAppAuthAllowAnonymous] = useState<boolean>(false);
  const [savingAuth, setSavingAuth] = useState<boolean>(false);

  // JWT Inspector State
  const [testTokenInput, setTestTokenInput] = useState<string>('');
  const [testingToken, setTestingToken] = useState<boolean>(false);
  const [tokenResult, setTokenResult] = useState<TestTokenResult | null>(null);

  // CEL Policies State
  const [testClaims, setTestClaims] = useState<string>(
    JSON.stringify(
      {
        sub: 'usr_123',
        email: 'alex@example.com',
        email_verified: true,
        plan: 'pro',
        role: 'admin',
        tenant_id: 'acme_corp',
      },
      null,
      2
    )
  );
  const [testResult, setTestResult] = useState<{ valid: boolean; allowed: boolean; error?: string } | null>(null);
  const [testingPolicy, setTestingPolicy] = useState<boolean>(false);
  const [savingPolicy, setSavingPolicy] = useState<boolean>(false);

  // App API Keys
  const [appKeys, setAppKeys] = useState<AppAPIKey[]>([]);
  const [showKeyModal, setShowKeyModal] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyExpires, setNewKeyExpires] = useState<number | undefined>(undefined);
  const [createdKeySecret, setCreatedKeySecret] = useState<string | null>(null);
  const [creatingKey, setCreatingKey] = useState(false);
  const [keyError, setKeyError] = useState<string | null>(null);
  const [integrationAuthType, setIntegrationAuthType] = useState<'app_key' | 'jwt'>('app_key');

  // Playground state
  const [chatMessages, setChatMessages] = useState<ChatMessageItem[]>([
    { role: 'assistant', content: 'Ready to test. Type a message below.' },
  ]);
  const [userInput, setUserInput] = useState('');
  const [simulatedToken, setSimulatedToken] = useState('');
  const [chatLoading, setChatLoading] = useState(false);
  const [useStreaming, setUseStreaming] = useState(true);
  const [activeThreadId, setActiveThreadId] = useState<string>('');
  const [pendingConfirmation, setPendingConfirmation] = useState<{
    confirmation_token: string;
    tool_name: string;
    arguments: any;
  } | null>(null);
  const [confirmLoading, setConfirmLoading] = useState(false);
  const chatEndRef = React.useRef<HTMLDivElement>(null);

  // Multimedia Playground state
  const [pendingAttachments, setPendingAttachments] = useState<ChatAttachment[]>([]);
  const [selectedOutputModalities, setSelectedOutputModalities] = useState<Modality[]>(['text']);
  const [showUrlModal, setShowUrlModal] = useState(false);
  const [urlInput, setUrlInput] = useState('');
  const [urlModality, setUrlModality] = useState<Modality>('image');
  const fileInputRef = React.useRef<HTMLInputElement>(null);

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || []);
    if (files.length === 0) return;
    for (const file of files.slice(0, 5)) {
      let type: Modality = 'document';
      if (file.type.startsWith('image/')) type = 'image';
      else if (file.type.startsWith('audio/')) type = 'audio';
      else if (file.type.startsWith('video/')) type = 'video';
      else if (file.type === 'application/pdf') type = 'document';

      const reader = new FileReader();
      reader.onload = (loadEvent) => {
        const result = loadEvent.target?.result as string;
        const base64Data = result && result.includes(',') ? result.split(',')[1] : result;
        setPendingAttachments((prev) => [
          ...prev,
          {
            type,
            mime_type: file.type || 'application/octet-stream',
            data: base64Data,
            filename: file.name,
            size: file.size,
          },
        ]);
      };
      reader.readAsDataURL(file);
    }
    e.target.value = '';
  };

  const handlePaste = (e: React.ClipboardEvent) => {
    if (e.clipboardData && e.clipboardData.files && e.clipboardData.files.length > 0) {
      const files = Array.from(e.clipboardData.files);
      for (const file of files.slice(0, 5)) {
        let type: Modality = 'document';
        if (file.type.startsWith('image/')) type = 'image';
        else if (file.type.startsWith('audio/')) type = 'audio';
        else if (file.type.startsWith('video/')) type = 'video';

        const reader = new FileReader();
        reader.onload = (loadEvent) => {
          const result = loadEvent.target?.result as string;
          const base64Data = result && result.includes(',') ? result.split(',')[1] : result;
          setPendingAttachments((prev) => [
            ...prev,
            {
              type,
              mime_type: file.type || 'image/png',
              data: base64Data,
              filename: file.name || `pasted-${type}-${Date.now()}`,
              size: file.size,
            },
          ]);
        };
        reader.readAsDataURL(file);
      }
    }
  };

  const handleAddUrlAttachment = () => {
    if (!urlInput.trim()) return;
    const url = urlInput.trim();
    let mime = 'application/octet-stream';
    if (urlModality === 'image') mime = 'image/jpeg';
    else if (urlModality === 'audio') mime = 'audio/mp3';
    else if (urlModality === 'video') mime = 'video/mp4';
    else if (urlModality === 'document') mime = 'application/pdf';

    setPendingAttachments((prev) => [
      ...prev,
      {
        type: urlModality,
        mime_type: mime,
        url,
        filename: url.split('/').pop()?.split('?')[0] || url,
      },
    ]);
    setUrlInput('');
    setShowUrlModal(false);
  };

  // Server conversation thread history (saved threads with actual backend thread_id)
  type SavedThread = {
    id: string;
    title: string;
    messages: ChatMessageItem[];
    updatedAt: number;
  };
  const threadHistoryKey = `beavend.playground.threads.${api.currentProject}.${api.currentEnv}.${appId}`;
  const [savedThreads, setSavedThreads] = useState<SavedThread[]>([]);
  const [showHistory, setShowHistory] = useState(false);
  const [savingThread, setSavingThread] = useState(false);

  useEffect(() => {
    try {
      const raw = localStorage.getItem(threadHistoryKey);
      const parsed = raw ? JSON.parse(raw) : [];
      setSavedThreads(Array.isArray(parsed) ? parsed : []);
    } catch (_) {
      setSavedThreads([]);
    }
  }, [threadHistoryKey]);

  // Persist the current conversation to an actual server thread
  const handleSaveToThread = async () => {
    if (savingThread) return;
    setSavingThread(true);
    try {
      const turnsToSave = chatMessages
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m) => ({ role: m.role, content: m.content }));

      const res = await api.createThread(
        appId,
        {
          messages: turnsToSave,
          ttl_seconds: memoryConfig.thread_ttl_sec || 86400,
        },
        simulatedToken
      );

      const tid = res?.id || res?.thread_id || `th_${Math.random().toString(36).substring(2, 10)}`;
      setActiveThreadId(tid);

      const firstUser = chatMessages.find((m) => m.role === 'user');
      const threadItem: SavedThread = {
        id: tid,
        title: (firstUser?.content || 'Thread').slice(0, 50),
        messages: chatMessages,
        updatedAt: Date.now(),
      };

      setSavedThreads((prev) => {
        const next = [threadItem, ...prev.filter((t) => t.id !== tid)].slice(0, 30);
        try { localStorage.setItem(threadHistoryKey, JSON.stringify(next)); } catch (_) {}
        return next;
      });
    } catch (err: any) {
      alert(`Failed to save thread on server: ${err.message}`);
    } finally {
      setSavingThread(false);
    }
  };

  const startNewChat = () => {
    setActiveThreadId('');
    setChatMessages([{ role: 'assistant', content: 'Ready to test. Type a message below.' }]);
    setPendingAttachments([]);
    setPendingConfirmation(null);
    setShowHistory(false);
  };

  const openThread = async (t: SavedThread) => {
    if (chatLoading) return;
    setActiveThreadId(t.id);
    setShowHistory(false);
    // Attempt to hydrate freshest messages from server; fallback to saved messages
    try {
      const remote = await api.getThread(appId, t.id, simulatedToken);
      if (remote && Array.isArray(remote.messages) && remote.messages.length > 0) {
        setChatMessages(remote.messages.map((m: any) => ({
          role: m.role,
          content: m.content || '',
          parts: m.parts,
        })));
        return;
      }
    } catch (_) {}
    setChatMessages(t.messages);
    setPendingConfirmation(null);
  };

  const deleteThreadItem = async (tid: string) => {
    try {
      await api.deleteThread(appId, tid, simulatedToken);
    } catch (_) {}
    setSavedThreads((prev) => {
      const next = prev.filter((t) => t.id !== tid);
      try { localStorage.setItem(threadHistoryKey, JSON.stringify(next)); } catch (_) {}
      return next;
    });
    if (activeThreadId === tid) {
      setActiveThreadId('');
    }
  };

  const loadAll = async () => {
    setLoading(true);
    setErrorMsg(null);
    try {
      const [details, pList, mList, tList, provList, kList] = await Promise.all([
        api.getApp(appId),
        api.listPrompts().catch(() => []),
        api.listModels().catch(() => []),
        api.listTools().catch(() => []),
        api.listProviders().catch(() => []),
        api.listAppAPIKeys(appId).catch(() => []),
      ]);
      setAppDetails(details as AIAppDetails);
      setPrompts(Array.isArray(pList) ? pList : []);
      setModels(Array.isArray(mList) ? mList : []);
      setAvailableTools(Array.isArray(tList) ? tList : []);
      setProviders(Array.isArray(provList) ? provList : []);
      setAppKeys(Array.isArray(kList) ? kList : []);
      if (details?.app) {
        setName(details.app.name || '');
        setDescription(details.app.description || '');

        const appAuth = parseAuthConfig(details.app.auth || details.app.auth_config);
        if (appAuth && appAuth.provider) {
          const provider = String(appAuth.provider).toLowerCase();
          setAppAuthProvider(provider);
          setAppAuthIssuer(appAuth.issuer || '');
          setAppAuthAudience(Array.isArray(appAuth.audience) ? appAuth.audience.join(', ') : (typeof appAuth.audience === 'string' ? appAuth.audience : ''));
          setAppAuthJwksUrl(appAuth.jwks_url || '');
          setAppAuthProjectId(appAuth.project_id || '');
          setAppAuthAllowAnonymous(Boolean(appAuth.allow_anonymous));
        } else {
          setAppAuthProvider('anonymous');
          setAppAuthIssuer('');
          setAppAuthAudience('');
          setAppAuthJwksUrl('');
          setAppAuthProjectId('');
          setAppAuthAllowAnonymous(true);
        }
      }
      const activeVer = Array.isArray(details?.versions)
        ? (details.versions.find((v: any) => v.version === details.app?.active_version) || details.versions[0])
        : undefined;
      if (activeVer) {
        setPromptId(activeVer.prompt_id || '');
        setPromptVersion(activeVer.prompt_version || 1);
        setModelRoutingId(activeVer.model_routing_id || '');
        setModelRoutes(parseModelRoutes((details.app as any).model_routes, activeVer.model_routing_id || ''));
        setAccessPolicy(activeVer.access_policy || '');

        let toolIds = activeVer.tool_ids;
        if (typeof toolIds === 'string') {
          try { toolIds = JSON.parse(toolIds); } catch (_) {
            try { toolIds = JSON.parse(atob(toolIds)); } catch (_) { toolIds = []; }
          }
        }
        setSelectedTools(Array.isArray(toolIds) ? toolIds : []);

        let guard = activeVer.guardrails;
        if (typeof guard === 'string') {
          try { guard = JSON.parse(guard); } catch (_) {
            try { guard = JSON.parse(atob(guard)); } catch (_) { guard = null; }
          }
        }
        if (guard && typeof guard === 'object') setGuardrails(guard);

        let cache = activeVer.cache_config;
        if (typeof cache === 'string') {
          try { cache = JSON.parse(cache); } catch (_) {
            try { cache = JSON.parse(atob(cache)); } catch (_) { cache = null; }
          }
        }
        if (cache && typeof cache === 'object') setCacheConfig(cache);

        if (activeVer.response_schema) setResponseSchema(activeVer.response_schema);

        let mem = activeVer.memory_config || activeVer.memory;
        if (typeof mem === 'string') {
          try { mem = JSON.parse(mem); } catch (_) {
            try { mem = JSON.parse(atob(mem)); } catch (_) { mem = null; }
          }
        }
        if (mem && typeof mem === 'object') {
          setMemoryConfig({
            enabled: mem.enabled ?? false,
            max_history: mem.max_history ?? 10,
            vector_recall: mem.vector_recall ?? false,
            recall_limit: mem.recall_limit ?? 5,
            provider_type: mem.provider_type ?? 'postgres',
            http_url: mem.http_url ?? '',
            http_auth_token: mem.http_auth_token ?? '',
            thread_ttl_sec: mem.thread_ttl_sec ?? 86400,
          });
        }

        let search = (activeVer as any).search_config || (activeVer as any).search;
        if (typeof search === 'string') {
          try { search = JSON.parse(search); } catch (_) {
            try { search = JSON.parse(atob(search)); } catch (_) { search = null; }
          }
        }
        if (search && typeof search === 'object') {
          setSearchConfig({
            enabled: Boolean(search.enabled),
            max_results: search.max_results || 5,
            categories: Array.isArray(search.categories) && search.categories.length > 0 ? search.categories : ['general'],
            engines: Array.isArray(search.engines) ? search.engines : [],
            time_range: search.time_range || '',
            safe_search: search.safe_search !== undefined ? search.safe_search : 1,
          });
        }

        let limits = activeVer.limits;
        if (typeof limits === 'string') {
          try { limits = JSON.parse(limits); } catch (_) {
            try { limits = JSON.parse(atob(limits)); } catch (_) { limits = []; }
          }
        }
        if (Array.isArray(limits)) {
          for (const rule of limits) {
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
    chatEndRef.current?.scrollIntoView({ behavior: chatLoading ? 'auto' : 'smooth' });
  }, [chatMessages, chatLoading]);

  const handleToolToggle = (tId: string) =>
    setSelectedTools((prev) => prev.includes(tId) ? prev.filter((id) => id !== tId) : [...prev, tId]);

  const buildAppPayload = (overrides?: {
    access_policy?: string;
    auth_config?: any;
    model_routing_id?: string;
    prompt_id?: string;
    description?: string;
  }) => {
    const compiledLimits: LimitRule[] = [
      { resource: 'inference', window: '1m', max: Number(rpmLimit) },
    ];
    if (Number(dailyTokenLimit) > 0) {
      compiledLimits.push({ resource: 'tokens', window: '24h', max: Number(dailyTokenLimit) });
    }
    if (enableTierCondition && tierExpression.trim()) {
      compiledLimits.push({ resource: 'inference', window: '1m', max: Number(proTierRpm), tier_condition: tierExpression.trim() });
    }
    const appAuthPayload = overrides && 'auth_config' in overrides
      ? overrides.auth_config
      : getAppAuthPayload();

    const pId = overrides && 'prompt_id' in overrides ? overrides.prompt_id : promptId;
    const mId = overrides && 'model_routing_id' in overrides ? overrides.model_routing_id : modelRoutingId;
    const desc = overrides && 'description' in overrides ? overrides.description : description;

    return {
      id: appId,
      name,
      description: desc,
      prompt_id: pId,
      prompt_version: Number(promptVersion),
      model_routing_id: modelRoutes.default || mId,
      model_routes: modelRoutes.default ? modelRoutes : undefined,
      access_policy: overrides?.access_policy !== undefined ? overrides.access_policy : accessPolicy,
      tool_ids: selectedTools,
      limits: compiledLimits,
      guardrails,
      cache_config: cacheConfig,
      response_schema: responseSchema,
      memory_config: memoryConfig,
      search_config: searchConfig,
      search: searchConfig,
      auth: appAuthPayload,
      auth_config: appAuthPayload,
    };
  };

  const getAppAuthPayload = () => {
    if (!appAuthProvider) {
      return null;
    }
    if (appAuthProvider === 'firebase') {
      const trimmedProj = appAuthProjectId.trim();
      return {
        provider: 'firebase',
        project_id: trimmedProj || undefined,
        issuer: trimmedProj ? `https://securetoken.google.com/${trimmedProj}` : undefined,
        audience: trimmedProj ? [trimmedProj] : [],
        jwks_url: 'https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com',
        allow_anonymous: appAuthAllowAnonymous,
      };
    }
    if (appAuthProvider === 'supabase') {
      let trimmedIssuer = appAuthIssuer.trim().replace(/\/+$/, '');
      if (trimmedIssuer && !trimmedIssuer.endsWith('/auth/v1') && !trimmedIssuer.includes('/auth/v1')) {
        trimmedIssuer = `${trimmedIssuer}/auth/v1`;
      }
      return {
        provider: 'supabase',
        issuer: trimmedIssuer || undefined,
        audience: ['authenticated'],
        jwks_url: trimmedIssuer ? `${trimmedIssuer}/.well-known/jwks.json` : undefined,
        allow_anonymous: appAuthAllowAnonymous,
      };
    }
    if (appAuthProvider === 'anonymous') {
      return {
        provider: 'anonymous',
        allow_anonymous: true,
      };
    }
    return {
      provider: appAuthProvider,
      issuer: appAuthIssuer.trim() || undefined,
      audience: appAuthAudience.split(',').map((s) => s.trim()).filter(Boolean),
      jwks_url: appAuthJwksUrl.trim() || undefined,
      allow_anonymous: appAuthAllowAnonymous,
    };
  };

  const handleSaveSettings = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setSaving(true);
    setSuccessMsg(null);
    setErrorMsg(null);

    try {
      await api.updateApp(appId, buildAppPayload());
      setSuccessMsg('App settings saved. Active snapshot invalidated.');
      onRefreshApps();
      loadAll();
    } catch (err: any) {
      setErrorMsg(err.message || 'Failed to save settings');
    } finally {
      setSaving(false);
    }
  };

  const handleSaveRouting = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setSuccessMsg(null);
    setErrorMsg(null);
    const problem = validateRoutes(modelRoutes, models);
    if (problem) {
      setErrorMsg(problem);
      return;
    }
    setSaving(true);
    try {
      await api.updateApp(appId, buildAppPayload());
      setSuccessMsg('Routing rules saved. Active snapshot invalidated.');
      onRefreshApps();
      loadAll();
    } catch (err: any) {
      setErrorMsg(err.message || 'Failed to save routing rules');
    } finally {
      setSaving(false);
    }
  };

  const handleSavePolicy = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setSavingPolicy(true);
    setSuccessMsg(null);
    setErrorMsg(null);

    try {
      await api.updateApp(appId, buildAppPayload({ access_policy: accessPolicy }));
      setSuccessMsg('CEL policy saved. Active snapshot invalidated.');
      onRefreshApps();
      loadAll();
    } catch (err: any) {
      setErrorMsg(err.message || 'Failed to save CEL policy');
    } finally {
      setSavingPolicy(false);
    }
  };

  const handleSaveAuth = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setSavingAuth(true);
    setSuccessMsg(null);
    setErrorMsg(null);

    const authPayload = getAppAuthPayload();
    try {
      const updated = await api.updateApp(appId, buildAppPayload({ auth_config: authPayload }));
      setSuccessMsg('Identity & authentication configuration saved. Active snapshot invalidated.');
      onRefreshApps();
      const parsedAuth = parseAuthConfig((updated as any)?.auth || (updated as any)?.auth_config) || authPayload;
      if (parsedAuth?.provider) {
        setAppAuthProvider(String(parsedAuth.provider).toLowerCase());
        if (parsedAuth.issuer !== undefined) setAppAuthIssuer(parsedAuth.issuer || '');
        if (parsedAuth.jwks_url !== undefined) setAppAuthJwksUrl(parsedAuth.jwks_url || '');
      }
      await loadAll();
    } catch (err: any) {
      setErrorMsg(err.message || 'Failed to save identity configuration');
    } finally {
      setSavingAuth(false);
    }
  };

  const handleTestPolicy = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setTestingPolicy(true);
    setTestResult(null);
    try {
      let parsedClaims = {};
      if (testClaims.trim()) {
        try {
          parsedClaims = JSON.parse(testClaims);
        } catch {
          throw new Error('Simulated claims must be valid JSON');
        }
      }
      const res = await api.testPolicy({
        expression: accessPolicy,
        claims: parsedClaims,
      });
      setTestResult(res);
    } catch (err: any) {
      setTestResult({ valid: false, allowed: false, error: err.message });
    } finally {
      setTestingPolicy(false);
    }
  };

  const handleInspectToken = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!testTokenInput.trim() || testingToken) return;
    setTestingToken(true);
    setTokenResult(null);
    try {
      const currentAppAuth = getAppAuthPayload() as AuthConfig | undefined;
      const res = await api.testToken({
        token: testTokenInput.trim(),
        auth_config: currentAppAuth || undefined,
      });
      setTokenResult(res);
    } catch (err: any) {
      setTokenResult({ valid: false, error: err.message || 'Token verification failed' });
    } finally {
      setTestingToken(false);
    }
  };

  const handleCopyClaimsToSandbox = () => {
    if (!tokenResult?.claims) return;
    const jsonStr = JSON.stringify(tokenResult.claims, null, 2);
    setTestClaims(jsonStr);
    setSuccessMsg('Claims copied from token! Switched to CEL Policies tab.');
    setActiveTab('policies');
  };

  const handleCreateKey = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newKeyName.trim()) return;
    setCreatingKey(true);
    setKeyError(null);
    try {
      const res = await api.createAppAPIKey(appId, newKeyName.trim(), newKeyExpires ? Number(newKeyExpires) : undefined);
      setCreatedKeySecret(res.token || res.secret_key || null);
      setAppKeys((prev) => [res, ...prev]);
      setNewKeyName('');
      setNewKeyExpires(undefined);
    } catch (err: any) {
      setKeyError(err.message || 'Failed to create API key');
    } finally {
      setCreatingKey(false);
    }
  };

  const handleDeleteKey = async (keyId: string) => {
    if (!confirm('Are you sure you want to revoke this API key? Applications using this key will immediately be denied.')) return;
    try {
      await api.deleteAppAPIKey(appId, keyId);
      setAppKeys((prev) => prev.filter((k) => k.id !== keyId));
    } catch (err: any) {
      alert(`Failed to delete API key: ${err.message}`);
    }
  };

  const handleCreateThread = async () => {
    try {
      const res = await api.createThread(appId, { ttl_seconds: memoryConfig.thread_ttl_sec || 86400 }, simulatedToken);
      if (res && (res.id || res.thread_id)) {
        const tid = res.id || res.thread_id;
        setActiveThreadId(tid);
        setChatMessages([{ role: 'assistant', content: `New thread created (${tid}). Conversation turns are now persisted server-side.` }]);
        return;
      }
    } catch (_) {}
    const tid = `th_${Math.random().toString(36).substring(2, 10)}`;
    setActiveThreadId(tid);
    setChatMessages([{ role: 'assistant', content: `Thread initialized (${tid}). Conversation turns are now persisted server-side.` }]);
  };

  const handleConfirmTool = async (approved: boolean) => {
    if (!pendingConfirmation) return;
    setConfirmLoading(true);
    try {
      const resp = await api.confirmToolExecution(
        appId,
        {
          confirmation_token: pendingConfirmation.confirmation_token,
          approved,
        },
        simulatedToken
      );
      setPendingConfirmation(null);
      const reply = resp.choices?.[0]?.message?.content || resp.message?.content || (approved ? 'Tool execution approved and completed.' : 'Tool execution declined by user.');
      setChatMessages((prev) => [...prev, { role: 'assistant', content: reply }]);
    } catch (err: any) {
      alert(`Confirmation failed: ${err.message}`);
    } finally {
      setConfirmLoading(false);
    }
  };

  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault();
    if ((!userInput.trim() && pendingAttachments.length === 0) || chatLoading) return;
    const currentAttachments = [...pendingAttachments];
    const userMsg: ChatMessageItem = {
      role: 'user',
      content: userInput.trim(),
      attachments: currentAttachments.length > 0 ? currentAttachments : undefined,
    };
    const updated = [...chatMessages, userMsg];
    setChatMessages(updated);
    setUserInput('');
    setPendingAttachments([]);
    setChatLoading(true);
    setPendingConfirmation(null);

    const outMods = selectedOutputModalities.length > 0 ? selectedOutputModalities : ['text'];

    if (useStreaming) {
      setChatMessages((prev) => [
        ...prev,
        { role: 'assistant', content: '', thinking: '', isStreaming: true },
      ]);
      try {
        let streamContent = '';
        let streamThinking = '';
        let streamModel = '';
        let streamMediaParts: MediaRef[] = [];
        let streamError: string | null = null;
        await api.streamChatWithApp(
          appId,
          updated.map((m) => ({ role: m.role, content: m.content })),
          {
            onError: (err) => {
              streamError = err.message;
              setChatMessages((prev) => {
                const next = [...prev];
                next[next.length - 1] = {
                  role: 'assistant',
                  content: `${streamContent}${streamContent ? '\n\n' : ''}[Error: ${err.message}]`,
                  thinking: streamThinking,
                  routedModel: streamModel || undefined,
                  parts: streamMediaParts.length > 0 ? streamMediaParts : undefined,
                  isStreaming: false,
                };
                return next;
              });
            },
            onModelRouted: (model) => {
              streamModel = model;
              setChatMessages((prev) => {
                const next = [...prev];
                if (next.length > 0 && next[next.length - 1].role === 'assistant') {
                  next[next.length - 1] = {
                    ...next[next.length - 1],
                    routedModel: model,
                  };
                }
                return next;
              });
            },
            onMediaPart: (media) => {
              streamMediaParts.push(media);
              setChatMessages((prev) => {
                const next = [...prev];
                if (next.length > 0 && next[next.length - 1].role === 'assistant') {
                  next[next.length - 1] = {
                    ...next[next.length - 1],
                    parts: [...streamMediaParts],
                  };
                }
                return next;
              });
            },
            onThinking: (chunk) => {
              streamThinking += chunk;
              setChatMessages((prev) => {
                const next = [...prev];
                next[next.length - 1] = {
                  role: 'assistant',
                  content: streamContent,
                  thinking: streamThinking,
                  routedModel: streamModel || undefined,
                  parts: streamMediaParts.length > 0 ? streamMediaParts : undefined,
                  isStreaming: true,
                };
                return next;
              });
            },
            onToken: (chunk) => {
              streamContent += chunk;
              setChatMessages((prev) => {
                const next = [...prev];
                next[next.length - 1] = {
                  role: 'assistant',
                  content: streamContent,
                  thinking: streamThinking,
                  routedModel: streamModel || undefined,
                  parts: streamMediaParts.length > 0 ? streamMediaParts : undefined,
                  isStreaming: true,
                };
                return next;
              });
            },
            onConfirmation: (ev) => {
              setPendingConfirmation({
                confirmation_token: ev.confirmation_token || ev.token,
                tool_name: ev.tool_name || ev.tool?.name || 'tool',
                arguments: ev.arguments || ev.tool?.arguments || {},
              });
            },
          },
          simulatedToken,
          activeThreadId || undefined,
          {
            attachments: currentAttachments.length > 0 ? currentAttachments : undefined,
            outputModalities: outMods,
          }
        );
        if (!streamContent && !streamError && !streamThinking && streamMediaParts.length === 0) {
          setChatMessages((prev) => {
            const next = [...prev];
            next[next.length - 1] = {
              role: 'assistant',
              content: '[No response received from the model. The provider may be rate-limited or returned empty output. Try again.]',
              thinking: '',
              routedModel: streamModel || undefined,
              isStreaming: false,
            };
            return next;
          });
        } else {
          setChatMessages((prev) => {
            const next = [...prev];
            if (next.length > 0 && next[next.length - 1].role === 'assistant') {
              next[next.length - 1] = {
                ...next[next.length - 1],
                routedModel: streamModel || next[next.length - 1].routedModel,
                parts: streamMediaParts.length > 0 ? streamMediaParts : next[next.length - 1].parts,
                isStreaming: false,
              };
            }
            return next;
          });
        }
      } catch (err: any) {
        setChatMessages((prev) => {
          const next = [...prev];
          next[next.length - 1] = {
            role: 'assistant',
            content: `[Error: ${err.message}]`,
            isStreaming: false,
          };
          return next;
        });
      } finally {
        setChatLoading(false);
      }
    } else {
      try {
        const resp = await api.chatWithApp(
          appId,
          updated.map((m) => ({ role: m.role, content: m.content })),
          simulatedToken,
          activeThreadId || undefined,
          {
            attachments: currentAttachments.length > 0 ? currentAttachments : undefined,
            outputModalities: outMods,
          }
        );
        if (resp.thread_id) {
          setActiveThreadId(resp.thread_id);
        }
        if (resp.finish_reason === 'confirmation_required' || resp.choices?.[0]?.finish_reason === 'confirmation_required') {
          const confirmToken = resp.metadata?.confirmation_token || resp.confirmation_token;
          if (confirmToken) {
            setPendingConfirmation({
              confirmation_token: confirmToken,
              tool_name: resp.tool_name || 'tool',
              arguments: resp.arguments || {},
            });
          }
        }
        const reply = resp.choices?.[0]?.message?.content || resp.message?.content || (resp.status ? '' : JSON.stringify(resp, null, 2));
        const reasoning = resp.choices?.[0]?.message?.reasoning_content || resp.message?.reasoning_content;
        const parsed = separateThinkingAndContent(reply, reasoning);

        // Extract media parts if returned in message.parts
        const parts: MediaRef[] = [];
        if (Array.isArray(resp.message?.parts)) {
          for (const p of resp.message.parts) {
            if (p.type === 'media' && p.media) {
              parts.push(p.media);
            }
          }
        }

        setChatMessages((prev) => [
          ...prev,
          {
            role: 'assistant',
            content: parsed.content,
            thinking: parsed.thinking,
            routedModel: resp.model || undefined,
            parts: parts.length > 0 ? parts : undefined,
            isStreaming: false,
          },
        ]);
      } catch (err: any) {
        setChatMessages((prev) => [...prev, { role: 'assistant', content: `[Error: ${err.message}]`, isStreaming: false }]);
      } finally {
        setChatLoading(false);
      }
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

  const curlSnippetAppKey = `curl -X POST "http://localhost:8080/v1/apps/${appId}/chat" \\
  -H "Content-Type: application/json" \\
  -H "X-Project-ID: ${api.currentProject}" \\
  -H "X-Environment: ${api.currentEnv}" \\
  -H "Authorization: Bearer bv_app_sec_YOUR_KEY_SECRET" \\
  -d '{"messages":[{"role":"user","content":"Hello from backend service!"}]}'`;

  const curlSnippetUserJwt = `curl -X POST "http://localhost:8080/v1/apps/${appId}/chat" \\
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
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3 min-w-0">
          <button
            onClick={onBack}
            className="p-1.5 text-neutral-500 hover:text-neutral-200 hover:bg-neutral-800 rounded-lg transition-colors border border-neutral-800 shrink-0"
            title="Back to Apps"
          >
            <ArrowLeft className="w-4 h-4" />
          </button>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <h1 className="text-base font-bold text-neutral-100 truncate">{name || appId}</h1>
              <Badge variant="success">active</Badge>
            </div>
            <p className="text-[11px] font-mono text-neutral-500 mt-0.5 truncate">/v1/apps/{appId}/chat</p>
          </div>
        </div>
      </div>

      {/* Sub-tabs */}
      <div className="flex items-center bg-neutral-900 border border-neutral-800 rounded-xl p-1 gap-0.5 overflow-x-auto">
        <TabButton active={activeTab === 'overview'} onClick={() => setActiveTab('overview')} icon={<Code2 className="w-3.5 h-3.5" />} label="Overview & SDK" />
        <TabButton active={activeTab === 'policies'} onClick={() => setActiveTab('policies')} icon={<ShieldCheck className="w-3.5 h-3.5" />} label="CEL Policies" />
        <TabButton active={activeTab === 'identity'} onClick={() => setActiveTab('identity')} icon={<KeyRound className="w-3.5 h-3.5" />} label="Identity & Auth" />
        <TabButton active={activeTab === 'routing'} onClick={() => setActiveTab('routing')} icon={<Radio className="w-3.5 h-3.5" />} label="Routing" />
        <TabButton active={activeTab === 'settings'} onClick={() => setActiveTab('settings')} icon={<Sliders className="w-3.5 h-3.5" />} label="Settings" />
        <TabButton active={activeTab === 'keys'} onClick={() => setActiveTab('keys')} icon={<KeyRound className="w-3.5 h-3.5" />} label={`API Keys (${(appKeys || []).length})`} />
        <TabButton active={activeTab === 'playground'} onClick={() => setActiveTab('playground')} icon={<Terminal className="w-3.5 h-3.5" />} label="Playground" />
      </div>

      {/* Global alerts */}
      {successMsg && <Alert variant="success" onDismiss={() => setSuccessMsg(null)}>{successMsg}</Alert>}
      {errorMsg && <Alert variant="error" onDismiss={() => setErrorMsg(null)}>{errorMsg}</Alert>}

      {/* ─── TAB 1: Overview ─── */}
      {activeTab === 'overview' && (() => {
        const matchedModel = models.find((m) => m.logical_model === (defaultModel));
        const matchedPrompt = prompts.find((p) => p.id === promptId);
        return (
        <div className="space-y-4">
          {/* App summary */}
          <Card padding="lg">
            <CardHeader
              title="Application Summary"
              icon={<Bot className="w-4 h-4 text-emerald-400" />}
            />
            <p className="text-xs text-neutral-400 mb-4">{description || 'No description configured.'}</p>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-3">
              <div className="bg-neutral-950 border border-neutral-800/80 p-3 rounded-lg">
                <p className="text-[10px] uppercase tracking-wider text-neutral-600 font-semibold">Logical Model</p>
                <p className="text-sm text-neutral-200 mt-1 font-mono font-medium truncate">
                  {matchedModel ? `${defaultModel} (${matchedModel.deployment})` : (defaultModel || '(none)')}
                </p>
              </div>

              <div className="bg-neutral-950 border border-neutral-800/80 p-3 rounded-lg">
                <p className="text-[10px] uppercase tracking-wider text-neutral-600 font-semibold">System Prompt</p>
                <p className="text-sm text-neutral-200 mt-1 font-medium truncate">
                  {matchedPrompt ? matchedPrompt.name : (promptId ? promptId : 'None (Pass-through)')}
                </p>
                {matchedPrompt && (
                  <p className="text-[10px] font-mono text-neutral-500 truncate mt-0.5">{matchedPrompt.id}</p>
                )}
              </div>

              <div className="bg-neutral-950 border border-neutral-800/80 p-3 rounded-lg">
                <p className="text-[10px] uppercase tracking-wider text-neutral-600 font-semibold">Active Tools</p>
                <p className="text-sm text-neutral-200 mt-1 font-medium truncate">
                  {selectedTools.length === 0
                    ? (searchConfig?.enabled ? 'Web Search only' : 'None attached')
                    : `${selectedTools.length} attached${searchConfig?.enabled ? ' + Web Search' : ''}`}
                </p>
              </div>

              <div className="bg-neutral-950 border border-neutral-800/80 p-3 rounded-lg">
                <p className="text-[10px] uppercase tracking-wider text-neutral-600 font-semibold">Rate Limit</p>
                <p className="text-sm text-neutral-200 mt-1 font-mono font-medium truncate">
                  {rpmLimit} RPM
                </p>
              </div>
            </div>
          </Card>

          {/* Integration Snippets */}
          <Card padding="lg">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <Code2 className="w-4 h-4 text-blue-400" />
                <h3 className="text-sm font-semibold text-neutral-200">Integration Snippets</h3>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-[11px] text-neutral-500">Target Env:</span>
                <Badge variant="purple">{api.currentEnv}</Badge>
                <span className="text-[11px] text-neutral-500 ml-1">Project:</span>
                <Badge variant="mono">{api.currentProject}</Badge>
              </div>
            </div>

            {/* Auth Selector */}
            <div className="flex rounded-lg bg-neutral-950 p-1 border border-neutral-800 mb-4 max-w-sm">
              <button
                type="button"
                onClick={() => setIntegrationAuthType('app_key')}
                className={`flex-1 py-1 text-xs rounded-md font-medium transition-all ${
                  integrationAuthType === 'app_key'
                    ? 'bg-neutral-800 text-neutral-100 shadow-sm'
                    : 'text-neutral-500 hover:text-neutral-300'
                }`}
              >
                App API Key (Server-to-Server)
              </button>
              <button
                type="button"
                onClick={() => setIntegrationAuthType('jwt')}
                className={`flex-1 py-1 text-xs rounded-md font-medium transition-all ${
                  integrationAuthType === 'jwt'
                    ? 'bg-neutral-800 text-neutral-100 shadow-sm'
                    : 'text-neutral-500 hover:text-neutral-300'
                }`}
              >
                User JWT (Client App)
              </button>
            </div>

            <div className="space-y-4">
              <CodeBlock
                code={integrationAuthType === 'app_key' ? curlSnippetAppKey : curlSnippetUserJwt}
                label={`cURL / HTTP Request (${integrationAuthType === 'app_key' ? 'App API Key' : 'User JWT'})`}
              />
              <CodeBlock code={tsSnippet} label="TypeScript / Web SDK" />
            </div>
          </Card>
        </div>
      ); })()}

      {/* ─── TAB 2: CEL Policies ─── */}
      {activeTab === 'policies' && (
        <div className="space-y-4">
          {/* Policy Editor */}
          <Card padding="lg">
            <div className="flex items-start justify-between gap-4 mb-4">
              <div>
                <div className="flex items-center gap-2">
                  <ShieldCheck className="w-5 h-5 text-emerald-400" />
                  <h3 className="text-base font-semibold text-neutral-100">App CEL Access Policy</h3>
                  {accessPolicy.trim() ? (
                    <Badge variant="success">Enforced</Badge>
                  ) : (
                    <Badge variant="default">Permissive (Allow All)</Badge>
                  )}
                </div>
                <p className="text-xs text-neutral-500 mt-1">
                  Google Common Expression Language (CEL) evaluated at request ingress against end-user JWT claims before model invocation.
                </p>
              </div>
              <Button
                variant="primary"
                size="sm"
                onClick={handleSavePolicy}
                loading={savingPolicy}
                icon={<Save className="w-3.5 h-3.5" />}
              >
                Save Policy
              </Button>
            </div>

            <div className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-neutral-400 mb-1.5">
                  CEL Boolean Expression
                </label>
                <Textarea
                  rows={3}
                  placeholder="e.g. claims.plan in ['pro', 'team'] && claims.email_verified == true"
                  value={accessPolicy}
                  onChange={(e) => setAccessPolicy(e.target.value)}
                  mono
                />
              </div>

              {/* Quick Claim Templates */}
              <div>
                <p className="text-[11px] font-medium text-neutral-500 mb-1.5">Quick Claim Templates:</p>
                <div className="flex flex-wrap gap-1.5">
                  {[
                    { label: 'Require Pro or Team Plan', expr: "claims.plan in ['pro', 'team']" },
                    { label: 'Email Verified Only', expr: 'claims.email_verified == true' },
                    { label: 'Admin or Staff Role', expr: "claims.role in ['admin', 'staff']" },
                    { label: 'Specific Tenant ID', expr: "claims.tenant_id == 'acme_corp'" },
                    { label: 'Allow All (Permissive)', expr: '' },
                  ].map((tpl) => (
                    <button
                      key={tpl.label}
                      type="button"
                      onClick={() => setAccessPolicy(tpl.expr)}
                      className="px-2.5 py-1 text-[11px] rounded-lg bg-neutral-900 hover:bg-neutral-800 text-neutral-300 border border-neutral-800 transition-colors"
                    >
                      {tpl.label}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          </Card>

          {/* Interactive Policy Sandbox */}
          <Card padding="lg">
            <CardHeader
              title="Interactive CEL Policy Sandbox"
              description="Simulate and test your expression against custom JSON claims in real-time."
              icon={<Play className="w-4 h-4 text-purple-400" />}
            />

            <div className="space-y-4">
              <div>
                <div className="flex items-center justify-between mb-1.5">
                  <label className="text-xs font-medium text-neutral-400">
                    Simulated JWT Claims (JSON)
                  </label>
                  <span className="text-[11px] text-neutral-500 font-mono">claims.*</span>
                </div>
                <Textarea
                  rows={6}
                  value={testClaims}
                  onChange={(e) => setTestClaims(e.target.value)}
                  mono
                />
              </div>

              <div className="flex items-center justify-between pt-2 border-t border-neutral-800/80">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handleTestPolicy}
                  loading={testingPolicy}
                  icon={<Play className="w-3.5 h-3.5" />}
                >
                  Test Expression
                </Button>
              </div>

              {testResult && (
                <div
                  className={`border rounded-xl p-4 space-y-2 ${
                    !testResult.valid
                      ? 'bg-red-500/5 border-red-500/20'
                      : testResult.allowed
                      ? 'bg-emerald-500/5 border-emerald-500/20'
                      : 'bg-amber-500/5 border-amber-500/20'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    {!testResult.valid ? (
                      <AlertTriangle className="w-4 h-4 text-red-400 flex-shrink-0" />
                    ) : testResult.allowed ? (
                      <CheckCircle2 className="w-4 h-4 text-emerald-400 flex-shrink-0" />
                    ) : (
                      <XCircle className="w-4 h-4 text-amber-400 flex-shrink-0" />
                    )}
                    <span
                      className={`text-xs font-bold uppercase tracking-wider ${
                        !testResult.valid
                          ? 'text-red-400'
                          : testResult.allowed
                          ? 'text-emerald-400'
                          : 'text-amber-400'
                      }`}
                    >
                      {!testResult.valid
                        ? 'Compilation / Syntax Error'
                        : testResult.allowed
                        ? 'Allowed - Policy Passed'
                        : 'Denied - Policy Evaluated to False'}
                    </span>
                  </div>

                  {testResult.error && (
                    <p className="text-xs font-mono text-red-300 bg-red-500/10 p-2.5 rounded-lg border border-red-500/20">
                      {testResult.error}
                    </p>
                  )}

                  {testResult.valid && (
                    <p className="text-xs text-neutral-400">
                      {testResult.allowed
                        ? 'Requests with these claims satisfy the access policy and proceed to inference.'
                        : 'Requests with these claims will be blocked at request ingress with 403 Forbidden.'}
                    </p>
                  )}
                </div>
              )}
            </div>
          </Card>

          {/* CEL Syntax Reference */}
          <Card padding="lg">
            <CardHeader
              title="CEL Variable Reference"
              description="Variables available in your CEL expressions at evaluation time."
              icon={<FileCode className="w-4 h-4 text-neutral-400" />}
            />
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
              <div className="bg-neutral-950 p-3 rounded-lg border border-neutral-800 space-y-1.5">
                <span className="font-mono text-emerald-400 font-semibold">claims.&lt;field&gt;</span>
                <p className="text-neutral-400">
                  Custom claims present in the authenticated end-user JWT token (e.g. <code className="text-neutral-300">claims.plan</code>, <code className="text-neutral-300">claims.role</code>, <code className="text-neutral-300">claims.email_verified</code>).
                </p>
              </div>
              <div className="bg-neutral-950 p-3 rounded-lg border border-neutral-800 space-y-1.5">
                <span className="font-mono text-blue-400 font-semibold">identity.subject</span>
                <p className="text-neutral-400">
                  The authenticated user identifier (<code className="text-neutral-300">sub</code> claim).
                </p>
              </div>
              <div className="bg-neutral-950 p-3 rounded-lg border border-neutral-800 space-y-1.5">
                <span className="font-mono text-purple-400 font-semibold">identity.issuer / audience</span>
                <p className="text-neutral-400">
                  Token issuer (<code className="text-neutral-300">iss</code>) and token audience (<code className="text-neutral-300">aud</code>) strings.
                </p>
              </div>
              <div className="bg-neutral-950 p-3 rounded-lg border border-neutral-800 space-y-1.5">
                <span className="font-mono text-amber-400 font-semibold">meta.client_ip</span>
                <p className="text-neutral-400">
                  Client IP address executing the request (<code className="text-neutral-300">string</code>).
                </p>
              </div>
            </div>
          </Card>
        </div>
      )}

      {/* ─── TAB 3: Identity & Auth ─── */}
      {activeTab === 'identity' && (
        <div className="space-y-4">
          {/* Identity Provider Configuration */}
          <Card padding="lg">
            <div className="flex items-start justify-between gap-4 mb-4">
              <div>
                <div className="flex items-center gap-2">
                  <KeyRound className="w-5 h-5 text-emerald-400" />
                  <h3 className="text-base font-semibold text-neutral-100">App Identity & Authentication</h3>
                  <Badge variant={appAuthProvider === 'anonymous' ? 'default' : 'purple'}>
                    {appAuthProvider.toUpperCase()}
                  </Badge>
                </div>
                <p className="text-xs text-neutral-500 mt-1">
                  Configure how end-user JWT identity tokens are authenticated for this specific AI App.
                </p>
              </div>
              <Button
                variant="primary"
                size="sm"
                onClick={handleSaveAuth}
                loading={savingAuth}
                icon={<Save className="w-3.5 h-3.5" />}
              >
                Save Identity Config
              </Button>
            </div>

            <div className="space-y-4">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <Select
                  label="Identity Provider Mode"
                  value={appAuthProvider}
                  onChange={(e) => setAppAuthProvider(e.target.value)}
                  options={[
                    { value: 'firebase', label: 'Firebase Auth (Google Identity)' },
                    { value: 'supabase', label: 'Supabase Auth' },
                    { value: 'oidc', label: 'Generic OIDC' },
                    { value: 'jwt', label: 'Custom JWT (RS256 / ES256 / EdDSA)' },
                    { value: 'anonymous', label: 'Anonymous (Public / No Token Required)' },
                  ]}
                />
                {appAuthProvider === 'firebase' && (
                  <Input
                    label="Firebase Project ID"
                    placeholder="my-firebase-project-id"
                    value={appAuthProjectId}
                    onChange={(e) => setAppAuthProjectId(e.target.value)}
                  />
                )}
                {appAuthProvider === 'supabase' && (
                  <Input
                    label="Supabase Project URL / Issuer"
                    placeholder="https://xyzproject.supabase.co/auth/v1"
                    value={appAuthIssuer}
                    onChange={(e) => setAppAuthIssuer(e.target.value)}
                  />
                )}
                {(appAuthProvider === 'oidc' || appAuthProvider === 'jwt') && (
                  <Input
                    label="Token Issuer (iss)"
                    placeholder="https://auth.example.com"
                    value={appAuthIssuer}
                    onChange={(e) => setAppAuthIssuer(e.target.value)}
                  />
                )}
              </div>

              {appAuthProvider === 'firebase' && (
                <div className="p-3 bg-neutral-950 border border-neutral-800 rounded-lg text-xs text-neutral-400 space-y-1">
                  <p className="text-neutral-300 font-medium">Automatic Google JWKS Derivation:</p>
                  <p className="text-[11px] text-neutral-500 font-mono">
                    Issuer: https://securetoken.google.com/{appAuthProjectId.trim() || '<project-id>'}
                  </p>
                  <p className="text-[11px] text-neutral-500 font-mono">
                    Audience: [{appAuthProjectId.trim() || '<project-id>'}]
                  </p>
                </div>
              )}

              {appAuthProvider === 'supabase' && (
                <div className="p-3 bg-neutral-950 border border-neutral-800 rounded-lg text-xs text-neutral-400 space-y-1">
                  <p className="text-neutral-300 font-medium">Automatic Supabase JWKS Derivation:</p>
                  <p className="text-[11px] text-neutral-500 font-mono">
                    JWKS URL: {(appAuthIssuer.trim() ? appAuthIssuer.trim().replace(/\/+$/, '') : 'https://<project>.supabase.co/auth/v1')}/.well-known/jwks.json
                  </p>
                  <p className="text-[11px] text-neutral-500 font-mono">
                    Audience: ["authenticated"]
                  </p>
                </div>
              )}

              {appAuthProvider === 'anonymous' && (
                <div className="p-3.5 bg-neutral-950 border border-neutral-800 rounded-lg text-xs text-neutral-400 space-y-1">
                  <div className="flex items-center gap-1.5 font-medium text-neutral-300">
                    <Info className="w-3.5 h-3.5 text-amber-400" />
                    Public / Anonymous Mode
                  </div>
                  <p>
                    All incoming requests to this AI App are accepted without requiring an Authorization Bearer token and assigned subject identity <code className="text-neutral-300">anonymous</code>.
                  </p>
                </div>
              )}

              {(appAuthProvider === 'oidc' || appAuthProvider === 'jwt') && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2">
                  <Input
                    label="Audiences (comma-separated aud)"
                    placeholder="client-web, client-mobile"
                    value={appAuthAudience}
                    onChange={(e) => setAppAuthAudience(e.target.value)}
                  />
                  <Input
                    label="JWKS Key Set URL"
                    placeholder="https://auth.example.com/.well-known/jwks.json"
                    value={appAuthJwksUrl}
                    onChange={(e) => setAppAuthJwksUrl(e.target.value)}
                  />
                </div>
              )}

              {appAuthProvider !== 'anonymous' && (
                <div className="pt-3 border-t border-neutral-800">
                  <Toggle
                    label="Allow Anonymous Access"
                    description="Permit requests without Authorization header to execute with an anonymous identity (sub: anonymous)"
                    checked={appAuthAllowAnonymous}
                    onChange={setAppAuthAllowAnonymous}
                  />
                </div>
              )}
            </div>
          </Card>

          {/* Live JWT Inspector */}
          <Card padding="lg">
            <CardHeader
              title="JWT Token Inspector & Claims Tester"
              description="Test client tokens directly against this app's authentication configuration without sending a chat request."
              icon={<Search className="w-4 h-4 text-blue-400" />}
            />

            <div className="space-y-4">
              <Textarea
                rows={3}
                placeholder="Paste Bearer JWT token (eyJhbGciOiJSUzI1NiIs...)"
                value={testTokenInput}
                onChange={(e) => setTestTokenInput(e.target.value)}
                mono
              />

              <div className="flex items-center justify-between">
                <p className="text-[11px] text-neutral-600">
                  Tokens are verified locally against the active JWKS cache. No token data is stored.
                </p>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handleInspectToken}
                  loading={testingToken}
                  disabled={!testTokenInput.trim()}
                  icon={<Search className="w-3.5 h-3.5" />}
                >
                  Verify & Inspect Token
                </Button>
              </div>

              {tokenResult && (
                <div
                  className={`mt-4 border rounded-xl p-4 space-y-4 ${
                    tokenResult.valid
                      ? 'bg-emerald-500/5 border-emerald-500/20'
                      : 'bg-red-500/5 border-red-500/20'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    {tokenResult.valid ? (
                      <CheckCircle2 className="w-4 h-4 text-emerald-400 flex-shrink-0" />
                    ) : (
                      <XCircle className="w-4 h-4 text-red-400 flex-shrink-0" />
                    )}
                    <span
                      className={`text-xs font-bold uppercase tracking-wider ${
                        tokenResult.valid ? 'text-emerald-400' : 'text-red-400'
                      }`}
                    >
                      {tokenResult.valid ? 'Token Valid & Signature Verified' : 'Token Verification Failed'}
                    </span>
                    {tokenResult.expires_at && (
                      <span className="text-[11px] text-neutral-500 ml-auto">
                        Expires: {new Date(tokenResult.expires_at).toLocaleString()}
                      </span>
                    )}
                  </div>

                  {tokenResult.error && (
                    <div className="text-xs font-mono text-red-300 bg-red-500/10 p-3 rounded-lg border border-red-500/20">
                      {tokenResult.error}
                    </div>
                  )}

                  {tokenResult.valid && (
                    <div className="space-y-3">
                      <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
                        {[
                          { label: 'Subject (`sub`)', value: tokenResult.subject || '(none)' },
                          { label: 'Issuer (`iss`)', value: tokenResult.issuer || '(none)' },
                          { label: 'Audience (`aud`)', value: tokenResult.audience || '(none)' },
                        ].map((item) => (
                          <div key={item.label} className="bg-neutral-950 p-2.5 rounded-lg border border-neutral-800">
                            <p className="text-[10px] uppercase text-neutral-600 font-bold">{item.label}</p>
                            <p className="text-[11px] font-mono text-neutral-300 mt-0.5 truncate" title={item.value}>
                              {item.value}
                            </p>
                          </div>
                        ))}
                      </div>

                      <div>
                        <div className="flex items-center justify-between mb-1.5">
                          <p className="text-[11px] font-medium text-neutral-400">Decoded Claims Payload</p>
                          <div className="flex items-center gap-2">
                            <button
                              type="button"
                              onClick={handleCopyClaimsToSandbox}
                              className="text-[11px] text-emerald-400 hover:text-emerald-300 flex items-center gap-1 font-medium transition-colors"
                            >
                              <ShieldCheck className="w-3.5 h-3.5" />
                              Copy Claims to CEL Sandbox &rarr;
                            </button>
                          </div>
                        </div>
                        <pre className="bg-neutral-950 p-3 rounded-lg text-[11px] font-mono text-emerald-400 border border-neutral-800 overflow-x-auto max-h-56 leading-relaxed">
                          {JSON.stringify(tokenResult.claims, null, 2)}
                        </pre>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          </Card>
        </div>
      )}

      {/* ─── TAB 4: Settings ─── */}
      {/* ─── TAB: Routing ─── */}
      {activeTab === 'routing' && (
        <Card padding="lg">
          <form onSubmit={handleSaveRouting} className="space-y-4">
            <CardHeader
              title="Model Routing"
              description="Choose which logical model serves each kind of request."
              action={
                <Button type="submit" variant="primary" size="xs" loading={saving} icon={<Save className="w-3.5 h-3.5" />}>
                  Save Routing
                </Button>
              }
            />
            <RoutingEditor models={models} value={modelRoutes} onChange={setModelRoutes} />
          </form>
        </Card>
      )}

      {activeTab === 'settings' && (
        <form onSubmit={handleSaveSettings} className="space-y-4">
          {/* General */}
          <Card padding="lg">
            <div className="flex items-center justify-between mb-4">
              <SectionTitle icon={<Bot className="w-4 h-4" />} title="General & Model Assignment" />
              <Button
                type="submit"
                variant="primary"
                size="xs"
                loading={saving}
                icon={<Save className="w-3.5 h-3.5" />}
              >
                Save Settings
              </Button>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <Input label="Display Name" required value={name} onChange={(e) => setName(e.target.value)} />
              <Select
                label="Logical Model Routing"
                value={defaultModel}
                onChange={(e) => {
                  setModelRoutingId(e.target.value);
                  setModelRoutes((prev) => ({ ...prev, default: e.target.value }));
                }}
                options={[
                  { value: '', label: '(select a model)' },
                  ...models.map((m) => ({ value: m.logical_model, label: `${m.logical_model} → ${m.deployment}` })),
                ]}
              />
            </div>
            <div className="mt-4">
              <Textarea label="Description" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
            </div>
            <div className="mt-4">
              <Select
                label="System Prompt"
                value={promptId}
                onChange={(e) => setPromptId(e.target.value)}
                options={[
                  { value: '', label: '(None / Pass-through)' },
                  ...prompts.map((p) => ({ value: p.id, label: `${p.name} (${p.id})` })),
                ]}
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
            <div className="mt-4 pt-4 border-t border-neutral-800/60 max-w-sm">
              <Input
                label="Max Agent Loop Depth (Tool Iterations)"
                type="number"
                min="1"
                max="30"
                value={guardrails.max_depth || 5}
                onChange={(e) => setGuardrails((prev) => ({ ...prev, max_depth: Number(e.target.value) || 5 }))}
                className="font-mono"
                hint="Upper bound on consecutive tool turns per request before forcing final summary (default: 5)"
              />
            </div>
          </Card>

          {/* Web Search Engine (SearXNG) */}
          <Card padding="lg">
            <SectionTitle
              icon={<Search className="w-4 h-4 text-blue-400" />}
              title="Web Search (SearXNG Meta-Search Engine)"
              description="Allow the agent to perform live web searches via the self-hosted SearXNG engine"
            />
            <div className="mb-4">
              <Toggle
                checked={Boolean(searchConfig.enabled)}
                onChange={(v) => setSearchConfig((prev) => ({ ...prev, enabled: v }))}
                label="Enable Live Web Search"
                description="Automatically registers the web_search tool in the agent loop for this app"
              />
            </div>

            {searchConfig.enabled && (
              <div className="space-y-4 pt-2 border-t border-neutral-800/60">
                <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                  <Input
                    label="Max Search Results"
                    type="number"
                    min="1"
                    max="20"
                    value={searchConfig.max_results || 5}
                    onChange={(e) => setSearchConfig((prev) => ({ ...prev, max_results: Number(e.target.value) || 5 }))}
                    className="font-mono"
                    hint="Number of results returned to LLM per search"
                  />
                  <Select
                    label="Default Category"
                    value={searchConfig.categories?.[0] || 'general'}
                    onChange={(e) => setSearchConfig((prev) => ({ ...prev, categories: [e.target.value] }))}
                    options={[
                      { value: 'general', label: 'General Web' },
                      { value: 'science', label: 'Science / Academic' },
                      { value: 'it', label: 'IT & Software Engineering' },
                      { value: 'news', label: 'News' },
                    ]}
                  />
                  <Select
                    label="Safe Search Filter"
                    value={String(searchConfig.safe_search ?? 1)}
                    onChange={(e) => setSearchConfig((prev) => ({ ...prev, safe_search: Number(e.target.value) }))}
                    options={[
                      { value: '0', label: 'Off' },
                      { value: '1', label: 'Moderate' },
                      { value: '2', label: 'Strict' },
                    ]}
                  />
                </div>
                <div className="p-3 bg-neutral-950 border border-neutral-800 rounded-lg text-xs text-neutral-400">
                  <span className="font-semibold text-neutral-300">How it works:</span> When enabled, Beavend equips this app&apos;s model with a function-calling schema for <code className="text-amber-400 font-mono">web_search</code>. If the user asks for real-time or external data, the LLM generates a search query which Beavend executes through the SearXNG microservice over private HTTP JSON.
                </div>
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
                min="0"
                step="any"
                value={dailyTokenLimit}
                onChange={(e) => setDailyTokenLimit(Number(e.target.value))}
                hint="Pre-allocated tokens per 24h period (set to 0 for unlimited)"
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
              <Toggle
                checked={guardrails.content_safety ?? true}
                onChange={(v) => setGuardrails((prev) => ({ ...prev, content_safety: v }))}
                label="Content Safety & Toxicity Filter"
                description="Blocks harmful categories (violence, hate speech, sexual content)"
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
              <div>
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
            </div>
            {guardrails.streaming_mode === 'passthrough_with_post_check' && (
              <div className="p-3 mt-3 bg-amber-500/10 border border-amber-500/20 rounded-xl text-xs text-amber-300">
                <p className="font-semibold flex items-center gap-1.5">
                  <AlertCircle className="w-3.5 h-3.5 text-amber-400" />
                  Passthrough Low-Latency Mode Active
                </p>
                <p className="text-[11px] text-amber-300/80 mt-1">
                  Tokens are streamed to clients immediately. If post-completion safety checks trigger a violation, an error/retract event is emitted, but already-rendered tokens cannot be un-seen by users.
                </p>
              </div>
            )}
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

          {/* Conversation Context & Memory Engine */}
          <Card padding="lg">
            <SectionTitle
              icon={<Brain className="w-4 h-4 text-purple-400" />}
              title="Conversation Context & Memory Engine"
              description="Server-side multi-turn conversation threads and user-scoped semantic memory recall"
            />
            <div className="mb-4">
              <Toggle
                checked={memoryConfig.enabled ?? false}
                onChange={(v) => setMemoryConfig((prev) => ({ ...prev, enabled: v }))}
                label="Enable Threads & Semantic Memory"
                description="Persist message turns and recall semantic memories automatically"
              />
            </div>
            {memoryConfig.enabled && (
              <div className="space-y-4 pt-2 border-t border-neutral-800/60">
                <div className="grid grid-cols-2 gap-3">
                  <Input
                    label="Max Message History (Turns)"
                    type="number"
                    min="1"
                    max="100"
                    value={memoryConfig.max_history || 10}
                    onChange={(e) => setMemoryConfig((prev) => ({ ...prev, max_history: Number(e.target.value) }))}
                    className="font-mono"
                    hint="Number of previous conversational turns loaded into prompt context"
                  />
                  <Input
                    label="Thread TTL (Seconds)"
                    type="number"
                    min="60"
                    value={memoryConfig.thread_ttl_sec || 86400}
                    onChange={(e) => setMemoryConfig((prev) => ({ ...prev, thread_ttl_sec: Number(e.target.value) }))}
                    className="font-mono"
                    hint="Default thread lifetime (e.g. 86400 = 24 hours)"
                  />
                </div>

                <div className="grid grid-cols-3 gap-3">
                  <div className="flex items-end pb-1">
                    <Toggle
                      checked={memoryConfig.vector_recall ?? false}
                      onChange={(v) => setMemoryConfig((prev) => ({ ...prev, vector_recall: v }))}
                      label="Semantic Vector Recall"
                      description="Auto-inject relevant memories"
                    />
                  </div>
                  <Input
                    label="Recall Limit (Memories)"
                    type="number"
                    min="1"
                    max="20"
                    value={memoryConfig.recall_limit || 5}
                    onChange={(e) => setMemoryConfig((prev) => ({ ...prev, recall_limit: Number(e.target.value) }))}
                    className="font-mono"
                    hint="Max relevant memories to recall"
                  />
                  <Select
                    label="Memory Provider"
                    value={memoryConfig.provider_type || 'postgres'}
                    onChange={(e) => setMemoryConfig((prev) => ({ ...prev, provider_type: e.target.value as any }))}
                    options={[
                      { value: 'postgres', label: 'PostgreSQL + pgvector' },
                      { value: 'http', label: 'External HTTP Provider' },
                      { value: 'local', label: 'In-Memory (Ephemeral)' },
                    ]}
                  />
                </div>

                {memoryConfig.provider_type === 'http' && (
                  <div className="grid grid-cols-2 gap-3 p-3 bg-neutral-950 border border-neutral-800 rounded-xl">
                    <Input
                      label="External HTTP Memory Endpoint"
                      placeholder="https://memory.internal.com/v1"
                      value={memoryConfig.http_url || ''}
                      onChange={(e) => setMemoryConfig((prev) => ({ ...prev, http_url: e.target.value }))}
                      className="font-mono text-xs"
                      hint="SSRF-safe HTTP endpoint implementing MemoryProvider"
                    />
                    <Input
                      label="Bearer Auth Token (Optional)"
                      type="password"
                      placeholder="••••••••••••••••"
                      value={memoryConfig.http_auth_token || ''}
                      onChange={(e) => setMemoryConfig((prev) => ({ ...prev, http_auth_token: e.target.value }))}
                      className="font-mono text-xs"
                    />
                  </div>
                )}
              </div>
            )}
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

      {/* ─── TAB: API Keys ─── */}
      {activeTab === 'keys' && (
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div className="min-w-0 flex-1">
              <h2 className="text-sm font-semibold text-neutral-200 flex items-center gap-2">
                <KeyRound className="w-4 h-4 text-amber-400 flex-shrink-0" />
                App-Scoped Authorization Keys
              </h2>
              <p className="text-xs text-neutral-500 mt-0.5">
                Generate secret API keys (<code className="text-neutral-400">bv_app_sec_...</code>) for server-to-server or backend worker integration.
                Requests authenticate directly to this AI App without requiring end-user JWT issuance.
              </p>
            </div>
            <Button
              variant="primary"
              size="sm"
              className="flex-shrink-0 whitespace-nowrap"
              icon={<Plus className="w-3.5 h-3.5" />}
              onClick={() => { setCreatedKeySecret(null); setKeyError(null); setShowKeyModal(true); }}
            >
              Generate App Key
            </Button>
          </div>

          <Card padding="md">
            {(appKeys || []).length === 0 ? (
              <div className="text-center py-8 text-neutral-500 text-xs">
                No API keys generated for this app yet. Click &quot;Generate App Key&quot; to create a scoped key.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-xs text-left">
                  <thead>
                    <tr className="border-b border-neutral-800 text-neutral-500 text-[11px] uppercase tracking-wider">
                      <th className="pb-2.5 font-medium">Key Name</th>
                      <th className="pb-2.5 font-medium">Key Prefix</th>
                      <th className="pb-2.5 font-medium">Created</th>
                      <th className="pb-2.5 font-medium">Last Used</th>
                      <th className="pb-2.5 font-medium">Expires</th>
                      <th className="pb-2.5 font-medium text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y border-neutral-800/60 font-mono">
                    {(appKeys || []).map((k) => (
                      <tr key={k.id} className="hover:bg-neutral-900/40">
                        <td className="py-2.5 text-neutral-200 font-sans font-medium">{k.name}</td>
                        <td className="py-2.5 text-neutral-400">
                          <code>{k.key_prefix}...</code>
                        </td>
                        <td className="py-2.5 text-neutral-500 text-[11px] font-sans">
                          {formatRelativeTime(k.created_at)}
                        </td>
                        <td className="py-2.5 text-neutral-500 text-[11px] font-sans">
                          {k.last_used_at ? formatRelativeTime(k.last_used_at) : 'Never'}
                        </td>
                        <td className="py-2.5 text-neutral-500 text-[11px] font-sans">
                          {k.expires_at ? formatRelativeTime(k.expires_at) : 'Never'}
                        </td>
                        <td className="py-2.5 text-right font-sans">
                          <button
                            onClick={() => handleDeleteKey(k.id)}
                            className="p-1 text-neutral-600 hover:text-red-400 hover:bg-neutral-800 rounded transition-colors"
                            title="Revoke key"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>
      )}

      {/* ─── TAB 3: Playground ─── */}
      {activeTab === 'playground' && (
        <div className="space-y-3">
          {/* Pre-flight readiness checks */}
          {providers.length === 0 && (
            <Alert variant="warning">
              ⚠️ <b>No AI Provider Configured:</b> Outgoing requests will fail until upstream credentials (e.g. LiteLLM or direct provider) are configured in <b>Models &amp; Routing</b>.
            </Alert>
          )}
          {models.length > 0 && !models.some((m) => m.logical_model === (defaultModel)) && (
            <Alert variant="warning">
              ⚠️ <b>Unmapped Model Alias:</b> This app targets logical model &quot;<b>{defaultModel}</b>&quot;, which has no active deployment mapped in <b>Models &amp; Routing</b>.
            </Alert>
          )}
          {models.length === 0 && (
            <Alert variant="warning">
              ⚠️ <b>No Model Routing Configured:</b> No logical model mappings exist. Configure models before testing.
            </Alert>
          )}
          {(!appAuthProvider || appAuthProvider === 'anonymous') && (
            <Alert variant="info">
              ℹ️ <b>Auth Provider Not Required:</b> End-user JWT validation is not configured (or is set to Anonymous) in the <b>Identity &amp; Auth</b> tab. The playground executes with simulated administrative identity.
            </Alert>
          )}

          {/* JWT input */}
          <Card padding="sm">
            <Input
              label="Simulated Bearer JWT (optional)"
              placeholder="Optional: Paste a token or subject to test specific claims/policies, or leave blank to test as admin"
              value={simulatedToken}
              onChange={(e) => setSimulatedToken(e.target.value)}
              className="font-mono text-xs"
            />
          </Card>

          {/* Chat area */}
          <div className="bg-neutral-900 border border-neutral-800 rounded-xl flex flex-col" style={{ height: '520px' }}>
            <div className="px-4 py-3 border-b border-neutral-800 space-y-2.5">
              {/* Row 1: Title & Telemetry */}
              <div className="flex items-center justify-between gap-2 flex-wrap">
                <div className="flex items-center gap-2 flex-wrap">
                  <Terminal className="w-4 h-4 text-neutral-500" />
                  <span className="text-xs font-semibold text-neutral-300">Interactive Test Playground</span>
                  <Badge variant="mono" className="text-[10px]">
                    Context: ~{Math.round(chatMessages.reduce((sum, m) => sum + (m.content.length / 3.8), 0) + (userInput.length / 3.8))} tokens ({chatMessages.length} turns)
                  </Badge>
                  {responseSchema && (
                    <Badge variant="purple" className="text-[10px]">
                      <FileCode className="w-2.5 h-2.5" />
                      Schema Enforced
                    </Badge>
                  )}
                </div>
                {chatLoading && (
                  <span className="text-[10px] text-neutral-500 animate-pulse flex items-center gap-1">
                    <span className="w-1.5 h-1.5 bg-emerald-400 rounded-full animate-ping" />
                    {useStreaming ? 'Streaming tokens...' : 'Generating...'}
                  </span>
                )}
              </div>

              {/* Row 2: Controls Baseline - Out selector on left, Actions on right */}
              <div className="flex items-center justify-between gap-2 flex-wrap pt-0.5">
                {/* Output Modality Selector - aligned left below title */}
                <div className="flex items-center gap-1 bg-neutral-950 border border-neutral-800 rounded-lg p-0.5 px-2">
                  <span className="text-[10px] text-neutral-500 font-medium flex items-center gap-1" title="Requested output modalities. 'Auto' delegates media generation to tools.">
                    <Sparkles className="w-3 h-3 text-amber-400" />
                    Out:
                  </span>
                  <button
                    type="button"
                    onClick={() => setSelectedOutputModalities(['text'])}
                    className={`px-1.5 py-0.5 rounded text-[10px] font-medium transition-colors ${
                      selectedOutputModalities.length === 1 && selectedOutputModalities[0] === 'text'
                        ? 'bg-purple-500/20 text-purple-300 border border-purple-500/40'
                        : 'text-neutral-500 hover:text-neutral-300'
                    }`}
                    title="Auto: Standard text chat, model calls media tools (image, audio, video) automatically from prompt"
                  >
                    Auto
                  </button>
                  {(['image', 'audio', 'video'] as Modality[]).map((m) => {
                    const isSelected = selectedOutputModalities.includes(m);
                    return (
                      <button
                        key={m}
                        type="button"
                        onClick={() => {
                          if (isSelected) {
                            const filtered = selectedOutputModalities.filter((x) => x !== m);
                            setSelectedOutputModalities(filtered.length === 0 ? ['text'] : filtered);
                          } else {
                            const withoutText = selectedOutputModalities.filter((x) => x !== 'text');
                            setSelectedOutputModalities([...withoutText, m]);
                          }
                        }}
                        className={`px-1.5 py-0.5 rounded text-[10px] font-medium transition-colors ${
                          isSelected
                            ? 'bg-purple-500/20 text-purple-300 border border-purple-500/40'
                            : 'text-neutral-500 hover:text-neutral-300'
                        }`}
                        title={`Explicit override: Direct ${m} output requested for testing model_routes`}
                      >
                        {m}
                      </button>
                    );
                  })}
                </div>

                <div className="flex items-center gap-2 ml-auto shrink-0 flex-wrap">

                {/* Streaming Mode Toggle */}
                <button
                  type="button"
                  onClick={() => setUseStreaming(!useStreaming)}
                  className={`flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-[11px] font-medium border transition-colors ${
                    useStreaming
                      ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-400'
                      : 'bg-neutral-800 border-neutral-700 text-neutral-400 hover:text-neutral-200'
                  }`}
                  title="Toggle SSE Streaming vs Unary HTTP completion"
                >
                  <Radio className={`w-3 h-3 ${useStreaming ? 'animate-pulse' : ''}`} />
                  {useStreaming ? 'Streaming (SSE)' : 'Unary HTTP'}
                </button>

                {/* Thread Controls (Stateless vs Active Server Thread) */}
                <div className="flex items-center gap-1.5 bg-neutral-950 border border-neutral-800 rounded-lg p-0.5 px-2">
                  <span className="text-[10px] text-neutral-500">
                    {activeThreadId ? (
                      <span className="font-mono text-purple-400">th: {activeThreadId.slice(0, 10)}...</span>
                    ) : (
                      'Stateless'
                    )}
                  </span>
                  {!activeThreadId ? (
                    <button
                      type="button"
                      disabled={savingThread || chatLoading || !chatMessages.some((m) => m.role === 'user')}
                      onClick={handleSaveToThread}
                      className="flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-medium text-purple-400 hover:text-purple-300 hover:bg-purple-500/10 rounded transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
                      title="Save this conversation to a server-persisted conversation thread"
                    >
                      <Brain className="w-3 h-3" />
                      {savingThread ? 'Saving...' : 'Save to Thread'}
                    </button>
                  ) : (
                    <button
                      type="button"
                      onClick={() => {
                        setActiveThreadId('');
                        setChatMessages([{ role: 'assistant', content: 'Detached from thread. Now in stateless mode.' }]);
                        setPendingConfirmation(null);
                      }}
                      className="p-1 text-neutral-500 hover:text-red-400 rounded transition-colors"
                      title="Detach from thread and return to stateless mode"
                    >
                      <X className="w-3 h-3" />
                    </button>
                  )}
                </div>

                {/* Threads Drawer Toggle */}
                <Button
                  size="xs"
                  variant="ghost"
                  onClick={() => setShowHistory((v) => !v)}
                  icon={<RefreshCw className="w-3 h-3" />}
                  title="View saved server threads"
                >
                  Threads ({savedThreads.length})
                </Button>

                {/* New Chat */}
                <Button
                  size="xs"
                  variant="ghost"
                  onClick={startNewChat}
                  icon={<Trash2 className="w-3 h-3" />}
                  title="Clear chat and start fresh stateless test"
                >
                  New chat
                </Button>
              </div>
            </div>
          </div>

            {showHistory && (
              <div className="border-b border-neutral-800 bg-neutral-950 max-h-48 overflow-y-auto">
                {savedThreads.length === 0 ? (
                  <div className="px-4 py-3 text-[11px] text-neutral-500">
                    No saved server threads yet. Chat and click <strong>&quot;Save to Thread&quot;</strong> to persist a conversation on the server.
                  </div>
                ) : (
                  savedThreads.map((t) => (
                    <div
                      key={t.id}
                      className={`flex items-center justify-between gap-2 px-4 py-2 text-xs hover:bg-neutral-900 ${t.id === activeThreadId ? 'bg-neutral-900' : ''}`}
                    >
                      <button type="button" onClick={() => openThread(t)} className="flex-1 text-left truncate text-neutral-300">
                        <span className="font-mono text-purple-400 mr-2 text-[11px]">{t.id.slice(0, 14)}</span>
                        {t.title}
                        <span className="ml-2 text-[10px] text-neutral-600">
                          {new Date(t.updatedAt).toLocaleString()} · {t.messages.length} turns
                        </span>
                      </button>
                      <button
                        type="button"
                        onClick={() => deleteThreadItem(t.id)}
                        className="p-1 text-neutral-600 hover:text-red-400"
                        title="Delete thread from server and history"
                      >
                        <X className="w-3 h-3" />
                      </button>
                    </div>
                  ))
                )}
              </div>
            )}

            {/* Messages */}
            <div className="flex-1 overflow-y-auto px-4 py-3 space-y-3">
              {chatMessages.map((msg, idx) => {
                const isUser = msg.role === 'user';
                if (isUser) {
                  return (
                    <div key={idx} className="flex justify-end">
                      <div className="max-w-[80%] rounded-2xl rounded-br-none bg-emerald-600 text-white px-3.5 py-2.5 text-xs leading-relaxed whitespace-pre-wrap">
                        {msg.attachments && msg.attachments.length > 0 && (
                          <MediaMessageContent attachments={msg.attachments} role="user" />
                        )}
                        {msg.content}
                      </div>
                    </div>
                  );
                }

                const { thinking, content } = separateThinkingAndContent(msg.content, msg.thinking);
                const isBlank = !content && !thinking && (!msg.parts || msg.parts.length === 0);

                return (
                  <div key={idx} className="flex justify-start">
                    <div className="max-w-[85%] rounded-2xl rounded-bl-none bg-neutral-800 text-neutral-200 border border-neutral-700/60 px-3.5 py-2.5 text-xs leading-relaxed space-y-2">
                      {msg.routedModel && (
                        <div className="flex items-center gap-1.5 pb-1 border-b border-neutral-700/40 text-[10px] text-neutral-400">
                          <span className="font-mono text-purple-300 bg-purple-500/10 border border-purple-500/20 px-1.5 py-0.5 rounded">
                            ⚡ {msg.routedModel}
                          </span>
                          <span className="text-neutral-500">• routed model</span>
                        </div>
                      )}

                      {thinking ? (
                        <ThinkingBlock
                          thinking={thinking}
                          isStreaming={msg.isStreaming && !content}
                          hasContent={Boolean(content)}
                        />
                      ) : null}

                      {msg.parts && msg.parts.length > 0 && (
                        <MediaMessageContent mediaParts={msg.parts} role="assistant" />
                      )}

                      {isBlank && msg.isStreaming ? (
                        <div className="flex items-center gap-2 text-neutral-400 py-1">
                          <Brain className="w-3.5 h-3.5 text-purple-400 animate-pulse" />
                          <span className="text-xs text-neutral-300 font-medium">Thinking...</span>
                          <span className="flex gap-1 items-center ml-1">
                            <span className="w-1.5 h-1.5 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
                            <span className="w-1.5 h-1.5 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
                            <span className="w-1.5 h-1.5 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
                          </span>
                        </div>
                      ) : content ? (
                        <Markdown content={content} />
                      ) : null}
                    </div>
                  </div>
                );
              })}

              {/* Tool Confirmation Prompt */}
              {pendingConfirmation && (
                <div className="flex justify-start">
                  <div className="bg-amber-500/10 border border-amber-500/25 rounded-2xl rounded-bl-none p-3.5 max-w-[85%] space-y-2.5 shadow-lg">
                    <div className="flex items-center gap-2">
                      <AlertTriangle className="w-4 h-4 text-amber-400 shrink-0" />
                      <span className="text-xs font-semibold text-amber-400">Write Confirmation Required</span>
                    </div>
                    <p className="text-xs text-neutral-300">
                      The model requests permission to execute mutating tool:{' '}
                      <strong className="text-amber-300 font-mono">{pendingConfirmation.tool_name}</strong>
                    </p>
                    {pendingConfirmation.arguments && (
                      <pre className="p-2.5 bg-neutral-950/80 border border-neutral-800 rounded-lg text-[10px] font-mono text-neutral-300 overflow-x-auto max-h-32">
                        {JSON.stringify(pendingConfirmation.arguments, null, 2)}
                      </pre>
                    )}
                    <div className="flex justify-end gap-2 pt-1">
                      <Button
                        size="xs"
                        variant="secondary"
                        disabled={confirmLoading}
                        onClick={() => handleConfirmTool(false)}
                      >
                        Decline
                      </Button>
                      <Button
                        size="xs"
                        variant="primary"
                        loading={confirmLoading}
                        onClick={() => handleConfirmTool(true)}
                        icon={<Check className="w-3 h-3" />}
                      >
                        Approve & Execute
                      </Button>
                    </div>
                  </div>
                </div>
              )}

              {chatLoading && !useStreaming && (
                <div className="flex justify-start">
                  <div className="bg-neutral-800 border border-neutral-700/60 text-neutral-300 rounded-2xl rounded-bl-none px-3.5 py-2.5 text-xs flex items-center gap-2">
                    <Brain className="w-3.5 h-3.5 text-purple-400 animate-pulse" />
                    <span className="font-medium">Thinking...</span>
                    <span className="flex gap-1 items-center ml-1">
                      <span className="w-1.5 h-1.5 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
                      <span className="w-1.5 h-1.5 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
                      <span className="w-1.5 h-1.5 bg-purple-400 rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
                    </span>
                  </div>
                </div>
              )}
              <div ref={chatEndRef} />
            </div>

            {/* Pending attachments preview */}
            <MediaAttachmentPreview
              attachments={pendingAttachments}
              onRemove={(i) => setPendingAttachments((prev) => prev.filter((_, idx) => idx !== i))}
              disabled={chatLoading}
            />

            {/* Input */}
            <div className="border-t border-neutral-800 p-3">
              <form onSubmit={handleSendMessage} className="flex items-center gap-2">
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  accept="image/*,audio/*,video/*,application/pdf"
                  className="hidden"
                  onChange={handleFileSelect}
                />
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={chatLoading}
                  className="p-2 rounded-xl text-neutral-400 hover:text-neutral-200 hover:bg-neutral-800/80 transition-colors border border-transparent hover:border-neutral-700 shrink-0"
                  title="Attach media (Image, Audio, Video, PDF)"
                >
                  <Paperclip className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  onClick={() => setShowUrlModal(true)}
                  disabled={chatLoading}
                  className="p-2 rounded-xl text-neutral-400 hover:text-neutral-200 hover:bg-neutral-800/80 transition-colors border border-transparent hover:border-neutral-700 shrink-0"
                  title="Attach media by HTTPS URL"
                >
                  <LinkIcon className="w-4 h-4" />
                </button>
                <input
                  type="text"
                  placeholder="Ask anything or attach multimedia..."
                  value={userInput}
                  onChange={(e) => setUserInput(e.target.value)}
                  onPaste={handlePaste}
                  className="flex-1 bg-neutral-950 border border-neutral-800 rounded-xl px-4 py-2.5 text-xs text-neutral-100 placeholder-neutral-600 focus:outline-none focus:border-neutral-600"
                />
                <Button
                  type="submit"
                  variant="primary"
                  size="xs"
                  disabled={chatLoading || (!userInput.trim() && pendingAttachments.length === 0)}
                  icon={<Send className="w-3.5 h-3.5" />}
                >
                  Send
                </Button>
              </form>
            </div>
          </div>

          {/* Attach Media URL Modal */}
          <Modal
            open={showUrlModal}
            onClose={() => setShowUrlModal(false)}
            title="Attach Media by URL"
            description="Provide an HTTPS URL for an image, audio clip, video, or document."
            size="sm"
          >
            <div className="space-y-4">
              <Select
                label="Media Type"
                value={urlModality}
                onChange={(e) => setUrlModality(e.target.value as Modality)}
                options={[
                  { value: 'image', label: 'Image (image/*)' },
                  { value: 'audio', label: 'Audio (audio/*)' },
                  { value: 'video', label: 'Video (video/*)' },
                  { value: 'document', label: 'Document (PDF)' },
                ]}
              />
              <Input
                label="Media HTTPS URL"
                placeholder="https://example.com/sample.jpg"
                value={urlInput}
                onChange={(e) => setUrlInput(e.target.value)}
              />
              <div className="flex justify-end gap-2 pt-2">
                <Button size="xs" variant="secondary" onClick={() => setShowUrlModal(false)}>
                  Cancel
                </Button>
                <Button
                  size="xs"
                  variant="primary"
                  disabled={!urlInput.trim()}
                  onClick={handleAddUrlAttachment}
                >
                  Attach
                </Button>
              </div>
            </div>
          </Modal>
        </div>
      )}

      {/* Generate App Key Modal */}
      <Modal
        open={showKeyModal}
        onClose={() => { setShowKeyModal(false); setCreatedKeySecret(null); }}
        title="Generate App API Key"
        description="Creates a secret key scoped strictly to this AI App."
        size="md"
      >
        {createdKeySecret ? (
          <div className="space-y-4">
            <div className="p-3 bg-emerald-500/10 border border-emerald-500/25 rounded-xl text-xs text-emerald-300">
              <p className="font-semibold flex items-center gap-1.5 mb-1">
                <Check className="w-4 h-4 text-emerald-400" />
                API Key Generated Successfully
              </p>
              <p className="text-[11px] text-emerald-400/80 leading-relaxed">
                Copy this secret key now. For security, it will <b>never</b> be displayed again.
              </p>
            </div>
            <CodeBlock code={createdKeySecret} label="App Secret Key" />
            <div className="flex justify-end pt-2">
              <Button
                variant="primary"
                size="sm"
                onClick={() => { setShowKeyModal(false); setCreatedKeySecret(null); }}
              >
                Done
              </Button>
            </div>
          </div>
        ) : (
          <form onSubmit={handleCreateKey} className="space-y-4">
            {keyError && <Alert variant="error" onDismiss={() => setKeyError(null)}>{keyError}</Alert>}
            <Input
              label="Key Name / Identifier"
              required
              placeholder="e.g. backend-worker-staging"
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
            />
            <Select
              label="Expiration"
              value={newKeyExpires ? String(newKeyExpires) : ''}
              onChange={(e) => setNewKeyExpires(e.target.value ? Number(e.target.value) : undefined)}
              options={[
                { value: '', label: 'Never expires' },
                { value: '30', label: '30 days' },
                { value: '90', label: '90 days' },
                { value: '365', label: '1 year' },
              ]}
            />
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="ghost" size="sm" onClick={() => setShowKeyModal(false)}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" size="sm" loading={creatingKey}>
                Generate Key
              </Button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  );
};
