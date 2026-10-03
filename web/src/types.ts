export type ChunkKind = 'function' | 'method' | 'struct' | 'interface' | 'import';

export interface CodeChunk {
  id: number;
  name: string;
  kind: ChunkKind;
  parentName?: string;
  parentId?: number;
  sourceFile: string;
  startLine: number;
  endLine: number;
  text: string;
}

export interface WorkspaceFile {
  path: string;
  chunks: CodeChunk[];
  symbolCount: number;
}

export interface HybridSearchResult {
  chunk: CodeChunk;
  combinedScore: number;
  semanticScore: number;
  lexicalScore: number;
  semanticRank?: number;
  lexicalRank?: number;
}

export interface AgentTraceStep {
  step: number;
  phase: 'thought' | 'action' | 'observation' | 'final_answer';
  thought?: string;
  tool?: string;
  toolInput?: Record<string, any>;
  toolOutput?: string;
  durationMs: number;
  evidenceUsed?: {
    chunkId: number;
    name: string;
    file: string;
    lines: string;
    relationType: 'direct' | 'parent' | 'callee';
  }[];
}

export interface AgentTrace {
  id: string;
  query: string;
  timestamp: string;
  totalDurationMs: number;
  tokensUsed: number;
  steps: AgentTraceStep[];
  finalAnswer: string;
}

export interface RepositoryStats {
  repositoryId: string;
  filesScanned: number;
  chunksEmbedded: number;
  indexingTimeMs: number;
  indexSizeBytes: number;
  p50LatencyMs: number;
  p99LatencyMs: number;
}
