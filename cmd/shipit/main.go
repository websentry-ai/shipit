package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	apiURL   string
	apiToken string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "shipit",
		Short: "ShipIt CLI - Deploy apps to Kubernetes",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			loadConfig()
		},
	}

	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "", "API server URL")
	rootCmd.PersistentFlags().StringVar(&apiToken, "token", "", "API token")

	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(projectsCmd())
	rootCmd.AddCommand(clustersCmd())
	rootCmd.AddCommand(appsCmd())
	rootCmd.AddCommand(deployCmd())
	rootCmd.AddCommand(logsCmd())
	rootCmd.AddCommand(secretsCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// Config management

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set-url <url>",
		Short: "Set the API server URL",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			saveConfigValue("api_url", args[0])
			fmt.Println("API URL set successfully")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set-token <token>",
		Short: "Set the API token",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			saveConfigValue("api_token", args[0])
			fmt.Println("API token set successfully")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("API URL: %s\n", apiURL)
			if apiToken != "" {
				fmt.Printf("API Token: %s...%s\n", apiToken[:8], apiToken[len(apiToken)-4:])
			} else {
				fmt.Println("API Token: (not set)")
			}
		},
	})

	return cmd
}

// Projects

func projectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"project", "p"},
		Short:   "Manage projects",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all projects",
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := apiRequest("GET", "/api/projects", nil)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			body := map[string]string{"name": args[0]}
			resp, err := apiRequest("POST", "/api/projects", body)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a project",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, err := apiRequest("DELETE", "/api/projects/"+args[0], nil)
			if err != nil {
				fatal(err)
			}
			fmt.Println("Project deleted")
		},
	})

	return cmd
}

// Clusters

func clustersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "clusters",
		Aliases: []string{"cluster", "c"},
		Short:   "Manage clusters",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list <project-id>",
		Short: "List clusters in a project",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := apiRequest("GET", "/api/projects/"+args[0]+"/clusters", nil)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	})

	connectCmd := &cobra.Command{
		Use:   "connect <project-id>",
		Short: "Connect a Kubernetes cluster",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name, _ := cmd.Flags().GetString("name")
			kubeconfigPath, _ := cmd.Flags().GetString("kubeconfig")

			if name == "" {
				fatal(fmt.Errorf("--name is required"))
			}

			// Read kubeconfig
			var kubeconfig []byte
			var err error
			if kubeconfigPath == "" {
				kubeconfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
			}
			kubeconfig, err = os.ReadFile(kubeconfigPath)
			if err != nil {
				fatal(fmt.Errorf("failed to read kubeconfig: %w", err))
			}

			body := map[string]string{
				"name":       name,
				"kubeconfig": string(kubeconfig),
			}
			resp, err := apiRequest("POST", "/api/projects/"+args[0]+"/clusters", body)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	}
	connectCmd.Flags().String("name", "", "Cluster name")
	connectCmd.Flags().String("kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	cmd.AddCommand(connectCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a cluster",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, err := apiRequest("DELETE", "/api/clusters/"+args[0], nil)
			if err != nil {
				fatal(err)
			}
			fmt.Println("Cluster deleted")
		},
	})

	return cmd
}

// Apps

func appsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apps",
		Aliases: []string{"app", "a"},
		Short:   "Manage applications",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list <cluster-id>",
		Short: "List apps in a cluster",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := apiRequest("GET", "/api/clusters/"+args[0]+"/apps", nil)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	})

	createCmd := &cobra.Command{
		Use:   "create <cluster-id>",
		Short: "Create a new app (without deploying)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name, _ := cmd.Flags().GetString("name")
			image, _ := cmd.Flags().GetString("image")
			replicas, _ := cmd.Flags().GetInt("replicas")
			port, _ := cmd.Flags().GetInt("port")
			namespace, _ := cmd.Flags().GetString("namespace")
			envFlags, _ := cmd.Flags().GetStringSlice("env")
			// Resource limits
			cpuRequest, _ := cmd.Flags().GetString("cpu-request")
			cpuLimit, _ := cmd.Flags().GetString("cpu-limit")
			memRequest, _ := cmd.Flags().GetString("memory-request")
			memLimit, _ := cmd.Flags().GetString("memory-limit")
			// Health check
			healthPath, _ := cmd.Flags().GetString("health-path")
			healthPort, _ := cmd.Flags().GetInt("health-port")
			healthDelay, _ := cmd.Flags().GetInt("health-initial-delay")
			healthPeriod, _ := cmd.Flags().GetInt("health-period")

			if name == "" || image == "" {
				fatal(fmt.Errorf("--name and --image are required"))
			}

			envVars := make(map[string]string)
			for _, e := range envFlags {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					envVars[parts[0]] = parts[1]
				}
			}

			body := map[string]interface{}{
				"name":      name,
				"image":     image,
				"replicas":  replicas,
				"namespace": namespace,
				"env_vars":  envVars,
			}
			if port > 0 {
				body["port"] = port
			}
			// Resource limits (use defaults if not specified)
			if cpuRequest != "" {
				body["cpu_request"] = cpuRequest
			}
			if cpuLimit != "" {
				body["cpu_limit"] = cpuLimit
			}
			if memRequest != "" {
				body["memory_request"] = memRequest
			}
			if memLimit != "" {
				body["memory_limit"] = memLimit
			}
			// Health check
			if healthPath != "" {
				body["health_path"] = healthPath
				if healthPort > 0 {
					body["health_port"] = healthPort
				}
				if healthDelay > 0 {
					body["health_initial_delay"] = healthDelay
				}
				if healthPeriod > 0 {
					body["health_period"] = healthPeriod
				}
			}

			resp, err := apiRequest("POST", "/api/clusters/"+args[0]+"/apps", body)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	}
	createCmd.Flags().String("name", "", "App name (required)")
	createCmd.Flags().String("image", "", "Container image (required)")
	createCmd.Flags().Int("replicas", 1, "Number of replicas")
	createCmd.Flags().Int("port", 0, "Container port")
	createCmd.Flags().String("namespace", "default", "Kubernetes namespace")
	createCmd.Flags().StringSlice("env", nil, "Environment variables (KEY=VALUE)")
	// Resource limits
	createCmd.Flags().String("cpu-request", "", "CPU request (e.g., 100m) - default: 100m")
	createCmd.Flags().String("cpu-limit", "", "CPU limit (e.g., 500m) - default: 500m")
	createCmd.Flags().String("memory-request", "", "Memory request (e.g., 128Mi) - default: 128Mi")
	createCmd.Flags().String("memory-limit", "", "Memory limit (e.g., 256Mi) - default: 256Mi")
	// Health check
	createCmd.Flags().String("health-path", "", "Health check HTTP path (e.g., /health)")
	createCmd.Flags().Int("health-port", 0, "Health check port (defaults to app port)")
	createCmd.Flags().Int("health-initial-delay", 10, "Initial delay before first health check (seconds)")
	createCmd.Flags().Int("health-period", 30, "Period between health checks (seconds)")
	cmd.AddCommand(createCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "get <app-id>",
		Short: "Get app details",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := apiRequest("GET", "/api/apps/"+args[0], nil)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status <app-id>",
		Short: "Get app deployment status",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := apiRequest("GET", "/api/apps/"+args[0]+"/status", nil)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "deploy <app-id>",
		Short: "Deploy an existing app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, err := apiRequest("POST", "/api/apps/"+args[0]+"/deploy", nil)
			if err != nil {
				fatal(err)
			}
			fmt.Println("Deployment triggered. Use 'shipit apps status " + args[0] + "' to check status")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <app-id>",
		Short: "Delete an app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, err := apiRequest("DELETE", "/api/apps/"+args[0], nil)
			if err != nil {
				fatal(err)
			}
			fmt.Println("App deleted")
		},
	})

	// Revisions subcommand
	revisionsCmd := &cobra.Command{
		Use:   "revisions <app-id>",
		Short: "List deployment revisions for an app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			limit, _ := cmd.Flags().GetInt("limit")
			url := "/api/apps/" + args[0] + "/revisions"
			if limit > 0 {
				url += fmt.Sprintf("?limit=%d", limit)
			}
			resp, err := apiRequest("GET", url, nil)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	}
	revisionsCmd.Flags().Int("limit", 10, "Number of revisions to show")
	cmd.AddCommand(revisionsCmd)

	// Rollback subcommand
	rollbackCmd := &cobra.Command{
		Use:   "rollback <app-id>",
		Short: "Rollback app to a previous revision",
		Long:  "Rollback an app to the previous revision, or to a specific revision with --revision",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			revision, _ := cmd.Flags().GetInt("revision")

			var body map[string]interface{}
			if revision > 0 {
				body = map[string]interface{}{"revision": revision}
			}

			resp, err := apiRequest("POST", "/api/apps/"+args[0]+"/rollback", body)
			if err != nil {
				fatal(err)
			}

			var result map[string]interface{}
			json.Unmarshal(resp, &result)

			fmt.Printf("Rolling back to revision %v (image: %v)\n",
				result["target_revision"], result["target_image"])
			fmt.Println("Use 'shipit apps status " + args[0] + "' to check status")
		},
	}
	rollbackCmd.Flags().Int("revision", 0, "Specific revision number to rollback to (default: previous)")
	cmd.AddCommand(rollbackCmd)

	return cmd
}

// Deploy

func deployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an application",
	}

	deployCreateCmd := &cobra.Command{
		Use:   "create <cluster-id>",
		Short: "Create and deploy a new app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name, _ := cmd.Flags().GetString("name")
			image, _ := cmd.Flags().GetString("image")
			replicas, _ := cmd.Flags().GetInt("replicas")
			port, _ := cmd.Flags().GetInt("port")
			namespace, _ := cmd.Flags().GetString("namespace")
			envFlags, _ := cmd.Flags().GetStringSlice("env")
			// Resource limits
			cpuRequest, _ := cmd.Flags().GetString("cpu-request")
			cpuLimit, _ := cmd.Flags().GetString("cpu-limit")
			memRequest, _ := cmd.Flags().GetString("memory-request")
			memLimit, _ := cmd.Flags().GetString("memory-limit")
			// Health check
			healthPath, _ := cmd.Flags().GetString("health-path")
			healthPort, _ := cmd.Flags().GetInt("health-port")
			healthDelay, _ := cmd.Flags().GetInt("health-initial-delay")
			healthPeriod, _ := cmd.Flags().GetInt("health-period")

			if name == "" || image == "" {
				fatal(fmt.Errorf("--name and --image are required"))
			}

			envVars := make(map[string]string)
			for _, e := range envFlags {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					envVars[parts[0]] = parts[1]
				}
			}

			body := map[string]interface{}{
				"name":      name,
				"image":     image,
				"replicas":  replicas,
				"namespace": namespace,
				"env_vars":  envVars,
			}
			if port > 0 {
				body["port"] = port
			}
			// Resource limits (use defaults if not specified)
			if cpuRequest != "" {
				body["cpu_request"] = cpuRequest
			}
			if cpuLimit != "" {
				body["cpu_limit"] = cpuLimit
			}
			if memRequest != "" {
				body["memory_request"] = memRequest
			}
			if memLimit != "" {
				body["memory_limit"] = memLimit
			}
			// Health check
			if healthPath != "" {
				body["health_path"] = healthPath
				if healthPort > 0 {
					body["health_port"] = healthPort
				}
				if healthDelay > 0 {
					body["health_initial_delay"] = healthDelay
				}
				if healthPeriod > 0 {
					body["health_period"] = healthPeriod
				}
			}

			// Create app
			resp, err := apiRequest("POST", "/api/clusters/"+args[0]+"/apps", body)
			if err != nil {
				fatal(err)
			}

			var app map[string]interface{}
			json.Unmarshal(resp, &app)
			appID := app["id"].(string)

			fmt.Println("App created, deploying...")

			// Trigger deploy
			_, err = apiRequest("POST", "/api/apps/"+appID+"/deploy", nil)
			if err != nil {
				fatal(err)
			}

			fmt.Println("Deployment started. Use 'shipit apps status " + appID + "' to check status")
		},
	}
	deployCreateCmd.Flags().String("name", "", "App name")
	deployCreateCmd.Flags().String("image", "", "Container image")
	deployCreateCmd.Flags().Int("replicas", 1, "Number of replicas")
	deployCreateCmd.Flags().Int("port", 0, "Container port")
	deployCreateCmd.Flags().String("namespace", "default", "Kubernetes namespace")
	deployCreateCmd.Flags().StringSlice("env", nil, "Environment variables (KEY=VALUE)")
	// Resource limits
	deployCreateCmd.Flags().String("cpu-request", "", "CPU request (e.g., 100m) - default: 100m")
	deployCreateCmd.Flags().String("cpu-limit", "", "CPU limit (e.g., 500m) - default: 500m")
	deployCreateCmd.Flags().String("memory-request", "", "Memory request (e.g., 128Mi) - default: 128Mi")
	deployCreateCmd.Flags().String("memory-limit", "", "Memory limit (e.g., 256Mi) - default: 256Mi")
	// Health check
	deployCreateCmd.Flags().String("health-path", "", "Health check HTTP path (e.g., /health)")
	deployCreateCmd.Flags().Int("health-port", 0, "Health check port (defaults to app port)")
	deployCreateCmd.Flags().Int("health-initial-delay", 10, "Initial delay before first health check (seconds)")
	deployCreateCmd.Flags().Int("health-period", 30, "Period between health checks (seconds)")
	cmd.AddCommand(deployCreateCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "trigger <app-id>",
		Short: "Trigger a deployment for an existing app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, err := apiRequest("POST", "/api/apps/"+args[0]+"/deploy", nil)
			if err != nil {
				fatal(err)
			}
			fmt.Println("Deployment triggered")
		},
	})

	return cmd
}

// Logs

func logsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <app-id>",
		Short: "Stream logs from an app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			follow, _ := cmd.Flags().GetBool("follow")
			tail, _ := cmd.Flags().GetString("tail")

			url := apiURL + "/api/apps/" + args[0] + "/logs"
			if follow {
				url += "?follow=true"
			}
			if tail != "" {
				if strings.Contains(url, "?") {
					url += "&tail=" + tail
				} else {
					url += "?tail=" + tail
				}
			}

			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Set("Authorization", "Bearer "+apiToken)

			client := &http.Client{Timeout: 0} // No timeout for streaming
			resp, err := client.Do(req)
			if err != nil {
				fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				fatal(fmt.Errorf("API error: %s", string(body)))
			}

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().String("tail", "", "Number of lines to show from the end")

	return cmd
}

// Secrets

func secretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Aliases: []string{"secret", "s"},
		Short:   "Manage application secrets",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list <app-id>",
		Short: "List secrets for an app (keys only, values are never shown)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := apiRequest("GET", "/api/apps/"+args[0]+"/secrets", nil)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
		},
	})

	setCmd := &cobra.Command{
		Use:   "set <app-id>",
		Short: "Set a secret for an app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			key, _ := cmd.Flags().GetString("key")
			value, _ := cmd.Flags().GetString("value")

			if key == "" || value == "" {
				fatal(fmt.Errorf("--key and --value are required"))
			}

			body := map[string]string{
				"key":   key,
				"value": value,
			}
			resp, err := apiRequest("POST", "/api/apps/"+args[0]+"/secrets", body)
			if err != nil {
				fatal(err)
			}
			printJSON(resp)
			fmt.Println("\nSecret set. Redeploy the app to apply: shipit apps deploy " + args[0])
		},
	}
	setCmd.Flags().String("key", "", "Secret key (required)")
	setCmd.Flags().String("value", "", "Secret value (required)")
	cmd.AddCommand(setCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <app-id>",
		Short: "Delete a secret from an app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			key, _ := cmd.Flags().GetString("key")

			if key == "" {
				fatal(fmt.Errorf("--key is required"))
			}

			_, err := apiRequest("DELETE", "/api/apps/"+args[0]+"/secrets/"+key, nil)
			if err != nil {
				fatal(err)
			}
			fmt.Println("Secret deleted. Redeploy the app to apply: shipit apps deploy " + args[0])
		},
	}
	deleteCmd.Flags().String("key", "", "Secret key to delete (required)")
	cmd.AddCommand(deleteCmd)

	return cmd
}

// Helpers

func loadConfig() {
	configPath := filepath.Join(os.Getenv("HOME"), ".shipit", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	var cfg map[string]string
	json.Unmarshal(data, &cfg)

	if apiURL == "" {
		apiURL = cfg["api_url"]
	}
	if apiToken == "" {
		apiToken = cfg["api_token"]
	}
}

func saveConfigValue(key, value string) {
	configDir := filepath.Join(os.Getenv("HOME"), ".shipit")
	os.MkdirAll(configDir, 0700)

	configPath := filepath.Join(configDir, "config.json")

	cfg := make(map[string]string)
	data, _ := os.ReadFile(configPath)
	json.Unmarshal(data, &cfg)

	cfg[key] = value

	data, _ = json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(configPath, data, 0600)
}

func apiRequest(method, path string, body interface{}) ([]byte, error) {
	if apiURL == "" {
		return nil, fmt.Errorf("API URL not set. Run: shipit config set-url <url>")
	}
	if apiToken == "" {
		return nil, fmt.Errorf("API token not set. Run: shipit config set-token <token>")
	}

	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, apiURL+path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func printJSON(data []byte) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		fmt.Println(string(data))
		return
	}
	formatted, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(formatted))
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
