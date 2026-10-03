package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AgentStep records a single iteration of reasoning, tool action, and observation.
type AgentStep struct {
	StepNumber  int    `json:"step_number"`
	Thought     string `json:"thought"`
	ToolName    string `json:"tool_name,omitempty"`
	ToolArgs    string `json:"tool_args,omitempty"`
	Observation string `json:"observation,omitempty"`
}

// AgentResult contains the trace of execution and final grounded answer.
type AgentResult struct {
	Goal      string        `json:"goal"`
	Steps     []AgentStep   `json:"steps"`
	Answer    string        `json:"answer"`
	Completed bool          `json:"completed"`
	Duration  time.Duration `json:"duration_ms"`
}

// CodePilotAgent executes autonomous multi-step code intelligence workflows.
type CodePilotAgent struct {
	Tools    *CodePilotTools
	LLM      *LLMClient
	MaxSteps int
}

func NewCodePilotAgent(tools *CodePilotTools, llm *LLMClient) *CodePilotAgent {
	return &CodePilotAgent{
		Tools:    tools,
		LLM:      llm,
		MaxSteps: 6,
	}
}

func (a *CodePilotAgent) systemPrompt() string {
	var toolDescriptions []string
	for _, def := range a.Tools.Definitions() {
		paramsJSON, _ := json.Marshal(def.Parameters)
		toolDescriptions = append(toolDescriptions, fmt.Sprintf("- %s: %s\n  Parameters: %s", def.Name, def.Description, string(paramsJSON)))
	}

	return fmt.Sprintf(`You are CodePilot Agent, an autonomous software engineering assistant.
You solve coding goals by iteratively inspecting the codebase with tools before producing a final answer.

Available Tools:
%s

Follow this strict step format:

Thought: <reasoning about what you need to look up next>
Action: <tool_name>({"param": "value"})

After receiving the tool Observation, decide whether you need more information or can answer.
When you have collected sufficient evidence to fulfill the goal, return:

Thought: <final reasoning>
Final Answer:
<grounded explanation with code references and file locations>

Rules:
1. Only call tools that exist in the list.
2. Tool arguments must be valid JSON matching the parameters.
3. Base your final answer strictly on evidence gathered in the observations.
4. Do not invent non-existent files or functions.
`, strings.Join(toolDescriptions, "\n\n"))
}

// Run executes the ReAct autonomous loop to achieve the given goal.
func (a *CodePilotAgent) Run(ctx context.Context, goal string) (*AgentResult, error) {
	if a.Tools == nil {
		return nil, fmt.Errorf("agent tools must not be nil")
	}
	if a.LLM == nil {
		return nil, fmt.Errorf("agent LLM must not be nil")
	}
	if strings.TrimSpace(goal) == "" {
		return nil, fmt.Errorf("goal must not be empty")
	}

	startTime := time.Now()
	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 6
	}

	messages := []Message{
		{Role: "system", Content: a.systemPrompt()},
		{Role: "user", Content: fmt.Sprintf("Goal: %s", goal)},
	}

	result := &AgentResult{
		Goal:  goal,
		Steps: make([]AgentStep, 0),
	}

	repeatedActionCount := 0
	lastActionSignature := ""

	for step := 1; step <= maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		response, err := a.LLM.Chat(messages)
		if err != nil {
			return nil, fmt.Errorf("step %d LLM chat failed: %w", step, err)
		}

		thought, toolName, toolArgsJSON, isFinal := parseAgentResponse(response)

		currentStep := AgentStep{
			StepNumber: step,
			Thought:    thought,
			ToolName:   toolName,
			ToolArgs:   toolArgsJSON,
		}

		if isFinal {
			currentStep.Observation = "Completed."
			result.Steps = append(result.Steps, currentStep)
			result.Answer = extractFinalAnswer(response)
			result.Completed = true
			result.Duration = time.Since(startTime)
			return result, nil
		}

		// Loop detection
		actionSig := fmt.Sprintf("%s:%s", toolName, toolArgsJSON)
		if actionSig == lastActionSignature && actionSig != ":" {
			repeatedActionCount++
			if repeatedActionCount >= 2 {
				// Intervene to prevent runaway infinite loop
				currentStep.Observation = "Error: Repeated action detected. Please synthesize your findings and produce a Final Answer."
				result.Steps = append(result.Steps, currentStep)
				messages = append(messages, Message{Role: "assistant", Content: response})
				messages = append(messages, Message{Role: "user", Content: currentStep.Observation})
				continue
			}
		} else {
			repeatedActionCount = 0
			lastActionSignature = actionSig
		}

		// Execute tool
		var observation string
		if toolName == "" {
			observation = "Error: No valid Action or Final Answer found in response. Use 'Action: <tool_name>(<json_args>)' or 'Final Answer:'"
		} else {
			var args map[string]any
			if toolArgsJSON != "" {
				if err := json.Unmarshal([]byte(toolArgsJSON), &args); err != nil {
					observation = fmt.Sprintf("Error: Invalid JSON arguments: %v", err)
				}
			}
			if observation == "" {
				obs, err := a.Tools.ExecuteTool(toolName, args)
				if err != nil {
					observation = fmt.Sprintf("Tool error: %v", err)
				} else {
					observation = obs
				}
			}
		}

		currentStep.Observation = observation
		result.Steps = append(result.Steps, currentStep)

		// Append to history
		messages = append(messages, Message{Role: "assistant", Content: response})
		messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("Observation:\n%s", observation)})
	}

	result.Answer = "Agent reached maximum step limit before completing the goal."
	result.Completed = false
	result.Duration = time.Since(startTime)
	return result, nil
}

func parseAgentResponse(resp string) (thought, toolName, toolArgs string, isFinal bool) {
	trimmed := strings.TrimSpace(resp)

	if idx := strings.Index(trimmed, "Final Answer:"); idx != -1 {
		isFinal = true
		thought = strings.TrimSpace(trimmed[:idx])
		thought = strings.TrimPrefix(thought, "Thought:")
		return strings.TrimSpace(thought), "", "", true
	}

	// Parse Thought
	if strings.HasPrefix(trimmed, "Thought:") {
		parts := strings.SplitN(trimmed, "Action:", 2)
		thought = strings.TrimSpace(strings.TrimPrefix(parts[0], "Thought:"))
		if len(parts) > 1 {
			trimmedAction := strings.TrimSpace(parts[1])
			toolName, toolArgs = parseAction(trimmedAction)
		}
		return thought, toolName, toolArgs, false
	}

	if idx := strings.Index(trimmed, "Action:"); idx != -1 {
		toolName, toolArgs = parseAction(strings.TrimSpace(trimmed[idx+len("Action:"): ]))
		return "", toolName, toolArgs, false
	}

	return trimmed, "", "", false
}

func parseAction(actionStr string) (name, args string) {
	actionStr = strings.TrimSpace(actionStr)
	openParen := strings.Index(actionStr, "(")
	closeParen := strings.LastIndex(actionStr, ")")
	if openParen != -1 && closeParen != -1 && closeParen > openParen {
		name = strings.TrimSpace(actionStr[:openParen])
		args = strings.TrimSpace(actionStr[openParen+1 : closeParen])
		return name, args
	}
	// Fallback space separated: tool {"arg": 1}
	parts := strings.SplitN(actionStr, " ", 2)
	name = parts[0]
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return name, args
}

func extractFinalAnswer(resp string) string {
	idx := strings.Index(resp, "Final Answer:")
	if idx != -1 {
		return strings.TrimSpace(resp[idx+len("Final Answer:"):])
	}
	return resp
}
