import React, { useEffect, useState } from 'react';
import { api, Policy } from '../api/client';
import { ShieldCheck, Plus, Trash2, Code2, GitBranch, Check, Play, CheckCircle2, XCircle } from 'lucide-react';
import { Card } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Modal } from '../components/ui/Modal';
import { Input, Textarea } from '../components/ui/Form';
import { Alert, EmptyState, Loading } from '../components/ui/Feedback';
import { formatRelativeTime } from '../lib/utils';

const examplePolicies = [
  { label: 'Require Pro plan', expr: "claims.plan in ['pro', 'team']" },
  { label: 'Email verified', expr: 'claims.email_verified == true' },
  { label: 'AI feature flag', expr: "claims.ai_access == true && claims.plan != 'free'" },
];

export const PoliciesPage: React.FC = () => {
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  // Version history
  const [selectedPolicy, setSelectedPolicy] = useState<string | null>(null);
  const [policyDetail, setPolicyDetail] = useState<{ policy: any; versions: any[] } | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [showVersionModal, setShowVersionModal] = useState(false);
  const [newExpression, setNewExpression] = useState('');
  const [newVersionSetActive, setNewVersionSetActive] = useState(true);
  const [savingVersion, setSavingVersion] = useState(false);

  // Test Sandbox
  const [showTestModal, setShowTestModal] = useState(false);
  const [testExpression, setTestExpression] = useState("claims.plan in ['pro', 'team'] && claims.email_verified == true");
  const [testClaims, setTestClaims] = useState('{\n  "sub": "usr_123",\n  "plan": "pro",\n  "email_verified": true\n}');
  const [testResult, setTestResult] = useState<{ valid: boolean; allowed: boolean; error?: string } | null>(null);
  const [testingPolicy, setTestingPolicy] = useState(false);

  const [policyId, setPolicyId] = useState('');
  const [policyName, setPolicyName] = useState('');
  const [policyDesc, setPolicyDesc] = useState('');
  const [expression, setExpression] = useState("claims.plan in ['pro', 'team'] && claims.ai_access == true");

  const loadData = async () => {
    setLoading(true);
    try {
      setPolicies(await api.listPolicies().catch(() => []));
    } finally {
      setLoading(false);
    }
  };

  const openDetail = async (id: string) => {
    setSelectedPolicy(id);
    setLoadingDetail(true);
    try {
      setPolicyDetail(await api.getPolicy(id));
    } catch (_) {
      setPolicyDetail(null);
    } finally {
      setLoadingDetail(false);
    }
  };

  useEffect(() => { loadData(); }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setCreating(true);
    try {
      await api.createPolicy({ id: policyId, name: policyName, description: policyDesc, expression });
      setShowModal(false);
      setPolicyId(''); setPolicyName(''); setPolicyDesc('');
      loadData();
    } catch (err: any) {
      setError(err.message || 'Failed to create policy');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(`Delete Policy "${id}"? This will invalidate active snapshots.`)) return;
    try {
      await api.deletePolicy(id);
      loadData();
    } catch (err: any) {
      alert(`Error: ${err.message}`);
    }
  };

  const handleCreateVersion = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedPolicy) return;
    setSavingVersion(true);
    try {
      await api.createPolicyVersion(selectedPolicy, { expression: newExpression, set_active: newVersionSetActive });
      setShowVersionModal(false);
      setNewExpression('');
      await openDetail(selectedPolicy);
      loadData();
    } catch (err: any) {
      alert(`Failed to create version: ${err.message}`);
    } finally {
      setSavingVersion(false);
    }
  };

  const handleSetActiveVersion = async (version: number) => {
    if (!selectedPolicy) return;
    try {
      await api.setActivePolicyVersion(selectedPolicy, version);
      await openDetail(selectedPolicy);
      loadData();
    } catch (err: any) {
      alert(`Failed to set active version: ${err.message}`);
    }
  };

  const handleTestPolicy = async (e: React.FormEvent) => {
    e.preventDefault();
    setTestingPolicy(true);
    setTestResult(null);
    try {
      let parsedClaims = {};
      try {
        parsedClaims = JSON.parse(testClaims);
      } catch {
        throw new Error('Claims must be valid JSON');
      }
      const res = await api.testPolicy({
        expression: testExpression,
        claims: parsedClaims,
      });
      setTestResult(res);
    } catch (err: any) {
      setTestResult({ valid: false, allowed: false, error: err.message });
    } finally {
      setTestingPolicy(false);
    }
  };

  return (
    <div className="space-y-5 max-w-[1100px]">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-bold text-neutral-100 tracking-tight flex items-center gap-2">
            <ShieldCheck className="w-5 h-5 text-neutral-400" />
            CEL Authorization Policies
          </h1>
          <p className="text-sm text-neutral-500 mt-0.5">
            Google Common Expression Language rules evaluated against caller JWT claims in microseconds.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="secondary" size="sm" icon={<Play className="w-3.5 h-3.5 text-emerald-400" />} onClick={() => { setShowTestModal(true); setTestResult(null); }}>
            Test Sandbox
          </Button>
          <Button variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />} onClick={() => setShowModal(true)}>
            New Policy
          </Button>
        </div>
      </div>

      {loading ? (
        <Loading text="Loading CEL policies..." />
      ) : policies.length === 0 ? (
        <div className="space-y-3">
          <EmptyState
            icon={<ShieldCheck className="w-6 h-6" />}
            title="No Policies defined"
            description="Create claim-based authorization rules that control access to AI apps and tools."
            action={
              <Button variant="primary" size="sm" icon={<Plus className="w-3.5 h-3.5" />} onClick={() => setShowModal(true)}>
                Create Policy
              </Button>
            }
          />
          {/* Example policies */}
          <div className="mt-4">
            <p className="text-xs font-medium text-neutral-500 mb-2">Example policies:</p>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              {examplePolicies.map((ex) => (
                <div key={ex.label} className="bg-neutral-900 border border-neutral-800 rounded-lg p-3 cursor-pointer hover:border-neutral-700 transition-colors" onClick={() => { setExpression(ex.expr); setShowModal(true); }}>
                  <p className="text-xs font-medium text-neutral-400 mb-1">{ex.label}</p>
                  <code className="text-[11px] font-mono text-emerald-400">{ex.expr}</code>
                </div>
              ))}
            </div>
          </div>
        </div>
      ) : (
        <div className="space-y-2">
          {policies.map((p) => (
            <div key={p.id} className="bg-neutral-900 border border-neutral-800 rounded-xl p-4 group flex items-start gap-4 hover:border-neutral-700 transition-colors cursor-pointer" onClick={() => openDetail(p.id)}>
              <div className="w-9 h-9 rounded-lg bg-purple-500/10 border border-purple-500/15 flex items-center justify-center flex-shrink-0">
                <ShieldCheck className="w-4 h-4 text-purple-400" />
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-0.5">
                  <h3 className="text-sm font-semibold text-neutral-100">{p.name}</h3>
                  <Badge variant="mono">v{p.active_version}</Badge>
                  <Badge variant="purple">CEL</Badge>
                </div>
                <p className="text-[11px] font-mono text-neutral-600 mb-1">{p.id}</p>
                {p.description && <p className="text-xs text-neutral-500 mb-1">{p.description}</p>}
                <p className="text-[11px] text-emerald-600 flex items-center gap-1">
                  <GitBranch className="w-3 h-3" /> View version history
                </p>
              </div>
              <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                <button
                  onClick={async (e) => {
                    e.stopPropagation();
                    const det = await api.getPolicy(p.id).catch(() => null);
                    const activeExpr = det?.versions?.find((v: any) => v.version === p.active_version)?.expression || det?.versions?.[0]?.expression || '';
                    if (activeExpr) setTestExpression(activeExpr);
                    setShowTestModal(true);
                    setTestResult(null);
                  }}
                  className="p-1.5 text-neutral-600 hover:text-emerald-400 hover:bg-neutral-800 rounded transition-colors"
                  title="Test in CEL Sandbox"
                >
                  <Play className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={(e) => { e.stopPropagation(); handleDelete(p.id); }}
                  className="p-1.5 text-neutral-600 hover:text-red-400 hover:bg-neutral-800 rounded transition-colors"
                  title="Delete Policy"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Create Policy Modal */}
      <Modal
        open={showModal}
        onClose={() => { setShowModal(false); setError(null); }}
        title="Define CEL Policy"
        description="Policy expressions are compiled and validated with the Google CEL compiler"
        size="md"
      >
        {error && <Alert variant="error" className="mb-4" onDismiss={() => setError(null)}>{error}</Alert>}
        <form onSubmit={handleCreate} className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Input label="Policy ID" required placeholder="e.g. require_pro_tier" value={policyId} onChange={(e) => setPolicyId(e.target.value)} className="font-mono" />
            <Input label="Display Name" required placeholder="e.g. Require Pro Plan" value={policyName} onChange={(e) => setPolicyName(e.target.value)} />
          </div>
          <Input label="Description" placeholder="What this policy authorizes..." value={policyDesc} onChange={(e) => setPolicyDesc(e.target.value)} />
          <Textarea
            label="CEL Expression"
            rows={4}
            required
            value={expression}
            onChange={(e) => setExpression(e.target.value)}
            mono
            hint="Evaluated against caller's JWT claims. Available: claims.sub, claims.iss, claims.email, claims.plan, etc."
          />
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button type="submit" variant="primary" size="sm" loading={creating} icon={<ShieldCheck className="w-3.5 h-3.5" />}>Save Policy</Button>
          </div>
        </form>
      </Modal>

      {/* Version History Panel */}
      {selectedPolicy && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" onClick={() => { setSelectedPolicy(null); setPolicyDetail(null); }} />
          <div className="relative w-full max-w-xl bg-neutral-900 border border-neutral-800 rounded-2xl shadow-2xl max-h-[80vh] flex flex-col">
            <div className="flex items-center justify-between p-5 border-b border-neutral-800">
              <div>
                <h3 className="text-sm font-semibold text-neutral-100">Policy Version History</h3>
                <p className="text-[11px] font-mono text-neutral-600 mt-0.5">{selectedPolicy}</p>
              </div>
              <Button variant="secondary" size="xs" icon={<Plus className="w-3.5 h-3.5" />} onClick={() => { setNewExpression(policyDetail?.versions?.[0]?.expression || ''); setShowVersionModal(true); }}>
                New Version
              </Button>
            </div>

            <div className="overflow-y-auto p-5 space-y-2">
              {loadingDetail ? (
                <p className="text-xs text-neutral-600 py-4 text-center">Loading versions...</p>
              ) : !policyDetail?.versions?.length ? (
                <p className="text-xs text-neutral-600 text-center py-4">No version history available.</p>
              ) : (
                policyDetail.versions.map((v: any) => {
                  const isActive = v.version === policyDetail.policy?.active_version || v.is_active;
                  return (
                    <div key={v.id} className="bg-neutral-950 border border-neutral-800 rounded-xl p-3">
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          <Badge variant="mono">v{v.version}</Badge>
                          {isActive && <Badge variant="success" dot>active</Badge>}
                        </div>
                        <div className="flex items-center gap-2">
                          {!isActive && (
                            <Button
                              variant="secondary"
                              size="xs"
                              onClick={() => handleSetActiveVersion(v.version)}
                            >
                              Set Active
                            </Button>
                          )}
                          <button
                            onClick={() => {
                              setTestExpression(v.expression);
                              setShowTestModal(true);
                              setTestResult(null);
                            }}
                            className="text-xs text-neutral-500 hover:text-emerald-400 flex items-center gap-1 transition-colors"
                          >
                            <Play className="w-3 h-3" /> Test
                          </button>
                        </div>
                      </div>
                      <pre className="text-[11px] font-mono text-emerald-400 whitespace-pre-wrap">{v.expression}</pre>
                    </div>
                  );
                })
              )}
            </div>
          </div>
        </div>
      )}

      {/* New Version Modal */}
      <Modal open={showVersionModal} onClose={() => setShowVersionModal(false)} title="Create New Policy Version" size="md">
        <form onSubmit={handleCreateVersion} className="space-y-4">
          <Textarea label="CEL Expression" rows={5} required mono value={newExpression} onChange={(e) => setNewExpression(e.target.value)} hint="Backend validates against Google CEL compiler before saving" />
          <div className="flex items-center gap-3">
            <input type="checkbox" checked={newVersionSetActive} onChange={(e) => setNewVersionSetActive(e.target.checked)} className="accent-emerald-500" id="setActive" />
            <label htmlFor="setActive" className="text-xs text-neutral-400">Set as active version immediately</label>
          </div>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowVersionModal(false)}>Cancel</Button>
            <Button type="submit" variant="primary" size="sm" loading={savingVersion}>Create Version</Button>
          </div>
        </form>
      </Modal>

      {/* CEL Test Sandbox Modal */}
      <Modal
        open={showTestModal}
        onClose={() => { setShowTestModal(false); setTestResult(null); }}
        title="CEL Policy Sandbox"
        description="Compile and evaluate CEL expressions against mock JWT caller claims"
        size="lg"
      >
        <form onSubmit={handleTestPolicy} className="space-y-4">
          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs font-medium text-neutral-400">CEL Expression</label>
              <div className="flex items-center gap-1">
                {examplePolicies.map((ex) => (
                  <button
                    key={ex.label}
                    type="button"
                    onClick={() => setTestExpression(ex.expr)}
                    className="text-[10px] text-neutral-500 hover:text-neutral-300 px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-800 transition-colors"
                  >
                    {ex.label}
                  </button>
                ))}
              </div>
            </div>
            <Textarea
              rows={3}
              required
              mono
              value={testExpression}
              onChange={(e) => setTestExpression(e.target.value)}
              placeholder="claims.plan == 'pro'"
            />
          </div>

          <Textarea
            label="Mock JWT Claims (JSON)"
            rows={5}
            required
            mono
            value={testClaims}
            onChange={(e) => setTestClaims(e.target.value)}
            hint="Input claims object as delivered by validated identity token"
          />

          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setShowTestModal(false)}>Close</Button>
            <Button type="submit" variant="primary" size="sm" loading={testingPolicy} icon={<Play className="w-3.5 h-3.5" />}>
              Evaluate Expression
            </Button>
          </div>
        </form>

        {testResult && (
          <div className="mt-4 pt-4 border-t border-neutral-800 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-neutral-300">Evaluation Result</span>
              <div className="flex items-center gap-2">
                <Badge variant={testResult.valid ? 'success' : 'danger'}>
                  {testResult.valid ? 'Syntax Valid' : 'Syntax Error'}
                </Badge>
                {testResult.valid && (
                  <Badge variant={testResult.allowed ? 'success' : 'danger'}>
                    {testResult.allowed ? 'ALLOWED' : 'DENIED'}
                  </Badge>
                )}
              </div>
            </div>

            {testResult.error ? (
              <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-xl">
                <div className="flex items-center gap-2 mb-1">
                  <XCircle className="w-4 h-4 text-red-400" />
                  <span className="text-xs font-semibold text-red-400">Compilation / Eval Error</span>
                </div>
                <pre className="text-[11px] font-mono text-red-300 whitespace-pre-wrap">{testResult.error}</pre>
              </div>
            ) : (
              <div className={`p-3 rounded-xl border ${testResult.allowed ? 'bg-emerald-500/10 border-emerald-500/20' : 'bg-amber-500/10 border-amber-500/20'}`}>
                <div className="flex items-center gap-2">
                  {testResult.allowed ? (
                    <>
                      <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                      <span className="text-xs font-semibold text-emerald-300">Policy Passed: Caller is authorized</span>
                    </>
                  ) : (
                    <>
                      <XCircle className="w-4 h-4 text-amber-400" />
                      <span className="text-xs font-semibold text-amber-300">Policy Failed: Caller access denied</span>
                    </>
                  )}
                </div>
              </div>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
};
