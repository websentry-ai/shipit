import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { getProject, listClusters, deleteCluster } from '../api/client';
import type { Cluster } from '../types';
import { Card } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { ConfirmModal } from '../components/ui/Modal';
import { SkeletonCard } from '../components/ui/Skeleton';

// Color accents for cluster cards
const CARD_ACCENTS = ['blue', 'green', 'purple', 'orange', 'pink'] as const;

export default function Clusters() {
  const { projectId } = useParams<{ projectId: string }>();
  const [deleteConfirm, setDeleteConfirm] = useState<Cluster | null>(null);
  const queryClient = useQueryClient();

  const { data: project } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => getProject(projectId!),
    enabled: !!projectId,
  });

  const { data: clusters, isLoading, error } = useQuery({
    queryKey: ['clusters', projectId],
    queryFn: () => listClusters(projectId!),
    enabled: !!projectId,
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteCluster(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters', projectId] });
      setDeleteConfirm(null);
    },
  });

  if (isLoading) {
    return (
      <div className="space-y-4">
        <div className="flex justify-between items-center mb-6">
          <div className="h-8 w-32 bg-surface-hover rounded animate-pulse" />
        </div>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          <SkeletonCard />
          <SkeletonCard />
          <SkeletonCard />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <Card className="border-error/20 bg-error-muted">
        <p className="text-error">Error loading clusters: {(error as Error).message}</p>
      </Card>
    );
  }

  return (
    <div>
      {/* Breadcrumb */}
      <nav className="mb-4 text-sm flex items-center gap-2">
        <Link to="/projects?manage=true" className="text-text-secondary hover:text-text-primary transition-colors">Projects</Link>
        <span className="text-text-muted">/</span>
        <span className="text-text-primary font-medium">{project?.name || 'Loading...'}</span>
      </nav>

      <div className="flex justify-between items-center mb-6">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Clusters</h1>
          <p className="text-text-secondary mt-1">
            Connect clusters via CLI
          </p>
        </div>
      </div>

      {/* Delete Confirmation Modal */}
      <ConfirmModal
        open={!!deleteConfirm}
        onClose={() => setDeleteConfirm(null)}
        onConfirm={() => deleteConfirm && deleteMutation.mutate(deleteConfirm.id)}
        title="Delete Cluster"
        description={`Are you sure you want to delete "${deleteConfirm?.name}"? This will remove all apps deployed to this cluster.`}
        confirmText="Delete"
        variant="danger"
        loading={deleteMutation.isPending}
      />

      {/* Clusters List */}
      {clusters?.length === 0 ? (
        <Card className="text-center py-12">
          <div className="w-16 h-16 mx-auto mb-4 bg-warning-muted rounded-full flex items-center justify-center">
            <svg className="w-8 h-8 text-warning" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-text-primary mb-2">No clusters connected</h3>
          <p className="text-text-secondary mb-4">Use the CLI to connect a cluster</p>
          <code className="block bg-surface-hover text-text-secondary px-4 py-3 rounded-lg text-sm font-mono">
            shipit clusters connect {projectId} --name my-cluster --kubeconfig ~/.kube/config
          </code>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {clusters?.map((cluster, index) => (
            <Card
              key={cluster.id}
              accent={CARD_ACCENTS[index % CARD_ACCENTS.length]}
              hover
              className="group"
            >
              <div className="flex justify-between items-start">
                <div>
                  <Link
                    to={`/clusters/${cluster.id}`}
                    className="text-lg font-semibold text-text-primary hover:text-accent transition-colors"
                  >
                    {cluster.name}
                  </Link>
                  <div className="mt-2">
                    <StatusBadge status={cluster.status} />
                  </div>
                </div>
                <button
                  onClick={() => setDeleteConfirm(cluster)}
                  className="p-1.5 text-text-muted hover:text-error rounded-lg hover:bg-error-muted transition-colors opacity-0 group-hover:opacity-100"
                  title="Delete cluster"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </button>
              </div>

              {cluster.endpoint && (
                <p className="text-sm text-text-muted mt-3 truncate" title={cluster.endpoint}>
                  {cluster.endpoint}
                </p>
              )}

              {cluster.status_message && (
                <p className="text-sm text-error mt-2">{cluster.status_message}</p>
              )}

              <p className="text-sm text-text-muted mt-3">
                Connected {new Date(cluster.created_at).toLocaleDateString()}
              </p>

              <Link
                to={`/clusters/${cluster.id}`}
                className="mt-4 inline-flex items-center gap-1 text-sm text-accent hover:text-accent-hover transition-colors"
              >
                View apps
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                </svg>
              </Link>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
