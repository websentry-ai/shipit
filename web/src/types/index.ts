export interface Project {
  id: string;
  name: string;
  created_at: string;
}

export interface Cluster {
  id: string;
  project_id: string;
  name: string;
  endpoint: string;
  status: string;
  status_message?: string;
  created_at: string;
}

export interface App {
  id: string;
  cluster_id: string;
  name: string;
  namespace: string;
  image: string;
  replicas: number;
  port?: number;
  env_vars?: Record<string, string>;
  status: string;
  status_message?: string;
  created_at: string;
  updated_at: string;
  // Resource limits
  cpu_request: string;
  cpu_limit: string;
  memory_request: string;
  memory_limit: string;
  // Health checks
  health_path?: string;
  health_port?: number;
  health_initial_delay?: number;
  health_period?: number;
  // Revision tracking
  current_revision: number;
  // Custom domain
  domain?: string;
  domain_status?: string;
  // Pre-deploy hook
  pre_deploy_command?: string;
  // Porter migration fields (Phase 3)
  service_name?: string;  // Service within the app (e.g., "gateway", "web")
  app_group?: string;     // Logical app grouping (e.g., "staging-gateway")
  managed_by: 'shipit' | 'porter';
  porter_app_id?: string;
  porter_app_url?: string;
}

export interface AppRevision {
  id: string;
  app_id: string;
  revision_number: number;
  image: string;
  replicas: number;
  port?: number;
  env_vars?: Record<string, string>;
  cpu_request?: string;
  cpu_limit?: string;
  memory_request?: string;
  memory_limit?: string;
  health_path?: string;
  health_port?: number;
  health_initial_delay?: number;
  health_period?: number;
  created_at: string;
  deployed_by?: string;
  // Deployment status
  deploy_status: string;
  deploy_message?: string;
  deployed_at?: string;
  // Pre-deploy hook
  pre_deploy_command?: string;
}

export interface AppSecret {
  key: string;
  created_at: string;
  updated_at: string;
}

export interface AppStatus {
  app_id: string;
  deployment_status: string;
  ready_replicas: number;
  desired_replicas: number;
  pods: PodStatus[];
}

export interface PodStatus {
  name: string;
  phase: string;
  ready: boolean;
  restarts: number;
  age: string;
  // Resource metrics (from metrics-server)
  cpu_usage?: string;    // e.g., "50m" (millicores)
  memory_usage?: string; // e.g., "128Mi"
  cpu_percent?: number;  // percentage of limit
  mem_percent?: number;  // percentage of limit
}

export interface CreateAppRequest {
  name: string;
  namespace?: string;
  image: string;
  replicas?: number;
  port?: number;
  env_vars?: Record<string, string>;
  cpu_request?: string;
  cpu_limit?: string;
  memory_request?: string;
  memory_limit?: string;
  health_path?: string;
  health_port?: number;
  health_initial_delay?: number;
  health_period?: number;
}

export interface UpdateAppRequest {
  image?: string;
  replicas?: number;
  env_vars?: Record<string, string>;
  cpu_request?: string;
  cpu_limit?: string;
  memory_request?: string;
  memory_limit?: string;
  health_path?: string;
  health_port?: number;
  health_initial_delay?: number;
  health_period?: number;
}

export interface HPAStatus {
  enabled: boolean;
  min_replicas: number;
  max_replicas: number;
  current_replicas: number;
  desired_replicas: number;
  current_cpu_percent?: number;
  current_memory_percent?: number;
  target_cpu_percent?: number;
  target_memory_percent?: number;
}

export interface HPAConfig {
  enabled: boolean;
  min_replicas?: number;
  max_replicas?: number;
  target_cpu_percent?: number;
  target_memory_percent?: number;
}

export interface IngressStatus {
  domain: string;
  tls_enabled: boolean;
  ready: boolean;
  load_balancer?: string;
  hosts?: string[];
}

export interface DomainStatus {
  domain?: string;
  domain_status?: string;
  ingress?: IngressStatus;
}

export interface DomainConfig {
  domain?: string;
}

export interface PreDeployHookStatus {
  pre_deploy_command?: string;
}

export interface PreDeployHookConfig {
  command?: string;
}

// User types (SSO)
export interface User {
  id: string;
  email: string;
  name?: string;
  picture_url?: string;
  created_at: string;
  last_login_at?: string;
}

export interface UserToken {
  id: string;
  user_id: string;
  name: string;
  created_at: string;
  last_used_at?: string;
  expires_at?: string;
}

export interface CreateTokenRequest {
  name: string;
  expires_in?: number; // days
}

export interface CreateTokenResponse {
  id: string;
  name: string;
  token: string; // Only shown once
  created_at: string;
  expires_at?: string;
}

// Cluster ingress controller info
export interface IngressControllerInfo {
  available: boolean;
  load_balancer?: string;
  message?: string;
  base_domain?: string; // e.g., "apps.shipit.unboundsec.dev"
}
