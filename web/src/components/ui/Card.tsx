import { forwardRef } from 'react';
import type { HTMLAttributes } from 'react';

type CardAccent = 'purple' | 'blue' | 'green' | 'orange' | 'pink' | 'none';

interface CardProps extends HTMLAttributes<HTMLDivElement> {
  accent?: CardAccent;
  hover?: boolean;
  padding?: 'none' | 'sm' | 'md' | 'lg';
}

const accentStyles: Record<CardAccent, string> = {
  purple: 'bg-gradient-to-br from-card-purple to-transparent',
  blue: 'bg-gradient-to-br from-card-blue to-transparent',
  green: 'bg-gradient-to-br from-card-green to-transparent',
  orange: 'bg-gradient-to-br from-card-orange to-transparent',
  pink: 'bg-gradient-to-br from-card-pink to-transparent',
  none: '',
};

const paddingStyles = {
  none: '',
  sm: 'p-3',
  md: 'p-4',
  lg: 'p-6',
};

export const Card = forwardRef<HTMLDivElement, CardProps>(
  ({ accent = 'none', hover = false, padding = 'md', className = '', children, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={`
          bg-surface border border-border rounded-xl
          ${accentStyles[accent]}
          ${paddingStyles[padding]}
          ${hover ? 'hover:border-border transition-colors duration-150 hover:bg-surface-hover cursor-pointer' : ''}
          ${className}
        `}
        {...props}
      >
        {children}
      </div>
    );
  }
);

Card.displayName = 'Card';

// Card sub-components for structured layouts
export const CardHeader = ({ className = '', children, ...props }: HTMLAttributes<HTMLDivElement>) => (
  <div className={`flex items-center justify-between mb-4 ${className}`} {...props}>
    {children}
  </div>
);

export const CardTitle = ({ className = '', children, ...props }: HTMLAttributes<HTMLHeadingElement>) => (
  <h3 className={`text-lg font-semibold text-text-primary ${className}`} {...props}>
    {children}
  </h3>
);

export const CardDescription = ({ className = '', children, ...props }: HTMLAttributes<HTMLParagraphElement>) => (
  <p className={`text-sm text-text-secondary ${className}`} {...props}>
    {children}
  </p>
);

export const CardContent = ({ className = '', children, ...props }: HTMLAttributes<HTMLDivElement>) => (
  <div className={`${className}`} {...props}>
    {children}
  </div>
);

export const CardFooter = ({ className = '', children, ...props }: HTMLAttributes<HTMLDivElement>) => (
  <div className={`flex items-center gap-2 mt-4 pt-4 border-t border-border ${className}`} {...props}>
    {children}
  </div>
);
