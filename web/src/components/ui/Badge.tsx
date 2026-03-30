import { forwardRef } from 'react';
import type { HTMLAttributes } from 'react';

type BadgeVariant = 'success' | 'warning' | 'error' | 'info' | 'neutral' | 'accent';
type BadgeSize = 'sm' | 'md';

interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: BadgeVariant;
  size?: BadgeSize;
  dot?: boolean;
}

const variantStyles: Record<BadgeVariant, string> = {
  success: 'bg-success-muted text-success border-success/20',
  warning: 'bg-warning-muted text-warning border-warning/20',
  error: 'bg-error-muted text-error border-error/20',
  info: 'bg-info-muted text-info border-info/20',
  neutral: 'bg-surface-hover text-text-secondary border-border',
  accent: 'bg-accent-muted text-accent border-accent/20',
};

const dotColors: Record<BadgeVariant, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  error: 'bg-error',
  info: 'bg-info',
  neutral: 'bg-text-muted',
  accent: 'bg-accent',
};

const sizeStyles: Record<BadgeSize, string> = {
  sm: 'px-2 py-0.5 text-xs',
  md: 'px-2.5 py-1 text-sm',
};

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(
  ({ variant = 'neutral', size = 'sm', dot = false, className = '', children, ...props }, ref) => {
    return (
      <span
        ref={ref}
        className={`
          inline-flex items-center gap-1.5 font-medium rounded-full border
          ${variantStyles[variant]}
          ${sizeStyles[size]}
          ${className}
        `}
        {...props}
      >
        {dot && (
          <span className={`w-1.5 h-1.5 rounded-full ${dotColors[variant]}`} />
        )}
        {children}
      </span>
    );
  }
);

Badge.displayName = 'Badge';

// Convenience component for status badges
export function StatusBadge({ status, className = '' }: { status: string; className?: string }) {
  const getVariant = (status: string): BadgeVariant => {
    const s = status.toLowerCase();
    if (s === 'running' || s === 'active' || s === 'ready' || s === 'success' || s === 'deployed') return 'success';
    if (s === 'pending' || s === 'deploying' || s === 'creating' || s === 'updating') return 'warning';
    if (s === 'failed' || s === 'error' || s === 'crashed') return 'error';
    if (s === 'stopped' || s === 'inactive' || s === 'unknown') return 'neutral';
    return 'info';
  };

  return (
    <Badge variant={getVariant(status)} dot className={className}>
      {status}
    </Badge>
  );
}
