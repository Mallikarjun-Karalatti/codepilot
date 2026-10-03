# CodePilot: Zero-Dependency AI Software Engineering Assistant & Retrieval Engine

> A high-performance, repository-aware code intelligence engine, hybrid retrieval pipeline, ReAct agent, and Model Context Protocol (MCP) server written in **pure Go standard library** (Go 1.26+). No external vector databases, no Python sidecars, no CGO.

[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-0%20(stdlib%20only)-success)](go.mod)

---

## Architecture Overview

CodePilot is architected from first principles to provide deep semantic and structural code understanding with minimal resource overhead and maximum operational reliability.

```
                              ┌────────────────────────────────────────┐
                              │             User / Client              │
                              └───────────────────┬────────────────────┘
                                                  │
                ┌─────────────────────────────────┴─────────────────────────────────┐
                ▼                                                                   ▼
   ┌─────────────────────────┐                                         ┌─────────────────────────┐
   │       Unified CLI       │                                         │    stdio MCP Server     │
   │ (index, ask, agent, …)  │                                         │  (JSON-RPC 2.0 / tools) │
   └────────────┬────────────┘                                         └────────────┬────────────┘
                │                                                                   │
                ├─────────────────────────────────┬─────────────────────────────────┤
                ▼                                 ▼                                 ▼
   ┌─────────────────────────┐       ┌─────────────────────────┐       ┌─────────────────────────┐
   │    Incremental Indexer  │       │     Hybrid Retriever    │       │       ReAct Agent       │
   │  • Content-hash manifest│       │  • Lexical TF-IDF       │       │  • Thought-Action loop  │
   │  • AST chunking         │       │  • Dense Cosine Vector  │       │  • Cycle / stall guard  │
   │  • Atomic context cancel│       │  • RRF Fusion (k=60)    │       │  • Bounded step budget  │
   │  • Bounded worker pools │       │  • Structural expansion │       └────────────┬────────────┘
   └────────────┬────────────┘       └────────────┬────────────┘                    │
                │                                 │                                 │
                ▼                                 ▼                                 ▼
   ┌─────────────────────────┐       ┌─────────────────────────┐       ┌─────────────────────────┐
   │ Atomic Filesystem Store │       │ DirectFirst Selector    │       │ Intelligence Tools API  │
   │  • OS temp-file rename  │       │  • Token budgeter       │       │  • search_code, read    │
   │  • Corrupt recovery     │       │  • Provenance tracking  │       │  • callers, outline     │
   └─────────────────────────┘       └─────────────────────────┘       └─────────────────────────┘
```

---

## Key Technical Decisions & Invariants

1. **Pure Standard Library Runtime**: Zero third-party dependencies (`net/http`, `go/parser`, `go/ast`, `sync`, `crypto/sha256`). Compiles to a single static binary.
2. **AST-Aware Syntactic Chunking**: Rather than arbitrary character- or line-based windowing, CodePilot parses the Go AST to produce semantically atomic units (top-level functions, methods with receiver bindings, structs, interfaces, imports).
3. **Reciprocal Rank Fusion (RRF, $k=60$)**: Combines inverted-index lexical search (TF-IDF with term frequency saturation) and 4096-dimensional dense cosine similarity without requiring calibrated score normalization across spaces.
4. **1-Hop Structural Graph Expansion**: Enriches top direct hits with AST relationship context (1 parent struct declaration + 1 direct callee method), resolving callers and definitions across package directories while gracefully decoupling dangling references.
5. **DirectFirst Evidence Selection & Context Budgeting**: Prioritizes direct retrieval hits before relationship context within a strict token budget (default 2,000 tokens), preventing context-window exhaustion and hallucination.
6. **Concurrent Worker Pools with Immediate Cancellation**: Employs atomic chunk dispatch (`sync/atomic`) and child context cancellation (`context.WithCancel`) so worker errors fail fast without leaking memory or file descriptors (clamped to $\le 16$ open FDs).
7. **Crash-Safe Atomic Persistence**: Manifests and vector indices write to OS temp files (`.tmp`) followed by atomic rename, preventing index corruption across sudden halts or power failures.

---

## Measured Performance & Benchmark Metrics

All benchmarks run on Apple Silicon using the built-in Go benchmark and test suites (`go test -race`).

### 1. Concurrency & Throughput Scaling (52 files, 205 AST chunks)

| Workers | Index Duration | Files / Sec | Chunks / Sec | Speedup |
| :---: | :---: | :---: | :---: | :---: |
| **1 Worker** (Seq) | 1.065s | 48.8 | 192.5 | 1.00x |
| **2 Workers** | 809ms | 64.2 | 253.2 | 1.32x |
| **4 Workers** | 686ms | 75.8 | 298.7 | 1.55x |
| **8 Workers** | **630ms** | **82.5** | **325.2** | **1.69x** |

### 2. Incremental Work Avoidance & Cache Efficiency

| Scenario | Files Reparsed | Chunks Re-embedded | Time | Work Avoided |
| :--- | :---: | :---: | :---: | :---: |
| **Cold Index** (52 files) | 52 | 205 | 562ms | 0% (baseline) |
| **Zero Mutation Run** | 0 | 0 | 1.8ms | **100% avoided** |
| **1-File Mutation** | 1 | 4 | 14.2ms | **98.0% avoided** |
| **Dangling Struct Deletion** | 1 | 4 | 15.1ms | Graceful decouple |

### 3. Query Latency Percentiles (100 retrieval iterations over 205 chunks)

| Metric | Measured Latency | Target SLA | Status |
| :--- | :---: | :---: | :---: |
| **p50 (Median)** | **50.6ms** | < 100ms | PASS |
| **p95** | **56.8ms** | < 120ms | PASS |
| **p99** | **77.1ms** | < 150ms | PASS |

### 4. Memory & Storage Footprint

- **On-Disk Index Size**: 9.10 MB (9,326,160 bytes) for 205 chunks including full 4096-dimensional vectors and AST metadata.
- **Heap Allocation during Indexing**: 66.53 MB total heap allocations during full repository indexing.
- **FD Safety Limit**: Bounded to $\le 16$ concurrent file descriptors to eliminate OS `EMFILE` exhaustion.

### 5. Deterministic Retrieval Quality Gates (30-Query Evaluation Suite)

- **Recall@1**: $\ge 0.50$ (Gate Pass)
- **Recall@3**: $\ge 0.75$ (Gate Pass)
- **Recall@5**: $\ge 0.85$ (Gate Pass)
- **MRR (Mean Reciprocal Rank)**: $\ge 0.65$ (Gate Pass)

---

## Installation & Build

Requires Go 1.26 or higher:

```bash
cd experiments/lesson-01
go build -o codepilot .
```

---

## CLI Usage Guide

### 1. Incrementally Index a Repository
Scans source files, computes SHA-256 hashes against stored manifest, updates the AST chunk database and vector embeddings:
```bash
./codepilot index /path/to/repository
# For detailed telemetry & stage timings:
./codepilot index --verbose /path/to/repository
```

### 2. Ask Grounded Architectural Questions
Executes hybrid retrieval, structural graph expansion, evidence selection, and prompt formatting:
```bash
./codepilot ask /path/to/repository "How is payment processing orchestrated?"
```

### 3. Run Autonomous Multi-Step ReAct Agent
Runs an agent equipped with cycle detection, tool execution, and dynamic intervention:
```bash
./codepilot agent /path/to/repository "Trace where OrderPlaced event is published and find its handlers"
```

### 4. Execute Code Intelligence Tools Directly
Query symbols, callers, callees, and file outlines directly via the CLI:
```bash
./codepilot tool /path/to/repository find_symbol PaymentGateway
./codepilot tool /path/to/repository find_callers ProcessOrder
./codepilot tool /path/to/repository get_file_outline service/order.go
./codepilot tool /path/to/repository list_files
```

### 5. Run Quality Evaluation & Regression Gates
Evaluates retrieval accuracy against benchmark test cases:
```bash
./codepilot eval /path/to/repository
```

### 6. Start Model Context Protocol (MCP) Server
Exposes tools over standard I/O conforming to the official MCP specification:
```bash
./codepilot mcp /path/to/repository
```

Configure in Claude Desktop or IDE MCP client:
```json
{
  "mcpServers": {
    "codepilot": {
      "command": "/path/to/codepilot",
      "args": ["mcp", "/path/to/repository"]
    }
  }
}
```

---

## Test & Verification

Run the full suite of unit, integration, concurrency, resilience, and performance tests:

```bash
cd experiments/lesson-01
# Run full unit and regression test suite
go test -race ./...

# Run performance and throughput scaling benchmarks
go test -race -v -run TestPerformanceBenchmark ./...

# Run resilience and crash recovery tests
go test -race -v -run TestFailureRecovery ./...
```

---

## Project Structure

```
experiments/lesson-01/
├── agent.go                   # ReAct autonomous agent with cycle/stall detection
├── cli.go                     # Unified CLI implementation (index, ask, agent, tool, eval, mcp)
├── code_chunker.go            # AST-aware Go parser & chunk generator
├── code_indexer.go            # Incremental indexing coordinator & cross-file resolution
├── context_builder.go         # DirectFirst evidence selector & token budgeter
├── evaluator.go               # Retrieval evaluation runner & regression gates
├── hybrid_retriever.go        # RRF (k=60) combining lexical inverted index + dense vectors
├── lexical_retriever.go       # Inverted index with TF-IDF calculation
├── mcp.go                     # Model Context Protocol stdio JSON-RPC 2.0 server
├── relationship_graph.go      # AST structural relationship graph & 1-hop expansion
├── repository_scanner.go      # Filesystem scanner & SHA-256 change detection
├── store.go                   # Atomic filesystem persistence & recovery
├── telemetry.go               # Structured logging & stage execution metrics
├── tools.go                   # Deterministic repository intelligence tools API
└── *_test.go                  # 100% passing unit, integration, and benchmark suites
```
