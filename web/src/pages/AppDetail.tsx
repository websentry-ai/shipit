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
} from '../api/client';
import type { AppRevision, AppSecret, UpdateAppRequest, HPAConfig, DomainConfig } from '../types';

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    running: 'bg-green-100 text-green-800',
    pending: 'bg-yellow-100 text-yellow-800',
    failed: 'bg-red-100 text-red-800',
    created: 'bg-blue-100 text-blue-800',
    deploying: 'bg-purple-100 text-purple-800',
  };
  return (
    <span className={`px-2 py-1 rounded-full text-xs font-medium ${colors[status] || 'bg-gray-100 text-gray-800'}`}>
      {status}
    </span>
  );
}

function Tab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`px-4 py-2 text-sm font-medium border-b-2 ${
        active
          ? 'border-indigo-500 text-indigo-600'
          : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
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
  const [activeTab, setActiveTab] = useState<'overview' | 'env' | 'secrets' | 'autoscaling' | 'domain' | 'revisions' | 'logs'>('overview');
  const [logs, setLogs] = useState<string[]>([]);
  const [showSecretModal, setShowSecretModal] = useState(false);
  const [newSecretKey, setNewSecretKey] = useState('');
  const [newSecretValue, setNewSecretValue] = useState('');
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
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteApp(appId!),
    onSuccess: () => {
      navigate(-1);
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

  const handleRollback = (revision?: AppRevision) => {
    const msg = revision
      ? `Rollback to revision ${revision.revision_number} (${revision.image})?`
      : 'Rollback to previous revision?';
    if (confirm(msg)) {
      rollbackMutation.mutate(revision?.revision_number);
    }
  };

  const handleDelete = () => {
    if (confirm(`Delete app "${app?.name}"? This will remove the deployment from Kubernetes.`)) {
      deleteMutation.mutate();
    }
  };

  const handleAddSecret = (e: React.FormEvent) => {
    e.preventDefault();
    if (newSecretKey && newSecretValue) {
      setSecretMutation.mutate({ key: newSecretKey, value: newSecretValue });
    }
  };

  const handleDeleteSecret = (key: string) => {
    if (confirm(`Delete secret "${key}"? You'll need to redeploy for changes to take effect.`)) {
      deleteSecretMutation.mutate(key);
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
    if (confirm(`Delete environment variable "${key}"? You'll need to redeploy for changes to take effect.`)) {
      const updated = { ...envVars };
      delete updated[key];
      updateAppMutation.mutate({ env_vars: updated });
    }
  };

  if (isLoading) {
    return (
      <div className="flex justify-center py-12">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
      </div>
    );
  }

  if (!app) {
    return (
      <div className="bg-red-50 border border-red-200 rounded-lg p-4">
        <p className="text-red-700">App not found</p>
      </div>
    );
  }

  return (
    <div>
      {/* Breadcrumb */}
      <nav className="mb-4 text-sm">
        <Link to="/" className="text-gray-500 hover:text-gray-700">Projects</Link>
        <span className="mx-2 text-gray-400">/</span>
        <span className="text-gray-500">...</span>
        <span className="mx-2 text-gray-400">/</span>
        <span className="text-gray-900">{app.name}</span>
      </nav>

      {/* Header */}
      <div className="bg-white rounded-lg border border-gray-200 p-6 mb-6">
        <div className="flex justify-between items-start">
          <div>
            <h1 className="text-2xl font-bold text-gray-900">{app.name}</h1>
            <div className="flex items-center gap-3 mt-2">
              <StatusBadge status={app.status} />
              <span className="text-sm text-gray-500">{app.namespace}</span>
              <span className="text-sm text-gray-500">v{app.current_revision}</span>
            </div>
            {app.status_message && (
              <p className="mt-2 text-sm text-red-600">{app.status_message}</p>
            )}
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => deployMutation.mutate()}
              disabled={deployMutation.isPending}
              className="px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700 disabled:opacity-50"
            >
              {deployMutation.isPending ? 'Deploying...' : 'Deploy'}
            </button>
            <button
              onClick={() => handleRollback()}
              disabled={rollbackMutation.isPending || app.current_revision === 0}
              className="px-4 py-2 bg-yellow-600 text-white rounded-md hover:bg-yellow-700 disabled:opacity-50"
            >
              Rollback
            </button>
            <button
              onClick={handleDelete}
              className="px-4 py-2 bg-red-600 text-white rounded-md hover:bg-red-700"
            >
              Delete
            </button>
          </div>
        </div>

        {/* Pod Status */}
        {status && (
          <div className="mt-4 pt-4 border-t border-gray-200">
            <p className="text-sm text-gray-600">
              {status.ready_replicas}/{status.desired_replicas} pods ready
            </p>
            {status.pods && status.pods.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-2">
                {status.pods.map((pod) => (
                  <span
                    key={pod.name}
                    className={`text-xs px-2 py-1 rounded ${
                      pod.ready ? 'bg-green-100 text-green-800' : 'bg-yellow-100 text-yellow-800'
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
      </div>

      {/* Tabs */}
      <div className="border-b border-gray-200 mb-6">
        <nav className="flex -mb-px">
          <Tab active={activeTab === 'overview'} onClick={() => setActiveTab('overview')}>Overview</Tab>
          <Tab active={activeTab === 'env'} onClick={() => setActiveTab('env')}>Environment</Tab>
          <Tab active={activeTab === 'secrets'} onClick={() => setActiveTab('secrets')}>Secrets</Tab>
          <Tab active={activeTab === 'autoscaling'} onClick={() => setActiveTab('autoscaling')}>Autoscaling</Tab>
          <Tab active={activeTab === 'domain'} onClick={() => setActiveTab('domain')}>Domain</Tab>
          <Tab active={activeTab === 'revisions'} onClick={() => setActiveTab('revisions')}>Revisions</Tab>
          <Tab active={activeTab === 'logs'} onClick={() => setActiveTab('logs')}>Logs</Tab>
        </nav>
      </div>

      {/* Tab Content */}
      {activeTab === 'overview' && (
        <div className="grid gap-6 md:grid-cols-2">
          <div className="bg-white rounded-lg border border-gray-200 p-6">
            <h3 className="text-lg font-medium text-gray-900 mb-4">Configuration</h3>
            <dl className="space-y-3">
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">Image</dt>
                <dd className="text-sm text-gray-900 font-mono truncate max-w-xs" title={app.image}>{app.image}</dd>
              </div>
              <div className="flex justify-between items-center">
                <dt className="text-sm text-gray-500">Replicas</dt>
                <dd className="flex items-center gap-2">
                  <button
                    onClick={() => {
                      if (app.replicas > 1) {
                        updateAppMutation.mutate({ replicas: app.replicas - 1 });
                      }
                    }}
                    disabled={app.replicas <= 1 || updateAppMutation.isPending}
                    className="w-8 h-8 flex items-center justify-center rounded bg-gray-100 hover:bg-gray-200 disabled:opacity-50 text-gray-700 font-bold"
                  >
                    −
                  </button>
                  <span className="w-8 text-center text-sm font-medium">{app.replicas}</span>
                  <button
                    onClick={() => {
                      updateAppMutation.mutate({ replicas: app.replicas + 1 });
                    }}
                    disabled={updateAppMutation.isPending}
                    className="w-8 h-8 flex items-center justify-center rounded bg-gray-100 hover:bg-gray-200 disabled:opacity-50 text-gray-700 font-bold"
                  >
                    +
                  </button>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">Port</dt>
                <dd className="text-sm text-gray-900">{app.port || 'Not set'}</dd>
              </div>
            </dl>
          </div>

          <div className="bg-white rounded-lg border border-gray-200 p-6">
            <h3 className="text-lg font-medium text-gray-900 mb-4">Resources</h3>
            <dl className="space-y-3">
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">CPU Request</dt>
                <dd className="text-sm text-gray-900 font-mono">{app.cpu_request}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">CPU Limit</dt>
                <dd className="text-sm text-gray-900 font-mono">{app.cpu_limit}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">Memory Request</dt>
                <dd className="text-sm text-gray-900 font-mono">{app.memory_request}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">Memory Limit</dt>
                <dd className="text-sm text-gray-900 font-mono">{app.memory_limit}</dd>
              </div>
            </dl>
          </div>

          {app.health_path && (
            <div className="bg-white rounded-lg border border-gray-200 p-6">
              <h3 className="text-lg font-medium text-gray-900 mb-4">Health Checks</h3>
              <dl className="space-y-3">
                <div className="flex justify-between">
                  <dt className="text-sm text-gray-500">Path</dt>
                  <dd className="text-sm text-gray-900 font-mono">{app.health_path}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-gray-500">Port</dt>
                  <dd className="text-sm text-gray-900">{app.health_port || app.port}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-gray-500">Initial Delay</dt>
                  <dd className="text-sm text-gray-900">{app.health_initial_delay}s</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-gray-500">Period</dt>
                  <dd className="text-sm text-gray-900">{app.health_period}s</dd>
                </div>
              </dl>
            </div>
          )}
        </div>
      )}

      {activeTab === 'env' && (
        <div className="bg-white rounded-lg border border-gray-200">
          <div className="p-4 border-b border-gray-200">
            <h3 className="text-lg font-medium text-gray-900">Environment Variables</h3>
            <p className="mt-1 text-sm text-gray-500">
              Configure environment variables for your application. Changes take effect on the next deploy.
            </p>
          </div>

          {/* Add new env var form */}
          <form onSubmit={handleAddEnvVar} className="p-4 bg-gray-50 border-b border-gray-200">
            <div className="flex gap-3">
              <input
                type="text"
                value={newEnvKey}
                onChange={(e) => setNewEnvKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, ''))}
                placeholder="KEY_NAME"
                className="flex-1 px-3 py-2 border border-gray-300 rounded-md font-mono text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
              <input
                type="text"
                value={newEnvValue}
                onChange={(e) => setNewEnvValue(e.target.value)}
                placeholder="value"
                className="flex-[2] px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
              <button
                type="submit"
                disabled={!newEnvKey || !newEnvValue || updateAppMutation.isPending}
                className="px-4 py-2 bg-indigo-600 text-white text-sm rounded-md hover:bg-indigo-700 disabled:opacity-50"
              >
                Add
              </button>
            </div>
          </form>

          {Object.keys(envVars).length === 0 ? (
            <div className="p-8 text-center text-gray-500">
              No environment variables configured
            </div>
          ) : (
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase w-1/3">Key</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Value</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase w-24">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {Object.entries(envVars).sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => (
                  <tr key={key}>
                    <td className="px-6 py-4 font-mono text-sm font-medium text-gray-900">{key}</td>
                    <td className="px-6 py-4">
                      {editingEnvKey === key ? (
                        <div className="flex gap-2">
                          <input
                            type="text"
                            value={editingEnvValue}
                            onChange={(e) => setEditingEnvValue(e.target.value)}
                            className="flex-1 px-2 py-1 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                            autoFocus
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') handleUpdateEnvVar(key);
                              if (e.key === 'Escape') { setEditingEnvKey(null); setEditingEnvValue(''); }
                            }}
                          />
                          <button
                            onClick={() => handleUpdateEnvVar(key)}
                            className="px-2 py-1 bg-green-600 text-white text-xs rounded hover:bg-green-700"
                          >
                            Save
                          </button>
                          <button
                            onClick={() => { setEditingEnvKey(null); setEditingEnvValue(''); }}
                            className="px-2 py-1 text-gray-600 hover:bg-gray-100 text-xs rounded"
                          >
                            Cancel
                          </button>
                        </div>
                      ) : (
                        <code className="text-sm text-gray-700 bg-gray-100 px-2 py-1 rounded max-w-md truncate block">
                          {value}
                        </code>
                      )}
                    </td>
                    <td className="px-6 py-4 text-right space-x-2">
                      {editingEnvKey !== key && (
                        <>
                          <button
                            onClick={() => { setEditingEnvKey(key); setEditingEnvValue(value); }}
                            className="text-indigo-600 hover:text-indigo-900 text-sm"
                          >
                            Edit
                          </button>
                          <button
                            onClick={() => handleDeleteEnvVar(key)}
                            className="text-red-600 hover:text-red-900 text-sm"
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

          {updateAppMutation.isPending && (
            <div className="p-4 text-sm text-indigo-600 border-t border-gray-200 flex items-center gap-2">
              <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-indigo-600"></div>
              Saving changes...
            </div>
          )}
          {updateAppMutation.isError && (
            <div className="p-4 text-sm text-red-600 border-t border-gray-200">
              Error saving changes. Please try again.
            </div>
          )}
          <p className="p-4 text-sm text-gray-500 border-t border-gray-200">
            Redeploy the app after adding or updating environment variables for changes to take effect.
          </p>
        </div>
      )}

      {activeTab === 'secrets' && (
        <div className="bg-white rounded-lg border border-gray-200">
          <div className="p-4 border-b border-gray-200 flex justify-between items-center">
            <h3 className="text-lg font-medium text-gray-900">Secrets</h3>
            <button
              onClick={() => setShowSecretModal(true)}
              className="px-3 py-1.5 bg-indigo-600 text-white text-sm rounded-md hover:bg-indigo-700"
            >
              Add Secret
            </button>
          </div>
          {secrets?.length === 0 ? (
            <div className="p-8 text-center text-gray-500">
              No secrets configured
            </div>
          ) : (
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Key</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Updated</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {secrets?.map((secret: AppSecret) => (
                  <tr key={secret.key}>
                    <td className="px-6 py-4 font-mono text-sm">{secret.key}</td>
                    <td className="px-6 py-4 text-sm text-gray-500">
                      {new Date(secret.updated_at).toLocaleDateString()}
                    </td>
                    <td className="px-6 py-4 text-right">
                      <button
                        onClick={() => handleDeleteSecret(secret.key)}
                        className="text-red-600 hover:text-red-900 text-sm"
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <p className="p-4 text-sm text-gray-500 border-t border-gray-200">
            Redeploy the app after adding or updating secrets for changes to take effect.
          </p>
        </div>
      )}

      {activeTab === 'autoscaling' && (
        <div className="bg-white rounded-lg border border-gray-200">
          <div className="p-4 border-b border-gray-200">
            <h3 className="text-lg font-medium text-gray-900">Horizontal Pod Autoscaler</h3>
            <p className="mt-1 text-sm text-gray-500">
              Automatically scale your app based on CPU or memory utilization.
            </p>
          </div>

          {autoscalingLoading ? (
            <div className="flex justify-center py-12">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
            </div>
          ) : (
            <div className="p-6">
              {/* Current Status */}
              {autoscaling?.enabled && (
                <div className="mb-6 p-4 bg-green-50 border border-green-200 rounded-lg">
                  <div className="flex items-center gap-2 mb-2">
                    <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                    <span className="text-sm font-medium text-green-800">Autoscaling Active</span>
                  </div>
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
                    <div>
                      <span className="text-gray-500">Current Replicas:</span>
                      <span className="ml-2 font-medium">{autoscaling.current_replicas}</span>
                    </div>
                    <div>
                      <span className="text-gray-500">Desired:</span>
                      <span className="ml-2 font-medium">{autoscaling.desired_replicas}</span>
                    </div>
                    {autoscaling.current_cpu_percent !== undefined && (
                      <div>
                        <span className="text-gray-500">CPU Usage:</span>
                        <span className="ml-2 font-medium">{autoscaling.current_cpu_percent}%</span>
                      </div>
                    )}
                    {autoscaling.current_memory_percent !== undefined && (
                      <div>
                        <span className="text-gray-500">Memory Usage:</span>
                        <span className="ml-2 font-medium">{autoscaling.current_memory_percent}%</span>
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* Configuration Form */}
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
                {/* Enable Toggle */}
                <div className="flex items-center gap-3">
                  <input
                    type="checkbox"
                    name="enabled"
                    id="hpa-enabled"
                    defaultChecked={autoscaling?.enabled}
                    className="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
                  />
                  <label htmlFor="hpa-enabled" className="text-sm font-medium text-gray-700">
                    Enable autoscaling
                  </label>
                </div>

                {/* Replica Range */}
                <div className="grid grid-cols-2 gap-6">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">
                      Minimum Replicas
                    </label>
                    <input
                      type="number"
                      name="min_replicas"
                      min="1"
                      max="100"
                      defaultValue={autoscaling?.min_replicas || 1}
                      className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">
                      Maximum Replicas
                    </label>
                    <input
                      type="number"
                      name="max_replicas"
                      min="1"
                      max="100"
                      defaultValue={autoscaling?.max_replicas || 10}
                      className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                    />
                  </div>
                </div>

                {/* Target Metrics */}
                <div className="grid grid-cols-2 gap-6">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">
                      Target CPU Utilization (%)
                    </label>
                    <input
                      type="number"
                      name="target_cpu"
                      min="1"
                      max="100"
                      placeholder="e.g., 70"
                      defaultValue={autoscaling?.target_cpu_percent || ''}
                      className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                    />
                    <p className="mt-1 text-xs text-gray-500">Leave empty to disable CPU-based scaling</p>
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">
                      Target Memory Utilization (%)
                    </label>
                    <input
                      type="number"
                      name="target_memory"
                      min="1"
                      max="100"
                      placeholder="e.g., 80"
                      defaultValue={autoscaling?.target_memory_percent || ''}
                      className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                    />
                    <p className="mt-1 text-xs text-gray-500">Leave empty to disable memory-based scaling</p>
                  </div>
                </div>

                {/* Submit Button */}
                <div className="flex items-center gap-4">
                  <button
                    type="submit"
                    disabled={autoscalingMutation.isPending}
                    className="px-4 py-2 bg-indigo-600 text-white text-sm rounded-md hover:bg-indigo-700 disabled:opacity-50"
                  >
                    {autoscalingMutation.isPending ? 'Saving...' : 'Save Configuration'}
                  </button>
                  {autoscalingMutation.isSuccess && (
                    <span className="text-sm text-green-600">Saved successfully!</span>
                  )}
                  {autoscalingMutation.isError && (
                    <span className="text-sm text-red-600">Error saving configuration</span>
                  )}
                </div>
              </form>

              <p className="mt-6 text-sm text-gray-500 border-t border-gray-200 pt-4">
                Note: HPA requires resource requests (CPU/Memory) to be set on your app. Autoscaling changes take effect immediately without requiring a redeploy.
              </p>
            </div>
          )}
        </div>
      )}

      {activeTab === 'domain' && (
        <div className="bg-white rounded-lg border border-gray-200">
          <div className="p-4 border-b border-gray-200">
            <h3 className="text-lg font-medium text-gray-900">Custom Domain</h3>
            <p className="mt-1 text-sm text-gray-500">
              Configure a custom domain for your application with automatic TLS certificates.
            </p>
          </div>

          {domainLoading ? (
            <div className="flex justify-center py-12">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
            </div>
          ) : (
            <div className="p-6">
              {/* Current Status */}
              {domainStatus?.domain && (
                <div className={`mb-6 p-4 rounded-lg border ${
                  domainStatus.domain_status === 'active'
                    ? 'bg-green-50 border-green-200'
                    : 'bg-yellow-50 border-yellow-200'
                }`}>
                  <div className="flex items-center gap-2 mb-2">
                    <span className={`w-2 h-2 rounded-full ${
                      domainStatus.domain_status === 'active' ? 'bg-green-500' : 'bg-yellow-500'
                    }`}></span>
                    <span className={`text-sm font-medium ${
                      domainStatus.domain_status === 'active' ? 'text-green-800' : 'text-yellow-800'
                    }`}>
                      {domainStatus.domain_status === 'active' ? 'Domain Active' : 'Provisioning...'}
                    </span>
                  </div>
                  <div className="space-y-2 text-sm">
                    <div>
                      <span className="text-gray-500">Domain:</span>
                      <a
                        href={`https://${domainStatus.domain}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="ml-2 text-indigo-600 hover:text-indigo-800 font-medium"
                      >
                        {domainStatus.domain}
                      </a>
                    </div>
                    {domainStatus.ingress && (
                      <>
                        <div>
                          <span className="text-gray-500">TLS Enabled:</span>
                          <span className="ml-2 font-medium">
                            {domainStatus.ingress.tls_enabled ? 'Yes' : 'No'}
                          </span>
                        </div>
                        {domainStatus.ingress.load_balancer && (
                          <div>
                            <span className="text-gray-500">Load Balancer:</span>
                            <span className="ml-2 font-mono text-xs">
                              {domainStatus.ingress.load_balancer}
                            </span>
                          </div>
                        )}
                      </>
                    )}
                  </div>
                </div>
              )}

              {/* Configuration Form */}
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
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">
                    Domain Name
                  </label>
                  <input
                    type="text"
                    name="domain"
                    placeholder="app.example.com"
                    defaultValue={domainStatus?.domain || ''}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    Enter your custom domain (e.g., app.example.com). Leave empty to remove domain.
                  </p>
                </div>

                {/* DNS Instructions */}
                {domainStatus?.ingress?.load_balancer && (
                  <div className="p-4 bg-blue-50 border border-blue-200 rounded-lg">
                    <h4 className="text-sm font-medium text-blue-800 mb-2">DNS Configuration</h4>
                    <p className="text-sm text-blue-700 mb-2">
                      Point your domain to the load balancer by creating a CNAME record:
                    </p>
                    <code className="block text-xs bg-blue-100 p-2 rounded font-mono">
                      {domainStatus.domain} CNAME {domainStatus.ingress.load_balancer}
                    </code>
                  </div>
                )}

                {/* Submit Button */}
                <div className="flex items-center gap-4">
                  <button
                    type="submit"
                    disabled={domainMutation.isPending}
                    className="px-4 py-2 bg-indigo-600 text-white text-sm rounded-md hover:bg-indigo-700 disabled:opacity-50"
                  >
                    {domainMutation.isPending ? 'Saving...' : 'Save Domain'}
                  </button>
                  {domainStatus?.domain && (
                    <button
                      type="button"
                      onClick={() => {
                        if (confirm('Remove custom domain?')) {
                          domainMutation.mutate({ domain: undefined });
                        }
                      }}
                      disabled={domainMutation.isPending}
                      className="px-4 py-2 text-red-600 hover:bg-red-50 text-sm rounded-md"
                    >
                      Remove Domain
                    </button>
                  )}
                  {domainMutation.isSuccess && (
                    <span className="text-sm text-green-600">Saved successfully!</span>
                  )}
                  {domainMutation.isError && (
                    <span className="text-sm text-red-600">Error saving domain configuration</span>
                  )}
                </div>
              </form>

              <div className="mt-6 border-t border-gray-200 pt-4">
                <h4 className="text-sm font-medium text-gray-700 mb-2">Prerequisites</h4>
                <ul className="text-sm text-gray-500 list-disc list-inside space-y-1">
                  <li>nginx-ingress controller must be installed on the cluster</li>
                  <li>cert-manager must be installed for automatic TLS certificates</li>
                  <li>DNS must be configured to point to the cluster's load balancer</li>
                </ul>
              </div>
            </div>
          )}
        </div>
      )}

      {activeTab === 'revisions' && (
        <div className="bg-white rounded-lg border border-gray-200">
          <div className="p-4 border-b border-gray-200">
            <h3 className="text-lg font-medium text-gray-900">Deployment History</h3>
          </div>
          {revisions?.length === 0 ? (
            <div className="p-8 text-center text-gray-500">
              No revisions yet. Deploy the app to create the first revision.
            </div>
          ) : (
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Revision</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Image</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Replicas</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {revisions?.map((rev: AppRevision) => (
                  <tr key={rev.id} className={rev.revision_number === app.current_revision ? 'bg-indigo-50' : ''}>
                    <td className="px-6 py-4 text-sm">
                      v{rev.revision_number}
                      {rev.revision_number === app.current_revision && (
                        <span className="ml-2 text-xs text-indigo-600">(current)</span>
                      )}
                    </td>
                    <td className="px-6 py-4 font-mono text-sm max-w-xs truncate" title={rev.image}>
                      {rev.image}
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-500">{rev.replicas}</td>
                    <td className="px-6 py-4 text-sm text-gray-500">
                      {new Date(rev.created_at).toLocaleString()}
                    </td>
                    <td className="px-6 py-4 text-right">
                      {rev.revision_number !== app.current_revision && (
                        <button
                          onClick={() => handleRollback(rev)}
                          disabled={rollbackMutation.isPending}
                          className="text-yellow-600 hover:text-yellow-900 text-sm disabled:opacity-50"
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
        </div>
      )}

      {activeTab === 'logs' && (
        <div className="bg-gray-900 rounded-lg p-4 h-96 overflow-y-auto font-mono text-sm">
          {logs.length === 0 ? (
            <p className="text-gray-500">Waiting for logs...</p>
          ) : (
            logs.map((line, i) => (
              <div key={i} className="text-green-400 whitespace-pre-wrap">
                {line}
              </div>
            ))
          )}
          <div ref={logsEndRef} />
        </div>
      )}

      {/* Add Secret Modal */}
      {showSecretModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg p-6 w-full max-w-md">
            <h2 className="text-lg font-semibold mb-4">Add Secret</h2>
            <form onSubmit={handleAddSecret} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Key</label>
                <input
                  type="text"
                  value={newSecretKey}
                  onChange={(e) => setNewSecretKey(e.target.value)}
                  placeholder="DATABASE_URL"
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Value</label>
                <input
                  type="password"
                  value={newSecretValue}
                  onChange={(e) => setNewSecretValue(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  required
                />
              </div>
              <div className="flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setShowSecretModal(false)}
                  className="px-4 py-2 text-gray-600 hover:bg-gray-100 rounded-md"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={setSecretMutation.isPending}
                  className="px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700 disabled:opacity-50"
                >
                  {setSecretMutation.isPending ? 'Saving...' : 'Save'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
