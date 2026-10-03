import React, { useState, useEffect } from 'react';
import { WorkspaceView } from './components/WorkspaceView';
import { RetrievalInspector } from './components/RetrievalInspector';
import { AgentTraceInspector } from './components/AgentTraceInspector';
import { mockFiles, mockStats, mockTraces } from './mockData';
import { RepositoryStats, WorkspaceFile, HybridSearchResult, AgentTrace } from './types';

export const App: React.FC = () => {
  const [activeTab, setActiveTab] = useState<'workspace' | 'retrieval' | 'agent'>('retrieval');
  const [stats, setStats] = useState<RepositoryStats>(mockStats);
  const [files, setFiles] = useState<WorkspaceFile[]>(mockFiles);
  const [traces] = useState<AgentTrace[]>(mockTraces);
  const [isLiveConnected, setIsLiveConnected] = useState<boolean>(false);

  useEffect(() => {
    // Attempt connecting to live CodePilot HTTP backend on port 8080 if running
    fetch('/api/workspace')
      .then(res => res.json())
      .then(data => {
        if (data && data.files) {
          setFiles(data.files);
          setIsLiveConnected(true);
        }
      })
      .catch(() => {
        setIsLiveConnected(false);
      });

    fetch('/api/stats')
      .then(res => res.json())
      .then(data => {
        if (data) setStats(data);
      })
      .catch(() => {});
  }, []);

  const handleLiveSearch = async (query: string, topK: number): Promise<HybridSearchResult[]> => {
    const res = await fetch(`/api/retrieve?q=${encodeURIComponent(query)}&k=${topK}`);
    if (!res.ok) throw new Error('Search failed');
    const data = await res.json();
    return data.results;
  };

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      {/* Top Header & Telemetry Bar */}
      <header style={{ backgroundColor: 'var(--bg-secondary)', borderBottom: '1px solid var(--border-color)', padding: '12px 24px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          {/* Logo & Version */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
            <div style={{ width: '32px', height: '32px', borderRadius: '8px', background: 'linear-gradient(135deg, #6366f1, #06b6d4)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2.2">
                <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/>
              </svg>
            </div>
            <div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <h1 style={{ fontSize: '16px', fontWeight: 800, letterSpacing: '-0.02em', color: 'var(--text-primary)' }}>
                  CodePilot
                </h1>
                <span className="badge" style={{ backgroundColor: 'rgba(99, 102, 241, 0.2)', color: '#818cf8', fontSize: '10px' }}>
                  v1.0.0
                </span>
              </div>
              <p style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                Zero-Dependency Code Intelligence & Retrieval Console
              </p>
            </div>
          </div>

          {/* Real-time Telemetry Metrics Pill */}
          <div style={{ display: 'flex', gap: '16px', alignItems: 'center' }}>
            <div style={{ display: 'flex', gap: '12px', backgroundColor: 'var(--bg-primary)', padding: '6px 14px', borderRadius: '8px', border: '1px solid var(--border-subtle)', fontSize: '12px' }}>
              <div>
                <span style={{ color: 'var(--text-muted)' }}>Target: </span>
                <strong style={{ color: '#fff' }}>{stats.repositoryId}</strong>
              </div>
              <div style={{ width: '1px', backgroundColor: 'var(--border-subtle)' }} />
              <div>
                <span style={{ color: 'var(--text-muted)' }}>Files: </span>
                <strong style={{ color: '#38bdf8' }}>{stats.filesScanned}</strong>
              </div>
              <div style={{ width: '1px', backgroundColor: 'var(--border-subtle)' }} />
              <div>
                <span style={{ color: 'var(--text-muted)' }}>AST Chunks: </span>
                <strong style={{ color: '#4ade80' }}>{stats.chunksEmbedded}</strong>
              </div>
              <div style={{ width: '1px', backgroundColor: 'var(--border-subtle)' }} />
              <div>
                <span style={{ color: 'var(--text-muted)' }}>p50 Latency: </span>
                <strong style={{ color: '#fbbf24' }}>{stats.p50LatencyMs.toFixed(2)}ms</strong>
              </div>
            </div>

            {/* Live / Mock Mode Status */}
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px' }}>
              <div style={{ width: '8px', height: '8px', borderRadius: '50%', backgroundColor: isLiveConnected ? '#10b981' : '#f59e0b' }} />
              <span style={{ color: 'var(--text-secondary)' }}>
                {isLiveConnected ? 'Live Backend' : 'Demo / Standalone'}
              </span>
            </div>
          </div>
        </div>

        {/* Navigation Tabs */}
        <div style={{ display: 'flex', gap: '8px', marginTop: '12px' }}>
          <button
            onClick={() => setActiveTab('workspace')}
            style={{
              padding: '8px 16px',
              backgroundColor: activeTab === 'workspace' ? 'var(--bg-tertiary)' : 'transparent',
              border: activeTab === 'workspace' ? '1px solid var(--border-color)' : '1px solid transparent',
              color: activeTab === 'workspace' ? '#fff' : 'var(--text-secondary)',
              borderRadius: '6px',
              cursor: 'pointer',
              fontSize: '13px',
              fontWeight: 600,
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
            }}
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z"/>
            </svg>
            Workspace View
          </button>

          <button
            onClick={() => setActiveTab('retrieval')}
            style={{
              padding: '8px 16px',
              backgroundColor: activeTab === 'retrieval' ? 'var(--bg-tertiary)' : 'transparent',
              border: activeTab === 'retrieval' ? '1px solid var(--border-color)' : '1px solid transparent',
              color: activeTab === 'retrieval' ? '#fff' : 'var(--text-secondary)',
              borderRadius: '6px',
              cursor: 'pointer',
              fontSize: '13px',
              fontWeight: 600,
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
            }}
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
            Retrieval Inspector
          </button>

          <button
            onClick={() => setActiveTab('agent')}
            style={{
              padding: '8px 16px',
              backgroundColor: activeTab === 'agent' ? 'var(--bg-tertiary)' : 'transparent',
              border: activeTab === 'agent' ? '1px solid var(--border-color)' : '1px solid transparent',
              color: activeTab === 'agent' ? '#fff' : 'var(--text-secondary)',
              borderRadius: '6px',
              cursor: 'pointer',
              fontSize: '13px',
              fontWeight: 600,
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
            }}
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
            </svg>
            Agent Trace Inspector
          </button>
        </div>
      </header>

      {/* Main Content Area */}
      <main style={{ flex: 1, padding: '20px', overflow: 'hidden' }}>
        {activeTab === 'workspace' && <WorkspaceView files={files} />}
        {activeTab === 'retrieval' && <RetrievalInspector onSearch={isLiveConnected ? handleLiveSearch : undefined} />}
        {activeTab === 'agent' && <AgentTraceInspector traces={traces} />}
      </main>
    </div>
  );
};

export default App;
