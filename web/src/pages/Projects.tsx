import { useState, useEffect } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { listProjects, createProject, deleteProject } from '../api/client';
import type { Project } from '../types';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Modal, ConfirmModal } from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { SkeletonCard } from '../components/ui/Skeleton';

// Color accents for project cards - cycle through these
const CARD_ACCENTS = ['purple', 'blue', 'green', 'orange', 'pink'] as const;

export default function Projects() {
  const [showCreate, setShowCreate] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<Project | null>(null);
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
      setDeleteConfirm(null);
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

  if (isLoading) {
    return (
      <div className="space-y-4">
        <div className="flex justify-between items-center mb-6">
          <div className="h-8 w-32 bg-surface-hover rounded animate-pulse" />
          <div className="h-10 w-28 bg-surface-hover rounded animate-pulse" />
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
        <p className="text-error">Error loading projects: {(error as Error).message}</p>
      </Card>
    );
  }

  // Show loading while redirecting (if not in manage mode and has projects)
  if (!showManage && projects && projects.length > 0) {
    return (
      <div className="flex justify-center py-12">
        <div className="flex items-center gap-2 text-text-secondary">
          <svg className="animate-spin h-5 w-5" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
          </svg>
          Loading...
        </div>
      </div>
    );
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold text-text-primary">Projects</h1>
        <Button onClick={() => setShowCreate(true)}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          New Project
        </Button>
      </div>

      {/* Create Project Modal */}
      <Modal
        open={showCreate}
        onClose={() => setShowCreate(false)}
        title="Create Project"
      >
        <form onSubmit={handleCreate} className="space-y-4">
          <Input
            label="Project Name"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="my-project"
            autoFocus
          />
          <div className="flex justify-end gap-3 pt-4">
            <Button variant="ghost" type="button" onClick={() => setShowCreate(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createMutation.isPending || !newName.trim()}
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
        title="Delete Project"
        description={`Are you sure you want to delete "${deleteConfirm?.name}"? This will delete all clusters and apps within this project.`}
        confirmText="Delete"
        variant="danger"
        loading={deleteMutation.isPending}
      />

      {/* Projects List */}
      {projects?.length === 0 ? (
        <Card className="text-center py-12">
          <div className="w-16 h-16 mx-auto mb-4 bg-accent-muted rounded-full flex items-center justify-center">
            <svg className="w-8 h-8 text-accent" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
            </svg>
          </div>
          <p className="text-text-secondary mb-4">No projects yet</p>
          <Button onClick={() => setShowCreate(true)}>
            Create your first project
          </Button>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {projects?.map((project, index) => (
            <Card
              key={project.id}
              accent={CARD_ACCENTS[index % CARD_ACCENTS.length]}
              hover
              className="group"
            >
              <div className="flex justify-between items-start">
                <Link
                  to={`/projects/${project.id}`}
                  className="text-lg font-semibold text-text-primary hover:text-accent transition-colors"
                >
                  {project.name}
                </Link>
                <button
                  onClick={() => setDeleteConfirm(project)}
                  className="p-1.5 text-text-muted hover:text-error rounded-lg hover:bg-error-muted transition-colors opacity-0 group-hover:opacity-100"
                  title="Delete project"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </button>
              </div>
              <p className="text-sm text-text-muted mt-2">
                Created {new Date(project.created_at).toLocaleDateString()}
              </p>
              <Link
                to={`/projects/${project.id}`}
                className="mt-4 inline-flex items-center gap-1 text-sm text-accent hover:text-accent-hover transition-colors"
              >
                View clusters
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
