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

export interface AIApp {
  id: string;
  project_id: string;
  environment: string;
  name: string;
  description: string;
  active_version: number;
  created_at: string;
  updated_at: string;
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
}

export interface ProviderConfig {
  id: string;
  project_id: string;
  environment: string;
  provider_name: string;
  base_url: string;
  masked_api_key: string;
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
  error_code?: string;
  error_message?: string;
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

  async createProject(payload: { id: string; name: string; description: string }): Promise<Project> {
    return this.request<Project>('/projects', {
      method: 'POST',
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

  // Prompts
  async listPrompts(): Promise<Prompt[]> {
    return this.request<Prompt[]>('/prompts');
  }

  async createPrompt(payload: any): Promise<Prompt> {
    return this.request<Prompt>('/prompts', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async createPromptVersion(promptID: string, payload: any): Promise<any> {
    return this.request(`/prompts/${promptID}/versions`, {
      method: 'POST',
      body: JSON.stringify(payload),
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

  async deleteTool(id: string): Promise<void> {
    return this.request(`/tools/${id}`, { method: 'DELETE' });
  }

  async upsertToolSecret(toolID: string, secretRef: string, secretValue: string): Promise<void> {
    return this.request(`/tools/${toolID}/secrets/${secretRef}`, {
      method: 'PUT',
      body: JSON.stringify({ secret_value: secretValue }),
    });
  }

  // Policies
  async listPolicies(): Promise<Policy[]> {
    return this.request<Policy[]>('/policies');
  }

  async createPolicy(payload: any): Promise<Policy> {
    return this.request<Policy>('/policies', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deletePolicy(id: string): Promise<void> {
    return this.request(`/policies/${id}`, { method: 'DELETE' });
  }

  // Observability & Audit
  async getUsage(granularity = 'hour'): Promise<any[]> {
    return this.request<any[]>(`/usage?granularity=${granularity}`);
  }

  async getLogs(limit = 50, offset = 0): Promise<RequestLog[]> {
    return this.request<RequestLog[]>(`/logs?limit=${limit}&offset=${offset}`);
  }

  async getAuditLogs(limit = 50, offset = 0): Promise<AuditLog[]> {
    return this.request<AuditLog[]>(`/audit-logs?limit=${limit}&offset=${offset}`);
  }

  async privacyPurge(issuer: string, subject: string): Promise<void> {
    return this.request(`/subjects/${encodeURIComponent(issuer)}/${encodeURIComponent(subject)}`, {
      method: 'DELETE',
    });
  }
}

export const api = new APIClient();
