import React, { useState } from 'react';
import { HybridSearchResult } from '../types';
import { mockSearchResults } from '../mockData';

interface ChunkInspectorProps {
  onSearch?: (query: string, topK: number) => Promise<HybridSearchResult[]>;
}

export const ChunkInspector: React.FC<ChunkInspectorProps> = ({ onSearch }) => {
  const [query, setQuery] = useState('How are HTTP routes registered and handled?');
  const [topK, setTopK] = useState(5);
  const [isLoading, setIsLoading] = useState(false);
  const [results, setResults] = useState<HybridSearchResult[]>(mockSearchResults['default']);
  const [expandedChunkId, setExpandedChunkId] = useState<number | null>(results[0]?.chunk.id || null);

  const handleSearch = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!query.trim()) return;

    setIsLoading(true);
    if (onSearch) {
      try {
        const liveResults = await onSearch(query, topK);
        setResults(liveResults);
        if (liveResults.length > 0) setExpandedChunkId(liveResults[0].chunk.id);
      } catch (err) {
        console.warn('Live search failed, falling back to mock results', err);
        setResults(mockSearchResults['default']);
      }
    } else {
      setTimeout(() => {
        setResults(mockSearchResults['default']);
        setIsLoading(false);
      }, 150);
      return;
    }
    setIsLoading(false);
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px', height: 'calc(100vh - 280px)' }}>
      {/* Search Input Bar & Fusion Controls */}
      <div className="glass-panel" style={{ padding: '16px 20px' }}>
        <form onSubmit={handleSearch} style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
          <div style={{ flex: 1, position: 'relative' }}>
            <input
              type="text"
              value={query}
              onChange={e => setQuery(e.target.value)}
              placeholder="Search code semantics or symbols (e.g. How are HTTP routes registered?)..."
              style={{
                width: '100%',
                padding: '12px 16px 12px 42px',
                backgroundColor: 'var(--bg-primary)',
                border: '1px solid var(--border-color)',
                borderRadius: '8px',
                color: 'var(--text-primary)',
                fontSize: '14px',
                outline: 'none',
              }}
            />
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--text-muted)"
              strokeWidth="2"
              style={{ position: 'absolute', left: '14px', top: '14px' }}
            >
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', backgroundColor: 'var(--bg-primary)', padding: '6px 12px', borderRadius: '8px', border: '1px solid var(--border-color)' }}>
            <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>Top-K:</span>
            <select
              value={topK}
              onChange={e => setTopK(Number(e.target.value))}
              style={{
                background: 'transparent',
                border: 'none',
                color: 'var(--text-primary)',
                fontSize: '13px',
                fontWeight: 600,
                outline: 'none',
                cursor: 'pointer',
              }}
            >
              <option value={3}>3</option>
              <option value={5}>5</option>
              <option value={10}>10</option>
            </select>
          </div>

          <button
            type="submit"
            disabled={isLoading}
            style={{
              padding: '12px 24px',
              backgroundColor: 'var(--accent-indigo)',
              border: 'none',
              borderRadius: '8px',
              color: '#fff',
              fontSize: '13px',
              fontWeight: 600,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
            }}
          >
            {isLoading ? 'Searching...' : 'Run Hybrid RRF'}
          </button>
        </form>

        <div style={{ display: 'flex', gap: '20px', marginTop: '12px', fontSize: '12px', color: 'var(--text-muted)' }}>
          <span>Formula: <code>RRF = 1/(60 + rank_dense) + 1/(60 + rank_lexical)</code></span>
          <span>•</span>
          <span>Dense: <code>qwen3-embedding (4096-dim)</code></span>
          <span>•</span>
          <span>Lexical: <code>TF-IDF Inverted Index</code></span>
        </div>
      </div>

      {/* Results List */}
      <div style={{ display: 'grid', gridTemplateColumns: '450px 1fr', gap: '20px', flex: 1, overflow: 'hidden' }}>
        {/* Candidates Pool */}
        <div className="glass-panel" style={{ overflowY: 'auto', padding: '12px' }}>
          <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', padding: '6px 8px 12px' }}>
            Retrieved Candidates ({results.length} chunks)
          </div>
          {results.map((res, idx) => {
            const isSelected = expandedChunkId === res.chunk.id;
            return (
              <div
                key={res.chunk.id}
                onClick={() => setExpandedChunkId(res.chunk.id)}
                style={{
                  padding: '12px 14px',
                  marginBottom: '10px',
                  borderRadius: '8px',
                  cursor: 'pointer',
                  backgroundColor: isSelected ? 'rgba(99, 102, 241, 0.15)' : 'var(--bg-secondary)',
                  border: isSelected ? '1px solid var(--accent-indigo)' : '1px solid var(--border-subtle)',
                  transition: 'all 0.15s ease',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '6px' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span style={{ fontSize: '12px', fontWeight: 700, color: 'var(--accent-amber)' }}>#{idx + 1}</span>
                    <span style={{ fontSize: '14px', fontWeight: 700, color: 'var(--text-primary)' }}>{res.chunk.name}</span>
                    <span className="badge badge-rrf">RRF {(res.combinedScore * 1000).toFixed(1)}m</span>
                  </div>
                  <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                    L{res.chunk.startLine}-L{res.chunk.endLine}
                  </span>
                </div>

                <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '8px' }}>
                  <code>{res.chunk.sourceFile}</code>
                </div>

                <div style={{ display: 'flex', gap: '8px', fontSize: '11px' }}>
                  <div style={{ background: 'rgba(6, 182, 212, 0.12)', color: '#67e8f9', padding: '2px 8px', borderRadius: '4px' }}>
                    Dense: {res.semanticScore.toFixed(3)} (rank #{res.semanticRank ?? idx+1})
                  </div>
                  <div style={{ background: 'rgba(16, 185, 129, 0.12)', color: '#6ee7b7', padding: '2px 8px', borderRadius: '4px' }}>
                    Lexical: {res.lexicalScore.toFixed(1)} (rank #{res.lexicalRank ?? idx+1})
                  </div>
                </div>
              </div>
            );
          })}
        </div>

        {/* Highlighted Chunk Detail */}
        <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          {expandedChunkId ? (
            (() => {
              const active = results.find(r => r.chunk.id === expandedChunkId) || results[0];
              return (
                <>
                  <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border-subtle)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '4px' }}>
                        <h3 style={{ fontSize: '18px', fontWeight: 700 }}>{active.chunk.name}</h3>
                        <span className="badge badge-method">{active.chunk.kind}</span>
                        {active.chunk.parentName && (
                          <span style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
                            parent: <strong>{active.chunk.parentName}</strong>
                          </span>
                        )}
                      </div>
                      <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                        File: <code>{active.chunk.sourceFile}</code> | Lines: <strong>{active.chunk.startLine} - {active.chunk.endLine}</strong> | Combined RRF: <strong>{active.combinedScore.toFixed(6)}</strong>
                      </div>
                    </div>
                  </div>
                  <div style={{ flex: 1, overflowY: 'auto', backgroundColor: '#060a12', padding: '20px' }}>
                    <pre className="font-mono" style={{ fontSize: '13px', lineHeight: '1.6', color: '#e2e8f0', margin: 0 }}>
                      <code>{active.chunk.text}</code>
                    </pre>
                  </div>
                </>
              );
            })()
          ) : (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)' }}>
              Select a retrieved chunk to view source code and line span highlights.
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
