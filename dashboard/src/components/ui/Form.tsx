import React, { useState, useRef, useEffect } from 'react';
import { Search, ChevronDown, Check, X, Plus } from 'lucide-react';

interface InputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'prefix'> {
  label?: React.ReactNode;
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

export interface SearchableOption {
  value: string;
  label: string;
  description?: string;
  badge?: string;
}

interface SearchableSelectProps {
  label?: React.ReactNode;
  hint?: string;
  error?: string;
  value: string;
  onChange: (value: string) => void;
  options: SearchableOption[];
  placeholder?: string;
  searchPlaceholder?: string;
  allowCustom?: boolean;
  customLabel?: (search: string) => React.ReactNode;
  emptyMessage?: string;
  disabled?: boolean;
  className?: string;
  required?: boolean;
}

export const SearchableSelect: React.FC<SearchableSelectProps> = ({
  label,
  hint,
  error,
  value,
  onChange,
  options,
  placeholder = 'Select an option...',
  searchPlaceholder = 'Search...',
  allowCustom = false,
  customLabel,
  emptyMessage = 'No matching options found',
  disabled = false,
  className = '',
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  useEffect(() => {
    if (isOpen) {
      setTimeout(() => inputRef.current?.focus(), 50);
    } else {
      setSearch('');
    }
  }, [isOpen]);

  const filteredOptions = options.filter(
    (opt) =>
      opt.label.toLowerCase().includes(search.toLowerCase()) ||
      opt.value.toLowerCase().includes(search.toLowerCase()) ||
      (opt.description && opt.description.toLowerCase().includes(search.toLowerCase())) ||
      (opt.badge && opt.badge.toLowerCase().includes(search.toLowerCase()))
  );

  const exactMatch = options.some(
    (opt) => opt.value.toLowerCase() === search.trim().toLowerCase()
  );

  const selectedOption = options.find((opt) => opt.value === value);

  return (
    <div className={`space-y-1 relative ${className}`} ref={containerRef}>
      {label && (
        <div className="block text-xs font-medium text-neutral-400">{label}</div>
      )}
      <div
        onClick={() => !disabled && setIsOpen(!isOpen)}
        className={`
          w-full bg-neutral-950 border rounded-lg px-3 py-2 text-sm flex items-center justify-between
          cursor-pointer transition-colors select-none
          ${disabled ? 'opacity-50 cursor-not-allowed border-neutral-800' : 'hover:border-neutral-700'}
          ${error ? 'border-red-500/50' : isOpen ? 'border-neutral-600 ring-1 ring-neutral-700' : 'border-neutral-800'}
        `}
      >
        <span className={`truncate font-mono text-xs ${value ? 'text-neutral-100' : 'text-neutral-500'}`}>
          {selectedOption ? (
            <span className="flex items-center gap-2">
              <span className="truncate">{selectedOption.label}</span>
              {selectedOption.badge && (
                <span className="text-[10px] px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-800 text-neutral-400 font-sans shrink-0">
                  {selectedOption.badge}
                </span>
              )}
            </span>
          ) : (
            value || placeholder
          )}
        </span>
        <div className="flex items-center gap-1.5 text-neutral-500 ml-2 shrink-0">
          {value && !disabled && (
            <span
              onClick={(e) => {
                e.stopPropagation();
                onChange('');
              }}
              className="p-0.5 hover:text-neutral-300 rounded hover:bg-neutral-800 transition-colors"
              title="Clear selection"
            >
              <X className="w-3.5 h-3.5" />
            </span>
          )}
          <ChevronDown className={`w-3.5 h-3.5 transition-transform ${isOpen ? 'rotate-180' : ''}`} />
        </div>
      </div>

      {isOpen && (
        <div className="absolute left-0 right-0 mt-1 z-50 bg-neutral-900 border border-neutral-800 rounded-lg shadow-2xl overflow-hidden flex flex-col max-h-72">
          <div className="p-2 border-b border-neutral-800/80 bg-neutral-950/60 sticky top-0 z-10">
            <div className="relative">
              <Search className="w-3.5 h-3.5 text-neutral-500 absolute left-2.5 top-2.5" />
              <input
                ref={inputRef}
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') setIsOpen(false);
                  if (e.key === 'Enter' && allowCustom && search.trim() && !exactMatch) {
                    e.preventDefault();
                    onChange(search.trim());
                    setIsOpen(false);
                  }
                }}
                placeholder={searchPlaceholder}
                className="w-full bg-neutral-900 border border-neutral-800 rounded-md pl-8 pr-3 py-1.5 text-xs text-neutral-200 placeholder-neutral-500 focus:outline-none focus:border-neutral-600 font-mono"
              />
            </div>
          </div>

          <div className="overflow-y-auto flex-1 p-1 divide-y divide-neutral-800/30">
            {allowCustom && search.trim() && !exactMatch && (
              <div
                onClick={() => {
                  onChange(search.trim());
                  setIsOpen(false);
                }}
                className="flex items-center gap-2 px-2.5 py-2 text-xs font-mono text-emerald-400 hover:bg-emerald-500/10 cursor-pointer rounded transition-colors"
              >
                <Plus className="w-3.5 h-3.5 shrink-0" />
                {customLabel ? (
                  customLabel(search.trim())
                ) : (
                  <span>Use custom: &quot;<strong className="text-emerald-300">{search.trim()}</strong>&quot;</span>
                )}
              </div>
            )}

            {filteredOptions.length === 0 && (!allowCustom || !search.trim()) ? (
              <div className="p-4 text-center text-xs text-neutral-500">
                {emptyMessage}
              </div>
            ) : (
              filteredOptions.map((opt) => {
                const isSelected = opt.value === value;
                return (
                  <div
                    key={opt.value}
                    onClick={() => {
                      onChange(opt.value);
                      setIsOpen(false);
                    }}
                    className={`
                      flex items-start justify-between px-2.5 py-2 text-xs rounded cursor-pointer transition-colors
                      ${isSelected ? 'bg-blue-500/15 text-blue-300' : 'text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100'}
                    `}
                  >
                    <div className="min-w-0 flex-1 pr-2">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className={`font-medium ${isSelected ? 'text-blue-300 font-semibold' : 'text-neutral-200'}`}>
                          {opt.label}
                        </span>
                        {opt.badge && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded bg-neutral-800/80 border border-neutral-700/80 text-neutral-400 font-sans">
                            {opt.badge}
                          </span>
                        )}
                      </div>
                      {opt.description && (
                        <p className="text-[11px] text-neutral-500 font-mono mt-0.5 truncate">
                          {opt.description}
                        </p>
                      )}
                    </div>
                    {isSelected && <Check className="w-3.5 h-3.5 text-blue-400 shrink-0 mt-0.5" />}
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}

      {hint && !error && <p className="text-[11px] text-neutral-600">{hint}</p>}
      {error && <p className="text-[11px] text-red-400">{error}</p>}
    </div>
  );
};

interface MultiSearchableSelectProps {
  label?: React.ReactNode;
  hint?: string;
  error?: string;
  values: string[];
  onChange: (values: string[]) => void;
  options: SearchableOption[];
  placeholder?: string;
  searchPlaceholder?: string;
  allowCustom?: boolean;
  disabled?: boolean;
  className?: string;
}

export const MultiSearchableSelect: React.FC<MultiSearchableSelectProps> = ({
  label,
  hint,
  error,
  values = [],
  onChange,
  options,
  placeholder = 'Select fallback models...',
  searchPlaceholder = 'Search fallback models...',
  allowCustom = true,
  disabled = false,
  className = '',
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  useEffect(() => {
    if (isOpen) {
      setTimeout(() => inputRef.current?.focus(), 50);
    } else {
      setSearch('');
    }
  }, [isOpen]);

  const filteredOptions = options.filter(
    (opt) =>
      opt.label.toLowerCase().includes(search.toLowerCase()) ||
      opt.value.toLowerCase().includes(search.toLowerCase())
  );

  const toggleOption = (val: string) => {
    if (values.includes(val)) {
      onChange(values.filter((v) => v !== val));
    } else {
      onChange([...values, val]);
    }
  };

  const removeValue = (val: string) => {
    onChange(values.filter((v) => v !== val));
  };

  const exactMatch =
    options.some((opt) => opt.value.toLowerCase() === search.trim().toLowerCase()) ||
    values.includes(search.trim());

  return (
    <div className={`space-y-1 relative ${className}`} ref={containerRef}>
      {label && (
        <div className="block text-xs font-medium text-neutral-400">{label}</div>
      )}

      {/* Trigger & Selected Tags */}
      <div
        onClick={() => !disabled && setIsOpen(!isOpen)}
        className={`
          w-full min-h-[38px] bg-neutral-950 border rounded-lg px-2.5 py-1.5 text-sm flex items-center justify-between
          cursor-pointer transition-colors select-none
          ${disabled ? 'opacity-50 cursor-not-allowed border-neutral-800' : 'hover:border-neutral-700'}
          ${error ? 'border-red-500/50' : isOpen ? 'border-neutral-600 ring-1 ring-neutral-700' : 'border-neutral-800'}
        `}
      >
        <div className="flex flex-wrap gap-1.5 flex-1 min-w-0 mr-2">
          {values.length === 0 ? (
            <span className="text-xs text-neutral-500 font-mono py-0.5">{placeholder}</span>
          ) : (
            values.map((val, idx) => (
              <span
                key={val}
                className="inline-flex items-center gap-1 text-[11px] font-mono px-2 py-0.5 rounded bg-neutral-900 border border-neutral-700 text-neutral-300 shadow-sm"
              >
                <span className="text-[9px] font-bold text-neutral-500">#{idx + 1}</span>
                <span className="truncate max-w-[180px]">{val}</span>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    removeValue(val);
                  }}
                  className="text-neutral-500 hover:text-red-400 ml-0.5 rounded transition-colors"
                  title="Remove fallback"
                >
                  <X className="w-3 h-3" />
                </button>
              </span>
            ))
          )}
        </div>

        <div className="flex items-center gap-1.5 text-neutral-500 shrink-0">
          {values.length > 0 && !disabled && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onChange([]);
              }}
              className="p-0.5 hover:text-neutral-300 rounded hover:bg-neutral-800 transition-colors text-[10px]"
              title="Clear all fallbacks"
            >
              <X className="w-3.5 h-3.5" />
            </button>
          )}
          <ChevronDown className={`w-3.5 h-3.5 transition-transform ${isOpen ? 'rotate-180' : ''}`} />
        </div>
      </div>

      {isOpen && (
        <div className="absolute left-0 right-0 mt-1 z-50 bg-neutral-900 border border-neutral-800 rounded-lg shadow-2xl overflow-hidden flex flex-col max-h-64">
          <div className="p-2 border-b border-neutral-800/80 bg-neutral-950/60 sticky top-0">
            <div className="relative">
              <Search className="w-3.5 h-3.5 text-neutral-500 absolute left-2.5 top-2.5" />
              <input
                ref={inputRef}
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') setIsOpen(false);
                  if (e.key === 'Enter' && allowCustom && search.trim() && !exactMatch) {
                    e.preventDefault();
                    toggleOption(search.trim());
                    setSearch('');
                  }
                }}
                placeholder={searchPlaceholder}
                className="w-full bg-neutral-900 border border-neutral-800 rounded-md pl-8 pr-3 py-1.5 text-xs text-neutral-200 placeholder-neutral-500 focus:outline-none focus:border-neutral-600 font-mono"
              />
            </div>
          </div>

          <div className="overflow-y-auto flex-1 p-1 divide-y divide-neutral-800/30">
            {allowCustom && search.trim() && !exactMatch && (
              <div
                onClick={() => {
                  toggleOption(search.trim());
                  setSearch('');
                }}
                className="flex items-center gap-2 px-2.5 py-2 text-xs font-mono text-emerald-400 hover:bg-emerald-500/10 cursor-pointer rounded transition-colors"
              >
                <Plus className="w-3.5 h-3.5 shrink-0" />
                <span>Add custom fallback: &quot;<strong className="text-emerald-300">{search.trim()}</strong>&quot;</span>
              </div>
            )}

            {filteredOptions.length === 0 && (!allowCustom || !search.trim()) ? (
              <div className="p-4 text-center text-xs text-neutral-500">
                No matching models found
              </div>
            ) : (
              filteredOptions.map((opt) => {
                const isSelected = values.includes(opt.value);
                const orderIndex = values.indexOf(opt.value);
                return (
                  <div
                    key={opt.value}
                    onClick={() => toggleOption(opt.value)}
                    className={`
                      flex items-center justify-between px-2.5 py-1.5 text-xs font-mono rounded cursor-pointer transition-colors
                      ${isSelected ? 'bg-blue-500/15 text-blue-300 font-medium' : 'text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100'}
                    `}
                  >
                    <span className="truncate">{opt.label}</span>
                    {isSelected && (
                      <div className="flex items-center gap-1.5 ml-2 shrink-0">
                        <span className="text-[10px] px-1 py-0.2 rounded bg-blue-500/20 text-blue-300 font-bold">
                          #{orderIndex + 1}
                        </span>
                        <Check className="w-3.5 h-3.5 text-blue-400" />
                      </div>
                    )}
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}

      {hint && !error && <p className="text-[11px] text-neutral-600">{hint}</p>}
      {error && <p className="text-[11px] text-red-400">{error}</p>}
    </div>
  );
};
