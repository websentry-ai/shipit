package k8s

import (
	"context"
	"fmt"
	"io"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	clientset *kubernetes.Clientset
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
}

type DeploymentStatus struct {
	Name              string `json:"name"`
	Replicas          int32  `json:"replicas"`
	ReadyReplicas     int32  `json:"ready_replicas"`
	AvailableReplicas int32  `json:"available_replicas"`
	Status            string `json:"status"`
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

	return &Client{clientset: clientset}, nil
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
		Name:  req.Name,
		Image: req.Image,
		Env:   envVars,
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

	// Configure health probes if health path is specified
	if req.HealthPath != nil && *req.HealthPath != "" {
		healthPort := req.Port
		if req.HealthPort != nil {
			healthPort = req.HealthPort
		}

		initialDelay := int32(10)
		if req.HealthInitialDelay != nil {
			initialDelay = int32(*req.HealthInitialDelay)
		}

		period := int32(30)
		if req.HealthPeriod != nil {
			period = int32(*req.HealthPeriod)
		}

		if healthPort != nil {
			probe := &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: *req.HealthPath,
						Port: intstr.FromInt(*healthPort),
					},
				},
				InitialDelaySeconds: initialDelay,
				PeriodSeconds:       period,
			}

			// Use same config for both liveness and readiness probes
			container.LivenessProbe = probe
			container.ReadinessProbe = probe.DeepCopy()
		}
	}

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
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": req.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
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
		// Update existing deployment
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

	return nil
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

func (c *Client) DeleteApp(name, namespace string) error {
	ctx := context.Background()

	// Delete deployment
	c.clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})

	// Delete service
	c.clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})

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
	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(
		context.Background(), name, metav1.GetOptions{})
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

	return &DeploymentStatus{
		Name:              name,
		Replicas:          *deployment.Spec.Replicas,
		ReadyReplicas:     deployment.Status.ReadyReplicas,
		AvailableReplicas: deployment.Status.AvailableReplicas,
		Status:            status,
	}, nil
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
