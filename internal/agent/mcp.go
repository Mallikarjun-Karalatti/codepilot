package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// MCP JSON-RPC 2.0 structures
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	mcpErrParseError     = -32700
	mcpErrInvalidRequest = -32600
	mcpErrMethodNotFound = -32601
	mcpErrInvalidParams  = -32602
	mcpErrInternalError  = -32603
)

// MCPServer exposes CodePilot repository intelligence tools via the Model Context Protocol (MCP).
type MCPServer struct {
	tools *CodePilotTools
	mu    sync.Mutex
}

func NewMCPServer(tools *CodePilotTools) *MCPServer {
	return &MCPServer{tools: tools}
}

// Serve reads JSON-RPC messages from reader and writes responses to writer until EOF or context cancellation.
func (s *MCPServer) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.sendError(w, nil, mcpErrParseError, fmt.Sprintf("parse error: %v", err))
			continue
		}

		resp := s.handleRequest(ctx, &req)
		if resp != nil {
			data, err := json.Marshal(resp)
			if err != nil {
				s.sendError(w, req.ID, mcpErrInternalError, fmt.Sprintf("marshal response: %v", err))
				continue
			}
			s.mu.Lock()
			_, _ = w.Write(append(data, '\n'))
			s.mu.Unlock()
		}
	}
	return scanner.Err()
}

func (s *MCPServer) handleRequest(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	_ = ctx
	switch req.Method {
	case "initialize":
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "codepilot-mcp",
					"version": "1.0.0",
				},
			},
		}

	case "notifications/initialized":
		// No response required for notifications
		return nil

	case "ping":
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		}

	case "tools/list":
		defs := s.tools.Definitions()
		toolList := make([]map[string]any, len(defs))
		for i, def := range defs {
			properties := make(map[string]any)
			var required []string
			for pName, pDef := range def.Parameters {
				properties[pName] = map[string]any{
					"type":        pDef.Type,
					"description": pDef.Description,
				}
				if pDef.Required {
					required = append(required, pName)
				}
			}

			toolList[i] = map[string]any{
				"name":        def.Name,
				"description": def.Description,
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
			}
		}
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": toolList,
			},
		}

	case "tools/call":
		var callParams struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			return &jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &jsonRPCError{
					Code:    mcpErrInvalidParams,
					Message: fmt.Sprintf("invalid tools/call params: %v", err),
				},
			}
		}

		resultText, err := s.tools.ExecuteTool(callParams.Name, callParams.Arguments)
		if err != nil {
			return &jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
					},
					"isError": true,
				},
			}
		}

		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": resultText},
				},
			},
		}

	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonRPCError{
				Code:    mcpErrMethodNotFound,
				Message: fmt.Sprintf("method %q not found", req.Method),
			},
		}
	}
}

func (s *MCPServer) sendError(w io.Writer, id any, code int, msg string) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonRPCError{
			Code:    code,
			Message: msg,
		},
	}
	data, _ := json.Marshal(resp)
	s.mu.Lock()
	_, _ = w.Write(append(data, '\n'))
	s.mu.Unlock()
}
