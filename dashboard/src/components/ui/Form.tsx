import React from 'react';

interface InputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'prefix'> {
  label?: string;
  hint?: string;
  error?: string;
  prefix?: React.ReactNode;
}

export const Input: React.FC<InputProps> = ({
  label,
  hint,
  error,
  prefix,
  className = '',
  ...props
}) => {
  return (
    <div className="space-y-1">
      {label && (
        <label className="block text-xs font-medium text-neutral-400">{label}</label>
      )}
      <div className="relative">
        {prefix && (
          <div className="absolute inset-y-0 left-0 flex items-center pl-3 pointer-events-none text-neutral-500">
            {prefix}
          </div>
        )}
        <input
          {...props}
          className={`
            w-full bg-neutral-950 border rounded-lg px-3 py-2 text-sm text-neutral-100
            placeholder-neutral-600 focus:outline-none transition-colors
            ${error ? 'border-red-500/50 focus:border-red-500' : 'border-neutral-800 focus:border-neutral-600'}
            ${prefix ? 'pl-9' : ''}
            ${className}
          `}
        />
      </div>
      {hint && !error && <p className="text-[11px] text-neutral-600">{hint}</p>}
      {error && <p className="text-[11px] text-red-400">{error}</p>}
    </div>
  );
};

interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string;
  hint?: string;
  error?: string;
  mono?: boolean;
}

export const Textarea: React.FC<TextareaProps> = ({
  label,
  hint,
  error,
  mono = false,
  className = '',
  ...props
}) => {
  return (
    <div className="space-y-1">
      {label && (
        <label className="block text-xs font-medium text-neutral-400">{label}</label>
      )}
      <textarea
        {...props}
        className={`
          w-full bg-neutral-950 border rounded-lg px-3 py-2 text-sm text-neutral-100
          placeholder-neutral-600 focus:outline-none transition-colors resize-none
          ${error ? 'border-red-500/50 focus:border-red-500' : 'border-neutral-800 focus:border-neutral-600'}
          ${mono ? 'font-mono text-xs' : ''}
          ${className}
        `}
      />
      {hint && !error && <p className="text-[11px] text-neutral-600">{hint}</p>}
      {error && <p className="text-[11px] text-red-400">{error}</p>}
    </div>
  );
};

interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  hint?: string;
  options: { value: string; label: string }[];
}

export const Select: React.FC<SelectProps> = ({
  label,
  hint,
  options,
  className = '',
  ...props
}) => {
  return (
    <div className="space-y-1">
      {label && (
        <label className="block text-xs font-medium text-neutral-400">{label}</label>
      )}
      <select
        {...props}
        className={`
          w-full bg-neutral-950 border border-neutral-800 rounded-lg px-3 py-2
          text-sm text-neutral-100 focus:outline-none focus:border-neutral-600
          transition-colors appearance-none cursor-pointer
          ${className}
        `}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value} className="bg-neutral-900">
            {opt.label}
          </option>
        ))}
      </select>
      {hint && <p className="text-[11px] text-neutral-600">{hint}</p>}
    </div>
  );
};

interface ToggleProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label?: string;
  description?: string;
}

export const Toggle: React.FC<ToggleProps> = ({ checked, onChange, label, description }) => {
  return (
    <div className="flex items-center justify-between">
      {(label || description) && (
        <div>
          {label && <p className="text-sm font-medium text-neutral-200">{label}</p>}
          {description && <p className="text-xs text-neutral-500">{description}</p>}
        </div>
      )}
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={`
          relative inline-flex h-5 w-9 flex-shrink-0 rounded-full transition-colors duration-200
          focus:outline-none cursor-pointer
          ${checked ? 'bg-emerald-600' : 'bg-neutral-700'}
        `}
      >
        <span
          className={`
            inline-block h-4 w-4 rounded-full bg-white shadow transition-transform duration-200 mt-0.5
            ${checked ? 'translate-x-4' : 'translate-x-0.5'}
          `}
        />
      </button>
    </div>
  );
};
