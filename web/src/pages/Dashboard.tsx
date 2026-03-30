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
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Modal, ConfirmModal } from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { SkeletonCard } from '../components/ui/Skeleton';

interface AppWithContext extends App {
  cluster_name: string;
  project_name: string;
  cluster_id: string;
}

// Color accents for app cards - cycle through these
const CARD_ACCENTS = ['purple', 'blue', 'green', 'orange', 'pink'] as const;

export default function Dashboard() {
  const [showCreate, setShowCreate] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<AppWithContext | null>(null);
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
      setDeleteConfirm(null);
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
      <div className="space-y-4">
        <div className="flex justify-between items-center mb-8">
          <div>
            <div className="h-8 w-48 bg-surface-hover rounded animate-pulse" />
            <div className="h-5 w-32 bg-surface-hover rounded animate-pulse mt-2" />
          </div>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          <SkeletonCard />
          <SkeletonCard />
          <SkeletonCard />
        </div>
      </div>
    );
  }

  // Filter apps and clusters by selected project
  const filteredClusters = filterProjectId === 'all'
    ? allClusters
    : allClusters.filter((c: Cluster & { project_name: string }) => {
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
            <h1 className="text-2xl font-bold text-text-primary">Applications</h1>
            <p className="text-text-secondary mt-1">
              {filteredApps.length} app{filteredApps.length !== 1 ? 's' : ''} across {filteredClusters.length} cluster{filteredClusters.length !== 1 ? 's' : ''}
            </p>
          </div>
          {projects.length > 1 && (
            <select
              value={filterProjectId}
              onChange={(e) => setFilterProjectId(e.target.value)}
              className="ml-4 px-3 py-1.5 text-sm border border-border rounded-lg bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
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
        <Button onClick={() => setShowCreate(true)}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          Deploy New App
        </Button>
      </div>

      {/* Create App Modal */}
      <Modal
        open={showCreate}
        onClose={() => setShowCreate(false)}
        title="Deploy New Application"
        size="md"
      >
        <form onSubmit={handleCreate} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1.5">Cluster *</label>
            <select
              value={selectedClusterId}
              onChange={(e) => setSelectedClusterId(e.target.value)}
              className="w-full px-3 py-2 border border-border rounded-lg bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
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
          <Input
            label="App Name *"
            value={newApp.name}
            onChange={(e) => setNewApp({ ...newApp, name: e.target.value })}
            placeholder="my-app"
            required
          />
          <Input
            label="Image *"
            value={newApp.image}
            onChange={(e) => setNewApp({ ...newApp, image: e.target.value })}
            placeholder="nginx:latest"
            required
          />
          <div className="grid grid-cols-2 gap-4">
            <Input
              label="Port"
              type="number"
              value={newApp.port}
              onChange={(e) => setNewApp({ ...newApp, port: parseInt(e.target.value) || 80 })}
            />
            <Input
              label="Replicas"
              type="number"
              value={newApp.replicas}
              onChange={(e) => setNewApp({ ...newApp, replicas: parseInt(e.target.value) || 1 })}
              min={1}
            />
          </div>
          <div className="flex justify-end gap-3 pt-4">
            <Button variant="ghost" type="button" onClick={() => setShowCreate(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createMutation.isPending || !selectedClusterId}
              loading={createMutation.isPending}
            >
              Deploy
            </Button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <ConfirmModal
        open={!!deleteConfirm}
        onClose={() => setDeleteConfirm(null)}
        onConfirm={() => deleteConfirm && deleteMutation.mutate(deleteConfirm.id)}
        title="Delete Application"
        description={`Are you sure you want to delete "${deleteConfirm?.name}"? This action cannot be undone.`}
        confirmText="Delete"
        variant="danger"
        loading={deleteMutation.isPending}
      />

      {/* Empty State */}
      {filteredApps.length === 0 && filteredClusters.length > 0 && (
        <Card className="text-center py-16">
          <div className="w-16 h-16 mx-auto mb-4 bg-accent-muted rounded-full flex items-center justify-center">
            <svg className="w-8 h-8 text-accent" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-text-primary mb-2">No applications yet</h3>
          <p className="text-text-secondary mb-6">Deploy your first application to get started</p>
          <Button onClick={() => setShowCreate(true)}>
            Deploy Your First App
          </Button>
        </Card>
      )}

      {/* No Clusters State */}
      {filteredClusters.length === 0 && (
        <Card className="text-center py-16">
          <div className="w-16 h-16 mx-auto mb-4 bg-warning-muted rounded-full flex items-center justify-center">
            <svg className="w-8 h-8 text-warning" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-text-primary mb-2">No clusters connected</h3>
          <p className="text-text-secondary mb-2">Connect a Kubernetes cluster to start deploying apps</p>
          <code className="block bg-surface-hover text-text-secondary px-4 py-2 rounded-lg text-sm mb-6 font-mono">
            shipit clusters connect --name my-cluster
          </code>
        </Card>
      )}

      {/* Apps Grid */}
      {filteredApps.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 auto-rows-fr">
          {filteredApps.map((app, index) => (
            <Card
              key={app.id}
              accent={CARD_ACCENTS[index % CARD_ACCENTS.length]}
              hover
              className="group flex flex-col h-full"
            >
              <div className="flex items-start justify-between mb-3">
                <div className="flex items-center gap-2 min-w-0 flex-1">
                  <Link
                    to={`/apps/${app.id}`}
                    className="text-lg font-semibold text-text-primary hover:text-accent transition-colors truncate"
                  >
                    {app.name}
                  </Link>
                  {app.managed_by === 'porter' && (
                    <span className="inline-flex items-center px-1.5 py-0.5 text-xs font-medium bg-blue-500/10 text-blue-400 rounded border border-blue-500/20 flex-shrink-0">
                      Porter
                    </span>
                  )}
                </div>
                <StatusBadge status={app.status || 'unknown'} />
              </div>

              <div className="space-y-2 text-sm text-text-secondary mb-4">
                <div className="flex items-center gap-2">
                  <svg className="w-4 h-4 text-text-muted flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
                  </svg>
                  <span className="truncate">{app.image}</span>
                </div>
                <div className="flex items-center gap-4">
                  <span className="flex items-center gap-1">
                    <svg className="w-4 h-4 text-text-muted" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 10h16M4 14h16M4 18h16" />
                    </svg>
                    {app.replicas} replica{app.replicas !== 1 ? 's' : ''}
                  </span>
                  <span className="flex items-center gap-1">
                    <svg className="w-4 h-4 text-text-muted" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A2 2 0 013 12V7a4 4 0 014-4z" />
                    </svg>
                    v{app.current_revision || 1}
                  </span>
                </div>
              </div>

              <div className="flex items-center gap-2 text-xs text-text-muted mb-4">
                <span>{app.cluster_name}</span>
                <span>•</span>
                <span>{app.namespace}</span>
                {app.updated_at && (
                  <>
                    <span>•</span>
                    <span>{formatTimeAgo(app.updated_at)}</span>
                  </>
                )}
              </div>

              <div className="flex items-center gap-2 pt-3 mt-auto border-t border-border">
                {app.managed_by !== 'porter' && (
                  <Button
                    size="sm"
                    onClick={() => deployMutation.mutate(app.id)}
                    disabled={deployMutation.isPending}
                  >
                    Deploy
                  </Button>
                )}
                <Link to={`/apps/${app.id}`}>
                  <Button size="sm" variant="secondary">
                    Details
                  </Button>
                </Link>
                {app.managed_by !== 'porter' && (
                  <button
                    onClick={() => setDeleteConfirm(app)}
                    className="ml-auto p-1.5 text-text-muted hover:text-error rounded-lg hover:bg-error-muted transition-colors"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                    </svg>
                  </button>
                )}
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
