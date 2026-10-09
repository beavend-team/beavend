export interface AdminUser {
  id: string;
  email: string;
  role: 'owner' | 'editor' | 'viewer';
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface Environment {
  project_id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface LimitRule {
  resource: string;
  window: string;
  max: number;
  tier_condition?: string;
}

export interface GuardrailConfig {
  pii_detection?: boolean;
  pii_redaction?: boolean;
  prompt_injection_defense?: boolean;
  content_safety?: boolean;
  max_input_length?: number;
  max_output_tokens?: number;
  streaming_mode?: 'buffered' | 'passthrough_with_post_check';
  max_depth?: number;
}

export interface CacheConfig {
  enabled?: boolean;
  ttl_seconds?: number;
  tool_caching?: boolean;
  scope?: 'user' | 'shared';
}

export interface MemoryConfig {
  enabled?: boolean;
  max_history?: number;
  vector_recall?: boolean;
  recall_limit?: number;
  provider_type?: 'postgres' | 'http' | 'local';
  http_url?: string;
  http_auth_token?: string;
  thread_ttl_sec?: number;
}

export interface SearchConfig {
  enabled?: boolean;
  max_results?: number;
  categories?: string[];
  engines?: string[];
  time_range?: string;
  safe_search?: number;
}

export interface AppAPIKey {
  id: string;
  app_id: string;
  project_id: string;
  environment: string;
  name: string;
  key_prefix: string;
  token?: string;
  secret_key?: string;
  created_at: string;
  last_used_at?: string;
  expires_at?: string;
}

export interface SearchHealthStatus {
  configured: boolean;
  enabled: boolean;
  url: string;
  status: 'healthy' | 'unreachable' | 'disabled' | 'error';
  latency_ms?: number;
  error?: string;
}

export interface AIAppVersion {
  id: string;
  app_id: string;
  project_id: string;
  environment: string;
  version: number;
  access_policy: string;
  prompt_id: string;
  prompt_version: number;
  model_routing_id: string;
  tool_ids: string[];
  limits: LimitRule[];
  cache_config: CacheConfig;
  guardrails: GuardrailConfig;
  memory_config?: MemoryConfig;
  memory?: MemoryConfig;
  search_config?: SearchConfig;
  search?: SearchConfig;
  response_schema: string;
  is_active: boolean;
  created_at: string;
}

export interface AIApp {
  id: string;
  project_id: string;
  environment: string;
  name: string;
  description: string;
  active_version: number;
  auth?: AuthConfig;
  auth_config?: AuthConfig;
  created_at: string;
  updated_at: string;
}

export interface AIAppDetails {
  app: AIApp;
  versions: AIAppVersion[];
}

export interface AuthConfig {
  provider: string; // "oidc" | "firebase" | "supabase" | "jwt" | "anonymous"
  issuer?: string;
  audience?: string[];
  jwks_url?: string;
  allowed_algorithms?: string[];
  project_id?: string;
  clock_skew?: string | number;
  allow_anonymous?: boolean;
}

/** Decode auth_config as returned by the control plane (object, JSON string, or base64 JSON). */
export function parseAuthConfig(raw: unknown): AuthConfig | null {
  if (!raw) return null;
  let val: any = raw;
  if (typeof val === 'string' && val.trim()) {
    try {
      val = JSON.parse(val);
    } catch (_) {
      try {
        val = JSON.parse(atob(val));
      } catch (_) {
        val = null;
      }
    }
  }
  if (val && typeof val === 'object' && val.provider) {
    const provider = String(val.provider).toLowerCase();
    return {
      ...val,
      provider,
      audience: Array.isArray(val.audience)
        ? val.audience
        : (typeof val.audience === 'string' && val.audience.trim()
          ? val.audience.split(',').map((s: string) => s.trim()).filter(Boolean)
          : []),
    };
  }
  return null;
}

export interface TestTokenResult {
  valid: boolean;
  error?: string;
  subject?: string;
  issuer?: string;
  audience?: string;
  issued_at?: string;
  expires_at?: string;
  claims?: Record<string, any>;
}

export interface Prompt {
  id: string;
  project_id: string;
  environment: string;
  name: string;
  description: string;
  active_version: number;
  created_at: string;
  updated_at: string;
}

export interface ModelRouting {
  id: string;
  project_id: string;
  environment: string;
  logical_model: string;
  deployment: string;
  adapter: string;
  fallback_deployments: string[];
  input_cost_per_1m?: number;
  output_cost_per_1m?: number;
  input_modalities?: Modality[];
  output_modalities?: Modality[];
}

export type Modality = 'text' | 'image' | 'audio' | 'video' | 'document';

export const ALL_MODALITIES: Modality[] = ['text', 'image', 'audio', 'video', 'document'];

export interface RouteCondition {
  input_has?: Modality[];
  output?: Modality[];
  metadata?: Record<string, string>;
  expr?: string;
}

export interface ModelRouteRule {
  when: RouteCondition;
  use: string;
}

export interface ModelRoutes {
  default: string;
  rules?: ModelRouteRule[];
}

export interface ChatAttachment {
  type: Modality;
  mime_type: string;
  url?: string;
  data?: string; // base64
  filename?: string;
  size?: number;
}

export interface MediaRef {
  media_id?: string;
  mime_type: string;
  digest?: string;
  url?: string;
  caption?: string;
  filename?: string;
  size?: number;
}

export interface MessagePart {
  type: 'text' | 'media' | 'tool_call' | 'tool_result';
  text?: string;
  media?: MediaRef;
  tool_call?: any;
  tool_result?: any;
}

export interface ProviderConfig {
  id: string;
  project_id: string;
  environment: string;
  provider_name: string;
  base_url: string;
  masked_api_key: string;
}

export interface ToolExecutorConfig {
  type: 'http' | 'webhook' | 'mcp' | 'sql_template' | 'client_side';
  method?: string;
  url?: string;
  headers?: Record<string, string>;
  timeout_ms?: number;
  retries?: number;
  template?: string;
}

export interface DownstreamAuthConfig {
  mode?: 'forward_user_token' | 'token_exchange' | 'service_token' | 'signed_headers';
  secret_ref?: string;
  header_name?: string;
}

export interface ResultConfig {
  max_bytes?: number;
  redact?: string[];
  transform?: string;
}

export interface ToolVersion {
  id: string;
  tool_id: string;
  version: number;
  side_effect: 'read' | 'write';
  input_schema: any;
  identity_bindings?: Record<string, string>;
  executor: ToolExecutorConfig;
  downstream_auth?: DownstreamAuthConfig;
  confirmation: 'none' | 'required';
  authorization?: string;
  result_config?: ResultConfig;
  cache_config?: CacheConfig;
  created_at: string;
}

export interface Tool {
  id: string;
  project_id: string;
  environment: string;
  name: string;
  description: string;
  active_version: number;
}

export interface Policy {
  id: string;
  project_id: string;
  environment: string;
  name: string;
  description: string;
  active_version: number;
}

export interface RequestLog {
  id: string;
  request_id: string;
  project_id: string;
  environment: string;
  ai_app_id: string;
  user_id: string;
  user_subject: string;
  logical_model: string;
  provider: string;
  model_deployment: string;
  status: string;
  status_code: number;
  latency_ms: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_usd: string;
  is_streaming: boolean;
  tool_calls?: any[];
  error_code?: string;
  error_message?: string;
  metadata?: Record<string, any>;
  created_at: string;
}

export interface AuditLog {
  id: number;
  project_id: string;
  environment: string;
  user_id: string;
  action: string;
  resource_type: string;
  resource_id: string;
  payload: any;
  created_at: string;
}

class APIClient {
  private token: string | null = null;
  private projectID: string = 'default';
  private environment: string = 'development';

  constructor() {
    this.token = localStorage.getItem('beavend_token');
    const storedProj = localStorage.getItem('beavend_project_id');
    if (storedProj) this.projectID = storedProj;
    const storedEnv = localStorage.getItem('beavend_environment');
    if (storedEnv) this.environment = storedEnv;
  }

  setToken(token: string | null) {
    this.token = token;
    if (token) {
      localStorage.setItem('beavend_token', token);
    } else {
      localStorage.removeItem('beavend_token');
    }
  }

  getToken(): string | null {
    return this.token;
  }

  setContext(projectID: string, environment: string) {
    this.projectID = projectID;
    this.environment = environment;
    localStorage.setItem('beavend_project_id', projectID);
    localStorage.setItem('beavend_environment', environment);
  }

  getContext() {
    return { projectID: this.projectID, environment: this.environment };
  }

  get currentProject(): string {
    return this.projectID;
  }

  get currentEnv(): string {
    return this.environment;
  }

  private async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      'X-Project-ID': this.projectID,
      'X-Environment': this.environment,
      ...(options.headers as Record<string, string>),
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    const response = await fetch(`/v1/admin${path}`, {
      ...options,
      headers,
    });

    if (response.status === 401) {
      this.setToken(null);
      window.dispatchEvent(new Event('beavend:unauthorized'));
      throw new Error('Authentication required');
    }

    if (!response.ok) {
      let errorMsg = `HTTP Error ${response.status}`;
      try {
        const errorJson = await response.json();
        errorMsg = errorJson.message || errorJson.code || errorMsg;
      } catch (_) {}
      throw new Error(errorMsg);
    }

    const text = await response.text();
    if (!text) {
      return {} as T;
    }
    return JSON.parse(text) as T;
  }

  // Auth
  async login(credentials: { email: string; password: string }): Promise<{ token: string; user: AdminUser }> {
    const res = await this.request<{ token: string; user: AdminUser }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify(credentials),
    });
    this.setToken(res.token);
    return res;
  }

  async logout(): Promise<void> {
    try {
      await this.request('/auth/logout', { method: 'POST' });
    } finally {
      this.setToken(null);
    }
  }

  async getMe(): Promise<AdminUser> {
    return this.request<AdminUser>('/auth/me');
  }

  async listUsers(): Promise<AdminUser[]> {
    return this.request<AdminUser[]>('/users');
  }

  async createUser(payload: { email: string; password: string; role: string }): Promise<AdminUser> {
    return this.request<AdminUser>('/users', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteUser(id: string): Promise<void> {
    return this.request(`/users/${id}`, { method: 'DELETE' });
  }

  // Projects
  async listProjects(): Promise<Project[]> {
    return this.request<Project[]>('/projects');
  }

  async getProject(id: string): Promise<Project> {
    return this.request<Project>(`/projects/${id}`);
  }

  async createProject(payload: { id: string; name: string; description: string }): Promise<Project> {
    return this.request<Project>('/projects', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updateProject(id: string, payload: { name: string; description: string }): Promise<Project> {
    return this.request<Project>(`/projects/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async deleteProject(id: string): Promise<void> {
    return this.request(`/projects/${id}`, { method: 'DELETE' });
  }

  async listEnvironments(projectID: string): Promise<Environment[]> {
    return this.request<Environment[]>(`/projects/${projectID}/environments`);
  }

  async createEnvironment(projectID: string, payload: { name: string; description: string }): Promise<Environment> {
    return this.request<Environment>(`/projects/${projectID}/environments`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteEnvironment(projectID: string, name: string): Promise<void> {
    return this.request(`/projects/${projectID}/environments/${name}`, { method: 'DELETE' });
  }

  async getEnvironmentAuthConfig(projectID: string, env: string): Promise<AuthConfig> {
    return this.request<AuthConfig>(`/projects/${projectID}/environments/${env}/auth`);
  }

  async updateEnvironmentAuthConfig(projectID: string, env: string, config: AuthConfig): Promise<AuthConfig> {
    return this.request<AuthConfig>(`/projects/${projectID}/environments/${env}/auth`, {
      method: 'PUT',
      body: JSON.stringify(config),
    });
  }

  async testToken(payload: { token: string; auth_config?: AuthConfig }): Promise<TestTokenResult> {
    return this.request<TestTokenResult>('/auth/test-token', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  private getAppPlaygroundHeaders(bearerToken?: string): Record<string, string> {
    const headers: Record<string, string> = {
      'X-Project-ID': this.currentProject,
      'X-Environment': this.currentEnv,
      'X-Playground': 'true',
    };
    if (this.token) {
      headers['X-Admin-Token'] = this.token;
    }
    if (bearerToken && bearerToken.trim()) {
      headers['Authorization'] = `Bearer ${bearerToken.trim().replace(/^Bearer\s+/i, '')}`;
    } else if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }
    return headers;
  }

  async chatWithApp(
    appId: string,
    messages: { role: string; content: string; parts?: any[] }[],
    bearerToken?: string,
    threadId?: string,
    options?: {
      attachments?: ChatAttachment[];
      outputModalities?: string[];
      metadata?: Record<string, string>;
    }
  ): Promise<any> {
    const headers: Record<string, string> = {
      ...this.getAppPlaygroundHeaders(bearerToken),
      'Content-Type': 'application/json',
    };
    const body: Record<string, any> = { messages };
    if (threadId) {
      body.thread_id = threadId;
    }
    if (options?.attachments && options.attachments.length > 0) {
      body.attachments = options.attachments;
    }
    if (options?.outputModalities && options.outputModalities.length > 0) {
      body.output_modalities = options.outputModalities;
    }
    if (options?.metadata) {
      body.metadata = options.metadata;
    }
    const res = await fetch(`/v1/apps/${appId}/chat`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || err.error || `HTTP ${res.status}`);
    }
    const data = await res.json();
    const routedModel = res.headers.get('X-Beavend-Model');
    if (routedModel && !data.model) {
      data.model = routedModel;
    }
    return data;
  }

  async streamChatWithApp(
    appId: string,
    messages: { role: string; content: string; parts?: any[] }[],
    callbacks: {
      onToken?: (text: string) => void;
      onThinking?: (text: string) => void;
      onConfirmation?: (event: any) => void;
      onToolCall?: (event: any) => void;
      onError?: (err: Error) => void;
      onComplete?: (res: any) => void;
      onModelRouted?: (model: string) => void;
      onMediaPart?: (media: MediaRef) => void;
    },
    bearerToken?: string,
    threadId?: string,
    options?: {
      attachments?: ChatAttachment[];
      outputModalities?: string[];
      metadata?: Record<string, string>;
    }
  ): Promise<void> {
    const headers: Record<string, string> = {
      ...this.getAppPlaygroundHeaders(bearerToken),
      'Content-Type': 'application/json',
    };
    const body: Record<string, any> = { messages };
    if (threadId) {
      body.thread_id = threadId;
    }
    if (options?.attachments && options.attachments.length > 0) {
      body.attachments = options.attachments;
    }
    if (options?.outputModalities && options.outputModalities.length > 0) {
      body.output_modalities = options.outputModalities;
    }
    if (options?.metadata) {
      body.metadata = options.metadata;
    }
    const res = await fetch(`/v1/apps/${appId}/stream`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || err.error || `HTTP ${res.status}`);
    }
    const routedModel = res.headers.get('X-Beavend-Model');
    if (routedModel) {
      callbacks.onModelRouted?.(routedModel);
    }
    if (!res.body) return;

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n\n');
      buffer = lines.pop() || '';

      for (const block of lines) {
        if (!block.trim()) continue;
        let eventType = 'message';
        let eventData = '';
        for (const line of block.split('\n')) {
          if (line.startsWith('event: ')) {
            eventType = line.substring(7).trim();
          } else if (line.startsWith('data: ')) {
            eventData = line.substring(6).trim();
          }
        }
        if (!eventData || eventData === '[DONE]') continue;
        try {
          const parsed = JSON.parse(eventData);
          if (parsed.model) {
            callbacks.onModelRouted?.(parsed.model);
          }
          if (eventType === 'confirmation') {
            callbacks.onConfirmation?.(parsed);
          } else if (eventType === 'tool_call') {
            callbacks.onToolCall?.(parsed);
          } else if (eventType === 'thinking') {
            callbacks.onThinking?.(parsed.text ?? parsed.delta ?? parsed.content ?? '');
          } else if (eventType === 'delta' || eventType === 'content_delta') {
            callbacks.onToken?.(parsed.text ?? parsed.delta ?? parsed.content ?? '');
          } else if (eventType === 'media') {
            callbacks.onMediaPart?.(parsed.media || parsed);
          } else if (eventType === 'error') {
            callbacks.onError?.(new Error(parsed.message || parsed.error || 'Stream error'));
          } else {
            const reasoning = parsed.reasoning_content || parsed.choices?.[0]?.delta?.reasoning_content || parsed.reasoning || parsed.choices?.[0]?.delta?.reasoning;
            if (reasoning) callbacks.onThinking?.(reasoning);
            const token = parsed.text || parsed.choices?.[0]?.delta?.content || parsed.delta || parsed.content || '';
            if (token) callbacks.onToken?.(token);
            if (parsed.media) callbacks.onMediaPart?.(parsed.media);
          }
        } catch (_) {}
      }
    }
    callbacks.onComplete?.({ status: 'done', model: routedModel });
  }

  async confirmToolExecution(
    appId: string,
    payload: { confirmation_token: string; approved: boolean },
    bearerToken?: string
  ): Promise<any> {
    const headers: Record<string, string> = {
      ...this.getAppPlaygroundHeaders(bearerToken),
      'Content-Type': 'application/json',
    };
    const res = await fetch(`/v1/apps/${appId}/confirm`, {
      method: 'POST',
      headers,
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || err.error || `HTTP ${res.status}`);
    }
    return res.json();
  }

  async createThread(
    appId: string,
    payload?: { thread_id?: string; messages?: { role: string; content: string }[]; ttl_seconds?: number; metadata?: any },
    bearerToken?: string
  ): Promise<any> {
    const headers: Record<string, string> = {
      ...this.getAppPlaygroundHeaders(bearerToken),
      'Content-Type': 'application/json',
    };
    const res = await fetch(`/v1/apps/${appId}/threads`, {
      method: 'POST',
      headers,
      body: JSON.stringify(payload || {}),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || err.error || `HTTP ${res.status}`);
    }
    return res.json();
  }

  async getThread(appId: string, threadId: string, bearerToken?: string): Promise<any> {
    const headers = this.getAppPlaygroundHeaders(bearerToken);
    const res = await fetch(`/v1/apps/${appId}/threads/${encodeURIComponent(threadId)}`, {
      headers,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || err.error || `HTTP ${res.status}`);
    }
    return res.json();
  }

  async deleteThread(appId: string, threadId: string, bearerToken?: string): Promise<void> {
    const headers = this.getAppPlaygroundHeaders(bearerToken);
    const res = await fetch(`/v1/apps/${appId}/threads/${encodeURIComponent(threadId)}`, {
      method: 'DELETE',
      headers,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || err.error || `HTTP ${res.status}`);
    }
  }

  // Apps
  async listApps(): Promise<AIApp[]> {
    return this.request<AIApp[]>('/apps');
  }

  async getApp(id: string): Promise<{ app: AIApp; versions: any[] }> {
    return this.request(`/apps/${id}`);
  }

  async createApp(payload: any): Promise<AIApp> {
    return this.request<AIApp>('/apps', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updateApp(id: string, payload: any): Promise<AIApp> {
    return this.request<AIApp>(`/apps/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async deleteApp(id: string): Promise<void> {
    return this.request(`/apps/${id}`, { method: 'DELETE' });
  }

  // App API Keys
  async listAppAPIKeys(appId: string): Promise<AppAPIKey[]> {
    return this.request<AppAPIKey[]>(`/apps/${appId}/keys`);
  }

  async createAppAPIKey(appId: string, name: string, expiresInDays?: number): Promise<AppAPIKey> {
    return this.request<AppAPIKey>(`/apps/${appId}/keys`, {
      method: 'POST',
      body: JSON.stringify({ name, expires_in_days: expiresInDays }),
    });
  }

  async deleteAppAPIKey(appId: string, keyId: string): Promise<void> {
    return this.request(`/apps/${appId}/keys/${keyId}`, {
      method: 'DELETE',
    });
  }

  // Search Engine (SearXNG)
  async getSearchHealth(): Promise<SearchHealthStatus> {
    return this.request<SearchHealthStatus>('/search/status');
  }

  async updateSearchConfig(payload: { url: string; enabled: boolean; timeout_seconds?: number }): Promise<SearchHealthStatus> {
    return this.request<SearchHealthStatus>('/search/config', {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async testSearch(query: string, categories?: string[], maxResults?: number): Promise<any> {
    return this.request('/search/test', {
      method: 'POST',
      body: JSON.stringify({ query, categories, max_results: maxResults }),
    });
  }

  // Prompts
  async listPrompts(): Promise<Prompt[]> {
    return this.request<Prompt[]>('/prompts');
  }

  async getPrompt(id: string): Promise<{ prompt: Prompt; versions: any[] }> {
    return this.request(`/prompts/${id}`);
  }

  async createPrompt(payload: any): Promise<Prompt> {
    return this.request<Prompt>('/prompts', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updatePrompt(id: string, payload: any): Promise<Prompt> {
    return this.request<Prompt>(`/prompts/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async createPromptVersion(promptID: string, payload: any): Promise<any> {
    return this.request(`/prompts/${promptID}/versions`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async setActivePromptVersion(promptID: string, version: number): Promise<any> {
    return this.request(`/prompts/${promptID}/active-version`, {
      method: 'PUT',
      body: JSON.stringify({ version }),
    });
  }

  async deletePrompt(id: string): Promise<void> {
    return this.request(`/prompts/${id}`, { method: 'DELETE' });
  }

  // Models
  async listModels(): Promise<ModelRouting[]> {
    return this.request<ModelRouting[]>('/models');
  }

  async upsertModel(logicalModel: string, payload: any): Promise<ModelRouting> {
    return this.request<ModelRouting>(`/models/${logicalModel}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async deleteModel(logicalModel: string): Promise<void> {
    return this.request(`/models/${logicalModel}`, { method: 'DELETE' });
  }

  async listProviders(): Promise<ProviderConfig[]> {
    return this.request<ProviderConfig[]>('/providers');
  }

  async upsertProvider(providerName: string, payload: { base_url: string; api_key: string }): Promise<ProviderConfig> {
    return this.request<ProviderConfig>(`/providers/${providerName}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async deleteProvider(providerName: string): Promise<void> {
    return this.request(`/providers/${providerName}`, { method: 'DELETE' });
  }

  async testProviderConnection(payload: { provider_name?: string; base_url?: string; api_key?: string }): Promise<{ success: boolean; status_code?: number; latency_ms: number; message: string }> {
    return this.request('/providers/test-connection', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async fetchProviderModels(providerName: string): Promise<{ provider_name: string; models: string[] }> {
    return this.request(`/providers/${encodeURIComponent(providerName)}/models`);
  }

  // Tools
  async listTools(): Promise<Tool[]> {
    return this.request<Tool[]>('/tools');
  }

  async createTool(payload: any): Promise<Tool> {
    return this.request<Tool>('/tools', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getTool(id: string): Promise<{ tool: Tool; versions: any[] }> {
    return this.request(`/tools/${id}`);
  }

  async updateTool(id: string, payload: any): Promise<Tool> {
    return this.request<Tool>(`/tools/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async deleteTool(id: string): Promise<void> {
    return this.request(`/tools/${id}`, { method: 'DELETE' });
  }

  async testTool(toolID: string, payload: { arguments?: Record<string, any>; identity?: Record<string, any> }): Promise<any> {
    return this.request(`/tools/${encodeURIComponent(toolID)}/test`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async upsertToolSecret(toolID: string, secretRef: string, secretValue: string): Promise<void> {
    return this.request(`/tools/${toolID}/secrets/${secretRef}`, {
      method: 'PUT',
      body: JSON.stringify({ secret_value: secretValue }),
    });
  }

  async setActiveToolVersion(toolID: string, version: number): Promise<any> {
    return this.request(`/tools/${toolID}/active-version`, {
      method: 'PUT',
      body: JSON.stringify({ version }),
    });
  }

  // Policies
  async listPolicies(): Promise<Policy[]> {
    return this.request<Policy[]>('/policies');
  }

  async getPolicy(id: string): Promise<{ policy: Policy; versions: any[] }> {
    return this.request(`/policies/${id}`);
  }

  async createPolicy(payload: any): Promise<Policy> {
    return this.request<Policy>('/policies', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async createPolicyVersion(policyID: string, payload: { expression: string; set_active: boolean }): Promise<any> {
    return this.request(`/policies/${policyID}/versions`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async setActivePolicyVersion(policyID: string, version: number): Promise<any> {
    return this.request(`/policies/${policyID}/active-version`, {
      method: 'PUT',
      body: JSON.stringify({ version }),
    });
  }

  async testPolicy(payload: {
    expression: string;
    claims?: Record<string, any>;
    identity?: Record<string, any>;
    meta?: Record<string, any>;
  }): Promise<{ valid: boolean; allowed: boolean; error?: string }> {
    return this.request<{ valid: boolean; allowed: boolean; error?: string }>('/policies/test', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deletePolicy(id: string): Promise<void> {
    return this.request(`/policies/${id}`, { method: 'DELETE' });
  }

  // Observability & Audit
  async getUsage(granularity = 'hour', from?: string, to?: string): Promise<any[]> {
    let url = `/usage?granularity=${granularity}`;
    if (from) url += `&from=${encodeURIComponent(from)}`;
    if (to) url += `&to=${encodeURIComponent(to)}`;
    return this.request<any[]>(url);
  }

  async getLogs(limit = 50, offset = 0, from?: string, to?: string): Promise<RequestLog[]> {
    let url = `/logs?limit=${limit}&offset=${offset}`;
    if (from) url += `&from=${encodeURIComponent(from)}`;
    if (to) url += `&to=${encodeURIComponent(to)}`;
    return this.request<RequestLog[]>(url);
  }

  async getLogByID(requestID: string): Promise<RequestLog> {
    return this.request<RequestLog>(`/logs/${encodeURIComponent(requestID)}`);
  }

  async getAuditLogs(limit = 50, offset = 0): Promise<AuditLog[]> {
    return this.request<AuditLog[]>(`/audit-logs?limit=${limit}&offset=${offset}`);
  }

  async privacyPurge(issuer: string, subject: string): Promise<void> {
    return this.request(`/subjects/${encodeURIComponent(issuer)}/${encodeURIComponent(subject)}`, {
      method: 'DELETE',
    });
  }

  // Configuration as Code (Export & Import)
  async exportConfig(projectId: string, env: string): Promise<string> {
    const res = await fetch(`/v1/admin/projects/${encodeURIComponent(projectId)}/environments/${encodeURIComponent(env)}/export`, {
      headers: this.token ? { Authorization: `Bearer ${this.token}` } : {},
      credentials: 'include',
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || 'Export failed');
    }
    return res.text();
  }

  async importConfig(projectId: string, env: string, content: string, dryRun = false): Promise<DiffReport> {
    const res = await fetch(`/v1/admin/projects/${encodeURIComponent(projectId)}/environments/${encodeURIComponent(env)}/import?dry_run=${dryRun}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-yaml',
        ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
      },
      credentials: 'include',
      body: content,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }));
      throw new Error(err.message || 'Import failed');
    }
    return res.json();
  }
}

export interface ResourceDiff {
  type: string;
  id: string;
  action: 'create' | 'update' | 'unchanged';
  changes?: string[];
}

export interface DiffReport {
  project_id: string;
  environment: string;
  resources: ResourceDiff[];
  summary: {
    total_created: number;
    total_updated: number;
    total_unchanged: number;
  };
}

export const api = new APIClient();
