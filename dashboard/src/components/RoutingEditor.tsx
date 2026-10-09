import React from 'react';
import { Plus, Trash2, ArrowUp, ArrowDown } from 'lucide-react';
import { ALL_MODALITIES, Modality, ModelRouteRule, ModelRouting, ModelRoutes } from '../api/client';
import { Button } from './ui/Button';
import { Input, Select } from './ui/Form';

/** Decode model_routes as returned by the control plane (object, JSON string, or base64 JSON). */
export function parseModelRoutes(raw: unknown, legacyDefault = ''): ModelRoutes {
  let value: any = raw;
  if (typeof value === 'string' && value) {
    try { value = JSON.parse(value); } catch (_) {
      try { value = JSON.parse(atob(value)); } catch (_) { value = null; }
    }
  }
  if (value && typeof value === 'object' && typeof value.default === 'string') {
    return { default: value.default, rules: Array.isArray(value.rules) ? value.rules : [] };
  }
  return { default: legacyDefault, rules: [] };
}

const inputCaps = (m: ModelRouting): string[] => (m.input_modalities && m.input_modalities.length ? m.input_modalities : ['text']);
const outputCaps = (m: ModelRouting): string[] => (m.output_modalities && m.output_modalities.length ? m.output_modalities : ['text']);

/** Models whose declared capabilities satisfy the rule's condition (used to filter pickers). */
export function capableModels(models: ModelRouting[], rule: ModelRouteRule): ModelRouting[] {
  const needIn = rule.when.input_has ?? [];
  const needOut = rule.when.output ?? [];
  return models.filter(
    (m) => needIn.every((x) => inputCaps(m).includes(x)) && needOut.every((x) => outputCaps(m).includes(x)),
  );
}

/** Returns a human-readable problem with the routes, or null when valid. Mirrors server-side shape checks. */
export function validateRoutes(routes: ModelRoutes, models: ModelRouting[]): string | null {
  if (!routes.default.trim()) return 'A default model is required.';
  const known = new Set(models.map((m) => m.logical_model));
  if (known.size > 0 && !known.has(routes.default)) return `Default model "${routes.default}" does not exist.`;
  for (const [i, r] of (routes.rules ?? []).entries()) {
    const c = r.when;
    const hasCondition = (c.input_has?.length ?? 0) + (c.output?.length ?? 0) + Object.keys(c.metadata ?? {}).length > 0 || !!c.expr?.trim();
    if (!hasCondition) return `Rule ${i + 1} needs at least one condition.`;
    if (!r.use) return `Rule ${i + 1} needs a target model.`;
    const target = models.find((m) => m.logical_model === r.use);
    if (!target) return `Rule ${i + 1} targets unknown model "${r.use}".`;
    if (!capableModels([target], r).length) return `Rule ${i + 1}: model "${r.use}" lacks the required capabilities.`;
  }
  return null;
}

const ModalityChips: React.FC<{ value: Modality[]; onChange: (v: Modality[]) => void }> = ({ value, onChange }) => (
  <div className="flex flex-wrap gap-2">
    {ALL_MODALITIES.map((m) => (
      <label key={m} className="flex items-center gap-1 text-[11px] text-neutral-300 cursor-pointer">
        <input
          type="checkbox"
          checked={value.includes(m)}
          onChange={() => onChange(value.includes(m) ? value.filter((x) => x !== m) : [...value, m])}
        />
        {m}
      </label>
    ))}
  </div>
);

interface RoutingEditorProps {
  models: ModelRouting[];
  value: ModelRoutes;
  onChange: (next: ModelRoutes) => void;
}

export const RoutingEditor: React.FC<RoutingEditorProps> = ({ models, value, onChange }) => {
  const rules = value.rules ?? [];
  const setRules = (next: ModelRouteRule[]) => onChange({ ...value, rules: next });
  const updateRule = (i: number, patch: Partial<ModelRouteRule>) =>
    setRules(rules.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  const updateWhen = (i: number, patch: Partial<ModelRouteRule['when']>) =>
    updateRule(i, { when: { ...rules[i].when, ...patch } });
  const move = (i: number, dir: -1 | 1) => {
    const j = i + dir;
    if (j < 0 || j >= rules.length) return;
    const next = [...rules];
    [next[i], next[j]] = [next[j], next[i]];
    setRules(next);
  };

  const modelOption = (m: ModelRouting) => ({
    value: m.logical_model,
    label: `${m.logical_model} (in: ${inputCaps(m).join('+')} / out: ${outputCaps(m).join('+')})`,
  });

  return (
    <div className="space-y-4">
      <Select
        label="Default model"
        value={value.default}
        onChange={(e) => onChange({ ...value, default: e.target.value })}
        options={[{ value: '', label: '(select a model)' }, ...models.map(modelOption)]}
      />

      <div className="space-y-3">
        <p className="text-[11px] text-neutral-500">
          Rules are evaluated top to bottom; the first match wins, otherwise the default model is used. Pickers only list
          models whose declared capabilities satisfy the rule.
        </p>
        {rules.map((rule, i) => {
          const options = capableModels(models, rule);
          const targetMissing = rule.use && !options.some((m) => m.logical_model === rule.use);
          return (
            <div key={i} className="rounded-lg border border-neutral-800 p-3 space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-neutral-300">Rule {i + 1}</span>
                <div className="flex gap-1">
                  <Button type="button" variant="ghost" size="xs" onClick={() => move(i, -1)} icon={<ArrowUp className="w-3 h-3" />} />
                  <Button type="button" variant="ghost" size="xs" onClick={() => move(i, 1)} icon={<ArrowDown className="w-3 h-3" />} />
                  <Button type="button" variant="ghost" size="xs" onClick={() => setRules(rules.filter((_, idx) => idx !== i))} icon={<Trash2 className="w-3 h-3" />} />
                </div>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div className="space-y-1">
                  <span className="text-[11px] text-neutral-400">When input has</span>
                  <ModalityChips value={rule.when.input_has ?? []} onChange={(v) => updateWhen(i, { input_has: v })} />
                </div>
                <div className="space-y-1">
                  <span className="text-[11px] text-neutral-400">When requested output is</span>
                  <ModalityChips value={rule.when.output ?? []} onChange={(v) => updateWhen(i, { output: v })} />
                </div>
                <Input
                  label="Metadata match (key=value)"
                  placeholder="complexity=high"
                  value={Object.entries(rule.when.metadata ?? {}).map(([k, v]) => `${k}=${v}`).join(', ')}
                  onChange={(e) => {
                    const metadata: Record<string, string> = {};
                    e.target.value.split(',').forEach((pair) => {
                      const [k, ...rest] = pair.split('=');
                      if (k && k.trim() && rest.length) metadata[k.trim()] = rest.join('=').trim();
                    });
                    updateWhen(i, { metadata });
                  }}
                />
                <Input
                  label="CEL expression (optional)"
                  className="font-mono"
                  value={rule.when.expr ?? ''}
                  onChange={(e) => updateWhen(i, { expr: e.target.value })}
                />
              </div>
              <Select
                label="Use model"
                value={rule.use}
                onChange={(e) => updateRule(i, { use: e.target.value })}
                options={[{ value: '', label: '(select a model)' }, ...options.map(modelOption)]}
              />
              {targetMissing && (
                <p className="text-[11px] text-amber-400">Selected model lacks the capabilities this rule requires.</p>
              )}
            </div>
          );
        })}
        <Button
          type="button"
          variant="secondary"
          size="xs"
          icon={<Plus className="w-3.5 h-3.5" />}
          onClick={() => setRules([...rules, { when: { input_has: ['image'] }, use: '' }])}
        >
          Add rule
        </Button>
      </div>
    </div>
  );
};
