import React, { useState } from 'react';
import { WorkspaceFile, CodeChunk } from '../types';

interface WorkspaceViewProps {
  files: WorkspaceFile[];
}

export const WorkspaceView: React.FC<WorkspaceViewProps> = ({ files }) => {
  const [selectedFile, setSelectedFile] = useState<string>(files[0]?.path || '');
  const [selectedChunk, setSelectedChunk] = useState<CodeChunk | null>(files[0]?.chunks[0] || null);
  const [filterQuery, setFilterQuery] = useState('');

  const filteredFiles = files.filter(f => 
    f.path.toLowerCase().includes(filterQuery.toLowerCase()) ||
    f.chunks.some(c => c.name.toLowerCase().includes(filterQuery.toLowerCase()))
  );

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '320px 1fr', gap: '20px', height: 'calc(100vh - 170px)' }}>
      {/* File & AST Symbol Tree */}
      <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <div style={{ padding: '16px', borderBottom: '1px solid var(--border-subtle)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
            <h2 style={{ fontSize: '15px', fontWeight: 700, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: '8px' }}>
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z"/>
              </svg>
              Repository AST Tree
            </h2>
            <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>{files.length} files</span>
          </div>
          <input
            type="text"
            placeholder="Filter files or symbols..."
            value={filterQuery}
            onChange={e => setFilterQuery(e.target.value)}
            style={{
              width: '100%',
              padding: '8px 12px',
              backgroundColor: 'var(--bg-primary)',
              border: '1px solid var(--border-color)',
              borderRadius: '6px',
              color: 'var(--text-primary)',
              fontSize: '13px',
              outline: 'none',
            }}
          />
        </div>

        <div style={{ flex: 1, overflowY: 'auto', padding: '10px' }}>
          {filteredFiles.map(file => {
            const isFileSelected = file.path === selectedFile;
            return (
              <div key={file.path} style={{ marginBottom: '8px' }}>
                <div
                  onClick={() => {
                    setSelectedFile(file.path);
                    if (file.chunks.length > 0) setSelectedChunk(file.chunks[0]);
                  }}
                  style={{
                    padding: '8px 10px',
                    borderRadius: '6px',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    backgroundColor: isFileSelected ? 'rgba(99, 102, 241, 0.18)' : 'transparent',
                    border: isFileSelected ? '1px solid rgba(99, 102, 241, 0.35)' : '1px solid transparent',
                    color: isFileSelected ? 'var(--accent-indigo-light)' : 'var(--text-primary)',
                    fontWeight: 600,
                    fontSize: '13px',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                      <polyline points="14 2 14 8 20 8"/>
                    </svg>
                    <span>{file.path}</span>
                  </div>
                  <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>{file.chunks.length}</span>
                </div>

                {/* Chunks / Symbols list inside file */}
                {isFileSelected && (
                  <div style={{ marginLeft: '16px', marginTop: '4px', borderLeft: '1px solid var(--border-subtle)', paddingLeft: '8px' }}>
                    {file.chunks.map(chunk => {
                      const isChunkActive = selectedChunk?.id === chunk.id;
                      const badgeClass = chunk.kind === 'struct' ? 'badge-struct' : chunk.kind === 'method' ? 'badge-method' : 'badge-function';
                      return (
                        <div
                          key={chunk.id}
                          onClick={() => setSelectedChunk(chunk)}
                          style={{
                            padding: '6px 8px',
                            margin: '2px 0',
                            borderRadius: '4px',
                            cursor: 'pointer',
                            fontSize: '12px',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            backgroundColor: isChunkActive ? 'var(--bg-tertiary)' : 'transparent',
                            color: isChunkActive ? '#fff' : 'var(--text-secondary)',
                          }}
                        >
                          <div style={{ display: 'flex', alignItems: 'center', gap: '6px', overflow: 'hidden' }}>
                            <span className={`badge ${badgeClass}`} style={{ fontSize: '9px', padding: '1px 5px' }}>
                              {chunk.kind === 'method' && chunk.parentName ? `${chunk.parentName}.` : ''}{chunk.kind}
                            </span>
                            <span style={{ fontWeight: 500, textOverflow: 'ellipsis', whiteSpace: 'nowrap', overflow: 'hidden' }}>
                              {chunk.name}
                            </span>
                          </div>
                          <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
                            L{chunk.startLine}
                          </span>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Code Chunk Detail & AST Metadata */}
      <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {selectedChunk ? (
          <>
            <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border-subtle)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '4px' }}>
                  <h3 style={{ fontSize: '17px', fontWeight: 700, color: 'var(--text-primary)' }}>
                    {selectedChunk.name}
                  </h3>
                  <span className={`badge ${selectedChunk.kind === 'struct' ? 'badge-struct' : selectedChunk.kind === 'method' ? 'badge-method' : 'badge-function'}`}>
                    {selectedChunk.kind}
                  </span>
                  {selectedChunk.parentName && (
                    <span style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
                      receiver: <strong style={{ color: 'var(--accent-indigo-light)' }}>{selectedChunk.parentName}</strong> (ID #{selectedChunk.parentId})
                    </span>
                  )}
                </div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)', display: 'flex', gap: '16px' }}>
                  <span>Source: <code>{selectedChunk.sourceFile}</code></span>
                  <span>Span: <strong>L{selectedChunk.startLine} - L{selectedChunk.endLine}</strong></span>
                  <span>Chunk ID: <strong>#{selectedChunk.id}</strong></span>
                </div>
              </div>
              <button
                onClick={() => navigator.clipboard.writeText(selectedChunk.text)}
                style={{
                  padding: '6px 12px',
                  backgroundColor: 'var(--bg-tertiary)',
                  border: '1px solid var(--border-color)',
                  color: 'var(--text-secondary)',
                  borderRadius: '6px',
                  cursor: 'pointer',
                  fontSize: '12px',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '6px',
                }}
              >
                Copy Chunk
              </button>
            </div>

            <div style={{ flex: 1, overflowY: 'auto', backgroundColor: '#060a12', padding: '20px' }}>
              <pre className="font-mono" style={{ fontSize: '13px', lineHeight: '1.6', color: '#e2e8f0', margin: 0 }}>
                <code>{selectedChunk.text}</code>
              </pre>
            </div>
          </>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)' }}>
            Select a file and AST chunk from the tree to inspect code bounds.
          </div>
        )}
      </div>
    </div>
  );
};
