package k8s

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

type Client struct {
	clientset kubernetes.Interface
	config    *rest.Config
}

type ClusterInfo struct {
	Endpoint string `json:"endpoint"`
	Version  string `json:"version"`
}

type DeployRequest struct {
	Name       string
	Namespace  string
	Image      string
	Replicas   int32
	Port       *int
	EnvVars    map[string]string
	SecretName string // Optional: K8s Secret name to inject as env vars

	// Resource limits
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string

	// Health check configuration
	HealthPath         *string
	HealthPort         *int
	HealthInitialDelay *int // seconds
	HealthPeriod       *int // seconds

	// HPA (auto-scaling) configuration. DeployApp reconciles the HPA every
	// call: creates/updates when HPAEnabled, deletes when HPAEnabled=false.
	HPAEnabled      bool
	HPAMinReplicas  *int32
	HPAMaxReplicas  *int32
	HPATargetCPU    *int32
	HPATargetMemory *int32

	// Default ingress hostname (auto-generated URL)
	BaseDomain string // e.g., "apps.shipit.unboundsec.dev" - if set, creates ingress at <name>.apps.shipit.unboundsec.dev
}

type DeploymentStatus struct {
	Name            string      `json:"name"`
	Replicas        int32       `json:"replicas"`
	ReadyReplicas   int32       `json:"ready_replicas"`
	DesiredReplicas int32       `json:"desired_replicas"`
	Status          string      `json:"status"`
	Pods            []PodStatus `json:"pods"`
}

type PodStatus struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`
	Age      string `json:"age"`
	// Resource metrics (from metrics-server)
	CPUUsage    string `json:"cpu_usage,omitempty"`    // e.g., "50m" (millicores)
	MemoryUsage string `json:"memory_usage,omitempty"` // e.g., "128Mi"
	CPUPercent  *int   `json:"cpu_percent,omitempty"`  // percentage of limit
	MemPercent  *int   `json:"mem_percent,omitempty"`  // percentage of limit
}

// PodMetrics represents resource usage for a pod
type PodMetrics struct {
	Name        string `json:"name"`
	CPUUsage    string `json:"cpu_usage"`    // e.g., "50m"
	MemoryUsage string `json:"memory_usage"` // e.g., "128Mi"
}

// HPAConfig represents Horizontal Pod Autoscaler configuration
type HPAConfig struct {
	Enabled           bool  `json:"enabled"`
	MinReplicas       int32 `json:"min_replicas"`
	MaxReplicas       int32 `json:"max_replicas"`
	TargetCPUPercent  *int32 `json:"target_cpu_percent,omitempty"`
	TargetMemPercent  *int32 `json:"target_memory_percent,omitempty"`
}

// HPAStatus represents the current state of an HPA
type HPAStatus struct {
	Enabled         bool   `json:"enabled"`
	MinReplicas     int32  `json:"min_replicas"`
	MaxReplicas     int32  `json:"max_replicas"`
	CurrentReplicas int32  `json:"current_replicas"`
	DesiredReplicas int32  `json:"desired_replicas"`
	CurrentCPU      *int32 `json:"current_cpu_percent,omitempty"`
	CurrentMemory   *int32 `json:"current_memory_percent,omitempty"`
	TargetCPU       *int32 `json:"target_cpu_percent,omitempty"`
	TargetMemory    *int32 `json:"target_memory_percent,omitempty"`
}

func NewClient(kubeconfig []byte) (*Client, error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Client{clientset: clientset, config: config}, nil
}

func (c *Client) GetClusterInfo() (*ClusterInfo, error) {
	version, err := c.clientset.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}

	// Get first node to determine endpoint (simplified)
	nodes, err := c.clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{Limit: 1})
	if err != nil {
		return nil, err
	}

	endpoint := "unknown"
	if len(nodes.Items) > 0 {
		for _, addr := range nodes.Items[0].Status.Addresses {
			if addr.Type == corev1.NodeExternalIP {
				endpoint = addr.Address
				break
			}
		}
	}

	return &ClusterInfo{
		Endpoint: endpoint,
		Version:  version.GitVersion,
	}, nil
}

func (c *Client) DeployApp(req DeployRequest) error {
	ctx := context.Background()

	// Ensure namespace exists
	if err := c.ensureNamespace(ctx, req.Namespace); err != nil {
		return fmt.Errorf("failed to ensure namespace: %w", err)
	}

	// Build env vars
	var envVars []corev1.EnvVar
	for k, v := range req.EnvVars {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
	}

	// Build container
	container := corev1.Container{
		Name:            req.Name,
		Image:           req.Image,
		Env:             envVars,
		ImagePullPolicy: imagePullPolicyFor(req.Image),
		// preStop delay covers kube-proxy/ingress endpoint propagation so in-flight
		// requests are not dropped when the pod is removed from Service rotation
		// during a rolling update. The /bin/sh fallback is a best-effort no-op on
		// distroless images (kubelet treats a failed preStop exec as complete).
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", "sleep 5"},
				},
			},
		},
	}

	// Inject secrets from K8s Secret if specified
	if req.SecretName != "" {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: req.SecretName,
				},
			},
		}}
	}

	if req.Port != nil {
		container.Ports = []corev1.ContainerPort{{ContainerPort: int32(*req.Port)}}
	}

	// Set resource requests and limits
	if req.CPURequest != "" || req.CPULimit != "" || req.MemoryRequest != "" || req.MemoryLimit != "" {
		container.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{},
			Limits:   corev1.ResourceList{},
		}
		if req.CPURequest != "" {
			container.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(req.CPURequest)
		}
		if req.CPULimit != "" {
			container.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(req.CPULimit)
		}
		if req.MemoryRequest != "" {
			container.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(req.MemoryRequest)
		}
		if req.MemoryLimit != "" {
			container.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(req.MemoryLimit)
		}
	}

	// Configure health probes. Preference order:
	//   1. Explicit HealthPath  → HTTP GET probe on that path/port.
	//   2. Port set, no HealthPath → TCP probe on the port (liveness + readiness).
	//   3. Neither set → no probes (silent pods can't be safely rolled; warn upstream).
	//
	// Readiness and liveness are split so slow cold-starts don't cause restart loops:
	// readiness polls often to gate ingress traffic; liveness polls slower to allow warmup.
	if req.HealthPath != nil && *req.HealthPath != "" {
		healthPort := req.Port
		if req.HealthPort != nil {
			healthPort = req.HealthPort
		}

		initialDelay := int32(10)
		if req.HealthInitialDelay != nil {
			initialDelay = int32(*req.HealthInitialDelay)
		}

		period := int32(10)
		if req.HealthPeriod != nil {
			period = int32(*req.HealthPeriod)
		}

		if healthPort != nil {
			handler := corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: *req.HealthPath,
					Port: intstr.FromInt(*healthPort),
				},
			}
			container.ReadinessProbe = &corev1.Probe{
				ProbeHandler:        handler,
				InitialDelaySeconds: initialDelay,
				PeriodSeconds:       5,
				TimeoutSeconds:      3,
				FailureThreshold:    3,
				SuccessThreshold:    1,
			}
			container.LivenessProbe = &corev1.Probe{
				ProbeHandler:        handler,
				InitialDelaySeconds: initialDelay + 20,
				PeriodSeconds:       period,
				TimeoutSeconds:      3,
				FailureThreshold:    5,
			}
		}
	} else if req.Port != nil {
		handler := corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(*req.Port)},
		}
		container.ReadinessProbe = &corev1.Probe{
			ProbeHandler:        handler,
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
			SuccessThreshold:    1,
		}
		container.LivenessProbe = &corev1.Probe{
			ProbeHandler:        handler,
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			FailureThreshold:    5,
		}
	}

	maxSurge, maxUnavailable := rollingUpdateBudget(req.Replicas)
	terminationGrace := int64(30)
	progressDeadline := int32(600)
	historyLimit := int32(10)

	// Create or update deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
			Labels:    map[string]string{"app": req.Name, "managed-by": "shipit"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &req.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": req.Name},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       &maxSurge,
					MaxUnavailable: &maxUnavailable,
				},
			},
			ProgressDeadlineSeconds: &progressDeadline,
			RevisionHistoryLimit:    &historyLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": req.Name},
				},
				Spec: corev1.PodSpec{
					Containers:                    []corev1.Container{container},
					TerminationGracePeriodSeconds: &terminationGrace,
					TopologySpreadConstraints:     topologySpreadFor(req.Name),
				},
			},
		},
	}

	deploymentsClient := c.clientset.AppsV1().Deployments(req.Namespace)

	// Try to get existing deployment
	existing, err := deploymentsClient.Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		// Create new deployment
		_, err = deploymentsClient.Create(ctx, deployment, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create deployment: %w", err)
		}
	} else {
		// When HPA owns the replica count, preserve whatever the HPA last set.
		// Writing req.Replicas (the static DB value) on every deploy would fight
		// the HPA controller: a 4→12 scale-up would bounce back to 4 on the
		// next redeploy. See https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/
		if req.HPAEnabled && existing.Spec.Replicas != nil {
			deployment.Spec.Replicas = existing.Spec.Replicas
		}
		deployment.ResourceVersion = existing.ResourceVersion
		_, err = deploymentsClient.Update(ctx, deployment, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update deployment: %w", err)
		}
	}

	// Create service if port is specified
	if req.Port != nil {
		if err := c.ensureService(req); err != nil {
			return err
		}
	}

	// Create ingress for default URL if base domain is specified
	if req.BaseDomain != "" && req.Port != nil {
		if err := c.ensureIngress(req); err != nil {
			return fmt.Errorf("failed to create ingress: %w", err)
		}
	}

	// PDB: without this a node drain can evict every replica at once.
	// Only meaningful for replicas >= 2; for single-replica apps a PDB of
	// minAvailable=1 blocks all voluntary disruptions which is worse than
	// accepting that a single-replica app is inherently non-HA.
	if err := c.ensurePodDisruptionBudget(ctx, req); err != nil {
		return fmt.Errorf("failed to reconcile poddisruptionbudget: %w", err)
	}

	// HPA: reconciled from the same app record every deploy so the cluster
	// never drifts from the UI/DB. CreateOrUpdateHPA deletes the HPA when
	// Enabled=false, so this one call handles enable/update/disable.
	if err := c.reconcileHPA(req); err != nil {
		return fmt.Errorf("failed to reconcile hpa: %w", err)
	}

	return nil
}

// minHPAReplicas is the floor enforced when HPA is enabled. A single-replica
// HPA creates a single point of failure, blocks the PodDisruptionBudget from
// allowing any voluntary disruption, and defeats the zero-downtime guarantees
// the rest of DeployApp works to provide.
const minHPAReplicas = int32(2)

// reconcileHPA translates the DeployRequest HPA fields into an HPAConfig and
// applies it. Called unconditionally by DeployApp; CreateOrUpdateHPA handles
// the disabled-case delete internally so there is no drift between deploys.
//
// Clamping to minHPAReplicas is logged at WARN so ops can correlate any
// user-visible UI/DB drift (user sees min=1, cluster runs min=2) with a
// specific deploy. The permanent fix is API-level validation in
// SetAutoscaling + FE change; until then, the log trail lets us unblock
// incident triage.
func (c *Client) reconcileHPA(req DeployRequest) error {
	cfg := HPAConfig{Enabled: req.HPAEnabled}
	if req.HPAEnabled {
		requested := int32(0)
		if req.HPAMinReplicas != nil {
			requested = *req.HPAMinReplicas
		}
		minR := requested
		if minR < minHPAReplicas {
			if requested > 0 {
				log.Printf("hpa: clamped min_replicas from %d to %d for app=%s ns=%s (single-replica HPA is unsafe with PDB)", requested, minHPAReplicas, req.Name, req.Namespace)
			}
			minR = minHPAReplicas
		}
		maxR := int32(10)
		if req.HPAMaxReplicas != nil && *req.HPAMaxReplicas > 0 {
			maxR = *req.HPAMaxReplicas
		}
		if maxR < minR {
			log.Printf("hpa: coerced max_replicas from %d to %d for app=%s ns=%s (max below min is invalid)", maxR, minR, req.Name, req.Namespace)
			maxR = minR
		}
		cfg.MinReplicas = minR
		cfg.MaxReplicas = maxR
		cfg.TargetCPUPercent = req.HPATargetCPU
		cfg.TargetMemPercent = req.HPATargetMemory
		log.Printf("hpa: reconciling app=%s ns=%s enabled=true min=%d max=%d", req.Name, req.Namespace, minR, maxR)
	} else {
		log.Printf("hpa: reconciling app=%s ns=%s enabled=false (delete-if-exists)", req.Name, req.Namespace)
	}
	return c.CreateOrUpdateHPA(req.Name, req.Namespace, cfg)
}

// rollingUpdateBudget returns sane maxSurge/maxUnavailable per replica count.
// For small fleets (<=3 replicas) 25% rounds poorly, so we pin surge=1,
// unavailable=0 to never lose capacity during a rollout.
func rollingUpdateBudget(replicas int32) (intstr.IntOrString, intstr.IntOrString) {
	if replicas <= 3 {
		return intstr.FromInt(1), intstr.FromInt(0)
	}
	return intstr.FromString("25%"), intstr.FromString("25%")
}

// imagePullPolicyFor returns IfNotPresent for immutable tags (the expected
// case once branch-tracking ships), Always for mutable tags we can detect.
// Unknown/no tag falls back to IfNotPresent — callers should pin to :<sha>.
func imagePullPolicyFor(image string) corev1.PullPolicy {
	// Only consider the tag portion after the last colon that is NOT inside
	// the registry port (registry:5000/repo:tag → tag after final colon).
	at := strings.LastIndex(image, "@")
	if at >= 0 {
		return corev1.PullIfNotPresent // digest-pinned, always cacheable
	}
	slash := strings.LastIndex(image, "/")
	tag := ""
	if i := strings.LastIndex(image, ":"); i > slash {
		tag = image[i+1:]
	}
	if tag == "latest" || tag == "" {
		return corev1.PullAlways
	}
	return corev1.PullIfNotPresent
}

// topologySpreadFor returns constraints that spread pods across zones and
// nodes. whenUnsatisfiable=ScheduleAnyway so a constrained cluster still
// schedules (vs DoNotSchedule which can wedge small clusters).
func topologySpreadFor(appName string) []corev1.TopologySpreadConstraint {
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": appName},
	}
	return []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           1,
			TopologyKey:       "topology.kubernetes.io/zone",
			WhenUnsatisfiable: corev1.ScheduleAnyway,
			LabelSelector:     labelSelector,
		},
		{
			MaxSkew:           1,
			TopologyKey:       "kubernetes.io/hostname",
			WhenUnsatisfiable: corev1.ScheduleAnyway,
			LabelSelector:     labelSelector,
		},
	}
}

// ensurePodDisruptionBudget reconciles a PDB that keeps at least
// (effective replicas - 1) pods available during voluntary disruptions
// (node drains, cluster upgrades, Karpenter consolidation).
//
// "Effective replicas" = max(static Replicas, HPA MinReplicas when HPA
// enabled). This matters because an HPA-managed deployment typically runs
// well above its static Replicas value; a PDB computed from Replicas=2 on
// an HPA-scaled fleet of 10 would only guarantee 1 pod stays up during a
// drain, which wastes the redundancy.
//
// For effective replicas <= 1 we delete any existing PDB rather than
// create a blocking one.
func (c *Client) ensurePodDisruptionBudget(ctx context.Context, req DeployRequest) error {
	pdbs := c.clientset.PolicyV1().PodDisruptionBudgets(req.Namespace)

	effective := req.Replicas
	if req.HPAEnabled {
		hpaMin := minHPAReplicas
		if req.HPAMinReplicas != nil && *req.HPAMinReplicas > hpaMin {
			hpaMin = *req.HPAMinReplicas
		}
		if hpaMin > effective {
			effective = hpaMin
		}
	}

	if effective <= 1 {
		err := pdbs.Delete(ctx, req.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	}

	minAvailable := intstr.FromInt(int(effective - 1))
	desired := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
			Labels:    map[string]string{"app": req.Name, "managed-by": "shipit"},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": req.Name},
			},
		},
	}

	existing, err := pdbs.Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = pdbs.Create(ctx, desired, metav1.CreateOptions{})
			return err
		}
		return err
	}
	desired.ResourceVersion = existing.ResourceVersion
	_, err = pdbs.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func (c *Client) ensureNamespace(ctx context.Context, namespace string) error {
	// Skip for default namespace
	if namespace == "default" || namespace == "kube-system" || namespace == "kube-public" {
		return nil
	}

	nsClient := c.clientset.CoreV1().Namespaces()

	// Check if namespace exists
	_, err := nsClient.Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil // Already exists
	}

	// Create namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: map[string]string{"managed-by": "shipit"},
		},
	}

	_, err = nsClient.Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		// Ignore "already exists" errors (race condition)
		if !isAlreadyExists(err) {
			return err
		}
	}

	return nil
}

func isAlreadyExists(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func (c *Client) ensureService(req DeployRequest) error {
	ctx := context.Background()
	servicesClient := c.clientset.CoreV1().Services(req.Namespace)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
			Labels:    map[string]string{"app": req.Name, "managed-by": "shipit"},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": req.Name},
			Ports: []corev1.ServicePort{{
				Port:       int32(*req.Port),
				TargetPort: intstr.FromInt(*req.Port),
			}},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	existing, err := servicesClient.Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		_, err = servicesClient.Create(ctx, service, metav1.CreateOptions{})
		return err
	}

	service.ResourceVersion = existing.ResourceVersion
	service.Spec.ClusterIP = existing.Spec.ClusterIP // Preserve cluster IP
	_, err = servicesClient.Update(ctx, service, metav1.UpdateOptions{})
	return err
}

// ensureIngress creates or updates an Ingress for the app with automatic TLS
func (c *Client) ensureIngress(req DeployRequest) error {
	if req.BaseDomain == "" || req.Port == nil {
		return nil // No base domain or port, skip ingress
	}

	ctx := context.Background()
	ingressClient := c.clientset.NetworkingV1().Ingresses(req.Namespace)

	// Construct hostname: <app-name>.<base-domain>
	hostname := req.Name + "." + req.BaseDomain
	pathType := networkingv1.PathTypePrefix

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
			Labels:    map[string]string{"app": req.Name, "managed-by": "shipit"},
			Annotations: map[string]string{
				"cert-manager.io/cluster-issuer":           "letsencrypt-prod",
				"nginx.ingress.kubernetes.io/ssl-redirect": "true",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: stringPtr("nginx"),
			TLS: []networkingv1.IngressTLS{{
				Hosts:      []string{hostname},
				SecretName: req.Name + "-tls",
			}},
			Rules: []networkingv1.IngressRule{{
				Host: hostname,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: req.Name,
									Port: networkingv1.ServiceBackendPort{
										Number: int32(*req.Port),
									},
								},
							},
						}},
					},
				},
			}},
		},
	}

	existing, err := ingressClient.Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		_, err = ingressClient.Create(ctx, ingress, metav1.CreateOptions{})
		return err
	}

	ingress.ResourceVersion = existing.ResourceVersion
	_, err = ingressClient.Update(ctx, ingress, metav1.UpdateOptions{})
	return err
}

func stringPtr(s string) *string {
	return &s
}

func (c *Client) DeleteApp(name, namespace string) error {
	ctx := context.Background()

	// Delete deployment
	c.clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})

	// Delete service
	c.clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})

	// Delete ingress (if exists)
	c.clientset.NetworkingV1().Ingresses(namespace).Delete(ctx, name, metav1.DeleteOptions{})

	// Delete secret (if exists)
	c.clientset.CoreV1().Secrets(namespace).Delete(ctx, name+"-secrets", metav1.DeleteOptions{})

	return nil
}

// CreateOrUpdateSecret creates or updates a K8s Secret with the given key-value pairs
func (c *Client) CreateOrUpdateSecret(name, namespace string, data map[string]string) error {
	ctx := context.Background()

	// Ensure namespace exists
	if err := c.ensureNamespace(ctx, namespace); err != nil {
		return fmt.Errorf("failed to ensure namespace: %w", err)
	}

	secretsClient := c.clientset.CoreV1().Secrets(namespace)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"managed-by": "shipit"},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: data,
	}

	existing, err := secretsClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		// Create new secret
		_, err = secretsClient.Create(ctx, secret, metav1.CreateOptions{})
		return err
	}

	// Update existing secret
	secret.ResourceVersion = existing.ResourceVersion
	_, err = secretsClient.Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

// DeleteSecret deletes a K8s Secret
func (c *Client) DeleteSecret(name, namespace string) error {
	return c.clientset.CoreV1().Secrets(namespace).Delete(
		context.Background(), name, metav1.DeleteOptions{})
}

func (c *Client) GetDeploymentStatus(name, namespace string) (*DeploymentStatus, error) {
	ctx := context.Background()

	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	status := "unknown"
	if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
		status = "running"
	} else if deployment.Status.ReadyReplicas > 0 {
		status = "partial"
	} else {
		status = "pending"
	}

	// Get pods for this deployment
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", name),
	})

	var podStatuses []PodStatus
	if err == nil && pods != nil {
		for _, pod := range pods.Items {
			// Calculate age
			age := time.Since(pod.CreationTimestamp.Time)
			ageStr := formatDuration(age)

			// Check if pod is ready
			ready := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}

			// Get restart count from container statuses
			var restarts int32
			for _, cs := range pod.Status.ContainerStatuses {
				restarts += cs.RestartCount
			}

			podStatuses = append(podStatuses, PodStatus{
				Name:     pod.Name,
				Phase:    string(pod.Status.Phase),
				Ready:    ready,
				Restarts: restarts,
				Age:      ageStr,
			})
		}
	}

	return &DeploymentStatus{
		Name:            name,
		Replicas:        *deployment.Spec.Replicas,
		ReadyReplicas:   deployment.Status.ReadyReplicas,
		DesiredReplicas: *deployment.Spec.Replicas,
		Status:          status,
		Pods:            podStatuses,
	}, nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (c *Client) GetLogs(appName, namespace string, follow bool, tail string) (io.ReadCloser, error) {
	// Get pods for this app
	pods, err := c.clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appName),
	})
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found for app %s", appName)
	}

	// Get logs from first pod (simplification for V1)
	podName := pods.Items[0].Name

	opts := &corev1.PodLogOptions{
		Follow: follow,
	}

	if tail != "" {
		if lines, err := strconv.ParseInt(tail, 10, 64); err == nil {
			opts.TailLines = &lines
		}
	}

	req := c.clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	return req.Stream(context.Background())
}

// CreateOrUpdateHPA creates or updates a Horizontal Pod Autoscaler for a deployment
func (c *Client) CreateOrUpdateHPA(name, namespace string, config HPAConfig) error {
	ctx := context.Background()
	hpaClient := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace)

	// If HPA is disabled, delete it if exists
	if !config.Enabled {
		err := hpaClient.Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete HPA: %w", err)
		}
		return nil
	}

	// Build metrics list
	var metrics []autoscalingv2.MetricSpec

	if config.TargetCPUPercent != nil && *config.TargetCPUPercent > 0 {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: config.TargetCPUPercent,
				},
			},
		})
	}

	if config.TargetMemPercent != nil && *config.TargetMemPercent > 0 {
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceMemory,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: config.TargetMemPercent,
				},
			},
		})
	}

	// Default to CPU 80% if no metrics specified
	if len(metrics) == 0 {
		defaultCPU := int32(80)
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &defaultCPU,
				},
			},
		})
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": name, "managed-by": "shipit"},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       name,
			},
			MinReplicas: &config.MinReplicas,
			MaxReplicas: config.MaxReplicas,
			Metrics:     metrics,
		},
	}

	// Try to get existing HPA
	existing, err := hpaClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new HPA
			_, err = hpaClient.Create(ctx, hpa, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create HPA: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get HPA: %w", err)
	}

	// Update existing HPA
	hpa.ResourceVersion = existing.ResourceVersion
	_, err = hpaClient.Update(ctx, hpa, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update HPA: %w", err)
	}

	return nil
}

// GetHPA returns the current HPA status for a deployment
func (c *Client) GetHPA(name, namespace string) (*HPAStatus, error) {
	ctx := context.Background()
	hpaClient := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace)

	hpa, err := hpaClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// No HPA exists - return disabled status
			return &HPAStatus{Enabled: false}, nil
		}
		return nil, fmt.Errorf("failed to get HPA: %w", err)
	}

	status := &HPAStatus{
		Enabled:         true,
		MinReplicas:     *hpa.Spec.MinReplicas,
		MaxReplicas:     hpa.Spec.MaxReplicas,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		DesiredReplicas: hpa.Status.DesiredReplicas,
	}

	// Extract target metrics from spec
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil {
			if metric.Resource.Name == corev1.ResourceCPU {
				status.TargetCPU = metric.Resource.Target.AverageUtilization
			} else if metric.Resource.Name == corev1.ResourceMemory {
				status.TargetMemory = metric.Resource.Target.AverageUtilization
			}
		}
	}

	// Extract current metrics from status
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType && metric.Resource != nil {
			if metric.Resource.Name == corev1.ResourceCPU && metric.Resource.Current.AverageUtilization != nil {
				status.CurrentCPU = metric.Resource.Current.AverageUtilization
			} else if metric.Resource.Name == corev1.ResourceMemory && metric.Resource.Current.AverageUtilization != nil {
				status.CurrentMemory = metric.Resource.Current.AverageUtilization
			}
		}
	}

	return status, nil
}

// DeleteHPA removes the HPA for a deployment
func (c *Client) DeleteHPA(name, namespace string) error {
	ctx := context.Background()
	err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete HPA: %w", err)
	}
	return nil
}

// IngressStatus represents the status of an Ingress resource
type IngressStatus struct {
	Domain      string   `json:"domain"`
	TLSEnabled  bool     `json:"tls_enabled"`
	Ready       bool     `json:"ready"`
	LoadBalancer string  `json:"load_balancer,omitempty"`
	Hosts       []string `json:"hosts,omitempty"`
}

// CreateOrUpdateIngress creates or updates an Ingress resource for an app with TLS
func (c *Client) CreateOrUpdateIngress(name, namespace, domain string, servicePort int) error {
	ctx := context.Background()

	pathType := networkingv1.PathTypePrefix
	ingressClassName := "nginx"

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"cert-manager.io/cluster-issuer":           "letsencrypt-prod",
				"nginx.ingress.kubernetes.io/ssl-redirect": "true",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{domain},
					SecretName: fmt.Sprintf("%s-tls", name),
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: name,
											Port: networkingv1.ServiceBackendPort{
												Number: int32(servicePort),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Try to get existing Ingress
	existing, err := c.clientset.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new Ingress
			_, err = c.clientset.NetworkingV1().Ingresses(namespace).Create(ctx, ingress, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create Ingress: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get Ingress: %w", err)
	}

	// Update existing Ingress
	ingress.ResourceVersion = existing.ResourceVersion
	_, err = c.clientset.NetworkingV1().Ingresses(namespace).Update(ctx, ingress, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update Ingress: %w", err)
	}

	return nil
}

// GetIngress retrieves the Ingress status for an app
func (c *Client) GetIngress(name, namespace string) (*IngressStatus, error) {
	ctx := context.Background()

	ingress, err := c.clientset.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get Ingress: %w", err)
	}

	status := &IngressStatus{
		TLSEnabled: len(ingress.Spec.TLS) > 0,
		Hosts:      make([]string, 0),
	}

	// Get domain from rules
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			status.Domain = rule.Host
			status.Hosts = append(status.Hosts, rule.Host)
		}
	}

	// Check if LoadBalancer is assigned
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		lb := ingress.Status.LoadBalancer.Ingress[0]
		if lb.Hostname != "" {
			status.LoadBalancer = lb.Hostname
		} else if lb.IP != "" {
			status.LoadBalancer = lb.IP
		}
		status.Ready = true
	}

	return status, nil
}

// DeleteIngress removes the Ingress resource for an app
func (c *Client) DeleteIngress(name, namespace string) error {
	ctx := context.Background()
	err := c.clientset.NetworkingV1().Ingresses(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete Ingress: %w", err)
	}
	return nil
}

// GetPodMetrics fetches CPU and memory usage for pods from metrics-server
func (c *Client) GetPodMetrics(namespace string, labelSelector string) (map[string]PodMetrics, error) {
	ctx := context.Background()

	// Use the REST client to query metrics.k8s.io API
	// Type-assert to *kubernetes.Clientset for RESTClient() which is not on the Interface.
	cs, ok := c.clientset.(*kubernetes.Clientset)
	if !ok {
		return nil, fmt.Errorf("metrics API requires a real Kubernetes clientset")
	}
	result := cs.RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1").
		Resource("pods").
		Namespace(namespace).
		Param("labelSelector", labelSelector).
		Do(ctx)

	if err := result.Error(); err != nil {
		return nil, fmt.Errorf("failed to get pod metrics: %w", err)
	}

	raw, err := result.Raw()
	if err != nil {
		return nil, fmt.Errorf("failed to read metrics response: %w", err)
	}

	// Parse the metrics response
	var metricsResponse struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Containers []struct {
				Name  string `json:"name"`
				Usage struct {
					CPU    string `json:"cpu"`
					Memory string `json:"memory"`
				} `json:"usage"`
			} `json:"containers"`
		} `json:"items"`
	}

	if err := json.Unmarshal(raw, &metricsResponse); err != nil {
		return nil, fmt.Errorf("failed to parse metrics response: %w", err)
	}

	metrics := make(map[string]PodMetrics)
	for _, item := range metricsResponse.Items {
		// Aggregate container metrics for the pod
		var totalCPU, totalMem int64
		for _, container := range item.Containers {
			// Parse CPU (e.g., "50m" or "100000000n")
			cpuQty, err := resource.ParseQuantity(container.Usage.CPU)
			if err == nil {
				totalCPU += cpuQty.MilliValue()
			}
			// Parse memory (e.g., "128Mi")
			memQty, err := resource.ParseQuantity(container.Usage.Memory)
			if err == nil {
				totalMem += memQty.Value()
			}
		}

		metrics[item.Metadata.Name] = PodMetrics{
			Name:        item.Metadata.Name,
			CPUUsage:    fmt.Sprintf("%dm", totalCPU),
			MemoryUsage: formatBytes(totalMem),
		}
	}

	return metrics, nil
}

// formatBytes converts bytes to human readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ci", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// GetEnhancedDeploymentStatus returns deployment status with pod metrics
func (c *Client) GetEnhancedDeploymentStatus(name, namespace string) (*DeploymentStatus, error) {
	ctx := context.Background()

	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	status := "unknown"
	if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
		status = "running"
	} else if deployment.Status.ReadyReplicas > 0 {
		status = "partial"
	} else {
		status = "pending"
	}

	// Get pods for this deployment
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", name),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Try to get pod metrics (may fail if metrics-server not available)
	podMetrics, _ := c.GetPodMetrics(namespace, fmt.Sprintf("app=%s", name))

	var podStatuses []PodStatus
	for _, pod := range pods.Items {
		// Calculate age
		age := time.Since(pod.CreationTimestamp.Time)
		ageStr := formatDuration(age)

		// Check if pod is ready
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}

		// Get restart count and resource limits from container statuses
		var restarts int32
		var cpuLimit, memLimit int64
		for _, cs := range pod.Status.ContainerStatuses {
			restarts += cs.RestartCount
		}
		// Get limits from spec
		for _, container := range pod.Spec.Containers {
			if container.Resources.Limits != nil {
				if cpu, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
					cpuLimit += cpu.MilliValue()
				}
				if mem, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
					memLimit += mem.Value()
				}
			}
		}

		podStatus := PodStatus{
			Name:     pod.Name,
			Phase:    string(pod.Status.Phase),
			Ready:    ready,
			Restarts: restarts,
			Age:      ageStr,
		}

		// Add metrics if available
		if metrics, ok := podMetrics[pod.Name]; ok {
			podStatus.CPUUsage = metrics.CPUUsage
			podStatus.MemoryUsage = metrics.MemoryUsage

			// Calculate percentages if limits are set
			if cpuLimit > 0 {
				// Parse the CPU usage to get millicores
				cpuUsageStr := metrics.CPUUsage
				if len(cpuUsageStr) > 1 && cpuUsageStr[len(cpuUsageStr)-1] == 'm' {
					if cpuUsage, err := strconv.ParseInt(cpuUsageStr[:len(cpuUsageStr)-1], 10, 64); err == nil {
						pct := int((cpuUsage * 100) / cpuLimit)
						podStatus.CPUPercent = &pct
					}
				}
			}
			if memLimit > 0 {
				// Parse the memory usage
				memQty, err := resource.ParseQuantity(metrics.MemoryUsage)
				if err == nil {
					pct := int((memQty.Value() * 100) / memLimit)
					podStatus.MemPercent = &pct
				}
			}
		}

		podStatuses = append(podStatuses, podStatus)
	}

	return &DeploymentStatus{
		Name:            name,
		Replicas:        *deployment.Spec.Replicas,
		ReadyReplicas:   deployment.Status.ReadyReplicas,
		DesiredReplicas: *deployment.Spec.Replicas,
		Status:          status,
		Pods:            podStatuses,
	}, nil
}

// PreDeployJobRequest contains parameters for running a pre-deploy job
type PreDeployJobRequest struct {
	AppName    string
	Namespace  string
	Image      string
	Command    string
	EnvVars    map[string]string
	SecretName string // Optional: K8s Secret name to inject as env vars
	Timeout    time.Duration
}

// PreDeployJobResult contains the result of a pre-deploy job
type PreDeployJobResult struct {
	Success bool
	Logs    string
	Error   string
}

// RunPreDeployJob creates and runs a Kubernetes Job for pre-deploy commands
// It waits for completion and returns the result with logs
func (c *Client) RunPreDeployJob(ctx context.Context, req PreDeployJobRequest) (*PreDeployJobResult, error) {
	if req.Timeout == 0 {
		req.Timeout = 5 * time.Minute
	}

	jobName := fmt.Sprintf("%s-predeploy-%d", req.AppName, time.Now().Unix())

	// Build env vars
	var envVars []corev1.EnvVar
	for k, v := range req.EnvVars {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
	}

	// Build container
	container := corev1.Container{
		Name:    "predeploy",
		Image:   req.Image,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{req.Command},
		Env:     envVars,
	}

	// Inject secrets if specified
	if req.SecretName != "" {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: req.SecretName,
				},
			},
		}}
	}

	// Job configuration
	backoffLimit := int32(0)  // No retries
	ttlSeconds := int32(300)  // Auto-delete after 5 minutes

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: req.Namespace,
			Labels: map[string]string{
				"app":        req.AppName,
				"managed-by": "shipit",
				"job-type":   "predeploy",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":      req.AppName,
						"job-name": jobName,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{container},
				},
			},
		},
	}

	// Create the job
	jobsClient := c.clientset.BatchV1().Jobs(req.Namespace)
	_, err := jobsClient.Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pre-deploy job: %w", err)
	}

	// Wait for job completion with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	result := &PreDeployJobResult{}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			// Cleanup job on timeout
			_ = jobsClient.Delete(ctx, jobName, metav1.DeleteOptions{})
			result.Success = false
			result.Error = "pre-deploy job timed out"
			result.Logs = c.getJobLogs(ctx, req.Namespace, jobName)
			return result, nil

		case <-ticker.C:
			// Check job status
			currentJob, err := jobsClient.Get(ctx, jobName, metav1.GetOptions{})
			if err != nil {
				continue
			}

			// Check if completed
			if currentJob.Status.Succeeded > 0 {
				result.Success = true
				result.Logs = c.getJobLogs(ctx, req.Namespace, jobName)
				return result, nil
			}

			// Check if failed
			if currentJob.Status.Failed > 0 {
				result.Success = false
				result.Error = "pre-deploy job failed"
				result.Logs = c.getJobLogs(ctx, req.Namespace, jobName)
				return result, nil
			}
		}
	}
}

// getJobLogs retrieves logs from a job's pod
func (c *Client) getJobLogs(ctx context.Context, namespace, jobName string) string {
	// Find the pod created by the job
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}

	podName := pods.Items[0].Name

	// Get logs from the pod
	opts := &corev1.PodLogOptions{
		Container: "predeploy",
	}

	req := c.clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return ""
	}
	defer stream.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, stream)
	return buf.String()
}

// IngressControllerInfo contains information about the cluster's ingress controller
type IngressControllerInfo struct {
	Available    bool   `json:"available"`
	LoadBalancer string `json:"load_balancer,omitempty"`
	Message      string `json:"message,omitempty"`
}

// GetIngressController finds the ingress controller service and returns its load balancer endpoint
// It searches in common namespaces: ingress-nginx, nginx-ingress, ingress
func (c *Client) GetIngressController() (*IngressControllerInfo, error) {
	ctx := context.Background()

	// Common namespaces and service name patterns for ingress controllers
	searchPatterns := []struct {
		namespace string
		labelSelector string
	}{
		{"ingress-nginx", "app.kubernetes.io/component=controller"},
		{"ingress-nginx", "app=ingress-nginx"},
		{"nginx-ingress", "app=nginx-ingress"},
		{"ingress", "app=nginx-ingress"},
		{"kube-system", "app=nginx-ingress"},
	}

	for _, pattern := range searchPatterns {
		services, err := c.clientset.CoreV1().Services(pattern.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: pattern.labelSelector,
		})
		if err != nil {
			continue // Namespace might not exist
		}

		for _, svc := range services.Items {
			if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
				if len(svc.Status.LoadBalancer.Ingress) > 0 {
					lb := svc.Status.LoadBalancer.Ingress[0]
					loadBalancer := lb.Hostname
					if loadBalancer == "" {
						loadBalancer = lb.IP
					}
					if loadBalancer != "" {
						return &IngressControllerInfo{
							Available:    true,
							LoadBalancer: loadBalancer,
						}, nil
					}
				}
				// Service exists but no load balancer assigned yet
				return &IngressControllerInfo{
					Available: true,
					Message:   "Ingress controller found but load balancer is still provisioning",
				}, nil
			}
		}
	}

	// Try to find any service with nginx-ingress or ingress-controller in the name
	allNamespaces, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, ns := range allNamespaces.Items {
			services, err := c.clientset.CoreV1().Services(ns.Name).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			for _, svc := range services.Items {
				if svc.Spec.Type == corev1.ServiceTypeLoadBalancer &&
					(strings.Contains(svc.Name, "ingress") || strings.Contains(svc.Name, "nginx")) {
					if len(svc.Status.LoadBalancer.Ingress) > 0 {
						lb := svc.Status.LoadBalancer.Ingress[0]
						loadBalancer := lb.Hostname
						if loadBalancer == "" {
							loadBalancer = lb.IP
						}
						if loadBalancer != "" {
							return &IngressControllerInfo{
								Available:    true,
								LoadBalancer: loadBalancer,
							}, nil
						}
					}
				}
			}
		}
	}

	return &IngressControllerInfo{
		Available: false,
		Message:   "No ingress controller found. Install nginx-ingress to enable custom domains.",
	}, nil
}

// PorterApp represents a discovered Porter application with its services
type PorterApp struct {
	AppName  string          `json:"app_name"`
	Services []PorterService `json:"services"`
}

// PorterService represents a single service within a Porter app
type PorterService struct {
	ServiceName      string            `json:"service_name"`
	ServiceType      string            `json:"service_type"` // web, worker, job
	DeploymentName   string            `json:"deployment_name"`
	Namespace        string            `json:"namespace"`
	Image            string            `json:"image"`
	Replicas         int32             `json:"replicas"`
	Port             *int              `json:"port,omitempty"`
	EnvVars          map[string]string `json:"env_vars"`
	CPURequest       string            `json:"cpu_request"`
	CPULimit         string            `json:"cpu_limit"`
	MemoryRequest    string            `json:"memory_request"`
	MemoryLimit      string            `json:"memory_limit"`
	HealthPath       string            `json:"health_path,omitempty"`
	PreDeployCommand string            `json:"pre_deploy_command,omitempty"`
	Domain           string            `json:"domain,omitempty"`
	AppID            string            `json:"app_id"`            // Porter app ID
	AppInstanceID    string            `json:"app_instance_id"`  // Porter app instance ID
	RevisionID       string            `json:"revision_id"`      // Porter revision ID
}

// DiscoverPorterApps finds all Porter-managed applications in the cluster
func (c *Client) DiscoverPorterApps() ([]PorterApp, error) {
	ctx := context.Background()

	// Find all deployments with Porter labels
	deployments, err := c.clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{
		LabelSelector: "porter.run/porter-application=true",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Porter deployments: %w", err)
	}

	// Group services by app name
	appGroups := make(map[string][]PorterService)

	for _, deployment := range deployments.Items {
		labels := deployment.Labels
		appName := labels["porter.run/app-name"]
		serviceName := labels["porter.run/service-name"]
		serviceType := labels["porter.run/service-type"]

		if appName == "" || serviceName == "" {
			continue // Skip deployments without proper Porter labels
		}

		// Extract service configuration from deployment spec
		service := PorterService{
			ServiceName:    serviceName,
			ServiceType:    serviceType,
			DeploymentName: deployment.Name,
			Namespace:      deployment.Namespace,
			Replicas:       *deployment.Spec.Replicas,
			AppID:          labels["porter.run/app-id"],
			AppInstanceID:  labels["porter.run/app-instance-id"],
			RevisionID:     labels["porter.run/app-revision-id"],
			EnvVars:        make(map[string]string),
		}

		// Extract container configuration
		if len(deployment.Spec.Template.Spec.Containers) > 0 {
			container := deployment.Spec.Template.Spec.Containers[0]
			service.Image = container.Image

			// Extract port
			if len(container.Ports) > 0 {
				port := int(container.Ports[0].ContainerPort)
				service.Port = &port
			}

			// Extract environment variables (non-secret)
			for _, env := range container.Env {
				if env.Value != "" {
					service.EnvVars[env.Name] = env.Value
				}
			}

			// Extract resource limits
			if container.Resources.Requests != nil {
				if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
					service.CPURequest = cpu.String()
				}
				if mem, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
					service.MemoryRequest = mem.String()
				}
			}
			if container.Resources.Limits != nil {
				if cpu, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
					service.CPULimit = cpu.String()
				}
				if mem, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
					service.MemoryLimit = mem.String()
				}
			}

			// Extract health check path
			if container.LivenessProbe != nil && container.LivenessProbe.HTTPGet != nil {
				service.HealthPath = container.LivenessProbe.HTTPGet.Path
			}
		}

		// Check for associated ingress to get domain
		ingresses, err := c.clientset.NetworkingV1().Ingresses(deployment.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", labels["app.kubernetes.io/instance"]),
		})
		if err == nil && len(ingresses.Items) > 0 {
			// Get first domain from ingress rules
			if len(ingresses.Items[0].Spec.Rules) > 0 {
				service.Domain = ingresses.Items[0].Spec.Rules[0].Host
			}
		}

		appGroups[appName] = append(appGroups[appName], service)
	}

	// Convert map to slice of PorterApp
	var porterApps []PorterApp
	for appName, services := range appGroups {
		porterApps = append(porterApps, PorterApp{
			AppName:  appName,
			Services: services,
		})
	}

	return porterApps, nil
}

// EphemeralPodRequest contains parameters for creating an ephemeral pod
type EphemeralPodRequest struct {
	AppName    string
	Namespace  string
	Image      string
	EnvVars    map[string]string
	SecretName string   // K8s Secret name for EnvFrom injection
	CPU        string   // e.g., "500m"
	RAM        string   // e.g., "512Mi"
	Command    []string // command to run
}

// FindRunningPod finds a running pod for the given app and returns the pod name and container name.
// If container is non-empty, it validates that the container exists in the pod spec.
// If container is empty, it returns the first container in the pod spec.
func (c *Client) FindRunningPod(ctx context.Context, namespace, appName, container string) (podName string, containerName string, err error) {
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appName),
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "", "", fmt.Errorf("no pods found for app %s", appName)
	}

	// Find first pod that is Ready
	for _, pod := range pods.Items {
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			continue
		}

		if len(pod.Spec.Containers) == 0 {
			continue
		}

		// Resolve container name
		if container != "" {
			found := false
			for _, c := range pod.Spec.Containers {
				if c.Name == container {
					found = true
					break
				}
			}
			if !found {
				return "", "", fmt.Errorf("container %q not found in pod %s", container, pod.Name)
			}
			return pod.Name, container, nil
		}

		return pod.Name, pod.Spec.Containers[0].Name, nil
	}

	return "", "", fmt.Errorf("no running pods found for app %s", appName)
}

// CreateEphemeralPod creates a short-lived pod for running commands in the cluster.
// The pod runs sleep and is meant to be used with ExecInPod for interactive or one-shot commands.
// Returns the pod name once it reaches Running phase.
func (c *Client) CreateEphemeralPod(ctx context.Context, req EphemeralPodRequest) (string, error) {
	suffix, err := randomSuffix(8)
	if err != nil {
		return "", fmt.Errorf("failed to generate pod name suffix: %w", err)
	}
	podName := fmt.Sprintf("%s-run-%s", req.AppName, suffix)

	// Build env vars
	var envVars []corev1.EnvVar
	for k, v := range req.EnvVars {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
	}

	container := corev1.Container{
		Name:    "run",
		Image:   req.Image,
		Command: []string{"sleep", "3600"},
		Env:     envVars,
	}

	// Inject secrets if specified
	if req.SecretName != "" {
		container.EnvFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: req.SecretName,
				},
			},
		}}
	}

	// Set resource requests if provided
	if req.CPU != "" || req.RAM != "" {
		container.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{},
		}
		if req.CPU != "" {
			qty, err := resource.ParseQuantity(req.CPU)
			if err != nil {
				return "", fmt.Errorf("invalid CPU value %q: %w", req.CPU, err)
			}
			container.Resources.Requests[corev1.ResourceCPU] = qty
		}
		if req.RAM != "" {
			qty, err := resource.ParseQuantity(req.RAM)
			if err != nil {
				return "", fmt.Errorf("invalid RAM value %q: %w", req.RAM, err)
			}
			container.Resources.Requests[corev1.ResourceMemory] = qty
		}
	}

	activeDeadline := int64(3600)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: req.Namespace,
			Labels: map[string]string{
				"app":                  req.AppName,
				"shipit.dev/ephemeral": "true",
				"shipit.dev/app":       req.AppName,
				"managed-by":           "shipit",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: &activeDeadline,
			Containers:            []corev1.Container{container},
		},
	}

	_, err = c.clientset.CoreV1().Pods(req.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create ephemeral pod: %w", err)
	}

	// Wait for pod to reach Running phase
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			// Clean up on timeout — use a fresh context since the parent may already be canceled
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			_ = c.clientset.CoreV1().Pods(req.Namespace).Delete(cleanupCtx, podName, metav1.DeleteOptions{})
			return "", fmt.Errorf("timed out waiting for pod %s to start", podName)
		case <-ticker.C:
			current, err := c.clientset.CoreV1().Pods(req.Namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				continue
			}
			switch current.Status.Phase {
			case corev1.PodRunning:
				return podName, nil
			case corev1.PodFailed, corev1.PodSucceeded, corev1.PodUnknown:
				return "", fmt.Errorf("pod %s entered unexpected phase: %s", podName, current.Status.Phase)
			}
		}
	}
}

// ExecInPod executes a command in a running pod container via SPDY.
// Returns the exit code and any connection-level error.
// Exit code 0 means success; positive exit codes indicate the command failed;
// -1 with a non-nil error indicates a connection/protocol error.
func (c *Client) ExecInPod(ctx context.Context, namespace, podName, container string, command []string, stdin io.Reader, stdout, stderr io.Writer, tty bool) (int, error) {
	execOpts := &corev1.PodExecOptions{
		Container: container,
		Command:   command,
		TTY:       tty,
	}
	if stdin != nil {
		execOpts.Stdin = true
	}
	if stdout != nil {
		execOpts.Stdout = true
	}
	if stderr != nil && !tty {
		execOpts.Stderr = true
	}

	req := c.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(execOpts, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
	if err != nil {
		return -1, fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	streamOpts := remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Tty:    tty,
	}
	if stderr != nil && !tty {
		streamOpts.Stderr = stderr
	}

	err = executor.StreamWithContext(ctx, streamOpts)
	if err == nil {
		return 0, nil
	}

	// Check if it's an exit code error from the remote command
	if exitErr, ok := err.(utilexec.ExitError); ok {
		return exitErr.ExitStatus(), nil
	}

	return -1, err
}

// CleanupEphemeralPods deletes all ephemeral pods for the given app.
// Returns the number of pods deleted.
func (c *Client) CleanupEphemeralPods(ctx context.Context, namespace, appName string) (int, error) {
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("shipit.dev/ephemeral=true,shipit.dev/app=%s", appName),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list ephemeral pods: %w", err)
	}

	deleted := 0
	for _, pod := range pods.Items {
		if err := c.clientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
			return deleted, fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
		}
		deleted++
	}

	return deleted, nil
}

// DeletePod deletes a single pod by name.
func (c *Client) DeletePod(ctx context.Context, namespace, podName string) error {
	return c.clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
}

// randomSuffix generates a random hex string of the given length.
func randomSuffix(length int) (string, error) {
	b := make([]byte, (length+1)/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b)[:length], nil
}
