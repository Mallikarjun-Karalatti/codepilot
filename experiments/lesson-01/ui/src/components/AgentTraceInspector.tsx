import React, { useState } from 'react';
import { AgentTrace } from '../types';
import { mockTraces } from '../mockData';

interface AgentTraceInspectorProps {
  traces?: AgentTrace[];
}

export const AgentTraceInspector: React.FC<AgentTraceInspectorProps> = ({ traces = mockTraces }) => {
  const [selectedTraceId, setSelectedTraceId] = useState<string>(traces[0]?.id || '');
  const activeTrace = traces.find(t => t.id === selectedTraceId) || traces[0];

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '320px 1fr', gap: '20px', height: 'calc(100vh - 170px)' }}>
      {/* Traces List */}
      <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <div style={{ padding: '16px', borderBottom: '1px solid var(--border-subtle)' }}>
          <h2 style={{ fontSize: '15px', fontWeight: 700, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: '8px' }}>
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
            </svg>
            Execution Traces
          </h2>
          <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>{traces.length} recorded runs</span>
        </div>

        <div style={{ flex: 1, overflowY: 'auto', padding: '12px' }}>
          {traces.map(trace => {
            const isSelected = trace.id === selectedTraceId;
            return (
              <div
                key={trace.id}
                onClick={() => setSelectedTraceId(trace.id)}
                style={{
                  padding: '12px',
                  marginBottom: '10px',
                  borderRadius: '8px',
                  cursor: 'pointer',
                  backgroundColor: isSelected ? 'rgba(99, 102, 241, 0.15)' : 'var(--bg-secondary)',
                  border: isSelected ? '1px solid var(--accent-indigo)' : '1px solid var(--border-subtle)',
                }}
              >
                <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)', marginBottom: '6px' }}>
                  {trace.query}
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', color: 'var(--text-muted)' }}>
                  <span>{trace.steps.length} steps • {trace.totalDurationMs}ms</span>
                  <span>{trace.tokensUsed} tokens</span>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Trace Timeline Detail */}
      <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {activeTrace ? (
          <>
            <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border-subtle)' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '8px' }}>
                <h3 style={{ fontSize: '16px', fontWeight: 700, color: 'var(--text-primary)' }}>
                  {activeTrace.query}
                </h3>
                <span className="badge" style={{ backgroundColor: 'rgba(16, 185, 129, 0.15)', color: '#6ee7b7' }}>
                  Completed ({activeTrace.totalDurationMs}ms)
                </span>
              </div>
              <div style={{ fontSize: '12px', color: 'var(--text-muted)', display: 'flex', gap: '16px' }}>
                <span>Trace ID: <code>{activeTrace.id}</code></span>
                <span>Timestamp: {activeTrace.timestamp}</span>
                <span>Context Budget: <strong>{activeTrace.tokensUsed} / 2,000 tokens</strong></span>
              </div>
            </div>

            <div style={{ flex: 1, overflowY: 'auto', padding: '20px' }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
                {activeTrace.steps.map(step => (
                  <div key={step.step} style={{ display: 'flex', gap: '16px' }}>
                    {/* Step Indicator */}
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                      <div
                        style={{
                          width: '28px',
                          height: '28px',
                          borderRadius: '50%',
                          backgroundColor:
                            step.phase === 'thought' ? 'var(--accent-indigo)' :
                            step.phase === 'action' ? 'var(--accent-cyan)' : 'var(--accent-emerald)',
                          color: '#fff',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          fontSize: '12px',
                          fontWeight: 700,
                        }}
                      >
                        {step.step}
                      </div>
                      <div style={{ width: '2px', flex: 1, backgroundColor: 'var(--border-subtle)', margin: '4px 0' }} />
                    </div>

                    {/* Step Content */}
                    <div style={{ flex: 1, backgroundColor: 'var(--bg-secondary)', border: '1px solid var(--border-subtle)', borderRadius: '8px', padding: '14px 16px' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                          <span
                            className="badge"
                            style={{
                              backgroundColor:
                                step.phase === 'thought' ? 'rgba(99, 102, 241, 0.15)' :
                                step.phase === 'action' ? 'rgba(6, 182, 212, 0.15)' : 'rgba(16, 185, 129, 0.15)',
                              color:
                                step.phase === 'thought' ? '#a5b4fc' :
                                step.phase === 'action' ? '#67e8f9' : '#6ee7b7',
                            }}
                          >
                            {step.phase.toUpperCase()}
                          </span>
                          {step.tool && (
                            <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)' }}>
                              Tool: <code>{step.tool}</code>
                            </span>
                          )}
                        </div>
                        <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>+{step.durationMs}ms</span>
                      </div>

                      {/* Thought Phase */}
                      {step.thought && (
                        <div style={{ fontSize: '13px', color: 'var(--text-secondary)', fontStyle: 'italic', marginBottom: '8px' }}>
                          "{step.thought}"
                        </div>
                      )}

                      {/* Tool Input / Arguments */}
                      {step.toolInput && (
                        <div style={{ marginBottom: '8px' }}>
                          <span style={{ fontSize: '11px', color: 'var(--text-muted)', textTransform: 'uppercase', fontWeight: 600 }}>Arguments:</span>
                          <pre className="font-mono" style={{ fontSize: '11px', backgroundColor: 'var(--bg-primary)', padding: '6px 10px', borderRadius: '4px', marginTop: '2px', color: '#94a3b8' }}>
                            {JSON.stringify(step.toolInput, null, 2)}
                          </pre>
                        </div>
                      )}

                      {/* Tool Output */}
                      {step.toolOutput && (
                        <div style={{ marginBottom: '8px' }}>
                          <span style={{ fontSize: '11px', color: 'var(--text-muted)', textTransform: 'uppercase', fontWeight: 600 }}>Output:</span>
                          <div style={{ fontSize: '12px', color: '#cbd5e1', backgroundColor: 'var(--bg-primary)', padding: '8px 10px', borderRadius: '4px', marginTop: '2px' }}>
                            {step.toolOutput}
                          </div>
                        </div>
                      )}

                      {/* Evidence Selected */}
                      {step.evidenceUsed && step.evidenceUsed.length > 0 && (
                        <div style={{ marginTop: '10px' }}>
                          <span style={{ fontSize: '11px', color: 'var(--text-muted)', textTransform: 'uppercase', fontWeight: 600 }}>
                            Retrieved Evidence Candidates:
                          </span>
                          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px', marginTop: '4px' }}>
                            {step.evidenceUsed.map(ev => (
                              <div
                                key={ev.chunkId}
                                style={{
                                  backgroundColor: 'rgba(99, 102, 241, 0.1)',
                                  border: '1px solid rgba(99, 102, 241, 0.25)',
                                  borderRadius: '4px',
                                  padding: '4px 8px',
                                  fontSize: '11px',
                                  display: 'flex',
                                  alignItems: 'center',
                                  gap: '6px',
                                }}
                              >
                                <span style={{ fontWeight: 600, color: '#e0e7ff' }}>{ev.name}</span>
                                <span style={{ color: 'var(--text-muted)' }}>({ev.file}:{ev.lines})</span>
                                <span className="badge badge-rrf" style={{ fontSize: '9px', padding: '1px 4px' }}>
                                  {ev.relationType}
                                </span>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                      {/* Final Answer synthesis */}
                      {step.phase === 'final_answer' && (
                        <div>
                          <div style={{ fontSize: '13px', lineHeight: '1.6', color: '#f1f5f9', whiteSpace: 'pre-line' }}>
                            {activeTrace.finalAnswer}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)' }}>
            Select an execution trace to inspect the chronological timeline.
          </div>
        )}
      </div>
    </div>
  );
};
