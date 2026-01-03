import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { getCluster, listApps, createApp, deleteApp, deployApp } from '../api/client';
import type { App, CreateAppRequest } from '../types';

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

export default function Apps() {
  const { clusterId } = useParams<{ clusterId: string }>();
  const [showCreate, setShowCreate] = useState(false);
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
    },
  });

  const deployMutation = useMutation({
    mutationFn: (id: string) => deployApp(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps', clusterId] });
    },
  });

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (formData.name && formData.image) {
      createMutation.mutate(formData);
    }
  };

  const handleDelete = (app: App) => {
    if (confirm(`Delete app "${app.name}"? This will remove the deployment from Kubernetes.`)) {
      deleteMutation.mutate(app.id);
    }
  };

  const handleDeploy = (app: App) => {
    deployMutation.mutate(app.id);
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
        <p className="text-red-700">Error loading apps: {(error as Error).message}</p>
      </div>
    );
  }

  return (
    <div>
      {/* Breadcrumb */}
      <nav className="mb-4 text-sm">
        <Link to="/" className="text-gray-500 hover:text-gray-700">Projects</Link>
        <span className="mx-2 text-gray-400">/</span>
        <span className="text-gray-500">Clusters</span>
        <span className="mx-2 text-gray-400">/</span>
        <span className="text-gray-900">{cluster?.name || 'Loading...'}</span>
      </nav>

      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Applications</h1>
        <button
          onClick={() => setShowCreate(true)}
          className="bg-indigo-600 text-white px-4 py-2 rounded-md hover:bg-indigo-700"
        >
          New App
        </button>
      </div>

      {/* Create App Modal */}
      {showCreate && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg p-6 w-full max-w-lg max-h-[90vh] overflow-y-auto">
            <h2 className="text-lg font-semibold mb-4">Create Application</h2>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Name *</label>
                <input
                  type="text"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Image *</label>
                <input
                  type="text"
                  value={formData.image}
                  onChange={(e) => setFormData({ ...formData, image: e.target.value })}
                  placeholder="nginx:latest"
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  required
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Namespace</label>
                  <input
                    type="text"
                    value={formData.namespace}
                    onChange={(e) => setFormData({ ...formData, namespace: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Port</label>
                  <input
                    type="number"
                    value={formData.port}
                    onChange={(e) => setFormData({ ...formData, port: parseInt(e.target.value) || undefined })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Replicas</label>
                <input
                  type="number"
                  value={formData.replicas}
                  onChange={(e) => setFormData({ ...formData, replicas: parseInt(e.target.value) || 1 })}
                  min="1"
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                />
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
                  disabled={createMutation.isPending}
                  className="px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700 disabled:opacity-50"
                >
                  {createMutation.isPending ? 'Creating...' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Apps List */}
      {apps?.length === 0 ? (
        <div className="text-center py-12 bg-white rounded-lg border border-gray-200">
          <p className="text-gray-500 mb-4">No applications deployed</p>
          <button
            onClick={() => setShowCreate(true)}
            className="text-indigo-600 hover:text-indigo-700"
          >
            Deploy your first app
          </button>
        </div>
      ) : (
        <div className="bg-white rounded-lg border border-gray-200 overflow-hidden">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Image</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Replicas</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Revision</th>
                <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
              </tr>
            </thead>
            <tbody className="bg-white divide-y divide-gray-200">
              {apps?.map((app) => (
                <tr key={app.id} className="hover:bg-gray-50">
                  <td className="px-6 py-4 whitespace-nowrap">
                    <Link to={`/apps/${app.id}`} className="text-indigo-600 hover:text-indigo-900 font-medium">
                      {app.name}
                    </Link>
                    <div className="text-sm text-gray-500">{app.namespace}</div>
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap">
                    <div className="text-sm text-gray-900 max-w-xs truncate" title={app.image}>
                      {app.image}
                    </div>
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap">
                    <StatusBadge status={app.status} />
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                    {app.replicas}
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                    v{app.current_revision}
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium space-x-2">
                    <button
                      onClick={() => handleDeploy(app)}
                      disabled={deployMutation.isPending}
                      className="text-green-600 hover:text-green-900 disabled:opacity-50"
                    >
                      Deploy
                    </button>
                    <Link to={`/apps/${app.id}`} className="text-indigo-600 hover:text-indigo-900">
                      View
                    </Link>
                    <button
                      onClick={() => handleDelete(app)}
                      className="text-red-600 hover:text-red-900"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
