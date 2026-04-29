package api

import (
	"github.com/vigneshsubbiah/shipit/internal/db"
	"github.com/vigneshsubbiah/shipit/internal/k8s"
)

// buildDeployRequestFromApp maps the live app row to a DeployRequest.
// Used by deployApp on the forward path, where envVars and secretName were
// resolved from secrets sync.
func buildDeployRequestFromApp(app *db.App, baseDomain, secretName string, envVars map[string]string) k8s.DeployRequest {
	return k8s.DeployRequest{
		Name:                      app.Name,
		Namespace:                 app.Namespace,
		Image:                     app.Image,
		Replicas:                  int32(app.Replicas),
		Port:                      app.Port,
		EnvVars:                   envVars,
		SecretName:                secretName,
		CPURequest:                app.CPURequest,
		CPULimit:                  app.CPULimit,
		MemoryRequest:             app.MemoryRequest,
		MemoryLimit:               app.MemoryLimit,
		HealthPath:                app.HealthPath,
		HealthPort:                app.HealthPort,
		HealthInitialDelay:        app.HealthInitialDelay,
		HealthPeriod:              app.HealthPeriod,
		HPAEnabled:                app.HPAEnabled,
		HPAMinReplicas:            intPtrToInt32Ptr(app.MinReplicas),
		HPAMaxReplicas:            intPtrToInt32Ptr(app.MaxReplicas),
		HPATargetCPU:              intPtrToInt32Ptr(app.CPUTarget),
		HPATargetMemory:           intPtrToInt32Ptr(app.MemoryTarget),
		BaseDomain:                baseDomain,
		DisableZeroDowntime:       !app.ZeroDowntimeEnabled,
		MaxSurgeOverride:          app.MaxSurgeOverride,
		MaxUnavailableOverride:    app.MaxUnavailableOverride,
		MaxRequestDurationSeconds: app.MaxRequestDurationSeconds,
	}
}

// buildDeployRequestFromRevision maps a historical revision snapshot to a
// DeployRequest. Used by the auto-rollback path, which must reproduce the
// prior deploy without round-tripping through the apps row. Namespace,
// envVars, and secretName come from the live app: revisions only snapshot
// fields the user can change, while namespace is immutable and secrets are
// stored outside the revision.
func buildDeployRequestFromRevision(app *db.App, rev *db.AppRevision, baseDomain, secretName string, envVars map[string]string) k8s.DeployRequest {
	req := k8s.DeployRequest{
		Name:       app.Name,
		Namespace:  app.Namespace,
		Image:      rev.Image,
		Replicas:   int32(rev.Replicas),
		Port:       rev.Port,
		EnvVars:    envVars,
		SecretName: secretName,
		BaseDomain: baseDomain,
		HPAEnabled: rev.HPAEnabled,
	}
	if rev.CPURequest != nil {
		req.CPURequest = *rev.CPURequest
	}
	if rev.CPULimit != nil {
		req.CPULimit = *rev.CPULimit
	}
	if rev.MemoryRequest != nil {
		req.MemoryRequest = *rev.MemoryRequest
	}
	if rev.MemoryLimit != nil {
		req.MemoryLimit = *rev.MemoryLimit
	}
	req.HealthPath = rev.HealthPath
	req.HealthPort = rev.HealthPort
	req.HealthInitialDelay = rev.HealthDelay
	req.HealthPeriod = rev.HealthPeriod
	req.HPAMinReplicas = intPtrToInt32Ptr(rev.MinReplicas)
	req.HPAMaxReplicas = intPtrToInt32Ptr(rev.MaxReplicas)
	req.HPATargetCPU = intPtrToInt32Ptr(rev.CPUTarget)
	req.HPATargetMemory = intPtrToInt32Ptr(rev.MemoryTarget)
	// Zero-downtime: revisions written before the migration carry NULLs;
	// fall back to the app's current setting so rollbacks of pre-migration
	// revisions still get safe defaults.
	zdEnabled := app.ZeroDowntimeEnabled
	if rev.ZeroDowntimeEnabled != nil {
		zdEnabled = *rev.ZeroDowntimeEnabled
	}
	req.DisableZeroDowntime = !zdEnabled
	req.MaxSurgeOverride = rev.MaxSurgeOverride
	req.MaxUnavailableOverride = rev.MaxUnavailableOverride
	if rev.MaxRequestDurationSeconds != nil {
		req.MaxRequestDurationSeconds = *rev.MaxRequestDurationSeconds
	} else {
		req.MaxRequestDurationSeconds = app.MaxRequestDurationSeconds
	}
	return req
}
