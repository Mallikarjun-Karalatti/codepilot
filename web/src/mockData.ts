import { WorkspaceFile, HybridSearchResult, AgentTrace, RepositoryStats } from './types';

export const mockStats: RepositoryStats = {
  repositoryId: "gin-gonic/gin@v1.9.1",
  filesScanned: 59,
  chunksEmbedded: 590,
  indexingTimeMs: 716,
  indexSizeBytes: 9326160,
  p50LatencyMs: 2.01,
  p99LatencyMs: 2.25,
};

export const mockFiles: WorkspaceFile[] = [
  {
    path: "gin.go",
    symbolCount: 8,
    chunks: [
      {
        id: 1,
        name: "Engine",
        kind: "struct",
        sourceFile: "gin.go",
        startLine: 62,
        endLine: 104,
        text: `type Engine struct {
    RouterGroup
    RedirectTrailingSlash bool
    RedirectFixedPath     bool
    HandleMethodNotAllowed bool
    ForwardedByClientIP    bool
    AppEngine             bool
    UseRawPath            bool
    UnescapePathValues    bool
    MaxMultipartMemory    int64
    trees                 methodTrees
    maxParams             uint16
    trustedProxies        []string
    trustedCIDRs          []*net.IPNet
}`,
      },
      {
        id: 2,
        name: "New",
        kind: "function",
        sourceFile: "gin.go",
        startLine: 147,
        endLine: 168,
        text: `func New() *Engine {
    debugPrintWARNINGNew()
    engine := &Engine{
        RouterGroup: RouterGroup{
            Handlers: nil,
            basePath: "/",
            root:     true,
        },
        trees: make(methodTrees, 0, 9),
    }
    engine.RouterGroup.engine = engine
    return engine
}`,
      },
      {
        id: 3,
        name: "handleHTTPRequest",
        kind: "method",
        parentName: "Engine",
        parentId: 1,
        sourceFile: "gin.go",
        startLine: 719,
        endLine: 789,
        text: `func (engine *Engine) handleHTTPRequest(c *Context) {
    httpMethod := c.Request.Method
    rPath := c.Request.URL.Path
    t := engine.trees
    for i, len := 0, len(t); i < len; i++ {
        if t[i].method != httpMethod {
            continue
        }
        root := t[i].root
        value := root.getValue(rPath, c.params, c.skippedNodes, unescape)
        if value.params != nil {
            c.Params = *value.params
        }
        if value.handlers != nil {
            c.handlers = value.handlers
            c.fullPath = value.fullPath
            c.Next()
            c.writermem.WriteHeaderNow()
            return
        }
    }
}`,
      },
    ],
  },
  {
    path: "routergroup.go",
    symbolCount: 6,
    chunks: [
      {
        id: 10,
        name: "RouterGroup",
        kind: "struct",
        sourceFile: "routergroup.go",
        startLine: 35,
        endLine: 43,
        text: `type RouterGroup struct {
    Handlers HandlersChain
    basePath string
    engine   *Engine
    root     bool
}`,
      },
      {
        id: 11,
        name: "Handle",
        kind: "method",
        parentName: "RouterGroup",
        parentId: 10,
        sourceFile: "routergroup.go",
        startLine: 129,
        endLine: 134,
        text: `func (group *RouterGroup) Handle(httpMethod, relativePath string, handlers ...HandlerFunc) IRoutes {
    if matched := regEnLetter.MatchString(httpMethod); !matched {
        panic("http method " + httpMethod + " is not valid")
    }
    return group.handle(httpMethod, relativePath, handlers)
}`,
      },
    ],
  },
  {
    path: "tree.go",
    symbolCount: 5,
    chunks: [
      {
        id: 20,
        name: "node",
        kind: "struct",
        sourceFile: "tree.go",
        startLine: 82,
        endLine: 95,
        text: `type node struct {
    path      string
    indices   string
    wildChild bool
    nType     nodeType
    priority  uint32
    children  []*node
    handlers  HandlersChain
    fullPath  string
}`,
      },
      {
        id: 21,
        name: "addRoute",
        kind: "method",
        parentName: "node",
        parentId: 20,
        sourceFile: "tree.go",
        startLine: 135,
        endLine: 249,
        text: `func (n *node) addRoute(path string, handlers HandlersChain) {
    fullPath := path
    n.priority++
    // Insert new route segment into radix prefix tree
    if len(n.path) == 0 && len(n.children) == 0 {
        n.insertChild(path, fullPath, handlers)
        n.nType = root
        return
    }
}`,
      },
    ],
  },
  {
    path: "context.go",
    symbolCount: 12,
    chunks: [
      {
        id: 30,
        name: "Context",
        kind: "struct",
        sourceFile: "context.go",
        startLine: 45,
        endLine: 85,
        text: `type Context struct {
    writermem responseWriter
    Request   *http.Request
    Writer    ResponseWriter
    Params    Params
    handlers  HandlersChain
    index     int8
    fullPath  string
    engine    *Engine
    params    *Params
    Keys      map[string]any
}`,
      },
      {
        id: 31,
        name: "Next",
        kind: "method",
        parentName: "Context",
        parentId: 30,
        sourceFile: "context.go",
        startLine: 172,
        endLine: 181,
        text: `func (c *Context) Next() {
    c.index++
    for c.index < int8(len(c.handlers)) {
        c.handlers[c.index](c)
        c.index++
    }
}`,
      },
    ],
  },
];

export const mockSearchResults: Record<string, HybridSearchResult[]> = {
  "default": [
    {
      chunk: mockFiles[1].chunks[1], // RouterGroup.Handle
      combinedScore: 0.032787,
      semanticScore: 0.892,
      lexicalScore: 18.45,
      semanticRank: 1,
      lexicalRank: 1,
    },
    {
      chunk: mockFiles[2].chunks[1], // node.addRoute
      combinedScore: 0.032258,
      semanticScore: 0.865,
      lexicalScore: 16.12,
      semanticRank: 2,
      lexicalRank: 2,
    },
    {
      chunk: mockFiles[0].chunks[2], // Engine.handleHTTPRequest
      combinedScore: 0.031746,
      semanticScore: 0.841,
      lexicalScore: 14.88,
      semanticRank: 3,
      lexicalRank: 3,
    },
    {
      chunk: mockFiles[3].chunks[1], // Context.Next
      combinedScore: 0.016129,
      semanticScore: 0.785,
      lexicalScore: 5.12,
      semanticRank: 4,
      lexicalRank: 7,
    },
    {
      chunk: mockFiles[0].chunks[0], // Engine struct
      combinedScore: 0.015873,
      semanticScore: 0.742,
      lexicalScore: 4.89,
      semanticRank: 5,
      lexicalRank: 8,
    },
  ],
};

export const mockTraces: AgentTrace[] = [
  {
    id: "trace-9481",
    query: "How are HTTP routes registered and dispatched to handlers?",
    timestamp: "2026-10-03 14:13:36",
    totalDurationMs: 482,
    tokensUsed: 1420,
    steps: [
      {
        step: 1,
        phase: "thought",
        thought: "I need to locate where routing methods are exposed to callers, and then find where the routing tree matches incoming HTTP requests.",
        durationMs: 45,
      },
      {
        step: 2,
        phase: "action",
        tool: "search_code",
        toolInput: { query: "Handle route registration and tree dispatch", top_k: 5 },
        toolOutput: "Found 5 candidates: RouterGroup.Handle (routergroup.go), node.addRoute (tree.go), Engine.handleHTTPRequest (gin.go), Context.Next (context.go).",
        durationMs: 120,
        evidenceUsed: [
          { chunkId: 11, name: "Handle", file: "routergroup.go", lines: "L129-L134", relationType: "direct" },
          { chunkId: 21, name: "addRoute", file: "tree.go", lines: "L135-L249", relationType: "direct" },
          { chunkId: 3, name: "handleHTTPRequest", file: "gin.go", lines: "L719-L789", relationType: "direct" },
        ],
      },
      {
        step: 3,
        phase: "action",
        tool: "find_callers",
        toolInput: { symbol: "handleHTTPRequest" },
        toolOutput: "Engine.ServeHTTP in gin.go:420 delegates directly to engine.handleHTTPRequest(c).",
        durationMs: 65,
        evidenceUsed: [
          { chunkId: 1, name: "Engine", file: "gin.go", lines: "L62-L104", relationType: "parent" },
        ],
      },
      {
        step: 4,
        phase: "final_answer",
        durationMs: 252,
      },
    ],
    finalAnswer: `HTTP routes are registered using the Handle method on RouterGroup (routergroup.go:129-134), which validates the HTTP verb and delegates to group.handle. The routes are structured into radix prefix trees managed by addRoute (tree.go:135-249). During incoming requests, Engine.ServeHTTP delegates to handleHTTPRequest (gin.go:719-789), which traverses the methodTree for the request path and executes the handler chain via c.Next().`,
  },
  {
    id: "trace-9482",
    query: "Find where tokens are verified and suggest a fix for expired sessions",
    timestamp: "2026-10-03 13:45:10",
    totalDurationMs: 610,
    tokensUsed: 1850,
    steps: [
      {
        step: 1,
        phase: "thought",
        thought: "Searching for session token validation routines and token expiration check logic across database and auth modules.",
        durationMs: 50,
      },
      {
        step: 2,
        phase: "action",
        tool: "find_symbol",
        toolInput: { name: "ValidateToken" },
        toolOutput: "Found ValidateToken method in database.go:45-72 with receiver (db *Database).",
        durationMs: 95,
        evidenceUsed: [
          { chunkId: 45, name: "ValidateToken", file: "database.go", lines: "L45-L72", relationType: "direct" },
        ],
      },
      {
        step: 3,
        phase: "action",
        tool: "find_callers",
        toolInput: { symbol: "ValidateToken" },
        toolOutput: "Called by AuthService.AuthenticateUser (auth.go:32) and SessionMiddleware (auth.go:88).",
        durationMs: 85,
        evidenceUsed: [
          { chunkId: 12, name: "AuthenticateUser", file: "auth.go", lines: "L25-L60", relationType: "callee" },
        ],
      },
      {
        step: 4,
        phase: "final_answer",
        durationMs: 380,
      },
    ],
    finalAnswer: `Token validation is implemented in Database.ValidateToken (database.go:45-72) and invoked during AuthService.AuthenticateUser (auth.go:32). To fix expired sessions gracefully: in ValidateToken, add an explicit check for session.ExpiresAt.Before(time.Now()), revoke the session row via RevokeSession, and return ErrSessionExpired with HTTP 401 Unauthorized before checking credentials.`,
  },
];
