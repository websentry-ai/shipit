import type {
  Project,
  Cluster,
  App,
  AppRevision,
  AppSecret,
  AppStatus,
  CreateAppRequest,
  UpdateAppRequest,
} from '../types';

const API_BASE = '/api';

// Get token from localStorage
function getToken(): string | null {
  return localStorage.getItem('shipit_token');
}

// Set token in localStorage
export function setToken(token: string): void {
  localStorage.setItem('shipit_token', token);
}

// Clear token
export function clearToken(): void {
  localStorage.removeItem('shipit_token');
}

// Check if authenticated
export function isAuthenticated(): boolean {
  return !!getToken();
}

async function request<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const token = getToken();
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...options.headers,
  };

  const response = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers,
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({}));
    throw new Error(error.error || `HTTP ${response.status}`);
  }

  // Handle empty responses
  const text = await response.text();
  if (!text) return {} as T;
  return JSON.parse(text);
}

// Projects
export async function listProjects(): Promise<Project[]> {
  return request<Project[]>('/projects');
}

export async function getProject(id: string): Promise<Project> {
  return request<Project>(`/projects/${id}`);
}

export async function createProject(name: string): Promise<Project> {
  return request<Project>('/projects', {
    method: 'POST',
    body: JSON.stringify({ name }),
  });
}

export async function deleteProject(id: string): Promise<void> {
  return request(`/projects/${id}`, { method: 'DELETE' });
}

// Clusters
export async function listClusters(projectId: string): Promise<Cluster[]> {
  return request<Cluster[]>(`/projects/${projectId}/clusters`);
}

export async function getCluster(id: string): Promise<Cluster> {
  return request<Cluster>(`/clusters/${id}`);
}

export async function deleteCluster(id: string): Promise<void> {
  return request(`/clusters/${id}`, { method: 'DELETE' });
}

// Apps
export async function listApps(clusterId: string): Promise<App[]> {
  return request<App[]>(`/clusters/${clusterId}/apps`);
}

export async function getApp(id: string): Promise<App> {
  return request<App>(`/apps/${id}`);
}

export async function createApp(
  clusterId: string,
  data: CreateAppRequest
): Promise<App> {
  return request<App>(`/clusters/${clusterId}/apps`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function deleteApp(id: string): Promise<void> {
  return request(`/apps/${id}`, { method: 'DELETE' });
}

export async function updateApp(
  id: string,
  data: UpdateAppRequest
): Promise<App> {
  return request<App>(`/apps/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function deployApp(id: string): Promise<App> {
  return request<App>(`/apps/${id}/deploy`, { method: 'POST' });
}

export async function getAppStatus(id: string): Promise<AppStatus> {
  return request<AppStatus>(`/apps/${id}/status`);
}

// Revisions
export async function listRevisions(
  appId: string,
  limit = 10
): Promise<AppRevision[]> {
  return request<AppRevision[]>(`/apps/${appId}/revisions?limit=${limit}`);
}

export async function getRevision(
  appId: string,
  revision: number
): Promise<AppRevision> {
  return request<AppRevision>(`/apps/${appId}/revisions/${revision}`);
}

export async function rollbackApp(
  appId: string,
  revision?: number
): Promise<App> {
  const body = revision ? JSON.stringify({ revision }) : '{}';
  return request<App>(`/apps/${appId}/rollback`, {
    method: 'POST',
    body,
  });
}

// Secrets
export async function listSecrets(appId: string): Promise<AppSecret[]> {
  return request<AppSecret[]>(`/apps/${appId}/secrets`);
}

export async function setSecret(
  appId: string,
  key: string,
  value: string
): Promise<void> {
  return request(`/apps/${appId}/secrets`, {
    method: 'POST',
    body: JSON.stringify({ key, value }),
  });
}

export async function deleteSecret(appId: string, key: string): Promise<void> {
  return request(`/apps/${appId}/secrets/${key}`, { method: 'DELETE' });
}

// Logs (returns EventSource for streaming)
export function streamLogs(
  appId: string,
  tail = 100
): EventSource {
  const token = getToken();
  const url = `${API_BASE}/apps/${appId}/logs?tail=${tail}&follow=true`;

  // Note: EventSource doesn't support custom headers
  // For now, we'll append token as query param (less secure but works)
  const urlWithAuth = token ? `${url}&token=${token}` : url;
  return new EventSource(urlWithAuth);
}
