import { useState, useMemo } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { getCluster, listApps, createApp, deleteApp, deployApp } from '../api/client';
import type { App, CreateAppRequest } from '../types';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Modal, ConfirmModal } from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { SkeletonTable } from '../components/ui/Skeleton';

// AppRow component for rendering individual app rows
function AppRow({
  app,
  onDeploy,
  onDelete,
  isDeploying
}: {
  app: App;
  onDeploy: (id: string) => void;
  onDelete: (app: App) => void;
  isDeploying: boolean;
}) {
  return (
    <tr key={app.id} className="hover:bg-surface-hover transition-colors">
      <td className="px-6 py-4 whitespace-nowrap">
        <Link to={`/apps/${app.id}`} className="text-accent hover:text-accent-hover font-medium transition-colors">
          {app.service_name || app.name}
        </Link>
        <div className="text-sm text-text-muted">{app.namespace}</div>
      </td>
      <td className="px-6 py-4 whitespace-nowrap">
        <div className="text-sm text-text-secondary max-w-xs truncate" title={app.image}>
          {app.image}
        </div>
      </td>
      <td className="px-6 py-4 whitespace-nowrap">
        <StatusBadge status={app.status} />
      </td>
      <td className="px-6 py-4 whitespace-nowrap text-sm text-text-secondary">
        {app.replicas}
      </td>
      <td className="px-6 py-4 whitespace-nowrap text-sm text-text-secondary">
        v{app.current_revision}
      </td>
      <td className="px-6 py-4 whitespace-nowrap text-right">
        <div className="flex items-center justify-end gap-2">
          <Button
            size="sm"
            onClick={() => onDeploy(app.id)}
            disabled={isDeploying}
          >
            Deploy
          </Button>
          <Link to={`/apps/${app.id}`}>
            <Button size="sm" variant="secondary">
              View
            </Button>
          </Link>
          <button
            onClick={() => onDelete(app)}
            className="p-1.5 text-text-muted hover:text-error rounded-lg hover:bg-error-muted transition-colors"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
          </button>
        </div>
      </td>
    </tr>
  );
}

export default function Apps() {
  const { clusterId } = useParams<{ clusterId: string }>();
  const [showCreate, setShowCreate] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<App | null>(null);
  const [formData, setFormData] = useState<CreateAppRequest>({
    name: '',
    image: '',
    namespace: 'default',
    replicas: 1,
    port: 80,
  });
  const queryClient = useQueryClient();

  const { data: cluster } = useQuery({
    queryKey: ['cluster', clusterId],
    queryFn: () => getCluster(clusterId!),
    enabled: !!clusterId,
  });

  const { data: apps, isLoading, error } = useQuery({
    queryKey: ['apps', clusterId],
    queryFn: () => listApps(clusterId!),
    enabled: !!clusterId,
  });

  const createMutation = useMutation({
    mutationFn: (data: CreateAppRequest) => createApp(clusterId!, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps', clusterId] });
      setShowCreate(false);
      setFormData({ name: '', image: '', namespace: 'default', replicas: 1, port: 80 });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteApp(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps', clusterId] });
      setDeleteConfirm(null);
    },
  });

  const deployMutation = useMutation({
    mutationFn: (id: string) => deployApp(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps', clusterId] });
    },
  });

  // Group apps by app_group for Porter multi-service apps
  const groupedApps = useMemo(() => {
    if (!apps) return { shipitApps: [], porterGroups: [] };

    const shipitApps = apps.filter(app => app.managed_by === 'shipit');
    const porterApps = apps.filter(app => app.managed_by === 'porter');

    // Group Porter apps by app_group
    const groups = new Map<string, App[]>();
    porterApps.forEach(app => {
      const groupKey = app.app_group || app.name;
      if (!groups.has(groupKey)) {
        groups.set(groupKey, []);
      }
      groups.get(groupKey)!.push(app);
    });

    return {
      shipitApps,
      porterGroups: Array.from(groups.entries()).map(([name, services]) => ({
        name,
        services,
        isExpanded: false,
      })),
    };
  }, [apps]);

  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());

  const toggleGroup = (groupName: string) => {
    setExpandedGroups(prev => {
      const next = new Set(prev);
      if (next.has(groupName)) {
        next.delete(groupName);
      } else {
        next.add(groupName);
      }
      return next;
    });
  };

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (formData.name && formData.image) {
      createMutation.mutate(formData);
    }
  };

  if (isLoading) {
    return (
      <div className="space-y-4">
        <div className="flex justify-between items-center mb-6">
          <div className="h-8 w-48 bg-surface-hover rounded animate-pulse" />
          <div className="h-10 w-24 bg-surface-hover rounded animate-pulse" />
        </div>
        <SkeletonTable rows={5} columns={6} />
      </div>
    );
  }

  if (error) {
    return (
      <Card className="border-error/20 bg-error-muted">
        <p className="text-error">Error loading apps: {(error as Error).message}</p>
      </Card>
    );
  }

  return (
    <div>
      {/* Breadcrumb */}
      <nav className="mb-4 text-sm flex items-center gap-2">
        <Link to="/projects" className="text-text-secondary hover:text-text-primary transition-colors">Projects</Link>
        <span className="text-text-muted">/</span>
        <span className="text-text-secondary">Clusters</span>
        <span className="text-text-muted">/</span>
        <span className="text-text-primary font-medium">{cluster?.name || 'Loading...'}</span>
      </nav>

      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold text-text-primary">Applications</h1>
        <Button onClick={() => setShowCreate(true)}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          New App
        </Button>
      </div>

      {/* Create App Modal */}
      <Modal
        open={showCreate}
        onClose={() => setShowCreate(false)}
        title="Create Application"
        size="md"
      >
        <form onSubmit={handleCreate} className="space-y-4">
          <Input
            label="Name *"
            value={formData.name}
            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            placeholder="my-app"
            required
          />
          <Input
            label="Image *"
            value={formData.image}
            onChange={(e) => setFormData({ ...formData, image: e.target.value })}
            placeholder="nginx:latest"
            required
          />
          <div className="grid grid-cols-2 gap-4">
            <Input
              label="Namespace"
              value={formData.namespace}
              onChange={(e) => setFormData({ ...formData, namespace: e.target.value })}
            />
            <Input
              label="Port"
              type="number"
              value={formData.port}
              onChange={(e) => setFormData({ ...formData, port: parseInt(e.target.value) || undefined })}
            />
          </div>
          <Input
            label="Replicas"
            type="number"
            value={formData.replicas}
            onChange={(e) => setFormData({ ...formData, replicas: parseInt(e.target.value) || 1 })}
            min={1}
          />
          <div className="flex justify-end gap-3 pt-4">
            <Button variant="ghost" type="button" onClick={() => setShowCreate(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createMutation.isPending}
              loading={createMutation.isPending}
            >
              Create
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
        description={`Are you sure you want to delete "${deleteConfirm?.name}"? This will remove the deployment from Kubernetes.`}
        confirmText="Delete"
        variant="danger"
        loading={deleteMutation.isPending}
      />

      {/* Apps List */}
      {apps?.length === 0 ? (
        <Card className="text-center py-12">
          <div className="w-16 h-16 mx-auto mb-4 bg-accent-muted rounded-full flex items-center justify-center">
            <svg className="w-8 h-8 text-accent" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
            </svg>
          </div>
          <p className="text-text-secondary mb-4">No applications deployed</p>
          <Button onClick={() => setShowCreate(true)}>
            Deploy your first app
          </Button>
        </Card>
      ) : (
        <div className="space-y-4">
          {/* Shipit-managed apps (ungrouped) */}
          {groupedApps.shipitApps.length > 0 && (
            <Card padding="none">
              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-border">
                  <thead>
                    <tr className="bg-surface-hover">
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Name</th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Image</th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Status</th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Replicas</th>
                      <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Revision</th>
                      <th className="px-6 py-3 text-right text-xs font-medium text-text-secondary uppercase tracking-wider">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {groupedApps.shipitApps.map((app) => (
                      <AppRow
                        key={app.id}
                        app={app}
                        onDeploy={deployMutation.mutate}
                        onDelete={setDeleteConfirm}
                        isDeploying={deployMutation.isPending}
                      />
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}

          {/* Porter-managed apps (grouped) */}
          {groupedApps.porterGroups.map((group) => (
            <Card key={group.name} padding="none">
              {/* Group header */}
              <button
                onClick={() => toggleGroup(group.name)}
                className="w-full px-6 py-4 flex items-center justify-between bg-surface-hover hover:bg-surface transition-colors"
              >
                <div className="flex items-center gap-3">
                  <svg
                    className={`w-5 h-5 text-text-secondary transition-transform ${
                      expandedGroups.has(group.name) ? 'rotate-90' : ''
                    }`}
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                  </svg>
                  <div className="text-left">
                    <h3 className="text-lg font-semibold text-text-primary">{group.name}</h3>
                    <p className="text-sm text-text-muted">
                      {group.services.length} service{group.services.length !== 1 ? 's' : ''} · Porter managed
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {group.services[0].porter_app_url && (
                    <a
                      href={group.services[0].porter_app_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={(e) => e.stopPropagation()}
                      className="text-sm text-accent hover:text-accent-hover transition-colors"
                    >
                      View in Porter →
                    </a>
                  )}
                </div>
              </button>

              {/* Group services (collapsible) */}
              {expandedGroups.has(group.name) && (
                <div className="overflow-x-auto">
                  <table className="min-w-full divide-y divide-border">
                    <thead>
                      <tr className="bg-surface">
                        <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Service</th>
                        <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Image</th>
                        <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Status</th>
                        <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Replicas</th>
                        <th className="px-6 py-3 text-left text-xs font-medium text-text-secondary uppercase tracking-wider">Revision</th>
                        <th className="px-6 py-3 text-right text-xs font-medium text-text-secondary uppercase tracking-wider">Actions</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border bg-surface">
                      {group.services.map((app) => (
                        <AppRow
                          key={app.id}
                          app={app}
                          onDeploy={deployMutation.mutate}
                          onDelete={setDeleteConfirm}
                          isDeploying={deployMutation.isPending}
                        />
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
