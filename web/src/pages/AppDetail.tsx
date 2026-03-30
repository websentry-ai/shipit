import { useState, useEffect, useRef } from 'react';
import { Link, useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  getApp,
  getAppStatus,
  listRevisions,
  listSecrets,
  deployApp,
  rollbackApp,
  deleteApp,
  updateApp,
  setSecret,
  deleteSecret,
  streamLogs,
  getAutoscaling,
  setAutoscaling,
  getDomain,
  setDomain,
  getDeploymentHistory,
  getPreDeployHook,
  setPreDeployHook,
  getClusterIngress,
  switchAppManagement,
} from '../api/client';
import type { AppRevision, AppSecret, UpdateAppRequest, HPAConfig, DomainConfig, PreDeployHookConfig } from '../types';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Modal, ConfirmModal } from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { Skeleton } from '../components/ui/Skeleton';

function Tab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
        active
          ? 'border-accent text-accent'
          : 'border-transparent text-text-secondary hover:text-text-primary hover:border-border'
      }`}
    >
      {children}
    </button>
  );
}

export default function AppDetail() {
  const { appId } = useParams<{ appId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<'overview' | 'env' | 'secrets' | 'autoscaling' | 'domain' | 'hooks' | 'revisions' | 'monitoring' | 'logs'>('overview');
  const [logs, setLogs] = useState<string[]>([]);
  const [showSecretModal, setShowSecretModal] = useState(false);
  const [newSecretKey, setNewSecretKey] = useState('');
  const [newSecretValue, setNewSecretValue] = useState('');
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [rollbackConfirm, setRollbackConfirm] = useState<AppRevision | null>(null);
  const logsEndRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);

  // Environment variable editing state
  const [envVars, setEnvVars] = useState<Record<string, string>>({});
  const [newEnvKey, setNewEnvKey] = useState('');
  const [newEnvValue, setNewEnvValue] = useState('');
  const [editingEnvKey, setEditingEnvKey] = useState<string | null>(null);
  const [editingEnvValue, setEditingEnvValue] = useState('');

  const { data: app, isLoading } = useQuery({
    queryKey: ['app', appId],
    queryFn: () => getApp(appId!),
    enabled: !!appId,
  });

  const { data: status } = useQuery({
    queryKey: ['app-status', appId],
    queryFn: () => getAppStatus(appId!),
    enabled: !!appId,
    refetchInterval: 5000,
  });

  const { data: revisions } = useQuery({
    queryKey: ['revisions', appId],
    queryFn: () => listRevisions(appId!),
    enabled: !!appId && activeTab === 'revisions',
  });

  const { data: secrets } = useQuery({
    queryKey: ['secrets', appId],
    queryFn: () => listSecrets(appId!),
    enabled: !!appId && activeTab === 'secrets',
  });

  const { data: autoscaling, isLoading: autoscalingLoading } = useQuery({
    queryKey: ['autoscaling', appId],
    queryFn: () => getAutoscaling(appId!),
    enabled: !!appId && activeTab === 'autoscaling',
  });

  const { data: domainStatus, isLoading: domainLoading } = useQuery({
    queryKey: ['domain', appId],
    queryFn: () => getDomain(appId!),
    enabled: !!appId && activeTab === 'domain',
  });

  const { data: deploymentHistory, isLoading: historyLoading } = useQuery({
    queryKey: ['deployment-history', appId],
    queryFn: () => getDeploymentHistory(appId!),
    enabled: !!appId && activeTab === 'monitoring',
    refetchInterval: 10000,
  });

  const { data: preDeployHook, isLoading: hookLoading } = useQuery({
    queryKey: ['predeploy', appId],
    queryFn: () => getPreDeployHook(appId!),
    enabled: !!appId && activeTab === 'hooks',
  });

  const { data: clusterIngress, isLoading: ingressLoading } = useQuery({
    queryKey: ['cluster-ingress', app?.cluster_id],
    queryFn: () => getClusterIngress(app!.cluster_id),
    enabled: !!app?.cluster_id && activeTab === 'domain',
  });

  const deployMutation = useMutation({
    mutationFn: () => deployApp(appId!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['app', appId] });
      queryClient.invalidateQueries({ queryKey: ['revisions', appId] });
    },
  });

  const rollbackMutation = useMutation({
    mutationFn: (revision?: number) => rollbackApp(appId!, revision),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['app', appId] });
      queryClient.invalidateQueries({ queryKey: ['revisions', appId] });
      setRollbackConfirm(null);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteApp(appId!),
    onSuccess: () => {
      navigate(-1);
    },
  });

  const switchMutation = useMutation({
    mutationFn: (managedBy: 'shipit' | 'porter') => switchAppManagement(appId!, managedBy),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['app', appId] });
    },
  });

  const setSecretMutation = useMutation({
    mutationFn: ({ key, value }: { key: string; value: string }) => setSecret(appId!, key, value),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['secrets', appId] });
      setShowSecretModal(false);
      setNewSecretKey('');
      setNewSecretValue('');
    },
  });

  const deleteSecretMutation = useMutation({
    mutationFn: (key: string) => deleteSecret(appId!, key),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['secrets', appId] });
    },
  });

  const updateAppMutation = useMutation({
    mutationFn: (data: UpdateAppRequest) => updateApp(appId!, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['app', appId] });
    },
  });

  const autoscalingMutation = useMutation({
    mutationFn: (config: HPAConfig) => setAutoscaling(appId!, config),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['autoscaling', appId] });
    },
  });

  const domainMutation = useMutation({
    mutationFn: (config: DomainConfig) => setDomain(appId!, config),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['domain', appId] });
      queryClient.invalidateQueries({ queryKey: ['app', appId] });
    },
  });

  const preDeployMutation = useMutation({
    mutationFn: (config: PreDeployHookConfig) => setPreDeployHook(appId!, config),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['predeploy', appId] });
      queryClient.invalidateQueries({ queryKey: ['app', appId] });
    },
  });

  // Sync env vars with app data
  useEffect(() => {
    if (app?.env_vars) {
      setEnvVars(app.env_vars);
    }
  }, [app?.env_vars]);

  // Stream logs when logs tab is active
  useEffect(() => {
    if (activeTab === 'logs' && appId) {
      setLogs([]);
      const es = streamLogs(appId);
      eventSourceRef.current = es;

      es.onmessage = (event) => {
        setLogs((prev) => [...prev.slice(-500), event.data]);
      };

      es.onerror = () => {
        es.close();
      };

      return () => {
        es.close();
      };
    }
  }, [activeTab, appId]);

  // Auto-scroll logs
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  const handleAddSecret = (e: React.FormEvent) => {
    e.preventDefault();
    if (newSecretKey && newSecretValue) {
      setSecretMutation.mutate({ key: newSecretKey, value: newSecretValue });
    }
  };

  const handleAddEnvVar = (e: React.FormEvent) => {
    e.preventDefault();
    if (newEnvKey && newEnvValue) {
      const updated = { ...envVars, [newEnvKey]: newEnvValue };
      updateAppMutation.mutate({ env_vars: updated });
      setNewEnvKey('');
      setNewEnvValue('');
    }
  };

  const handleUpdateEnvVar = (key: string) => {
    if (editingEnvValue !== envVars[key]) {
      const updated = { ...envVars, [key]: editingEnvValue };
      updateAppMutation.mutate({ env_vars: updated });
    }
    setEditingEnvKey(null);
    setEditingEnvValue('');
  };

  const handleDeleteEnvVar = (key: string) => {
    const updated = { ...envVars };
    delete updated[key];
    updateAppMutation.mutate({ env_vars: updated });
  };

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton height={200} rounded="lg" />
        <Skeleton height={48} rounded="lg" />
        <div className="grid gap-6 md:grid-cols-2">
          <Skeleton height={200} rounded="lg" />
          <Skeleton height={200} rounded="lg" />
        </div>
      </div>
    );
  }

  if (!app) {
    return (
      <Card className="p-8 text-center">
        <div className="w-16 h-16 mx-auto mb-4 bg-error-muted rounded-full flex items-center justify-center">
          <svg className="w-8 h-8 text-error" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <h3 className="text-lg font-medium text-text-primary mb-2">App not found</h3>
        <p className="text-text-secondary mb-4">The application you're looking for doesn't exist.</p>
        <Link to="/">
          <Button>Back to Dashboard</Button>
        </Link>
      </Card>
    );
  }

  return (
    <div>
      {/* Breadcrumb */}
      <nav className="mb-4 text-sm">
        <Link to="/" className="text-text-secondary hover:text-text-primary">Apps</Link>
        <span className="mx-2 text-text-muted">/</span>
        <span className="text-text-primary">{app.name}</span>
      </nav>

      {/* Header Card */}
      <Card className="mb-6" padding="lg">
        <div className="flex justify-between items-start">
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-bold text-text-primary">{app.name}</h1>
              <StatusBadge status={app.status} />
              {app.managed_by === 'porter' && (
                <span className="inline-flex items-center px-2 py-1 text-xs font-medium bg-blue-500/10 text-blue-400 rounded border border-blue-500/20">
                  Porter Managed
                </span>
              )}
            </div>
            <div className="flex items-center gap-3 mt-2 text-sm text-text-secondary">
              <span>{app.namespace}</span>
              <span className="text-text-muted">|</span>
              <span>v{app.current_revision}</span>
            </div>
            {app.status_message && (
              <p className="mt-2 text-sm text-error">{app.status_message}</p>
            )}
          </div>
          <div className="flex gap-2">
            {/* Porter-discovered apps can be switched to shipit management */}
            {app.porter_app_id && (
              <Button
                variant="secondary"
                onClick={() => switchMutation.mutate(app.managed_by === 'porter' ? 'shipit' : 'porter')}
                disabled={switchMutation.isPending}
                loading={switchMutation.isPending}
              >
                {app.managed_by === 'porter' ? 'Switch to Shipit' : 'Switch to Porter'}
              </Button>
            )}
            {app.managed_by !== 'porter' && (
              <>
                <Button
                  onClick={() => deployMutation.mutate()}
                  disabled={deployMutation.isPending}
                  loading={deployMutation.isPending}
                >
                  Deploy
                </Button>
                <Button
                  variant="secondary"
                  onClick={() => setRollbackConfirm({ revision_number: app.current_revision - 1 } as AppRevision)}
                  disabled={rollbackMutation.isPending || app.current_revision === 0}
                >
                  Rollback
                </Button>
                <Button
                  variant="danger"
                  onClick={() => setDeleteConfirm(true)}
                >
                  Delete
                </Button>
              </>
            )}
          </div>
        </div>

        {/* Pod Status */}
        {status && (
          <div className="mt-4 pt-4 border-t border-border">
            <p className="text-sm text-text-secondary">
              {status.ready_replicas}/{status.desired_replicas} pods ready
            </p>
            {status.pods && status.pods.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-2">
                {status.pods.map((pod) => (
                  <span
                    key={pod.name}
                    className={`text-xs px-2 py-1 rounded ${
                      pod.ready ? 'bg-success-muted text-success' : 'bg-warning-muted text-warning'
                    }`}
                    title={`${pod.name} - ${pod.phase} - ${pod.restarts} restarts`}
                  >
                    {pod.name.split('-').slice(-2).join('-')}
                  </span>
                ))}
              </div>
            )}
          </div>
        )}
      </Card>

      {/* Delete Confirmation */}
      <ConfirmModal
        open={deleteConfirm}
        onClose={() => setDeleteConfirm(false)}
        onConfirm={() => deleteMutation.mutate()}
        title="Delete Application"
        description={`Are you sure you want to delete "${app.name}"? This will remove the deployment from Kubernetes and cannot be undone.`}
        confirmText="Delete"
        variant="danger"
        loading={deleteMutation.isPending}
      />

      {/* Rollback Confirmation */}
      <ConfirmModal
        open={!!rollbackConfirm}
        onClose={() => setRollbackConfirm(null)}
        onConfirm={() => rollbackMutation.mutate(rollbackConfirm?.revision_number)}
        title="Rollback Application"
        description={`Rollback to revision ${rollbackConfirm?.revision_number}?`}
        confirmText="Rollback"
        variant="primary"
        loading={rollbackMutation.isPending}
      />

      {/* Tabs */}
      <div className="border-b border-border mb-6">
        <nav className="flex -mb-px overflow-x-auto">
          <Tab active={activeTab === 'overview'} onClick={() => setActiveTab('overview')}>Overview</Tab>
          <Tab active={activeTab === 'monitoring'} onClick={() => setActiveTab('monitoring')}>Monitoring</Tab>
          <Tab active={activeTab === 'env'} onClick={() => setActiveTab('env')}>Environment</Tab>
          <Tab active={activeTab === 'secrets'} onClick={() => setActiveTab('secrets')}>Secrets</Tab>
          <Tab active={activeTab === 'autoscaling'} onClick={() => setActiveTab('autoscaling')}>Autoscaling</Tab>
          <Tab active={activeTab === 'domain'} onClick={() => setActiveTab('domain')}>Domain</Tab>
          <Tab active={activeTab === 'hooks'} onClick={() => setActiveTab('hooks')}>Hooks</Tab>
          <Tab active={activeTab === 'revisions'} onClick={() => setActiveTab('revisions')}>Revisions</Tab>
          <Tab active={activeTab === 'logs'} onClick={() => setActiveTab('logs')}>Logs</Tab>
        </nav>
      </div>

      {/* Tab Content */}
      {activeTab === 'overview' && (
        <div className="grid gap-6 md:grid-cols-2">
          <Card>
            <h3 className="text-lg font-medium text-text-primary mb-4">Configuration</h3>
            <dl className="space-y-3">
              <div className="flex justify-between">
                <dt className="text-sm text-text-secondary">Image</dt>
                <dd className="text-sm text-text-primary font-mono truncate max-w-xs" title={app.image}>{app.image}</dd>
              </div>
              <div className="flex justify-between items-center">
                <dt className="text-sm text-text-secondary">Replicas</dt>
                <dd className="flex items-center gap-2">
                  <button
                    onClick={() => {
                      if (app.replicas > 1) {
                        updateAppMutation.mutate({ replicas: app.replicas - 1 });
                      }
                    }}
                    disabled={app.replicas <= 1 || updateAppMutation.isPending}
                    className="w-8 h-8 flex items-center justify-center rounded bg-surface-hover hover:bg-surface-active disabled:opacity-50 text-text-primary font-bold"
                  >
                    -
                  </button>
                  <span className="w-8 text-center text-sm font-medium text-text-primary">{app.replicas}</span>
                  <button
                    onClick={() => {
                      updateAppMutation.mutate({ replicas: app.replicas + 1 });
                    }}
                    disabled={updateAppMutation.isPending}
                    className="w-8 h-8 flex items-center justify-center rounded bg-surface-hover hover:bg-surface-active disabled:opacity-50 text-text-primary font-bold"
                  >
                    +
                  </button>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-text-secondary">Port</dt>
                <dd className="text-sm text-text-primary">{app.port || 'Not set'}</dd>
              </div>
            </dl>
          </Card>

          {/* Porter Management Toggle */}
          {app.porter_app_id && (
            <Card>
              <h3 className="text-lg font-medium text-text-primary mb-4">Management</h3>
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-sm font-medium text-text-primary">Managed By</p>
                    <p className="text-xs text-text-muted mt-1">
                      {app.managed_by === 'porter'
                        ? 'This app is currently managed by Porter. Switch to shipit to deploy and manage from here.'
                        : 'This app is managed by shipit. Switch back to Porter to use Porter dashboard.'}
                    </p>
                  </div>
                  <div className="flex items-center gap-3">
                    <span className={`text-sm font-medium ${app.managed_by === 'porter' ? 'text-text-muted' : 'text-accent'}`}>
                      shipit
                    </span>
                    <button
                      onClick={() => {
                        const newManagedBy = app.managed_by === 'porter' ? 'shipit' : 'porter';
                        switchMutation.mutate(newManagedBy);
                      }}
                      disabled={switchMutation.isPending}
                      className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-accent focus:ring-offset-2 focus:ring-offset-surface ${
                        app.managed_by === 'porter' ? 'bg-accent' : 'bg-surface-hover'
                      } ${switchMutation.isPending ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
                    >
                      <span
                        className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                          app.managed_by === 'porter' ? 'translate-x-6' : 'translate-x-1'
                        }`}
                      />
                    </button>
                    <span className={`text-sm font-medium ${app.managed_by === 'porter' ? 'text-accent' : 'text-text-muted'}`}>
                      Porter
                    </span>
                  </div>
                </div>
                {app.porter_app_url && (
                  <a
                    href={app.porter_app_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-sm text-accent hover:text-accent-hover transition-colors"
                  >
                    View in Porter Dashboard
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                    </svg>
                  </a>
                )}
              </div>
            </Card>
          )}

          <Card>
            <h3 className="text-lg font-medium text-text-primary mb-4">Resources</h3>
            <dl className="space-y-3">
              <div className="flex justify-between">
                <dt className="text-sm text-text-secondary">CPU Request</dt>
                <dd className="text-sm text-text-primary font-mono">{app.cpu_request}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-text-secondary">CPU Limit</dt>
                <dd className="text-sm text-text-primary font-mono">{app.cpu_limit}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-text-secondary">Memory Request</dt>
                <dd className="text-sm text-text-primary font-mono">{app.memory_request}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-text-secondary">Memory Limit</dt>
                <dd className="text-sm text-text-primary font-mono">{app.memory_limit}</dd>
              </div>
            </dl>
          </Card>

          {app.health_path && (
            <Card>
              <h3 className="text-lg font-medium text-text-primary mb-4">Health Checks</h3>
              <dl className="space-y-3">
                <div className="flex justify-between">
                  <dt className="text-sm text-text-secondary">Path</dt>
                  <dd className="text-sm text-text-primary font-mono">{app.health_path}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-text-secondary">Port</dt>
                  <dd className="text-sm text-text-primary">{app.health_port || app.port}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-text-secondary">Initial Delay</dt>
                  <dd className="text-sm text-text-primary">{app.health_initial_delay}s</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-text-secondary">Period</dt>
                  <dd className="text-sm text-text-primary">{app.health_period}s</dd>
                </div>
              </dl>
            </Card>
          )}
        </div>
      )}

      {activeTab === 'monitoring' && (
        <div className="space-y-6">
          {/* Pod Metrics */}
          <Card padding="none">
            <div className="p-4 border-b border-border">
              <h3 className="text-lg font-medium text-text-primary">Pod Metrics</h3>
              <p className="mt-1 text-sm text-text-secondary">
                Real-time CPU and memory usage from Kubernetes metrics-server.
              </p>
            </div>
            <div className="p-6">
              {!status?.pods?.length ? (
                <div className="text-center text-text-secondary py-8">
                  No pods running
                </div>
              ) : (
                <div className="space-y-4">
                  {status.pods.map((pod) => (
                    <div key={pod.name} className="border border-border rounded-lg p-4 bg-surface-hover">
                      <div className="flex items-center justify-between mb-3">
                        <div className="flex items-center gap-3">
                          <span className={`w-3 h-3 rounded-full ${pod.ready ? 'bg-success' : 'bg-warning'}`}></span>
                          <span className="font-medium text-sm text-text-primary">{pod.name}</span>
                        </div>
                        <div className="flex items-center gap-4 text-sm text-text-secondary">
                          <span>Phase: {pod.phase}</span>
                          <span className={pod.restarts > 0 ? 'text-warning font-medium' : ''}>
                            Restarts: {pod.restarts}
                          </span>
                          <span>Age: {pod.age}</span>
                        </div>
                      </div>

                      {/* CPU Usage */}
                      <div className="mb-3">
                        <div className="flex justify-between text-sm mb-1">
                          <span className="text-text-secondary">CPU</span>
                          <span className="font-mono text-text-primary">
                            {pod.cpu_usage || 'N/A'}
                            {pod.cpu_percent !== undefined && ` (${pod.cpu_percent}%)`}
                          </span>
                        </div>
                        <div className="w-full bg-surface-active rounded-full h-2">
                          <div
                            className={`h-2 rounded-full transition-all ${
                              (pod.cpu_percent || 0) > 80 ? 'bg-error' :
                              (pod.cpu_percent || 0) > 60 ? 'bg-warning' : 'bg-success'
                            }`}
                            style={{ width: `${Math.min(pod.cpu_percent || 0, 100)}%` }}
                          ></div>
                        </div>
                      </div>

                      {/* Memory Usage */}
                      <div>
                        <div className="flex justify-between text-sm mb-1">
                          <span className="text-text-secondary">Memory</span>
                          <span className="font-mono text-text-primary">
                            {pod.memory_usage || 'N/A'}
                            {pod.mem_percent !== undefined && ` (${pod.mem_percent}%)`}
                          </span>
                        </div>
                        <div className="w-full bg-surface-active rounded-full h-2">
                          <div
                            className={`h-2 rounded-full transition-all ${
                              (pod.mem_percent || 0) > 80 ? 'bg-error' :
                              (pod.mem_percent || 0) > 60 ? 'bg-warning' : 'bg-info'
                            }`}
                            style={{ width: `${Math.min(pod.mem_percent || 0, 100)}%` }}
                          ></div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
              <p className="mt-4 text-xs text-text-muted">
                Metrics refresh every 5 seconds. Requires metrics-server installed on the cluster.
              </p>
            </div>
          </Card>

          {/* Deployment History */}
          <Card padding="none">
            <div className="p-4 border-b border-border">
              <h3 className="text-lg font-medium text-text-primary">Deployment History</h3>
              <p className="mt-1 text-sm text-text-secondary">
                Recent deployments with success/failure status.
              </p>
            </div>
            {historyLoading ? (
              <div className="flex justify-center py-12">
                <Skeleton width={32} height={32} rounded="full" />
              </div>
            ) : !deploymentHistory?.length ? (
              <div className="p-8 text-center text-text-secondary">
                No deployment history yet.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-border">
                  <thead className="bg-surface-hover">
                    <tr>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Revision</th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Status</th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Image</th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Deployed At</th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Message</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {deploymentHistory.map((rev: AppRevision) => (
                      <tr key={rev.id} className={rev.revision_number === app.current_revision ? 'bg-accent-muted' : ''}>
                        <td className="px-6 py-4 text-sm text-text-primary">
                          v{rev.revision_number}
                          {rev.revision_number === app.current_revision && (
                            <span className="ml-2 text-xs text-accent">(current)</span>
                          )}
                        </td>
                        <td className="px-6 py-4">
                          <StatusBadge status={rev.deploy_status || 'unknown'} />
                        </td>
                        <td className="px-6 py-4 font-mono text-xs text-text-secondary max-w-xs truncate" title={rev.image}>
                          {rev.image}
                        </td>
                        <td className="px-6 py-4 text-sm text-text-secondary">
                          {rev.deployed_at ? new Date(rev.deployed_at).toLocaleString() : '-'}
                        </td>
                        <td className="px-6 py-4 text-sm text-text-secondary max-w-xs truncate" title={rev.deploy_message || ''}>
                          {rev.deploy_message || '-'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>
      )}

      {activeTab === 'env' && (
        <Card padding="none">
          <div className="p-4 border-b border-border">
            <h3 className="text-lg font-medium text-text-primary">Environment Variables</h3>
            <p className="mt-1 text-sm text-text-secondary">
              Configure environment variables for your application. Changes take effect on the next deploy.
            </p>
          </div>

          {/* Add new env var form */}
          <form onSubmit={handleAddEnvVar} className="p-4 bg-surface-hover border-b border-border">
            <div className="flex gap-3">
              <input
                type="text"
                value={newEnvKey}
                onChange={(e) => setNewEnvKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, ''))}
                placeholder="KEY_NAME"
                className="flex-1 px-3 py-2 border border-border rounded-md font-mono text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
              />
              <input
                type="text"
                value={newEnvValue}
                onChange={(e) => setNewEnvValue(e.target.value)}
                placeholder="value"
                className="flex-[2] px-3 py-2 border border-border rounded-md text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
              />
              <Button type="submit" disabled={!newEnvKey || !newEnvValue || updateAppMutation.isPending}>
                Add
              </Button>
            </div>
          </form>

          {Object.keys(envVars).length === 0 ? (
            <div className="p-8 text-center text-text-secondary">
              No environment variables configured
            </div>
          ) : (
            <table className="min-w-full divide-y divide-border">
              <thead className="bg-surface-hover">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase w-1/3">Key</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Value</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-text-secondary uppercase w-24">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {Object.entries(envVars).sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => (
                  <tr key={key}>
                    <td className="px-6 py-4 font-mono text-sm font-medium text-text-primary">{key}</td>
                    <td className="px-6 py-4">
                      {editingEnvKey === key ? (
                        <div className="flex gap-2">
                          <input
                            type="text"
                            value={editingEnvValue}
                            onChange={(e) => setEditingEnvValue(e.target.value)}
                            className="flex-1 px-2 py-1 border border-border rounded text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
                            autoFocus
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') handleUpdateEnvVar(key);
                              if (e.key === 'Escape') { setEditingEnvKey(null); setEditingEnvValue(''); }
                            }}
                          />
                          <Button size="sm" onClick={() => handleUpdateEnvVar(key)}>Save</Button>
                          <Button size="sm" variant="ghost" onClick={() => { setEditingEnvKey(null); setEditingEnvValue(''); }}>Cancel</Button>
                        </div>
                      ) : (
                        <code className="text-sm text-text-secondary bg-surface-hover px-2 py-1 rounded max-w-md truncate block">
                          {value}
                        </code>
                      )}
                    </td>
                    <td className="px-6 py-4 text-right space-x-2">
                      {editingEnvKey !== key && (
                        <>
                          <button
                            onClick={() => { setEditingEnvKey(key); setEditingEnvValue(value); }}
                            className="text-accent hover:text-accent-hover text-sm"
                          >
                            Edit
                          </button>
                          <button
                            onClick={() => handleDeleteEnvVar(key)}
                            className="text-error hover:opacity-80 text-sm"
                          >
                            Delete
                          </button>
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          <p className="p-4 text-sm text-text-muted border-t border-border">
            Redeploy the app after adding or updating environment variables for changes to take effect.
          </p>
        </Card>
      )}

      {activeTab === 'secrets' && (
        <Card padding="none">
          <div className="p-4 border-b border-border flex justify-between items-center">
            <h3 className="text-lg font-medium text-text-primary">Secrets</h3>
            <Button onClick={() => setShowSecretModal(true)}>Add Secret</Button>
          </div>
          {secrets?.length === 0 ? (
            <div className="p-8 text-center text-text-secondary">
              No secrets configured
            </div>
          ) : (
            <table className="min-w-full divide-y divide-border">
              <thead className="bg-surface-hover">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Key</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Updated</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-text-secondary uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {secrets?.map((secret: AppSecret) => (
                  <tr key={secret.key}>
                    <td className="px-6 py-4 font-mono text-sm text-text-primary">{secret.key}</td>
                    <td className="px-6 py-4 text-sm text-text-secondary">
                      {new Date(secret.updated_at).toLocaleDateString()}
                    </td>
                    <td className="px-6 py-4 text-right">
                      <button
                        onClick={() => deleteSecretMutation.mutate(secret.key)}
                        className="text-error hover:opacity-80 text-sm"
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <p className="p-4 text-sm text-text-muted border-t border-border">
            Redeploy the app after adding or updating secrets for changes to take effect.
          </p>
        </Card>
      )}

      {activeTab === 'autoscaling' && (
        <Card padding="none">
          <div className="p-4 border-b border-border">
            <h3 className="text-lg font-medium text-text-primary">Horizontal Pod Autoscaler</h3>
            <p className="mt-1 text-sm text-text-secondary">
              Automatically scale your app based on CPU or memory utilization.
            </p>
          </div>

          {autoscalingLoading ? (
            <div className="flex justify-center py-12">
              <Skeleton width={32} height={32} rounded="full" />
            </div>
          ) : (
            <div className="p-6">
              {autoscaling?.enabled && (
                <div className="mb-6 p-4 bg-success-muted border border-success/20 rounded-lg">
                  <div className="flex items-center gap-2 mb-2">
                    <span className="w-2 h-2 bg-success rounded-full"></span>
                    <span className="text-sm font-medium text-success">Autoscaling Active</span>
                  </div>
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
                    <div>
                      <span className="text-text-secondary">Current Replicas:</span>
                      <span className="ml-2 font-medium text-text-primary">{autoscaling.current_replicas}</span>
                    </div>
                    <div>
                      <span className="text-text-secondary">Desired:</span>
                      <span className="ml-2 font-medium text-text-primary">{autoscaling.desired_replicas}</span>
                    </div>
                  </div>
                </div>
              )}

              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  const formData = new FormData(e.currentTarget);
                  const config: HPAConfig = {
                    enabled: formData.get('enabled') === 'on',
                    min_replicas: parseInt(formData.get('min_replicas') as string) || 1,
                    max_replicas: parseInt(formData.get('max_replicas') as string) || 10,
                    target_cpu_percent: formData.get('target_cpu')
                      ? parseInt(formData.get('target_cpu') as string)
                      : undefined,
                    target_memory_percent: formData.get('target_memory')
                      ? parseInt(formData.get('target_memory') as string)
                      : undefined,
                  };
                  autoscalingMutation.mutate(config);
                }}
                className="space-y-6"
              >
                <div className="flex items-center gap-3">
                  <input
                    type="checkbox"
                    name="enabled"
                    id="hpa-enabled"
                    defaultChecked={autoscaling?.enabled}
                    className="h-4 w-4 rounded border-border text-accent focus:ring-accent"
                  />
                  <label htmlFor="hpa-enabled" className="text-sm font-medium text-text-primary">
                    Enable autoscaling
                  </label>
                </div>

                <div className="grid grid-cols-2 gap-6">
                  <Input
                    label="Minimum Replicas"
                    type="number"
                    name="min_replicas"
                    min={1}
                    max={100}
                    defaultValue={autoscaling?.min_replicas || 1}
                  />
                  <Input
                    label="Maximum Replicas"
                    type="number"
                    name="max_replicas"
                    min={1}
                    max={100}
                    defaultValue={autoscaling?.max_replicas || 10}
                  />
                </div>

                <div className="grid grid-cols-2 gap-6">
                  <Input
                    label="Target CPU Utilization (%)"
                    type="number"
                    name="target_cpu"
                    min={1}
                    max={100}
                    placeholder="e.g., 70"
                    defaultValue={autoscaling?.target_cpu_percent || ''}
                    helperText="Leave empty to disable CPU-based scaling"
                  />
                  <Input
                    label="Target Memory Utilization (%)"
                    type="number"
                    name="target_memory"
                    min={1}
                    max={100}
                    placeholder="e.g., 80"
                    defaultValue={autoscaling?.target_memory_percent || ''}
                    helperText="Leave empty to disable memory-based scaling"
                  />
                </div>

                <div className="flex items-center gap-4">
                  <Button type="submit" disabled={autoscalingMutation.isPending} loading={autoscalingMutation.isPending}>
                    Save Configuration
                  </Button>
                  {autoscalingMutation.isSuccess && (
                    <span className="text-sm text-success">Saved successfully!</span>
                  )}
                </div>
              </form>
            </div>
          )}
        </Card>
      )}

      {activeTab === 'domain' && (
        <Card padding="none">
          <div className="p-4 border-b border-border">
            <h3 className="text-lg font-medium text-text-primary">Custom Domain</h3>
            <p className="mt-1 text-sm text-text-secondary">
              Configure a custom domain for your application with automatic TLS certificates.
            </p>
          </div>

          {domainLoading || ingressLoading ? (
            <div className="flex justify-center py-12">
              <Skeleton width={32} height={32} rounded="full" />
            </div>
          ) : (
            <div className="p-6">
              {clusterIngress?.available && clusterIngress?.base_domain && app && (
                <div className="mb-6 p-4 bg-success-muted border border-success/20 rounded-lg">
                  <h4 className="text-sm font-medium text-success mb-2 flex items-center gap-2">
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
                    </svg>
                    Default App URL
                  </h4>
                  <a
                    href={`https://${app.name}.${clusterIngress.base_domain}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="block text-sm text-text-primary hover:text-accent font-mono break-all"
                  >
                    https://{app.name}.{clusterIngress.base_domain}
                  </a>
                </div>
              )}

              {domainStatus?.domain && (
                <div className={`mb-6 p-4 rounded-lg border ${
                  domainStatus.domain_status === 'active'
                    ? 'bg-success-muted border-success/20'
                    : 'bg-warning-muted border-warning/20'
                }`}>
                  <div className="flex items-center gap-2 mb-2">
                    <span className={`w-2 h-2 rounded-full ${
                      domainStatus.domain_status === 'active' ? 'bg-success' : 'bg-warning'
                    }`}></span>
                    <span className={`text-sm font-medium ${
                      domainStatus.domain_status === 'active' ? 'text-success' : 'text-warning'
                    }`}>
                      {domainStatus.domain_status === 'active' ? 'Domain Active' : 'Provisioning...'}
                    </span>
                  </div>
                  <a
                    href={`https://${domainStatus.domain}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-accent hover:text-accent-hover font-medium"
                  >
                    {domainStatus.domain}
                  </a>
                </div>
              )}

              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  const formData = new FormData(e.currentTarget);
                  const domain = formData.get('domain') as string;
                  domainMutation.mutate({
                    domain: domain.trim() || undefined,
                  });
                }}
                className="space-y-6"
              >
                <Input
                  label="Domain Name"
                  name="domain"
                  placeholder="app.example.com"
                  defaultValue={domainStatus?.domain || ''}
                  helperText="Enter your custom domain (e.g., app.example.com). Leave empty to remove domain."
                />

                <div className="flex items-center gap-4">
                  <Button type="submit" disabled={domainMutation.isPending} loading={domainMutation.isPending}>
                    Save Domain
                  </Button>
                  {domainMutation.isSuccess && (
                    <span className="text-sm text-success">Saved successfully!</span>
                  )}
                </div>
              </form>
            </div>
          )}
        </Card>
      )}

      {activeTab === 'hooks' && (
        <Card padding="none">
          <div className="p-4 border-b border-border">
            <h3 className="text-lg font-medium text-text-primary">Pre-deploy Hooks</h3>
            <p className="mt-1 text-sm text-text-secondary">
              Run commands before deployment, such as database migrations or cache warming.
            </p>
          </div>

          {hookLoading ? (
            <div className="flex justify-center py-12">
              <Skeleton width={32} height={32} rounded="full" />
            </div>
          ) : (
            <div className="p-6">
              {preDeployHook?.pre_deploy_command && (
                <div className="mb-6 p-4 bg-accent-muted border border-accent/20 rounded-lg">
                  <div className="flex items-center gap-2 mb-2">
                    <span className="w-2 h-2 bg-accent rounded-full"></span>
                    <span className="text-sm font-medium text-accent">Pre-deploy Hook Configured</span>
                  </div>
                  <code className="text-sm font-mono text-text-primary bg-surface-hover px-2 py-1 rounded">
                    {preDeployHook.pre_deploy_command}
                  </code>
                </div>
              )}

              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  const formData = new FormData(e.currentTarget);
                  const command = (formData.get('command') as string)?.trim();
                  preDeployMutation.mutate({
                    command: command || undefined,
                  });
                }}
                className="space-y-6"
              >
                <Input
                  label="Pre-deploy Command"
                  name="command"
                  placeholder="e.g., python manage.py migrate"
                  defaultValue={preDeployHook?.pre_deploy_command || ''}
                  helperText="This command runs in a temporary container before each deployment. Leave empty to disable."
                />

                <div className="flex items-center gap-4">
                  <Button type="submit" disabled={preDeployMutation.isPending} loading={preDeployMutation.isPending}>
                    Save Hook
                  </Button>
                  {preDeployMutation.isSuccess && (
                    <span className="text-sm text-success">Saved successfully!</span>
                  )}
                </div>
              </form>
            </div>
          )}
        </Card>
      )}

      {activeTab === 'revisions' && (
        <Card padding="none">
          <div className="p-4 border-b border-border">
            <h3 className="text-lg font-medium text-text-primary">Deployment History</h3>
          </div>
          {revisions?.length === 0 ? (
            <div className="p-8 text-center text-text-secondary">
              No revisions yet. Deploy the app to create the first revision.
            </div>
          ) : (
            <table className="min-w-full divide-y divide-border">
              <thead className="bg-surface-hover">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Revision</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Image</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Replicas</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase">Created</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-text-secondary uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {revisions?.map((rev: AppRevision) => (
                  <tr key={rev.id} className={rev.revision_number === app.current_revision ? 'bg-accent-muted' : ''}>
                    <td className="px-6 py-4 text-sm text-text-primary">
                      v{rev.revision_number}
                      {rev.revision_number === app.current_revision && (
                        <span className="ml-2 text-xs text-accent">(current)</span>
                      )}
                    </td>
                    <td className="px-6 py-4 font-mono text-sm text-text-secondary max-w-xs truncate" title={rev.image}>
                      {rev.image}
                    </td>
                    <td className="px-6 py-4 text-sm text-text-secondary">{rev.replicas}</td>
                    <td className="px-6 py-4 text-sm text-text-secondary">
                      {new Date(rev.created_at).toLocaleString()}
                    </td>
                    <td className="px-6 py-4 text-right">
                      {rev.revision_number !== app.current_revision && (
                        <button
                          onClick={() => setRollbackConfirm(rev)}
                          disabled={rollbackMutation.isPending}
                          className="text-warning hover:opacity-80 text-sm disabled:opacity-50"
                        >
                          Rollback
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      )}

      {activeTab === 'logs' && (
        <div className="bg-background rounded-lg p-4 h-96 overflow-y-auto font-mono text-sm border border-border">
          {logs.length === 0 ? (
            <p className="text-text-muted">Waiting for logs...</p>
          ) : (
            logs.map((line, i) => (
              <div key={i} className="text-success whitespace-pre-wrap">
                {line}
              </div>
            ))
          )}
          <div ref={logsEndRef} />
        </div>
      )}

      {/* Add Secret Modal */}
      <Modal
        open={showSecretModal}
        onClose={() => setShowSecretModal(false)}
        title="Add Secret"
        size="sm"
      >
        <form onSubmit={handleAddSecret} className="space-y-4">
          <Input
            label="Key"
            value={newSecretKey}
            onChange={(e) => setNewSecretKey(e.target.value)}
            placeholder="DATABASE_URL"
            required
          />
          <Input
            label="Value"
            type="password"
            value={newSecretValue}
            onChange={(e) => setNewSecretValue(e.target.value)}
            required
          />
          <div className="flex justify-end gap-3">
            <Button variant="ghost" type="button" onClick={() => setShowSecretModal(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={setSecretMutation.isPending} loading={setSecretMutation.isPending}>
              Save
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
