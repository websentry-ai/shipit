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
}

type AppSecret struct {
	ID             string    `db:"id" json:"id"`
	AppID          string    `db:"app_id" json:"app_id"`
	Key            string    `db:"key" json:"key"`
	ValueEncrypted []byte    `db:"value_encrypted" json:"-"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}
