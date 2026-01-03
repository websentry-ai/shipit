import { useState, useEffect } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { listProjects, createProject, deleteProject } from '../api/client';
import type { Project } from '../types';

export default function Projects() {
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  // Check if we should show the manage view (from dropdown "Manage Projects...")
  const showManage = searchParams.get('manage') === 'true';

  const { data: projects, isLoading, error } = useQuery({
    queryKey: ['projects'],
    queryFn: listProjects,
  });

  // Auto-redirect to default project if not in manage mode
  useEffect(() => {
    if (showManage || isLoading || !projects || projects.length === 0) return;

    // Check for saved project in localStorage
    const savedProjectId = localStorage.getItem('shipit_project');
    const savedProject = savedProjectId ? projects.find(p => p.id === savedProjectId) : null;

    // Redirect to saved project or first project
    const targetProject = savedProject || projects[0];
    if (targetProject) {
      localStorage.setItem('shipit_project', targetProject.id);
      navigate(`/projects/${targetProject.id}`, { replace: true });
    }
  }, [projects, isLoading, navigate, showManage]);

  const createMutation = useMutation({
    mutationFn: (name: string) => createProject(name),
    onSuccess: (newProject) => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      setShowCreate(false);
      setNewName('');
      // Navigate to the new project
      localStorage.setItem('shipit_project', newProject.id);
      navigate(`/projects/${newProject.id}`);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteProject(id),
    onSuccess: (_, deletedId) => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      // If we deleted the saved project, clear it
      if (localStorage.getItem('shipit_project') === deletedId) {
        localStorage.removeItem('shipit_project');
      }
    },
  });

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (newName.trim()) {
      createMutation.mutate(newName.trim());
    }
  };

  const handleDelete = (project: Project) => {
    if (confirm(`Delete project "${project.name}"? This cannot be undone.`)) {
      deleteMutation.mutate(project.id);
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
        <p className="text-red-700">Error loading projects: {(error as Error).message}</p>
      </div>
    );
  }

  // Show loading while redirecting (if not in manage mode and has projects)
  if (!showManage && projects && projects.length > 0) {
    return (
      <div className="flex justify-center py-12">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600"></div>
      </div>
    );
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Projects</h1>
        <button
          onClick={() => setShowCreate(true)}
          className="bg-indigo-600 text-white px-4 py-2 rounded-md hover:bg-indigo-700"
        >
          New Project
        </button>
      </div>

      {/* Create Project Modal */}
      {showCreate && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg p-6 w-full max-w-md">
            <h2 className="text-lg font-semibold mb-4">Create Project</h2>
            <form onSubmit={handleCreate}>
              <input
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="Project name"
                className="w-full px-3 py-2 border border-gray-300 rounded-md mb-4 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                autoFocus
              />
              <div className="flex justify-end gap-3">
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

      {/* Projects List */}
      {projects?.length === 0 ? (
        <div className="text-center py-12 bg-white rounded-lg border border-gray-200">
          <p className="text-gray-500 mb-4">No projects yet</p>
          <button
            onClick={() => setShowCreate(true)}
            className="text-indigo-600 hover:text-indigo-700"
          >
            Create your first project
          </button>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {projects?.map((project) => (
            <div
              key={project.id}
              className="bg-white rounded-lg border border-gray-200 p-4 hover:shadow-md transition-shadow"
            >
              <div className="flex justify-between items-start">
                <Link
                  to={`/projects/${project.id}`}
                  className="text-lg font-medium text-gray-900 hover:text-indigo-600"
                >
                  {project.name}
                </Link>
                <button
                  onClick={() => handleDelete(project)}
                  className="text-gray-400 hover:text-red-600"
                  title="Delete project"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </button>
              </div>
              <p className="text-sm text-gray-500 mt-1">
                Created {new Date(project.created_at).toLocaleDateString()}
              </p>
              <Link
                to={`/projects/${project.id}`}
                className="mt-3 inline-block text-sm text-indigo-600 hover:text-indigo-700"
              >
                View clusters →
              </Link>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
