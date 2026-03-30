import { forwardRef } from 'react';
import type { InputHTMLAttributes, TextareaHTMLAttributes } from 'react';

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  error?: boolean;
  label?: string;
  helperText?: string;
}

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  error?: boolean;
  label?: string;
  helperText?: string;
}

const baseInputStyles = `
  w-full px-3 py-2 rounded-lg
  bg-surface border border-border
  text-text-primary placeholder:text-text-muted
  focus:outline-none focus:ring-2 focus:ring-accent focus:border-transparent
  disabled:opacity-50 disabled:cursor-not-allowed
  transition-colors duration-150
`;

const errorStyles = 'border-error focus:ring-error';

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ error, label, helperText, className = '', id, ...props }, ref) => {
    const inputId = id || label?.toLowerCase().replace(/\s+/g, '-');

    return (
      <div className="w-full">
        {label && (
          <label htmlFor={inputId} className="block text-sm font-medium text-text-secondary mb-1.5">
            {label}
          </label>
        )}
        <input
          ref={ref}
          id={inputId}
          className={`
            ${baseInputStyles}
            ${error ? errorStyles : ''}
            ${className}
          `}
          {...props}
        />
        {helperText && (
          <p className={`mt-1.5 text-sm ${error ? 'text-error' : 'text-text-muted'}`}>
            {helperText}
          </p>
        )}
      </div>
    );
  }
);

Input.displayName = 'Input';

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(
  ({ error, label, helperText, className = '', id, ...props }, ref) => {
    const inputId = id || label?.toLowerCase().replace(/\s+/g, '-');

    return (
      <div className="w-full">
        {label && (
          <label htmlFor={inputId} className="block text-sm font-medium text-text-secondary mb-1.5">
            {label}
          </label>
        )}
        <textarea
          ref={ref}
          id={inputId}
          className={`
            ${baseInputStyles}
            min-h-[100px] resize-y
            ${error ? errorStyles : ''}
            ${className}
          `}
          {...props}
        />
        {helperText && (
          <p className={`mt-1.5 text-sm ${error ? 'text-error' : 'text-text-muted'}`}>
            {helperText}
          </p>
        )}
      </div>
    );
  }
);

Textarea.displayName = 'Textarea';

export const Select = forwardRef<HTMLSelectElement, InputProps & { children: React.ReactNode }>(
  ({ error, label, helperText, className = '', id, children, ...props }, ref) => {
    const inputId = id || label?.toLowerCase().replace(/\s+/g, '-');

    return (
      <div className="w-full">
        {label && (
          <label htmlFor={inputId} className="block text-sm font-medium text-text-secondary mb-1.5">
            {label}
          </label>
        )}
        <select
          ref={ref as React.Ref<HTMLSelectElement>}
          id={inputId}
          className={`
            ${baseInputStyles}
            cursor-pointer
            ${error ? errorStyles : ''}
            ${className}
          `}
          {...(props as React.SelectHTMLAttributes<HTMLSelectElement>)}
        >
          {children}
        </select>
        {helperText && (
          <p className={`mt-1.5 text-sm ${error ? 'text-error' : 'text-text-muted'}`}>
            {helperText}
          </p>
        )}
      </div>
    );
  }
);

Select.displayName = 'Select';
