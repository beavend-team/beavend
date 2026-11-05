import React from 'react';

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'outline';
type ButtonSize = 'xs' | 'sm' | 'md';

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  icon?: React.ReactNode;
  iconRight?: React.ReactNode;
  loading?: boolean;
  children?: React.ReactNode;
}

const variantClasses: Record<ButtonVariant, string> = {
  primary:
    'bg-emerald-600 hover:bg-emerald-500 active:bg-emerald-700 text-white shadow-sm shadow-emerald-950/40 border border-emerald-500 focus-visible:ring-2 focus-visible:ring-emerald-500/50',
  secondary:
    'bg-neutral-800 hover:bg-neutral-700 active:bg-neutral-750 text-neutral-200 border border-neutral-700 focus-visible:ring-2 focus-visible:ring-neutral-500/50',
  ghost:
    'text-neutral-400 hover:text-neutral-100 hover:bg-neutral-800 active:bg-neutral-800/80 focus-visible:ring-2 focus-visible:ring-neutral-500/50',
  danger:
    'bg-red-600/20 hover:bg-red-600/30 active:bg-red-600/40 text-red-400 border border-red-500/30 focus-visible:ring-2 focus-visible:ring-red-500/50',
  outline:
    'border border-neutral-700 text-neutral-300 hover:border-neutral-600 hover:bg-neutral-800/50 active:bg-neutral-800/70 focus-visible:ring-2 focus-visible:ring-neutral-500/50',
};

const sizeClasses: Record<ButtonSize, string> = {
  xs: 'px-2.5 py-1.5 text-xs rounded-lg gap-1.5',
  sm: 'px-3 py-1.5 text-xs rounded-lg gap-2',
  md: 'px-4 py-2 text-sm rounded-lg gap-2',
};

export const Button: React.FC<ButtonProps> = ({
  variant = 'secondary',
  size = 'sm',
  icon,
  iconRight,
  loading = false,
  children,
  className = '',
  disabled,
  ...props
}) => {
  return (
    <button
      {...props}
      disabled={disabled || loading}
      className={`
        inline-flex items-center justify-center font-medium whitespace-nowrap flex-shrink-0 select-none transition-all duration-150 focus:outline-none focus-visible:ring-offset-2 focus-visible:ring-offset-neutral-950
        disabled:opacity-50 disabled:cursor-not-allowed active:scale-[0.98]
        ${variantClasses[variant]}
        ${sizeClasses[size]}
        ${className}
      `}
    >
      {loading ? (
        <svg className="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path
            className="opacity-75"
            fill="currentColor"
            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
          />
        </svg>
      ) : (
        icon && <span className="flex-shrink-0">{icon}</span>
      )}
      {children && <span>{children}</span>}
      {iconRight && <span className="flex-shrink-0">{iconRight}</span>}
    </button>
  );
};
