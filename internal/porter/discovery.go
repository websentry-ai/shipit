package porter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vigneshsubbiah/shipit/internal/db"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// PorterAppLabel is the label that identifies Porter-managed apps
const PorterAppLabel = "porter.run/porter-application"

// PorterLabels contains all Porter-specific label keys
type PorterLabels struct {
	AppID       string // porter.run/app-id
	AppName     string // porter.run/app-name
	ServiceName string // porter.run/service-name
	ServiceType string // porter.run/service-type
	ImageTag    string // porter.run/image-tag
	ProjectID   string // porter.run/project-id
}

// DiscoveredApp represents a Porter app discovered from K8s
type DiscoveredApp struct {
	DeploymentName string
	Namespace      string
	PorterLabels   PorterLabels
	Image          string
	Replicas       int32
	CPURequest     string
	CPULimit       string
	MemoryRequest  string
	MemoryLimit    string
	Port           *int
	CreatedAt      time.Time
}

// DiscoveryService handles Porter app discovery and sync
type DiscoveryService struct {
	database    *db.DB
	clusters    map[string]*clusterSync // clusterID -> sync state
	mu          sync.RWMutex
	stopCh      chan struct{}
	syncInterval time.Duration
}

type clusterSync struct {
	clusterID  string
	kubeconfig []byte
	lastSync   time.Time
}

// NewDiscoveryService creates a new Porter discovery service
func NewDiscoveryService(database *db.DB) *DiscoveryService {
	return &DiscoveryService{
		database:     database,
		clusters:     make(map[string]*clusterSync),
		stopCh:       make(chan struct{}),
		syncInterval: 1 * time.Minute, // Sync every minute
	}
}

// Start begins the background discovery process
func (s *DiscoveryService) Start(ctx context.Context) {
	log.Println("[Porter Discovery] Starting background sync service")

	// Initial sync on startup
	s.syncAllClusters(ctx)

	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Porter Discovery] Context cancelled, stopping")
			return
		case <-s.stopCh:
			log.Println("[Porter Discovery] Stop signal received, stopping")
			return
		case <-ticker.C:
			s.syncAllClusters(ctx)
		}
	}
}

// Stop stops the background discovery process
func (s *DiscoveryService) Stop() {
	close(s.stopCh)
}

// RegisterCluster registers a cluster for Porter discovery
func (s *DiscoveryService) RegisterCluster(clusterID string, kubeconfig []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clusters[clusterID] = &clusterSync{
		clusterID:  clusterID,
		kubeconfig: kubeconfig,
	}
	log.Printf("[Porter Discovery] Registered cluster %s for sync", clusterID)
}

// UnregisterCluster removes a cluster from Porter discovery
func (s *DiscoveryService) UnregisterCluster(clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.clusters, clusterID)
	log.Printf("[Porter Discovery] Unregistered cluster %s", clusterID)
}

// syncAllClusters syncs all registered clusters
func (s *DiscoveryService) syncAllClusters(ctx context.Context) {
	s.mu.RLock()
	clusters := make([]*clusterSync, 0, len(s.clusters))
	for _, c := range s.clusters {
		clusters = append(clusters, c)
	}
	s.mu.RUnlock()

	for _, cluster := range clusters {
		if err := s.SyncCluster(ctx, cluster.clusterID, cluster.kubeconfig); err != nil {
			log.Printf("[Porter Discovery] Error syncing cluster %s: %v", cluster.clusterID, err)
		}
	}
}

// SyncCluster discovers Porter apps in a specific cluster and syncs to database
func (s *DiscoveryService) SyncCluster(ctx context.Context, clusterID string, kubeconfig []byte) error {
	log.Printf("[Porter Discovery] Syncing cluster %s", clusterID)

	// Create K8s client
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Discover Porter apps
	apps, err := s.discoverPorterApps(ctx, clientset)
	if err != nil {
		return fmt.Errorf("failed to discover Porter apps: %w", err)
	}

	log.Printf("[Porter Discovery] Found %d Porter apps in cluster %s", len(apps), clusterID)

	// Sync to database
	for _, app := range apps {
		if err := s.syncAppToDatabase(ctx, clusterID, app); err != nil {
			log.Printf("[Porter Discovery] Error syncing app %s: %v", app.PorterLabels.AppName, err)
		}
	}

	// Update last sync time
	s.mu.Lock()
	if c, ok := s.clusters[clusterID]; ok {
		c.lastSync = time.Now()
	}
	s.mu.Unlock()

	return nil
}

// discoverPorterApps lists all Porter-managed deployments
func (s *DiscoveryService) discoverPorterApps(ctx context.Context, clientset *kubernetes.Clientset) ([]DiscoveredApp, error) {
	// List deployments with Porter label
	labelSelector := fmt.Sprintf("%s=true", PorterAppLabel)
	deployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	var apps []DiscoveredApp
	for _, dep := range deployments.Items {
		app := s.parseDeployment(&dep)
		if app != nil {
			apps = append(apps, *app)
		}
	}

	return apps, nil
}

// parseDeployment extracts Porter app info from a deployment
func (s *DiscoveryService) parseDeployment(dep *appsv1.Deployment) *DiscoveredApp {
	labels := dep.Labels
	if labels == nil {
		return nil
	}

	// Extract Porter labels
	porterLabels := PorterLabels{
		AppID:       labels["porter.run/app-id"],
		AppName:     labels["porter.run/app-name"],
		ServiceName: labels["porter.run/service-name"],
		ServiceType: labels["porter.run/service-type"],
		ImageTag:    labels["porter.run/image-tag"],
		ProjectID:   labels["porter.run/project-id"],
	}

	// Skip if missing critical labels
	if porterLabels.AppID == "" || porterLabels.AppName == "" {
		return nil
	}

	// Get container info
	var image string
	var port *int
	var cpuReq, cpuLim, memReq, memLim string

	if len(dep.Spec.Template.Spec.Containers) > 0 {
		container := dep.Spec.Template.Spec.Containers[0]
		image = container.Image

		if len(container.Ports) > 0 {
			p := int(container.Ports[0].ContainerPort)
			port = &p
		}

		// Extract resource requests/limits
		if container.Resources.Requests != nil {
			if cpu, ok := container.Resources.Requests["cpu"]; ok {
				cpuReq = cpu.String()
			}
			if mem, ok := container.Resources.Requests["memory"]; ok {
				memReq = mem.String()
			}
		}
		if container.Resources.Limits != nil {
			if cpu, ok := container.Resources.Limits["cpu"]; ok {
				cpuLim = cpu.String()
			}
			if mem, ok := container.Resources.Limits["memory"]; ok {
				memLim = mem.String()
			}
		}
	}

	replicas := int32(1)
	if dep.Spec.Replicas != nil {
		replicas = *dep.Spec.Replicas
	}

	return &DiscoveredApp{
		DeploymentName: dep.Name,
		Namespace:      dep.Namespace,
		PorterLabels:   porterLabels,
		Image:          image,
		Replicas:       replicas,
		CPURequest:     cpuReq,
		CPULimit:       cpuLim,
		MemoryRequest:  memReq,
		MemoryLimit:    memLim,
		Port:           port,
		CreatedAt:      dep.CreationTimestamp.Time,
	}
}

// syncAppToDatabase syncs a discovered Porter app to the database
func (s *DiscoveryService) syncAppToDatabase(ctx context.Context, clusterID string, app DiscoveredApp) error {
	// Use full deployment name (matches K8s deployment)
	// App grouping structure:
	// - name: full deployment name (e.g., "staging-gateway-gateway")
	// - app_group: Porter app name (e.g., "staging-gateway")
	// - service_name: service within the app (e.g., "gateway")
	deploymentName := app.DeploymentName
	appGroup := app.PorterLabels.AppName
	serviceName := app.PorterLabels.ServiceName

	if serviceName == "" {
		// Fallback: extract service name from deployment name
		// e.g., "staging-gateway-gateway" -> "gateway"
		serviceName = strings.TrimPrefix(deploymentName, appGroup+"-")
	}

	// Build Porter dashboard URL
	porterURL := fmt.Sprintf("https://dashboard.porter.run/apps/%s", app.PorterLabels.AppID)

	// Check if app already exists by porter_app_id
	existingApp, err := s.database.GetAppByPorterAppID(ctx, clusterID, app.PorterLabels.AppID)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return fmt.Errorf("failed to check existing app by porter_app_id: %w", err)
	}

	if existingApp != nil {
		// App exists with porter_app_id - check if it needs updating
		if existingApp.ManagedBy == "shipit" {
			// Already managed by shipit, don't update
			log.Printf("[Porter Discovery] App %s/%s is managed by shipit, skipping", appGroup, serviceName)
			return nil
		}

		// Update if image or replicas changed
		if existingApp.Image != app.Image || existingApp.Replicas != int(app.Replicas) {
			log.Printf("[Porter Discovery] Updating app %s/%s (image: %s -> %s, replicas: %d -> %d)",
				appGroup, serviceName, existingApp.Image, app.Image, existingApp.Replicas, app.Replicas)

			_, err = s.database.UpdatePorterApp(ctx, db.UpdatePorterAppParams{
				ID:         existingApp.ID,
				Image:      app.Image,
				Replicas:   int(app.Replicas),
				CPURequest: app.CPURequest,
				CPULimit:   app.CPULimit,
				MemRequest: app.MemoryRequest,
				MemLimit:   app.MemoryLimit,
			})
			return err
		}
		return nil
	}

	// App not found by porter_app_id - check if it exists by deployment name (legacy app)
	existingByName, err := s.database.GetAppByClusterNamespaceName(ctx, clusterID, app.Namespace, deploymentName)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return fmt.Errorf("failed to check existing app by name: %w", err)
	}

	if existingByName != nil {
		// Existing app found by name - link it to Porter and set grouping
		log.Printf("[Porter Discovery] Linking existing app %s to Porter (app_group: %s, service: %s)",
			deploymentName, appGroup, serviceName)

		err = s.database.LinkAppToPorter(ctx, existingByName.ID, app.PorterLabels.AppID, &porterURL)
		if err != nil {
			return fmt.Errorf("failed to link app to Porter: %w", err)
		}

		// Also update with current Porter state and grouping
		_, err = s.database.UpdatePorterApp(ctx, db.UpdatePorterAppParams{
			ID:         existingByName.ID,
			Image:      app.Image,
			Replicas:   int(app.Replicas),
			CPURequest: app.CPURequest,
			CPULimit:   app.CPULimit,
			MemRequest: app.MemoryRequest,
			MemLimit:   app.MemoryLimit,
		})
		return err
	}

	// Create new app entry
	log.Printf("[Porter Discovery] Creating new app record: %s (app_group: %s, service: %s)",
		deploymentName, appGroup, serviceName)

	// Create empty env vars JSON
	envVars, _ := json.Marshal(map[string]string{})

	_, err = s.database.CreatePorterApp(ctx, db.CreatePorterAppParams{
		ClusterID:    clusterID,
		Name:         deploymentName,    // Full deployment name
		ServiceName:  serviceName,       // Service within the app
		AppGroup:     appGroup,          // Porter app name
		Namespace:    app.Namespace,
		Image:        app.Image,
		Replicas:     int(app.Replicas),
		Port:         app.Port,
		EnvVars:      envVars,
		CPURequest:   app.CPURequest,
		CPULimit:     app.CPULimit,
		MemRequest:   app.MemoryRequest,
		MemLimit:     app.MemoryLimit,
		ManagedBy:    "porter",
		PorterAppID:  app.PorterLabels.AppID,
		PorterAppURL: &porterURL,
	})

	return err
}

// SwitchToShipit switches an app from Porter management to Shipit management
func (s *DiscoveryService) SwitchToShipit(ctx context.Context, appID string) error {
	log.Printf("[Porter Discovery] Switching app %s to shipit management", appID)
	return s.database.UpdateAppManagedBy(ctx, appID, "shipit")
}

// SwitchToPorter switches an app back to Porter management (observer mode)
func (s *DiscoveryService) SwitchToPorter(ctx context.Context, appID string) error {
	log.Printf("[Porter Discovery] Switching app %s to porter management (observer)", appID)
	return s.database.UpdateAppManagedBy(ctx, appID, "porter")
}

// GetPorterAppCount returns the count of Porter-managed apps for a cluster
func (s *DiscoveryService) GetPorterAppCount(ctx context.Context, clusterID string) (int, error) {
	apps, err := s.database.ListPorterApps(ctx, clusterID)
	if err != nil {
		return 0, err
	}
	return len(apps), nil
}

// parsePorterEnvValue parses resource values from Porter env vars
func parsePorterEnvValue(value string) string {
	// Porter uses env vars like PORTER_RESOURCES_RAM=2496M, PORTER_RESOURCES_CPU=0.1
	// Convert to K8s format
	if strings.HasSuffix(value, "M") {
		// Memory in MB, convert to Mi
		mb := strings.TrimSuffix(value, "M")
		return mb + "Mi"
	}

	// CPU as decimal (e.g., "0.1" -> "100m")
	if f, err := strconv.ParseFloat(value, 64); err == nil && f < 10 {
		return fmt.Sprintf("%dm", int(f*1000))
	}

	return value
}
