package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Token operations

func (db *DB) ValidateToken(ctx context.Context, token string) (*APIToken, error) {
	hash := hashToken(token)
	var t APIToken
	err := db.GetContext(ctx, &t, `
		SELECT id, name, token_hash, created_at, last_used_at
		FROM api_tokens WHERE token_hash = $1
	`, hash)
	if err != nil {
		return nil, err
	}

	// Update last used timestamp
	go db.Exec(`UPDATE api_tokens SET last_used_at = $1 WHERE id = $2`, time.Now(), t.ID)

	return &t, nil
}

func (db *DB) CreateToken(ctx context.Context, name, token string) (*APIToken, error) {
	hash := hashToken(token)
	var t APIToken
	err := db.GetContext(ctx, &t, `
		INSERT INTO api_tokens (name, token_hash)
		VALUES ($1, $2)
		RETURNING id, name, token_hash, created_at
	`, name, hash)
	return &t, err
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Project operations

func (db *DB) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	err := db.SelectContext(ctx, &projects, `SELECT * FROM projects ORDER BY created_at DESC`)
	return projects, err
}

func (db *DB) GetProject(ctx context.Context, id string) (*Project, error) {
	var p Project
	err := db.GetContext(ctx, &p, `SELECT * FROM projects WHERE id = $1`, id)
	return &p, err
}

func (db *DB) GetProjectByName(ctx context.Context, name string) (*Project, error) {
	var p Project
	err := db.GetContext(ctx, &p, `SELECT * FROM projects WHERE name = $1`, name)
	return &p, err
}

func (db *DB) CreateProject(ctx context.Context, name string) (*Project, error) {
	var p Project
	err := db.GetContext(ctx, &p, `
		INSERT INTO projects (name) VALUES ($1)
		RETURNING id, name, created_at
	`, name)
	return &p, err
}

func (db *DB) DeleteProject(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

// Cluster operations

func (db *DB) ListClusters(ctx context.Context, projectID string) ([]Cluster, error) {
	var clusters []Cluster
	err := db.SelectContext(ctx, &clusters, `
		SELECT id, project_id, name, endpoint, status, status_message, created_at
		FROM clusters WHERE project_id = $1 ORDER BY created_at DESC
	`, projectID)
	return clusters, err
}

func (db *DB) GetCluster(ctx context.Context, id string) (*Cluster, error) {
	var c Cluster
	err := db.GetContext(ctx, &c, `SELECT * FROM clusters WHERE id = $1`, id)
	return &c, err
}

func (db *DB) CreateCluster(ctx context.Context, projectID, name string, kubeconfigEncrypted []byte) (*Cluster, error) {
	var c Cluster
	err := db.GetContext(ctx, &c, `
		INSERT INTO clusters (project_id, name, kubeconfig_encrypted, status)
		VALUES ($1, $2, $3, 'pending')
		RETURNING id, project_id, name, status, created_at
	`, projectID, name, kubeconfigEncrypted)
	return &c, err
}

func (db *DB) UpdateClusterStatus(ctx context.Context, id, status string, message *string, endpoint string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE clusters SET status = $1, status_message = $2, endpoint = $3 WHERE id = $4
	`, status, message, endpoint, id)
	return err
}

func (db *DB) DeleteCluster(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM clusters WHERE id = $1`, id)
	return err
}

// App operations

func (db *DB) ListApps(ctx context.Context, clusterID string) ([]App, error) {
	var apps []App
	err := db.SelectContext(ctx, &apps, `SELECT * FROM apps WHERE cluster_id = $1 ORDER BY created_at DESC`, clusterID)
	return apps, err
}

func (db *DB) GetApp(ctx context.Context, id string) (*App, error) {
	var a App
	err := db.GetContext(ctx, &a, `SELECT * FROM apps WHERE id = $1`, id)
	return &a, err
}

// CreateAppParams contains all parameters for creating an app
type CreateAppParams struct {
	ClusterID   string
	Name        string
	Namespace   string
	Image       string
	Replicas    int
	Port        *int
	EnvVars     []byte
	CPURequest  string
	CPULimit    string
	MemRequest  string
	MemLimit    string
	HealthPath  *string
	HealthPort  *int
	HealthDelay *int
	HealthPeriod *int
}

func (db *DB) CreateApp(ctx context.Context, p CreateAppParams) (*App, error) {
	var a App
	err := db.GetContext(ctx, &a, `
		INSERT INTO apps (cluster_id, name, namespace, image, replicas, port, env_vars, status,
			cpu_request, cpu_limit, memory_request, memory_limit,
			health_path, health_port, health_initial_delay, health_period)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING *
	`, p.ClusterID, p.Name, p.Namespace, p.Image, p.Replicas, p.Port, p.EnvVars,
		p.CPURequest, p.CPULimit, p.MemRequest, p.MemLimit,
		p.HealthPath, p.HealthPort, p.HealthDelay, p.HealthPeriod)
	return &a, err
}

// UpdateAppParams contains parameters for updating an app
type UpdateAppParams struct {
	ID          string
	Image       string
	Replicas    int
	EnvVars     []byte
	CPURequest  string
	CPULimit    string
	MemRequest  string
	MemLimit    string
	HealthPath  *string
	HealthPort  *int
	HealthDelay *int
	HealthPeriod *int
}

func (db *DB) UpdateApp(ctx context.Context, p UpdateAppParams) (*App, error) {
	var a App
	err := db.GetContext(ctx, &a, `
		UPDATE apps SET image = $1, replicas = $2, env_vars = $3, updated_at = NOW(),
			cpu_request = $4, cpu_limit = $5, memory_request = $6, memory_limit = $7,
			health_path = $8, health_port = $9, health_initial_delay = $10, health_period = $11
		WHERE id = $12 RETURNING *
	`, p.Image, p.Replicas, p.EnvVars,
		p.CPURequest, p.CPULimit, p.MemRequest, p.MemLimit,
		p.HealthPath, p.HealthPort, p.HealthDelay, p.HealthPeriod, p.ID)
	return &a, err
}

func (db *DB) UpdateAppStatus(ctx context.Context, id, status string, message *string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE apps SET status = $1, status_message = $2, updated_at = NOW() WHERE id = $3
	`, status, message, id)
	return err
}

// UpdateAppHPAParams contains HPA configuration for an app
type UpdateAppHPAParams struct {
	ID           string
	HPAEnabled   bool
	MinReplicas  *int
	MaxReplicas  *int
	CPUTarget    *int
	MemoryTarget *int
}

func (db *DB) UpdateAppHPA(ctx context.Context, p UpdateAppHPAParams) (*App, error) {
	var a App
	err := db.GetContext(ctx, &a, `
		UPDATE apps SET
			hpa_enabled = $1,
			min_replicas = $2,
			max_replicas = $3,
			cpu_target = $4,
			memory_target = $5,
			updated_at = NOW()
		WHERE id = $6 RETURNING *
	`, p.HPAEnabled, p.MinReplicas, p.MaxReplicas, p.CPUTarget, p.MemoryTarget, p.ID)
	return &a, err
}

func (db *DB) DeleteApp(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM apps WHERE id = $1`, id)
	return err
}

// UpdateAppDomainParams contains domain configuration for an app
type UpdateAppDomainParams struct {
	ID           string
	Domain       *string
	DomainStatus *string
}

func (db *DB) UpdateAppDomain(ctx context.Context, p UpdateAppDomainParams) (*App, error) {
	var a App
	err := db.GetContext(ctx, &a, `
		UPDATE apps SET
			domain = $1,
			domain_status = $2,
			updated_at = NOW()
		WHERE id = $3 RETURNING *
	`, p.Domain, p.DomainStatus, p.ID)
	return &a, err
}

func (db *DB) GetAppByDomain(ctx context.Context, domain string) (*App, error) {
	var a App
	err := db.GetContext(ctx, &a, `SELECT * FROM apps WHERE domain = $1`, domain)
	return &a, err
}

// Secret operations

func (db *DB) ListSecrets(ctx context.Context, appID string) ([]AppSecret, error) {
	var secrets []AppSecret
	err := db.SelectContext(ctx, &secrets, `
		SELECT id, app_id, key, created_at, updated_at
		FROM app_secrets WHERE app_id = $1 ORDER BY key
	`, appID)
	return secrets, err
}

func (db *DB) GetSecret(ctx context.Context, appID, key string) (*AppSecret, error) {
	var s AppSecret
	err := db.GetContext(ctx, &s, `
		SELECT * FROM app_secrets WHERE app_id = $1 AND key = $2
	`, appID, key)
	return &s, err
}

func (db *DB) GetSecretsByAppID(ctx context.Context, appID string) ([]AppSecret, error) {
	var secrets []AppSecret
	err := db.SelectContext(ctx, &secrets, `
		SELECT * FROM app_secrets WHERE app_id = $1
	`, appID)
	return secrets, err
}

func (db *DB) SetSecret(ctx context.Context, appID, key string, valueEncrypted []byte) (*AppSecret, error) {
	var s AppSecret
	err := db.GetContext(ctx, &s, `
		INSERT INTO app_secrets (app_id, key, value_encrypted)
		VALUES ($1, $2, $3)
		ON CONFLICT (app_id, key) DO UPDATE SET
			value_encrypted = EXCLUDED.value_encrypted,
			updated_at = NOW()
		RETURNING id, app_id, key, created_at, updated_at
	`, appID, key, valueEncrypted)
	return &s, err
}

func (db *DB) DeleteSecret(ctx context.Context, appID, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM app_secrets WHERE app_id = $1 AND key = $2`, appID, key)
	return err
}

// Revision operations

// CreateRevisionParams contains parameters for creating a revision snapshot
type CreateRevisionParams struct {
	AppID          string
	RevisionNumber int
	Image          string
	Replicas       int
	Port           *int
	EnvVars        []byte
	CPURequest     *string
	CPULimit       *string
	MemRequest     *string
	MemLimit       *string
	HealthPath     *string
	HealthPort     *int
	HealthDelay    *int
	HealthPeriod   *int
	// HPA fields
	HPAEnabled   bool
	MinReplicas  *int
	MaxReplicas  *int
	CPUTarget    *int
	MemoryTarget *int
	// Domain
	Domain *string
}

func (db *DB) CreateRevision(ctx context.Context, p CreateRevisionParams) (*AppRevision, error) {
	var r AppRevision
	err := db.GetContext(ctx, &r, `
		INSERT INTO app_revisions (app_id, revision_number, image, replicas, port, env_vars,
			cpu_request, cpu_limit, memory_request, memory_limit,
			health_path, health_port, health_initial_delay, health_period,
			hpa_enabled, min_replicas, max_replicas, cpu_target, memory_target, domain)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		RETURNING *
	`, p.AppID, p.RevisionNumber, p.Image, p.Replicas, p.Port, p.EnvVars,
		p.CPURequest, p.CPULimit, p.MemRequest, p.MemLimit,
		p.HealthPath, p.HealthPort, p.HealthDelay, p.HealthPeriod,
		p.HPAEnabled, p.MinReplicas, p.MaxReplicas, p.CPUTarget, p.MemoryTarget, p.Domain)
	return &r, err
}

func (db *DB) ListRevisions(ctx context.Context, appID string, limit int) ([]AppRevision, error) {
	var revisions []AppRevision
	if limit <= 0 {
		limit = 10 // Default limit
	}
	err := db.SelectContext(ctx, &revisions, `
		SELECT * FROM app_revisions
		WHERE app_id = $1
		ORDER BY revision_number DESC
		LIMIT $2
	`, appID, limit)
	return revisions, err
}

func (db *DB) GetRevision(ctx context.Context, appID string, revisionNumber int) (*AppRevision, error) {
	var r AppRevision
	err := db.GetContext(ctx, &r, `
		SELECT * FROM app_revisions WHERE app_id = $1 AND revision_number = $2
	`, appID, revisionNumber)
	return &r, err
}

func (db *DB) GetLatestRevision(ctx context.Context, appID string) (*AppRevision, error) {
	var r AppRevision
	err := db.GetContext(ctx, &r, `
		SELECT * FROM app_revisions
		WHERE app_id = $1
		ORDER BY revision_number DESC
		LIMIT 1
	`, appID)
	return &r, err
}

func (db *DB) UpdateAppRevision(ctx context.Context, appID string, revision int) error {
	_, err := db.ExecContext(ctx, `
		UPDATE apps SET current_revision = $1, updated_at = NOW() WHERE id = $2
	`, revision, appID)
	return err
}

func (db *DB) DeleteOldRevisions(ctx context.Context, appID string, keepCount int) error {
	if keepCount <= 0 {
		keepCount = 10 // Default keep last 10
	}
	_, err := db.ExecContext(ctx, `
		DELETE FROM app_revisions
		WHERE app_id = $1
		AND revision_number NOT IN (
			SELECT revision_number FROM app_revisions
			WHERE app_id = $1
			ORDER BY revision_number DESC
			LIMIT $2
		)
	`, appID, keepCount)
	return err
}
