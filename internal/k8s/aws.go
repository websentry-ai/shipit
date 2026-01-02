package k8s

import (
	"fmt"
	"os"
	"text/template"
)

// AWSOIDCKubeconfigParams contains parameters for generating an OIDC-based kubeconfig
type AWSOIDCKubeconfigParams struct {
	ClusterName     string
	ClusterEndpoint string
	ClusterCA       string // Base64 encoded CA cert
	Region          string
}

// GenerateAWSOIDCKubeconfig generates a kubeconfig that uses IRSA/OIDC authentication
// This works when running on EKS with a service account that has an IAM role attached
func GenerateAWSOIDCKubeconfig(params AWSOIDCKubeconfigParams) ([]byte, error) {
	tmpl := `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: {{.ClusterCA}}
    server: {{.ClusterEndpoint}}
  name: {{.ClusterName}}
contexts:
- context:
    cluster: {{.ClusterName}}
    user: shipit
  name: {{.ClusterName}}
current-context: {{.ClusterName}}
users:
- name: shipit
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args:
        - --region
        - {{.Region}}
        - eks
        - get-token
        - --cluster-name
        - {{.ClusterName}}
        - --output
        - json
`

	t, err := template.New("kubeconfig").Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig template: %w", err)
	}

	var buf []byte
	writer := &byteWriter{buf: &buf}
	if err := t.Execute(writer, params); err != nil {
		return nil, fmt.Errorf("failed to execute kubeconfig template: %w", err)
	}

	return buf, nil
}

type byteWriter struct {
	buf *[]byte
}

func (w *byteWriter) Write(p []byte) (n int, err error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

// IsRunningOnAWS checks if we're running on AWS (EKS with IRSA)
func IsRunningOnAWS() bool {
	// IRSA sets these environment variables
	_, hasRoleArn := os.LookupEnv("AWS_ROLE_ARN")
	_, hasTokenFile := os.LookupEnv("AWS_WEB_IDENTITY_TOKEN_FILE")
	return hasRoleArn && hasTokenFile
}

// GetAWSRegion returns the AWS region from environment or default
func GetAWSRegion() string {
	if region := os.Getenv("AWS_REGION"); region != "" {
		return region
	}
	if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		return region
	}
	return "us-west-2" // Default
}
