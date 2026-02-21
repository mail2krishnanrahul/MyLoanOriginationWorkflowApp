import { AlertCircle } from 'lucide-react';
import type {
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes
} from 'react';
import { cn } from '@/lib/cn';

interface FormFieldProps {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  required?: boolean;
  children: ReactNode;
}

export function FormField({ id, label, hint, error, required, children }: FormFieldProps) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="text-sm font-medium text-neutral-700 dark:text-neutral-200">
        {label}
        {required ? <span className="ml-1 text-danger-500">*</span> : null}
      </label>
      {children}
      {hint && !error ? <p className="text-xs text-neutral-500 dark:text-neutral-400">{hint}</p> : null}
      {error ? (
        <p className="flex items-center gap-1 text-xs text-danger-500" role="alert">
          <AlertCircle className="size-3" aria-hidden="true" />
          <span>{error}</span>
        </p>
      ) : null}
    </div>
  );
}

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  hasError?: boolean;
}

export function TextInput({ className, hasError, ...props }: InputProps) {
  return (
    <input
      className={cn(
        'block w-full rounded-lg border border-neutral-300 bg-white px-3 py-2 text-sm text-neutral-900 transition focus:border-brand-500 focus:outline-none focus:ring-2 focus:ring-brand-200 dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-100 dark:focus:border-brand-400 dark:focus:ring-brand-500/30',
        hasError && 'border-danger-500 focus:border-danger-500 focus:ring-danger-300',
        className
      )}
      {...props}
    />
  );
}

export function NumberInput({ className, hasError, ...props }: InputProps) {
  return <TextInput type="number" className={className} hasError={hasError} {...props} />;
}

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  hasError?: boolean;
}

export function SelectInput({ className, hasError, children, ...props }: SelectProps) {
  return (
    <select
      className={cn(
        'block w-full rounded-lg border border-neutral-300 bg-white px-3 py-2 text-sm text-neutral-900 transition focus:border-brand-500 focus:outline-none focus:ring-2 focus:ring-brand-200 dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-100 dark:focus:border-brand-400 dark:focus:ring-brand-500/30',
        hasError && 'border-danger-500 focus:border-danger-500 focus:ring-danger-300',
        className
      )}
      {...props}
    >
      {children}
    </select>
  );
}

export function TextArea({
  className,
  hasError,
  ...props
}: TextareaHTMLAttributes<HTMLTextAreaElement> & { hasError?: boolean }) {
  return (
    <textarea
      className={cn(
        'block w-full rounded-lg border border-neutral-300 bg-white px-3 py-2 text-sm text-neutral-900 transition focus:border-brand-500 focus:outline-none focus:ring-2 focus:ring-brand-200 dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-100 dark:focus:border-brand-400 dark:focus:ring-brand-500/30',
        hasError && 'border-danger-500 focus:border-danger-500 focus:ring-danger-300',
        className
      )}
      {...props}
    />
  );
}
