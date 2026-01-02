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
}
