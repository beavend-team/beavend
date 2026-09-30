import React from 'react';

type BadgeVariant = 'default' | 'success' | 'warning' | 'danger' | 'info' | 'purple' | 'mono';

interface BadgeProps {
  variant?: BadgeVariant;
  children: React.ReactNode;
  className?: string;
  dot?: boolean;
}

const variantClasses: Record<BadgeVariant, string> = {
  default: 'bg-neutral-800 text-neutral-300 border-neutral-700',
  success: 'bg-emerald-500/12 text-emerald-400 border-emerald-500/20',
  warning: 'bg-amber-500/12 text-amber-400 border-amber-500/20',
  danger: 'bg-red-500/12 text-red-400 border-red-500/20',
  info: 'bg-blue-500/12 text-blue-400 border-blue-500/20',
  purple: 'bg-purple-500/12 text-purple-400 border-purple-500/20',
  mono: 'bg-neutral-900 text-neutral-300 border-neutral-700 font-mono',
};

const dotClasses: Record<BadgeVariant, string> = {
  default: 'bg-neutral-400',
  success: 'bg-emerald-400',
  warning: 'bg-amber-400',
  danger: 'bg-red-400',
  info: 'bg-blue-400',
  purple: 'bg-purple-400',
  mono: 'bg-neutral-400',
};

export const Badge: React.FC<BadgeProps> = ({
  variant = 'default',
  children,
  className = '',
  dot = false,
}) => {
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-[11px] font-medium border ${variantClasses[variant]} ${className}`}
    >
      {dot && (
        <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${dotClasses[variant]}`} />
      )}
      {children}
    </span>
  );
};
