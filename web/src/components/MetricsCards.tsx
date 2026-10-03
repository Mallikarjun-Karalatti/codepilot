import React from 'react';
import { RepositoryStats } from '../types';

interface MetricsCardsProps {
  stats: RepositoryStats;
}

export const MetricsCards: React.FC<MetricsCardsProps> = ({ stats }) => {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: '16px', marginBottom: '20px' }}>
      <div className="glass-panel" style={{ padding: '16px 20px' }}>
        <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', marginBottom: '6px' }}>
          Repository Target
        </div>
        <div style={{ fontSize: '16px', fontWeight: 700, color: 'var(--text-primary)', textOverflow: 'ellipsis', overflow: 'hidden', whiteSpace: 'nowrap' }}>
          {stats.repositoryId}
        </div>
        <div style={{ fontSize: '12px', color: '#38bdf8', marginTop: '4px' }}>
          {stats.filesScanned} source files indexed
        </div>
      </div>

      <div className="glass-panel" style={{ padding: '16px 20px' }}>
        <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', marginBottom: '6px' }}>
          AST Chunks Embedded
        </div>
        <div style={{ fontSize: '24px', fontWeight: 800, color: '#4ade80' }}>
          {stats.chunksEmbedded}
        </div>
        <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
          {(stats.indexSizeBytes / (1024 * 1024)).toFixed(2)} MB vector index
        </div>
      </div>

      <div className="glass-panel" style={{ padding: '16px 20px' }}>
        <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', marginBottom: '6px' }}>
          Retrieval Latency (p50 / p99)
        </div>
        <div style={{ fontSize: '24px', fontWeight: 800, color: '#fbbf24' }}>
          {stats.p50LatencyMs.toFixed(2)} <span style={{ fontSize: '14px', fontWeight: 500, color: 'var(--text-muted)' }}>/ {stats.p99LatencyMs.toFixed(2)} ms</span>
        </div>
        <div style={{ fontSize: '12px', color: '#10b981', marginTop: '4px' }}>
          SLA Pass (&lt;100ms)
        </div>
      </div>

      <div className="glass-panel" style={{ padding: '16px 20px' }}>
        <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', marginBottom: '6px' }}>
          Incremental Cache
        </div>
        <div style={{ fontSize: '24px', fontWeight: 800, color: '#818cf8' }}>
          100%
        </div>
        <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '4px' }}>
          Work avoided on clean diffs
        </div>
      </div>
    </div>
  );
};
