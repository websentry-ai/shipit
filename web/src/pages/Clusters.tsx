import { Link, useParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { getProject, listClusters, deleteCluster } from '../api/client';
import type { Cluster } from '../types';

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    connected: 'bg-green-100 text-green-800',
    disconnected: 'bg-red-100 text-red-800',
    pending: 'bg-yellow-100 text-yellow-800',
  };
  return (
    <span className={`px-2 py-1 rounded-full text-xs font-medium ${colors[status] || 'bg-gray-100 text-gray-800'}`}>
      {status}
    </span>
  );
}

export default function Clusters() {
  const { projectId } = useParams<{ projectId: string }>();
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
    },
  });

  const handleDelete = (cluster: Cluster) => {
    if (confirm(`Delete cluster "${cluster.name}"? This cannot be undone.`)) {
      deleteMutation.mutate(cluster.id);
    }
  };

  if (isLoading) {
    return (
      <div className="flex justify-center py-12">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="bg-red-50 border border-red-200 rounded-lg p-4">
        <p className="text-red-700">Error loading clusters: {(error as Error).message}</p>
      </div>
    );
  }

  return (
    <div>
      {/* Breadcrumb */}
      <nav className="mb-4 text-sm">
        <Link to="/" className="text-gray-500 hover:text-gray-700">Projects</Link>
        <span className="mx-2 text-gray-400">/</span>
        <span className="text-gray-900">{project?.name || 'Loading...'}</span>
      </nav>

      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Clusters</h1>
        <p className="text-sm text-gray-500">
          Connect clusters via CLI: <code className="bg-gray-100 px-2 py-1 rounded">shipit clusters connect</code>
        </p>
      </div>

      {/* Clusters List */}
      {clusters?.length === 0 ? (
        <div className="text-center py-12 bg-white rounded-lg border border-gray-200">
          <p className="text-gray-500 mb-4">No clusters connected</p>
          <p className="text-sm text-gray-400">
            Use the CLI to connect a cluster:<br />
            <code className="bg-gray-100 px-2 py-1 rounded mt-2 inline-block">
              shipit clusters connect {projectId} --name my-cluster --kubeconfig ~/.kube/config
            </code>
          </p>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {clusters?.map((cluster) => (
            <div
              key={cluster.id}
              className="bg-white rounded-lg border border-gray-200 p-4 hover:shadow-md transition-shadow"
            >
              <div className="flex justify-between items-start">
                <div>
                  <Link
                    to={`/clusters/${cluster.id}`}
                    className="text-lg font-medium text-gray-900 hover:text-indigo-600"
                  >
                    {cluster.name}
                  </Link>
                  <div className="mt-1">
                    <StatusBadge status={cluster.status} />
                  </div>
                </div>
                <button
                  onClick={() => handleDelete(cluster)}
                  className="text-gray-400 hover:text-red-600"
                  title="Delete cluster"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </button>
              </div>
              {cluster.endpoint && (
                <p className="text-sm text-gray-500 mt-2 truncate" title={cluster.endpoint}>
                  {cluster.endpoint}
                </p>
              )}
              {cluster.status_message && (
                <p className="text-sm text-red-500 mt-1">{cluster.status_message}</p>
              )}
              <p className="text-sm text-gray-400 mt-2">
                Connected {new Date(cluster.created_at).toLocaleDateString()}
              </p>
              <Link
                to={`/clusters/${cluster.id}`}
                className="mt-3 inline-block text-sm text-indigo-600 hover:text-indigo-700"
              >
                View apps →
              </Link>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
