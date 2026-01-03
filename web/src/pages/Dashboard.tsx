import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  listProjects,
  listClusters,
  listApps,
  deployApp,
  deleteApp,
  createApp,
} from '../api/client';
import type { Cluster, App, CreateAppRequest } from '../types';

interface AppWithContext extends App {
  cluster_name: string;
  project_name: string;
  cluster_id: string;
}

export default function Dashboard() {
  const [showCreate, setShowCreate] = useState(false);
  const [selectedClusterId, setSelectedClusterId] = useState<string>('');
  const [filterProjectId, setFilterProjectId] = useState<string>('all');
  const [newApp, setNewApp] = useState<CreateAppRequest>({
    name: '',
    image: 'nginx:latest',
    namespace: 'default',
    port: 80,
    replicas: 1,
  });
  const queryClient = useQueryClient();

  // Fetch all projects
  const { data: projects = [] } = useQuery({
    queryKey: ['projects'],
    queryFn: listProjects,
  });

  // Fetch clusters for all projects
  const { data: allClusters = [] } = useQuery({
    queryKey: ['all-clusters', projects.map(p => p.id)],
    queryFn: async () => {
      const clusterPromises = projects.map(async (project) => {
        const clusters = await listClusters(project.id);
        return clusters.map(c => ({ ...c, project_name: project.name }));
      });
      const results = await Promise.all(clusterPromises);
      return results.flat();
    },
    enabled: projects.length > 0,
  });

  // Fetch apps for all clusters
  const { data: allApps = [], isLoading } = useQuery({
    queryKey: ['all-apps', allClusters.map(c => c.id)],
    queryFn: async () => {
      const appPromises = allClusters.map(async (cluster: Cluster & { project_name: string }) => {
        try {
          const apps = await listApps(cluster.id);
          return apps.map(app => ({
            ...app,
            cluster_name: cluster.name,
            project_name: cluster.project_name,
            cluster_id: cluster.id,
          }));
        } catch {
          return [];
        }
      });
      const results = await Promise.all(appPromises);
      return results.flat() as AppWithContext[];
    },
    enabled: allClusters.length > 0,
  });

  const deployMutation = useMutation({
    mutationFn: (appId: string) => deployApp(appId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['all-apps'] });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (appId: string) => deleteApp(appId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['all-apps'] });
    },
  });

  const createMutation = useMutation({
    mutationFn: ({ clusterId, data }: { clusterId: string; data: CreateAppRequest }) =>
      createApp(clusterId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['all-apps'] });
      setShowCreate(false);
      setNewApp({ name: '', image: 'nginx:latest', namespace: 'default', port: 80, replicas: 1 });
    },
  });

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (newApp.name.trim() && selectedClusterId) {
      createMutation.mutate({ clusterId: selectedClusterId, data: newApp });
    }
  };

  const getStatusColor = (status: string) => {
    switch (status?.toLowerCase()) {
      case 'running':
        return 'bg-green-100 text-green-800';
      case 'deploying':
      case 'pending':
        return 'bg-yellow-100 text-yellow-800';
      case 'failed':
      case 'error':
        return 'bg-red-100 text-red-800';
      default:
        return 'bg-gray-100 text-gray-800';
    }
  };

  const getStatusIcon = (status: string) => {
    switch (status?.toLowerCase()) {
      case 'running':
        return '●';
      case 'deploying':
      case 'pending':
        return '◐';
      case 'failed':
      case 'error':
        return '○';
      default:
        return '○';
    }
  };

  const formatTimeAgo = (date: string) => {
    const now = new Date();
    const then = new Date(date);
    const seconds = Math.floor((now.getTime() - then.getTime()) / 1000);

    if (seconds < 60) return 'just now';
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
  };

  if (isLoading && projects.length > 0) {
    return (
      <div className="flex justify-center py-12">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
      </div>
    );
  }

  // Filter apps and clusters by selected project
  const filteredClusters = filterProjectId === 'all'
    ? allClusters
    : allClusters.filter((c: Cluster & { project_name: string, project_id?: string }) => {
        const project = projects.find(p => p.name === c.project_name);
        return project?.id === filterProjectId;
      });

  const filteredApps = filterProjectId === 'all'
    ? allApps
    : allApps.filter(app => {
        const project = projects.find(p => p.name === app.project_name);
        return project?.id === filterProjectId;
      });

  return (
    <div>
      {/* Header */}
      <div className="flex justify-between items-center mb-8">
        <div className="flex items-center gap-4">
          <div>
            <h1 className="text-2xl font-bold text-gray-900">Applications</h1>
            <p className="text-gray-500 mt-1">
              {filteredApps.length} app{filteredApps.length !== 1 ? 's' : ''} across {filteredClusters.length} cluster{filteredClusters.length !== 1 ? 's' : ''}
            </p>
          </div>
          {projects.length > 1 && (
            <select
              value={filterProjectId}
              onChange={(e) => setFilterProjectId(e.target.value)}
              className="ml-4 px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 bg-white"
            >
              <option value="all">All Projects</option>
              {projects.map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </select>
          )}
        </div>
        <button
          onClick={() => setShowCreate(true)}
          className="bg-indigo-600 text-white px-4 py-2 rounded-lg hover:bg-indigo-700 flex items-center gap-2 font-medium"
        >
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          Deploy New App
        </button>
      </div>

      {/* Create App Modal */}
      {showCreate && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg p-6 w-full max-w-md">
            <h2 className="text-lg font-semibold mb-4">Deploy New Application</h2>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Cluster *</label>
                <select
                  value={selectedClusterId}
                  onChange={(e) => setSelectedClusterId(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  required
                >
                  <option value="">Select a cluster...</option>
                  {allClusters.map((cluster: Cluster & { project_name: string }) => (
                    <option key={cluster.id} value={cluster.id}>
                      {cluster.name} ({cluster.project_name})
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">App Name *</label>
                <input
                  type="text"
                  value={newApp.name}
                  onChange={(e) => setNewApp({ ...newApp, name: e.target.value })}
                  placeholder="my-app"
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Image *</label>
                <input
                  type="text"
                  value={newApp.image}
                  onChange={(e) => setNewApp({ ...newApp, image: e.target.value })}
                  placeholder="nginx:latest"
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  required
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Port</label>
                  <input
                    type="number"
                    value={newApp.port}
                    onChange={(e) => setNewApp({ ...newApp, port: parseInt(e.target.value) || 80 })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Replicas</label>
                  <input
                    type="number"
                    value={newApp.replicas}
                    onChange={(e) => setNewApp({ ...newApp, replicas: parseInt(e.target.value) || 1 })}
                    min="1"
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
              </div>
              <div className="flex justify-end gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => setShowCreate(false)}
                  className="px-4 py-2 text-gray-600 hover:bg-gray-100 rounded-md"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={createMutation.isPending || !selectedClusterId}
                  className="px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700 disabled:opacity-50"
                >
                  {createMutation.isPending ? 'Deploying...' : 'Deploy'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Empty State */}
      {filteredApps.length === 0 && filteredClusters.length > 0 && (
        <div className="text-center py-16 bg-white rounded-xl border border-gray-200">
          <div className="w-16 h-16 mx-auto mb-4 bg-indigo-100 rounded-full flex items-center justify-center">
            <svg className="w-8 h-8 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-gray-900 mb-2">No applications yet</h3>
          <p className="text-gray-500 mb-6">Deploy your first application to get started</p>
          <button
            onClick={() => setShowCreate(true)}
            className="bg-indigo-600 text-white px-6 py-2 rounded-lg hover:bg-indigo-700"
          >
            Deploy Your First App
          </button>
        </div>
      )}

      {/* No Clusters State */}
      {filteredClusters.length === 0 && (
        <div className="text-center py-16 bg-white rounded-xl border border-gray-200">
          <div className="w-16 h-16 mx-auto mb-4 bg-yellow-100 rounded-full flex items-center justify-center">
            <svg className="w-8 h-8 text-yellow-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-gray-900 mb-2">No clusters connected</h3>
          <p className="text-gray-500 mb-2">Connect a Kubernetes cluster to start deploying apps</p>
          <code className="block bg-gray-100 text-gray-700 px-4 py-2 rounded-lg text-sm mb-6">
            shipit clusters connect --name my-cluster
          </code>
        </div>
      )}

      {/* Apps Grid */}
      {filteredApps.length > 0 && (
        <div className="space-y-4">
          {filteredApps.map((app) => (
            <div
              key={app.id}
              className="bg-white rounded-xl border border-gray-200 p-5 hover:shadow-md transition-shadow"
            >
              <div className="flex items-start justify-between">
                <div className="flex-1">
                  <div className="flex items-center gap-3">
                    <Link
                      to={`/apps/${app.id}`}
                      className="text-lg font-semibold text-gray-900 hover:text-indigo-600"
                    >
                      {app.name}
                    </Link>
                    <span className={`px-2.5 py-0.5 rounded-full text-xs font-medium ${getStatusColor(app.status)}`}>
                      {getStatusIcon(app.status)} {app.status || 'unknown'}
                    </span>
                  </div>

                  <div className="mt-2 flex items-center gap-4 text-sm text-gray-500">
                    <span className="flex items-center gap-1">
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
                      </svg>
                      {app.image}
                    </span>
                    <span className="flex items-center gap-1">
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 10h16M4 14h16M4 18h16" />
                      </svg>
                      {app.replicas} replica{app.replicas !== 1 ? 's' : ''}
                    </span>
                    <span className="flex items-center gap-1">
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A2 2 0 013 12V7a4 4 0 014-4z" />
                      </svg>
                      v{app.current_revision || 1}
                    </span>
                  </div>

                  <div className="mt-2 flex items-center gap-2 text-xs text-gray-400">
                    <span>{app.cluster_name}</span>
                    <span>•</span>
                    <span>{app.namespace}</span>
                    {app.updated_at && (
                      <>
                        <span>•</span>
                        <span>Updated {formatTimeAgo(app.updated_at)}</span>
                      </>
                    )}
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  <button
                    onClick={() => deployMutation.mutate(app.id)}
                    disabled={deployMutation.isPending}
                    className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-600 rounded-lg hover:bg-indigo-100 font-medium"
                  >
                    Deploy
                  </button>
                  <Link
                    to={`/apps/${app.id}`}
                    className="px-3 py-1.5 text-sm bg-gray-50 text-gray-600 rounded-lg hover:bg-gray-100 font-medium"
                  >
                    Details
                  </Link>
                  <button
                    onClick={() => {
                      if (confirm(`Delete "${app.name}"? This cannot be undone.`)) {
                        deleteMutation.mutate(app.id);
                      }
                    }}
                    className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50"
                  >
                    <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                    </svg>
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Quick Links */}
      {filteredApps.length > 0 && (
        <div className="mt-8 pt-6 border-t border-gray-200">
          <h3 className="text-sm font-medium text-gray-500 mb-3">Quick Links</h3>
          <div className="flex gap-4">
            <Link
              to="/?manage=true"
              className="text-sm text-indigo-600 hover:text-indigo-700"
            >
              Manage Projects
            </Link>
            <span className="text-gray-300">•</span>
            <button
              onClick={() => setShowCreate(true)}
              className="text-sm text-indigo-600 hover:text-indigo-700"
            >
              Deploy New App
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
