package db

import (
	"encoding/json"
	"time"
)

type APIToken struct {
	ID         string     `db:"id" json:"id"`
	Name       string     `db:"name" json:"name"`
	TokenHash  string     `db:"token_hash" json:"-"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	LastUsedAt *time.Time `db:"last_used_at" json:"last_used_at,omitempty"`
}

type Project struct {
	ID        string    `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type Cluster struct {
	ID                  string    `db:"id" json:"id"`
	ProjectID           string    `db:"project_id" json:"project_id"`
	Name                string    `db:"name" json:"name"`
	Endpoint            string    `db:"endpoint" json:"endpoint,omitempty"`
	KubeconfigEncrypted []byte    `db:"kubeconfig_encrypted" json:"-"`
	Status              string    `db:"status" json:"status"`
	StatusMessage       *string   `db:"status_message" json:"status_message,omitempty"`
	CreatedAt           time.Time `db:"created_at" json:"created_at"`
}

type App struct {
	ID            string          `db:"id" json:"id"`
	ClusterID     string          `db:"cluster_id" json:"cluster_id"`
	Name          string          `db:"name" json:"name"`
	ServiceName   *string         `db:"service_name" json:"service_name,omitempty"` // Service within Porter app (e.g., "gateway", "web")
	AppGroup      *string         `db:"app_group" json:"app_group,omitempty"`       // Porter app name for grouping (e.g., "staging-gateway")
	Namespace     string          `db:"namespace" json:"namespace"`
	Image         string          `db:"image" json:"image"`
	Replicas      int             `db:"replicas" json:"replicas"`
	Port          *int            `db:"port" json:"port,omitempty"`
	EnvVars       json.RawMessage `db:"env_vars" json:"env_vars"`
	Status        string          `db:"status" json:"status"`
	StatusMessage *string         `db:"status_message" json:"status_message,omitempty"`
	CreatedAt     time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at" json:"updated_at"`

	// Resource limits
	CPURequest    string `db:"cpu_request" json:"cpu_request"`
	CPULimit      string `db:"cpu_limit" json:"cpu_limit"`
	MemoryRequest string `db:"memory_request" json:"memory_request"`
	MemoryLimit   string `db:"memory_limit" json:"memory_limit"`

	// Health check configuration
	HealthPath         *string `db:"health_path" json:"health_path,omitempty"`
	HealthPort         *int    `db:"health_port" json:"health_port,omitempty"`
	HealthInitialDelay *int    `db:"health_initial_delay" json:"health_initial_delay,omitempty"`
	HealthPeriod       *int    `db:"health_period" json:"health_period,omitempty"`

	// Revision tracking
	CurrentRevision int `db:"current_revision" json:"current_revision"`

	// HPA (auto-scaling) configuration
	HPAEnabled   bool  `db:"hpa_enabled" json:"hpa_enabled"`
	MinReplicas  *int  `db:"min_replicas" json:"min_replicas,omitempty"`
	MaxReplicas  *int  `db:"max_replicas" json:"max_replicas,omitempty"`
	CPUTarget    *int  `db:"cpu_target" json:"cpu_target,omitempty"`
	MemoryTarget *int  `db:"memory_target" json:"memory_target,omitempty"`

	// Custom domain configuration
	Domain       *string `db:"domain" json:"domain,omitempty"`
	DomainStatus *string `db:"domain_status" json:"domain_status,omitempty"`

	// Pre-deploy hook (command to run before deployment, e.g., migrations)
	PreDeployCommand *string `db:"pre_deploy_command" json:"pre_deploy_command,omitempty"`

	// Porter migration fields (Phase 3)
	ManagedBy    string  `db:"managed_by" json:"managed_by"`                     // "shipit", "porter", or "observer"
	PorterAppID  *string `db:"porter_app_id" json:"porter_app_id,omitempty"`     // Porter's internal app ID
	PorterAppURL *string `db:"porter_app_url" json:"porter_app_url,omitempty"`   // Porter dashboard URL

	// Zero-downtime mode (Phase 2.11). When false, the renderer skips PDB,
	// topologySpread, and preStop, and falls back to RollingUpdate 25%/25%
	// — i.e. closer to raw kube defaults. Override fields hold user-supplied
	// rolling-update budget values; nil means "derive from replica count".
	ZeroDowntimeEnabled        bool    `db:"zero_downtime_enabled" json:"zero_downtime_enabled"`
	MaxSurgeOverride           *string `db:"max_surge_override" json:"max_surge_override,omitempty"`
	MaxUnavailableOverride     *string `db:"max_unavailable_override" json:"max_unavailable_override,omitempty"`
	MaxRequestDurationSeconds  int     `db:"max_request_duration_seconds" json:"max_request_duration_seconds"`
}

// AppRevision stores a snapshot of app configuration at deploy time
type AppRevision struct {
	ID             string          `db:"id" json:"id"`
	AppID          string          `db:"app_id" json:"app_id"`
	RevisionNumber int             `db:"revision_number" json:"revision_number"`
	Image          string          `db:"image" json:"image"`
	Replicas       int             `db:"replicas" json:"replicas"`
	Port           *int            `db:"port" json:"port,omitempty"`
	EnvVars        json.RawMessage `db:"env_vars" json:"env_vars"`
	CPURequest     *string         `db:"cpu_request" json:"cpu_request,omitempty"`
	CPULimit       *string         `db:"cpu_limit" json:"cpu_limit,omitempty"`
	MemoryRequest  *string         `db:"memory_request" json:"memory_request,omitempty"`
	MemoryLimit    *string         `db:"memory_limit" json:"memory_limit,omitempty"`
	HealthPath     *string         `db:"health_path" json:"health_path,omitempty"`
	HealthPort     *int            `db:"health_port" json:"health_port,omitempty"`
	HealthDelay    *int            `db:"health_initial_delay" json:"health_initial_delay,omitempty"`
	HealthPeriod   *int            `db:"health_period" json:"health_period,omitempty"`
	CreatedAt      time.Time       `db:"created_at" json:"created_at"`
	DeployedBy     *string         `db:"deployed_by" json:"deployed_by,omitempty"`

	// HPA snapshot
	HPAEnabled   bool `db:"hpa_enabled" json:"hpa_enabled"`
	MinReplicas  *int `db:"min_replicas" json:"min_replicas,omitempty"`
	MaxReplicas  *int `db:"max_replicas" json:"max_replicas,omitempty"`
	CPUTarget    *int `db:"cpu_target" json:"cpu_target,omitempty"`
	MemoryTarget *int `db:"memory_target" json:"memory_target,omitempty"`

	// Domain snapshot
	Domain *string `db:"domain" json:"domain,omitempty"`

	// Pre-deploy hook snapshot
	PreDeployCommand *string `db:"pre_deploy_command" json:"pre_deploy_command,omitempty"`

	// Phase 3: Multi-service support snapshots
	ServiceName *string `db:"service_name" json:"service_name,omitempty"`
	AppGroup    *string `db:"app_group" json:"app_group,omitempty"`
	ManagedBy   *string `db:"managed_by" json:"managed_by,omitempty"`

	// Deployment status
	DeployStatus  string     `db:"deploy_status" json:"deploy_status"`
	DeployMessage *string    `db:"deploy_message" json:"deploy_message,omitempty"`
	DeployedAt    *time.Time `db:"deployed_at" json:"deployed_at,omitempty"`

	// Zero-downtime snapshot (Phase 2.11). Pointer types because revisions
	// pre-dating the migration carry NULL.
	ZeroDowntimeEnabled       *bool   `db:"zero_downtime_enabled" json:"zero_downtime_enabled,omitempty"`
	MaxSurgeOverride          *string `db:"max_surge_override" json:"max_surge_override,omitempty"`
	MaxUnavailableOverride    *string `db:"max_unavailable_override" json:"max_unavailable_override,omitempty"`
	MaxRequestDurationSeconds *int    `db:"max_request_duration_seconds" json:"max_request_duration_seconds,omitempty"`
}

type AppSecret struct {
	ID             string    `db:"id" json:"id"`
	AppID          string    `db:"app_id" json:"app_id"`
	Key            string    `db:"key" json:"key"`
	ValueEncrypted []byte    `db:"value_encrypted" json:"-"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}

// User represents an authenticated user (via Google SSO)
type User struct {
	ID          string     `db:"id" json:"id"`
	Email       string     `db:"email" json:"email"`
	Name        *string    `db:"name" json:"name,omitempty"`
	PictureURL  *string    `db:"picture_url" json:"picture_url,omitempty"`
	GoogleID    *string    `db:"google_id" json:"-"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	LastLoginAt *time.Time `db:"last_login_at" json:"last_login_at,omitempty"`
}

// Session represents a web session (cookie-based auth)
type Session struct {
	ID               string    `db:"id" json:"id"`
	UserID           string    `db:"user_id" json:"user_id"`
	SessionTokenHash string    `db:"session_token_hash" json:"-"`
	ExpiresAt        time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt        time.Time `db:"created_at" json:"created_at"`
	UserAgent        *string   `db:"user_agent" json:"user_agent,omitempty"`
	IPAddress        *string   `db:"ip_address" json:"ip_address,omitempty"`
}

// UserToken represents a user-generated API token (for CLI)
type UserToken struct {
	ID         string     `db:"id" json:"id"`
	UserID     string     `db:"user_id" json:"user_id"`
	Name       string     `db:"name" json:"name"`
	TokenHash  string     `db:"token_hash" json:"-"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	LastUsedAt *time.Time `db:"last_used_at" json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `db:"expires_at" json:"expires_at,omitempty"`
}
