import { useState, useEffect } from 'react';
import { useAuth } from '../context/AuthContext';
import { listTokens, createToken, deleteToken } from '../api/client';
import type { UserToken, CreateTokenResponse } from '../types';
import { Button } from '../components/ui/Button';
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '../components/ui/Card';
import { Modal, ConfirmModal } from '../components/ui/Modal';
import { Input } from '../components/ui/Input';

export default function Settings() {
  const { user } = useAuth();
  const [tokens, setTokens] = useState<UserToken[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Create token form
  const [showCreate, setShowCreate] = useState(false);
  const [newTokenName, setNewTokenName] = useState('');
  const [newTokenExpiry, setNewTokenExpiry] = useState<number | ''>('');
  const [creating, setCreating] = useState(false);

  // Delete confirmation
  const [deleteConfirm, setDeleteConfirm] = useState<UserToken | null>(null);
  const [deleting, setDeleting] = useState(false);

  // Newly created token (shown once)
  const [newToken, setNewToken] = useState<CreateTokenResponse | null>(null);
  const [copied, setCopied] = useState(false);

  const loadTokens = async () => {
    try {
      setLoading(true);
      const data = await listTokens();
      setTokens(data || []);
    } catch {
      setError('Failed to load tokens');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTokens();
  }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newTokenName.trim()) return;

    try {
      setCreating(true);
      const token = await createToken({
        name: newTokenName.trim(),
        expires_in: newTokenExpiry ? Number(newTokenExpiry) : undefined,
      });
      setNewToken(token);
      setNewTokenName('');
      setNewTokenExpiry('');
      setShowCreate(false);
      loadTokens();
    } catch {
      setError('Failed to create token');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteConfirm) return;

    try {
      setDeleting(true);
      await deleteToken(deleteConfirm.id);
      setDeleteConfirm(null);
      loadTokens();
    } catch {
      setError('Failed to delete token');
    } finally {
      setDeleting(false);
    }
  };

  const handleCopy = () => {
    if (newToken) {
      navigator.clipboard.writeText(newToken.token);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <h1 className="text-2xl font-bold text-text-primary">Settings</h1>

      {/* User Profile */}
      <Card>
        <CardHeader>
          <CardTitle>Profile</CardTitle>
        </CardHeader>
        <CardContent>
          {user && (
            <div className="flex items-center gap-4">
              {user.picture_url ? (
                <img
                  src={user.picture_url}
                  alt={user.name || user.email}
                  className="w-16 h-16 rounded-full ring-2 ring-border"
                />
              ) : (
                <div className="w-16 h-16 rounded-full bg-accent-muted flex items-center justify-center ring-2 ring-border">
                  <span className="text-2xl font-medium text-accent">
                    {(user.name || user.email)[0].toUpperCase()}
                  </span>
                </div>
              )}
              <div>
                <p className="font-medium text-text-primary text-lg">{user.name || 'No name'}</p>
                <p className="text-text-secondary">{user.email}</p>
                <p className="text-sm text-text-muted mt-1">
                  Member since {formatDate(user.created_at)}
                </p>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* API Tokens */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>API Tokens</CardTitle>
              <CardDescription>
                Use API tokens to authenticate CLI tools and scripts.
              </CardDescription>
            </div>
            <Button onClick={() => setShowCreate(true)}>
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
              </svg>
              Create Token
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {error && (
            <div className="mb-4 p-3 bg-error-muted border border-error/20 rounded-lg">
              <p className="text-sm text-error">{error}</p>
            </div>
          )}

          {/* Create Token Modal */}
          <Modal
            open={showCreate}
            onClose={() => setShowCreate(false)}
            title="Create API Token"
          >
            <form onSubmit={handleCreate} className="space-y-4">
              <Input
                label="Token Name"
                value={newTokenName}
                onChange={(e) => setNewTokenName(e.target.value)}
                placeholder="e.g., CLI on MacBook"
                autoFocus
              />
              <Input
                label="Expiration (days)"
                type="number"
                value={newTokenExpiry}
                onChange={(e) => setNewTokenExpiry(e.target.value ? Number(e.target.value) : '')}
                placeholder="Leave empty for no expiration"
                min={1}
                helperText="Token will expire after this many days"
              />
              <div className="flex justify-end gap-3 pt-4">
                <Button variant="ghost" type="button" onClick={() => setShowCreate(false)}>
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={creating || !newTokenName.trim()}
                  loading={creating}
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
            onConfirm={handleDelete}
            title="Revoke Token"
            description={`Are you sure you want to revoke "${deleteConfirm?.name}"? Any applications using this token will lose access.`}
            confirmText="Revoke"
            variant="danger"
            loading={deleting}
          />

          {/* New Token Display (shown once) */}
          {newToken && (
            <div className="mb-6 p-4 bg-success-muted border border-success/20 rounded-lg">
              <div className="flex items-start justify-between">
                <div className="flex-1">
                  <p className="font-medium text-success">Token created: {newToken.name}</p>
                  <p className="text-sm text-text-secondary mt-1">
                    Make sure to copy your token now. You won't be able to see it again!
                  </p>
                  <code className="block mt-3 p-3 bg-surface border border-border rounded-lg text-sm font-mono break-all text-text-primary">
                    {newToken.token}
                  </code>
                </div>
                <button
                  onClick={() => setNewToken(null)}
                  className="p-1 text-text-muted hover:text-text-primary transition-colors"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
              <button
                onClick={handleCopy}
                className="mt-3 inline-flex items-center gap-2 text-sm text-success hover:text-success/80 transition-colors"
              >
                {copied ? (
                  <>
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                    </svg>
                    Copied!
                  </>
                ) : (
                  <>
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                    Copy to clipboard
                  </>
                )}
              </button>
            </div>
          )}

          {/* Token List */}
          {loading ? (
            <div className="space-y-3">
              {[1, 2, 3].map((i) => (
                <div key={i} className="flex items-center justify-between p-4 bg-surface-hover rounded-lg animate-pulse">
                  <div className="space-y-2">
                    <div className="h-4 w-32 bg-border rounded" />
                    <div className="h-3 w-48 bg-border rounded" />
                  </div>
                  <div className="h-8 w-16 bg-border rounded" />
                </div>
              ))}
            </div>
          ) : tokens.length === 0 ? (
            <div className="text-center py-8">
              <div className="w-12 h-12 mx-auto mb-3 bg-surface-hover rounded-full flex items-center justify-center">
                <svg className="w-6 h-6 text-text-muted" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
                </svg>
              </div>
              <p className="text-text-secondary">No API tokens yet</p>
              <p className="text-sm text-text-muted mt-1">Create one to get started</p>
            </div>
          ) : (
            <div className="space-y-3">
              {tokens.map((token) => (
                <div
                  key={token.id}
                  className="flex items-center justify-between p-4 bg-surface-hover rounded-lg group"
                >
                  <div>
                    <p className="font-medium text-text-primary">{token.name}</p>
                    <div className="flex items-center gap-3 text-sm text-text-muted mt-1">
                      <span>Created {formatDate(token.created_at)}</span>
                      {token.last_used_at && (
                        <>
                          <span className="text-border">•</span>
                          <span>Last used {formatDate(token.last_used_at)}</span>
                        </>
                      )}
                      {token.expires_at && (
                        <>
                          <span className="text-border">•</span>
                          <span className="text-warning">Expires {formatDate(token.expires_at)}</span>
                        </>
                      )}
                    </div>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setDeleteConfirm(token)}
                    className="text-error hover:text-error hover:bg-error-muted"
                  >
                    Revoke
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
