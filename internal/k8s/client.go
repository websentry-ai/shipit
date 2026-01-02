package k8s

import (
	"context"
	"fmt"
	"io"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	Name      string
	Namespace string
	Image     string
	Replicas  int32
	Port      *int
	EnvVars   map[string]string
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

	if req.Port != nil {
		container.Ports = []corev1.ContainerPort{{ContainerPort: int32(*req.Port)}}
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

	return nil
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
