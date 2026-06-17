package tasktools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

const builtInGeneralPurposeAgent = "general-purpose"

const (
	teamSendTargetMembers     = "members"
	teamSendTargetCoordinator = "coordinator"
	teamSendTargetAll         = "all"

	teamAutoScheduleSourceDeterministic   = "deterministic"
	teamAutoScheduleSourceCoordinatorPlan = "coordinator_plan"

	scheduleCronActionCreate  = "create"
	scheduleCronActionList    = "list"
	scheduleCronActionDelete  = "delete"
	scheduleCronActionTrigger = "trigger"
	scheduleCronActionRunDue  = "run_due"

	maxSleepDuration = 60 * time.Second
)

type taskInput struct {
	ID           string `json:"id,omitempty"`
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	SubagentType string `json:"subagent_type"`
	Worktree     bool   `json:"worktree,omitempty"`
	WorktreeSet  bool   `json:"-"`
	Run          bool   `json:"run,omitempty"`
}

type taskOutputInput struct {
	TaskID    string `json:"task_id,omitempty"`
	TailLines *int   `json:"tail_lines,omitempty"`
}

type taskKillInput struct {
	TaskID string `json:"task_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type taskSendMessageInput struct {
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type teamCreateInput struct {
	TeamID            string   `json:"team_id,omitempty"`
	Description       string   `json:"description,omitempty"`
	CoordinatorTaskID string   `json:"coordinator_task_id,omitempty"`
	TaskIDs           []string `json:"task_ids,omitempty"`
}

type teamDeleteInput struct {
	TeamID string `json:"team_id,omitempty"`
}

type teamOutputInput struct {
	TeamID string `json:"team_id,omitempty"`
}

type teamSendMessageInput struct {
	TeamID  string `json:"team_id,omitempty"`
	Message string `json:"message,omitempty"`
	Target  string `json:"target,omitempty"`
}

type teamDispatchAssignmentInput struct {
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type teamDispatchInput struct {
	TeamID      string                        `json:"team_id,omitempty"`
	Assignments []teamDispatchAssignmentInput `json:"assignments,omitempty"`
}

type teamScheduleInput struct {
	TeamID    string `json:"team_id,omitempty"`
	Objective string `json:"objective,omitempty"`
}

type teamAutoScheduleInput struct {
	TeamID      string                        `json:"team_id,omitempty"`
	Objective   string                        `json:"objective,omitempty"`
	Assignments []teamDispatchAssignmentInput `json:"assignments,omitempty"`
}

type teamCoordinateInput struct {
	TeamID  string `json:"team_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type taskResumeInput struct {
	TaskID string `json:"task_id,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
}

type sleepInput struct {
	DurationMS *int     `json:"duration_ms,omitempty"`
	Seconds    *float64 `json:"seconds,omitempty"`
	Duration   string   `json:"duration,omitempty"`
}

type briefInput struct {
	Title     string   `json:"title,omitempty"`
	Status    string   `json:"status,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Details   []string `json:"details,omitempty"`
	NextSteps []string `json:"next_steps,omitempty"`
	Risks     []string `json:"risks,omitempty"`
}

type scheduleCronInput struct {
	Action      string `json:"action,omitempty"`
	ScheduleID  string `json:"schedule_id,omitempty"`
	Description string `json:"description,omitempty"`
	Cron        string `json:"cron,omitempty"`
	Message     string `json:"message,omitempty"`
	TeamID      string `json:"team_id,omitempty"`
	Target      string `json:"target,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Now         string `json:"now,omitempty"`
}

type remoteTriggerInput struct {
	TeamID  string `json:"team_id,omitempty"`
	Target  string `json:"target,omitempty"`
	EventID string `json:"event_id,omitempty"`
	Source  string `json:"source,omitempty"`
	Event   string `json:"event,omitempty"`
	Message string `json:"message,omitempty"`
}

func NewTaskTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Task",
			Description:     "Start a subagent task.",
			SearchHint:      "launch subagent task",
			ReadOnly:        true,
			ConcurrencySafe: false,
			ShouldDefer:     true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"description", "prompt", "subagent_type"},
				"properties": map[string]any{
					"description": map[string]any{
						"type":        "string",
						"description": "A short description of the task.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "The full instructions for the subagent.",
					},
					"subagent_type": map[string]any{
						"type":        "string",
						"description": "The subagent type to run.",
					},
					"id": map[string]any{
						"type":        "string",
						"description": "Optional stable task id.",
					},
					"worktree": map[string]any{
						"type":        "boolean",
						"description": "Create an isolated git worktree for this task.",
					},
					"run": map[string]any{
						"type":        "boolean",
						"description": "Run the subagent synchronously after recording the task.",
					},
				},
			},
		},
		NormalizeFunc:   normalizeTaskInput,
		PromptFunc:      taskPrompt,
		InputSchemaFunc: taskInputSchema,
		ValidateFunc:    validateTask,
		CallFunc:        callTask,
		ReadOnlyFunc:    func(raw json.RawMessage) bool { return taskInputExplicitlyDisablesWorktree(raw) },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTaskOutputTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "TaskOutput",
			Aliases:            []string{"AgentOutputTool", "TaskOutputTool"},
			Description:        "Read subagent task status and output.",
			SearchHint:         "read subagent task output status progress",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"task_id":    map[string]any{"type": "string"},
					"tail_lines": map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Reads subagent task status and transcript output. Provide task_id to inspect one task, or omit it to list all known tasks for the current session.", nil
		},
		NormalizeFunc:   normalizeTaskOutputInput,
		ValidateFunc:    validateTaskOutput,
		PermissionFunc:  allowTaskOutput,
		CallFunc:        callTaskOutput,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewKillTaskTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "KillTask",
			Aliases:         []string{"TaskStop"},
			Description:     "Cancel a running subagent task.",
			SearchHint:      "kill cancel stop subagent task",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"task_id"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string"},
					"reason":  map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Cancels a running subagent task by task_id. Use TaskOutput afterwards to read the final status and cancellation summary.", nil
		},
		NormalizeFunc:   normalizeKillTaskInput,
		ValidateFunc:    validateKillTask,
		CallFunc:        callKillTask,
		DestructiveFunc: func(json.RawMessage) bool { return true },
	}
}

func NewSendMessageTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "SendMessage",
			Aliases:         []string{"TaskSendMessage"},
			Description:     "Append a user message to a running subagent task.",
			SearchHint:      "send message to subagent task",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"task_id", "message"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string"},
					"message": map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Sends an additional user message to a running subagent task by task_id. Use ResumeTask or TaskOutput afterwards to inspect the updated sidechain context.", nil
		},
		NormalizeFunc:   normalizeSendMessageInput,
		ValidateFunc:    validateSendMessage,
		CallFunc:        callSendMessage,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTeamCreateTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "TeamCreate",
			Description:     "Create or update a team of subagent tasks.",
			SearchHint:      "create team subagent tasks coordinator",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"team_id"},
				"properties": map[string]any{
					"team_id":             map[string]any{"type": "string"},
					"description":         map[string]any{"type": "string"},
					"coordinator_task_id": map[string]any{"type": "string"},
					"task_ids":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Creates or updates a named team of existing subagent tasks by team_id. task_ids and optional coordinator_task_id should reference sidechain task IDs that already exist.", nil
		},
		NormalizeFunc:   normalizeTeamCreateInput,
		ValidateFunc:    validateTeamCreate,
		CallFunc:        callTeamCreate,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTeamDeleteTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "TeamDelete",
			Description:     "Delete a subagent task team.",
			SearchHint:      "delete team subagent tasks",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"team_id"},
				"properties": map[string]any{
					"team_id": map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Deletes a named subagent task team by team_id. This does not cancel or delete the underlying tasks.", nil
		},
		NormalizeFunc:   normalizeTeamDeleteInput,
		ValidateFunc:    validateTeamDelete,
		CallFunc:        callTeamDelete,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTeamOutputTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "TeamOutput",
			Description:        "Read subagent task team status.",
			SearchHint:         "read team subagent task status",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"team_id": map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Reads subagent task team status. Provide team_id to inspect one team and member task summaries, or omit it to list all teams for the session.", nil
		},
		NormalizeFunc:   normalizeTeamOutputInput,
		ValidateFunc:    validateTeamOutput,
		PermissionFunc:  allowTaskOutput,
		CallFunc:        callTeamOutput,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewTeamSendMessageTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "TeamSendMessage",
			Description:     "Append the same user message to running task recipients in a subagent team.",
			SearchHint:      "broadcast message team subagent tasks coordinator",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"team_id", "message"},
				"properties": map[string]any{
					"team_id": map[string]any{"type": "string"},
					"message": map[string]any{"type": "string"},
					"target":  map[string]any{"type": "string", "enum": []any{teamSendTargetMembers, teamSendTargetCoordinator, teamSendTargetAll}},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Sends the same user message to running task recipients in a subagent team. target defaults to members; use coordinator to message only the coordinator task, or all for coordinator plus members. The operation validates every selected task is running before appending any message.", nil
		},
		NormalizeFunc:   normalizeTeamSendMessageInput,
		ValidateFunc:    validateTeamSendMessage,
		CallFunc:        callTeamSendMessage,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTeamDispatchTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "TeamDispatch",
			Description:     "Append individualized assignment messages to running task members in a subagent team.",
			SearchHint:      "dispatch assignments team subagent members",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"team_id", "assignments"},
				"properties": map[string]any{
					"team_id": map[string]any{"type": "string"},
					"assignments": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":     "object",
							"required": []any{"task_id", "message"},
							"properties": map[string]any{
								"task_id": map[string]any{"type": "string"},
								"message": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Dispatches distinct assignment messages to running members of a subagent team. Each assignment must name a team member task_id and message; all selected tasks are validated before any message is appended.", nil
		},
		NormalizeFunc:   normalizeTeamDispatchInput,
		ValidateFunc:    validateTeamDispatch,
		CallFunc:        callTeamDispatch,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTeamScheduleTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "TeamSchedule",
			Description:     "Deterministically schedule a team objective across running task members.",
			SearchHint:      "schedule objective team subagent members assignments",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"team_id", "objective"},
				"properties": map[string]any{
					"team_id":   map[string]any{"type": "string"},
					"objective": map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Builds deterministic assignment messages for every running member in a subagent team and appends them as user messages. Use this when a team objective should be split across members without hand-writing one TeamDispatch assignment per task.", nil
		},
		NormalizeFunc:   normalizeTeamScheduleInput,
		ValidateFunc:    validateTeamSchedule,
		CallFunc:        callTeamSchedule,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTeamAutoScheduleTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "TeamAutoSchedule",
			Description:     "Send a team objective to the coordinator and schedule it across running task members.",
			SearchHint:      "auto schedule objective team coordinator model plan members assignments",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"team_id", "objective"},
				"properties": map[string]any{
					"team_id":   map[string]any{"type": "string"},
					"objective": map[string]any{"type": "string"},
					"assignments": map[string]any{
						"type":        "array",
						"description": "Optional coordinator/model-produced member plan. When omitted, deterministic assignments are generated for every member.",
						"items": map[string]any{
							"type":     "object",
							"required": []any{"task_id", "message"},
							"properties": map[string]any{
								"task_id": map[string]any{"type": "string"},
								"message": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Combines coordinator briefing and member scheduling for a subagent team. If the team has a coordinator, the objective is first appended to the running coordinator with team status context. Provide assignments when a coordinator/model has produced a concrete member plan; otherwise every running member receives a deterministic assignment message.", nil
		},
		NormalizeFunc:   normalizeTeamAutoScheduleInput,
		ValidateFunc:    validateTeamAutoSchedule,
		CallFunc:        callTeamAutoSchedule,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTeamCoordinateTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "TeamCoordinate",
			Description:     "Send a coordination request with team status context to a team's coordinator task.",
			SearchHint:      "coordinate team coordinator subagent status briefing",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"team_id", "message"},
				"properties": map[string]any{
					"team_id": map[string]any{"type": "string"},
					"message": map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Sends a coordination request to a team's running coordinator task. The appended message includes the team description, member task statuses, and the requested objective.", nil
		},
		NormalizeFunc:   normalizeTeamCoordinateInput,
		ValidateFunc:    validateTeamCoordinate,
		CallFunc:        callTeamCoordinate,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewResumeTaskTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "ResumeTask",
			Aliases:            []string{"TaskResume"},
			Description:        "Build resume context for a subagent task.",
			SearchHint:         "resume subagent task context",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"task_id"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string"},
					"limit":   map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Builds a resume context for a subagent task by task_id, including status, can_resume, metadata, and the tail messages that should seed the resumed agent.", nil
		},
		NormalizeFunc:   normalizeResumeTaskInput,
		ValidateFunc:    validateResumeTask,
		PermissionFunc:  allowTaskOutput,
		CallFunc:        callResumeTask,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewSleepTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Sleep",
			Description:     "Wait for a bounded duration.",
			SearchHint:      "sleep wait delay timer",
			ReadOnly:        true,
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"duration_ms": map[string]any{"type": "integer", "description": "Duration to wait in milliseconds."},
					"seconds":     map[string]any{"type": "number", "description": "Duration to wait in seconds."},
					"duration":    map[string]any{"type": "string", "description": "Go duration string such as 500ms, 2s, or 1m."},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Waits for a bounded duration. Provide exactly one of duration_ms, seconds, or duration; the maximum duration is 60 seconds and the wait is cancelled if the tool context is cancelled.", nil
		},
		NormalizeFunc:   normalizeSleepInput,
		ValidateFunc:    validateSleep,
		CallFunc:        callSleep,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewBriefTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Brief",
			Description:     "Create a structured handoff brief.",
			SearchHint:      "brief handoff summary status next steps risks",
			ReadOnly:        true,
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"summary"},
				"properties": map[string]any{
					"title":      map[string]any{"type": "string"},
					"status":     map[string]any{"type": "string"},
					"summary":    map[string]any{"type": "string"},
					"details":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"next_steps": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"risks":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Creates a structured handoff brief from a summary plus optional title, status, details, next_steps, and risks. Use it when a concise state snapshot should be machine-readable for later UI, team, or remote handoff surfaces.", nil
		},
		NormalizeFunc:   normalizeBriefInput,
		ValidateFunc:    validateBrief,
		CallFunc:        callBrief,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewScheduleCronTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "ScheduleCron",
			Description:     "Create, list, or delete session-scoped cron schedules.",
			SearchHint:      "schedule cron proactive reminder team message",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"action":      map[string]any{"type": "string", "enum": []any{scheduleCronActionCreate, scheduleCronActionList, scheduleCronActionDelete, scheduleCronActionTrigger, scheduleCronActionRunDue}},
					"schedule_id": map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
					"cron":        map[string]any{"type": "string"},
					"message":     map[string]any{"type": "string"},
					"team_id":     map[string]any{"type": "string"},
					"target":      map[string]any{"type": "string", "enum": []any{teamSendTargetMembers, teamSendTargetCoordinator, teamSendTargetAll}},
					"enabled":     map[string]any{"type": "boolean"},
					"now":         map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Creates, lists, deletes, manually triggers, or runs due session-scoped cron schedules. action defaults to create. create requires cron and message, and can optionally bind the schedule to a team_id and target. trigger sends one saved schedule message to the bound team's running recipients. run_due executes enabled schedules whose cron matches the current minute.", nil
		},
		NormalizeFunc:   normalizeScheduleCronInput,
		ValidateFunc:    validateScheduleCron,
		CallFunc:        callScheduleCron,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewRemoteTriggerTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "RemoteTrigger",
			Description:     "Inject a remote trigger event into a running subagent team.",
			SearchHint:      "remote trigger event team coordinator",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"team_id", "message"},
				"properties": map[string]any{
					"team_id":  map[string]any{"type": "string"},
					"target":   map[string]any{"type": "string", "enum": []any{teamSendTargetMembers, teamSendTargetCoordinator, teamSendTargetAll}},
					"event_id": map[string]any{"type": "string"},
					"source":   map[string]any{"type": "string"},
					"event":    map[string]any{"type": "string"},
					"message":  map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Injects a remote trigger event into a running subagent team. The message sent to recipients includes source/event metadata plus the trigger message. target defaults to coordinator when the team has one, otherwise members. event_id is optional and dedupes retried remote deliveries.", nil
		},
		NormalizeFunc:   normalizeRemoteTriggerInput,
		ValidateFunc:    validateRemoteTrigger,
		CallFunc:        callRemoteTrigger,
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func taskPrompt(ctx tool.PromptContext) (string, error) {
	prompt := "Starts a subagent task with a short description, full prompt, and subagent_type. The task is recorded as a sidechain so it can be listed or resumed by the runtime. Set run=true only when the runtime should immediately execute a synchronous subagent pass."
	agents := availableTaskAgents(ctx.Metadata)
	if len(agents) == 0 {
		return prompt, nil
	}
	var lines []string
	for _, agent := range agents {
		if agent.Description != "" {
			lines = append(lines, fmt.Sprintf("- %s: %s", agent.Name, agent.Description))
		} else {
			lines = append(lines, "- "+agent.Name)
		}
	}
	return prompt + "\n\nAvailable subagent types:\n" + strings.Join(lines, "\n"), nil
}

func taskInputSchema(ctx tool.PromptContext) contracts.JSONSchema {
	schema := contracts.JSONSchema{
		"type":     "object",
		"required": []any{"description", "prompt", "subagent_type"},
		"properties": map[string]any{
			"description": map[string]any{
				"type":        "string",
				"description": "A short description of the task.",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The full instructions for the subagent.",
			},
			"subagent_type": map[string]any{
				"type":        "string",
				"description": "The subagent type to run.",
			},
			"id": map[string]any{
				"type":        "string",
				"description": "Optional stable task id.",
			},
			"worktree": map[string]any{
				"type":        "boolean",
				"description": "Create an isolated git worktree for this task.",
			},
			"run": map[string]any{
				"type":        "boolean",
				"description": "Run the subagent synchronously after recording the task.",
			},
		},
	}
	metadataAgents := taskAgentsFromMetadata(ctx.Metadata)
	if len(metadataAgents) == 0 {
		return schema
	}
	names := taskAgentNames(availableTaskAgents(ctx.Metadata))
	if len(names) > 0 {
		if properties, ok := schema["properties"].(map[string]any); ok {
			if subagent, ok := properties["subagent_type"].(map[string]any); ok {
				enumValues := make([]any, 0, len(names))
				for _, name := range names {
					enumValues = append(enumValues, name)
				}
				subagent["enum"] = enumValues
			}
		}
	}
	return schema
}

func normalizeTaskInput(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	input := taskInput{}
	if value, ok := firstString(obj, "id", "task_id", "taskId", "sidechain_id", "sidechainId"); ok {
		input.ID = value
	}
	if value, ok := firstString(obj, "description", "desc", "summary", "title", "task_description", "taskDescription"); ok {
		input.Description = value
	}
	if value, ok := firstString(obj, "prompt", "instructions", "instruction", "input", "task", "request"); ok {
		input.Prompt = value
	}
	if value, ok := firstString(obj, "subagent_type", "subagentType", "agent_type", "agentType", "agent", "type"); ok {
		input.SubagentType = value
	}
	worktreeSet := false
	if value, ok := firstBool(obj, "worktree", "isolated_worktree", "isolatedWorktree", "worktree_isolation", "worktreeIsolation", "create_worktree", "createWorktree"); ok {
		input.Worktree = value
		worktreeSet = true
	}
	if value, ok := firstBool(obj, "run", "execute", "sync", "synchronous", "await", "wait"); ok {
		input.Run = value
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if worktreeSet {
		var normalized map[string]any
		if err := json.Unmarshal(data, &normalized); err != nil {
			return nil, err
		}
		normalized["worktree"] = input.Worktree
		data, err = json.Marshal(normalized)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func validateTask(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTaskInput(raw)
	if err != nil {
		return err
	}
	if input.Description == "" {
		return fmt.Errorf("description is required")
	}
	if input.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if input.SubagentType == "" {
		return fmt.Errorf("subagent_type is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if taskInputRequestsWorktree(ctx, input) && strings.TrimSpace(ctx.WorkingDirectory) == "" {
		return fmt.Errorf("working directory is required for worktree isolation")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	if len(taskAgentsFromMetadata(ctx.Metadata)) > 0 && !taskAgentAllowed(input.SubagentType, availableTaskAgents(ctx.Metadata)) {
		return fmt.Errorf("subagent_type %q is not available (available: %s)", input.SubagentType, strings.Join(taskAgentNames(availableTaskAgents(ctx.Metadata)), ", "))
	}
	return nil
}

func validateTaskOutput(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTaskOutputInput(raw)
	if err != nil {
		return err
	}
	if input.TailLines != nil && *input.TailLines <= 0 {
		return fmt.Errorf("tail_lines must be positive")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func validateKillTask(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeKillTaskInput(raw)
	if err != nil {
		return err
	}
	if input.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func validateSendMessage(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeSendMessageInput(raw)
	if err != nil {
		return err
	}
	if input.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if input.Message == "" {
		return fmt.Errorf("message is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func validateTeamCreate(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTeamCreateInput(raw)
	if err != nil {
		return err
	}
	if input.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	if input.CoordinatorTaskID != "" {
		if _, err := findTaskState(manager, input.CoordinatorTaskID); err != nil {
			return err
		}
	}
	for _, taskID := range input.TaskIDs {
		if _, err := findTaskState(manager, taskID); err != nil {
			return err
		}
	}
	return nil
}

func validateTeamDelete(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTeamDeleteInput(raw)
	if err != nil {
		return err
	}
	if input.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func validateTeamOutput(ctx tool.Context, raw json.RawMessage) error {
	if _, err := decodeTeamOutputInput(raw); err != nil {
		return err
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func validateTeamSendMessage(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTeamSendMessageInput(raw)
	if err != nil {
		return err
	}
	if input.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}
	if input.Message == "" {
		return fmt.Errorf("message is required")
	}
	if err := validateTeamSendTarget(input.Target); err != nil {
		return err
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return err
	}
	taskIDs, err := teamSendMessageTaskIDs(team, resolvedTeamSendTarget(input.Target))
	if err != nil {
		return err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return err
	}
	return nil
}

func validateTeamDispatch(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTeamDispatchInput(raw)
	if err != nil {
		return err
	}
	if input.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}
	if len(input.Assignments) == 0 {
		return fmt.Errorf("assignments are required")
	}
	if len(input.Assignments) > 32 {
		return fmt.Errorf("assignments must include <= 32 items")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return err
	}
	taskIDs, err := teamDispatchTaskIDs(team, input.Assignments)
	if err != nil {
		return err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return err
	}
	return nil
}

func validateTeamSchedule(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTeamScheduleInput(raw)
	if err != nil {
		return err
	}
	if input.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}
	if input.Objective == "" {
		return fmt.Errorf("objective is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return err
	}
	taskIDs, err := teamScheduleTaskIDs(team)
	if err != nil {
		return err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return err
	}
	return nil
}

func validateTeamAutoSchedule(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTeamAutoScheduleInput(raw)
	if err != nil {
		return err
	}
	if input.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}
	if input.Objective == "" {
		return fmt.Errorf("objective is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return err
	}
	taskIDs, err := teamAutoScheduleTaskIDs(team, input)
	if err != nil {
		return err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return err
	}
	if team.CoordinatorTaskID != "" {
		if _, err := runningTeamCoordinatorState(manager, team); err != nil {
			return err
		}
	}
	return nil
}

func validateTeamCoordinate(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTeamCoordinateInput(raw)
	if err != nil {
		return err
	}
	if input.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}
	if input.Message == "" {
		return fmt.Errorf("message is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return err
	}
	if _, err := runningTeamCoordinatorState(manager, team); err != nil {
		return err
	}
	return nil
}

func validateResumeTask(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeResumeTaskInput(raw)
	if err != nil {
		return err
	}
	if input.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if input.Limit != nil && *input.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func validateSleep(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeSleepInput(raw)
	if err != nil {
		return err
	}
	if _, err := sleepDuration(input); err != nil {
		return err
	}
	if ctx.Context != nil {
		if err := ctx.Context.Err(); err != nil {
			return err
		}
	}
	return nil
}

func validateBrief(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeBriefInput(raw)
	if err != nil {
		return err
	}
	if input.Summary == "" {
		return fmt.Errorf("summary is required")
	}
	if briefCharCount(input) > 20_000 {
		return fmt.Errorf("brief content must be <= 20000 characters")
	}
	if ctx.Context != nil {
		if err := ctx.Context.Err(); err != nil {
			return err
		}
	}
	return nil
}

func validateScheduleCron(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeScheduleCronInput(raw)
	if err != nil {
		return err
	}
	if err := validateScheduleCronAction(input.Action); err != nil {
		return err
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	if input.Now != "" {
		if _, err := parseScheduleCronNow(input.Now); err != nil {
			return err
		}
	}
	switch resolvedScheduleCronAction(input.Action) {
	case scheduleCronActionCreate:
		if input.Cron == "" {
			return fmt.Errorf("cron is required")
		}
		if !validScheduleCronSpec(input.Cron) {
			return fmt.Errorf("cron must be a supported 5-field expression or @hourly/@daily/@weekly/@monthly/@yearly")
		}
		if input.Message == "" {
			return fmt.Errorf("message is required")
		}
		if err := validateTeamSendTarget(input.Target); err != nil {
			return err
		}
		if input.TeamID != "" {
			manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
			manifest, err := manager.TeamManifest()
			if err != nil {
				return err
			}
			if _, ok := findTeamState(manifest, input.TeamID); !ok {
				return fmt.Errorf("team not found: %s", input.TeamID)
			}
		}
	case scheduleCronActionDelete:
		if input.ScheduleID == "" {
			return fmt.Errorf("schedule_id is required")
		}
		manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
		if _, ok, err := findScheduleState(manager, input.ScheduleID); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("schedule not found: %s", sanitizeTaskLikeID(input.ScheduleID))
		}
	case scheduleCronActionTrigger:
		if input.ScheduleID == "" {
			return fmt.Errorf("schedule_id is required")
		}
		manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
		schedule, ok, err := findScheduleState(manager, input.ScheduleID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("schedule not found: %s", sanitizeTaskLikeID(input.ScheduleID))
		}
		if !schedule.Enabled {
			return fmt.Errorf("schedule %s is disabled", schedule.ID)
		}
		if schedule.TeamID == "" {
			return fmt.Errorf("schedule %s has no team", schedule.ID)
		}
		team, err := loadTeamForMessage(manager, schedule.TeamID)
		if err != nil {
			return err
		}
		taskIDs, err := teamSendMessageTaskIDs(team, schedule.Target)
		if err != nil {
			return err
		}
		if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
			return err
		}
	case scheduleCronActionRunDue:
		if input.ScheduleID != "" {
			manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
			if _, ok, err := findScheduleState(manager, input.ScheduleID); err != nil {
				return err
			} else if !ok {
				return fmt.Errorf("schedule not found: %s", sanitizeTaskLikeID(input.ScheduleID))
			}
		}
	}
	return nil
}

func validateRemoteTrigger(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeRemoteTriggerInput(raw)
	if err != nil {
		return err
	}
	if input.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}
	if input.Message == "" {
		return fmt.Errorf("message is required")
	}
	if err := validateTeamSendTarget(input.Target); err != nil {
		return err
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	if input.EventID != "" {
		if _, ok, err := manager.RemoteTriggerReceipt(input.EventID); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return err
	}
	target := resolvedRemoteTriggerTarget(team, input.Target)
	taskIDs, err := teamSendMessageTaskIDs(team, target)
	if err != nil {
		return err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return err
	}
	return nil
}

func allowTaskOutput(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{
		Behavior:       contracts.PermissionAllow,
		DecisionReason: "reading subagent task status is read-only",
	}, nil
}

func callTask(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTaskInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	sessionPath := sessionPathFromMetadata(ctx.Metadata)
	runtime := session.SidechainRuntime{SessionPath: sessionPath, SessionID: ctx.SessionID}
	agent, hasAgent := taskAgentForType(input.SubagentType, availableTaskAgents(ctx.Metadata))
	taskID := taskSidechainID(input.ID)
	input.ID = taskID
	if err := ensureTaskCanStart(sessionPath, ctx.SessionID, taskID); err != nil {
		return contracts.ToolResult{}, err
	}
	requestedWorktree := taskInputRequestsWorktree(ctx, input)
	worktree, err := prepareTaskWorktree(ctx, taskID, requestedWorktree)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	run, err := runtime.Start(session.SidechainOptions{
		ID:                  taskID,
		AgentType:           input.SubagentType,
		WorktreePath:        worktree.Path,
		WorktreeOwned:       worktree.Owned,
		WorktreeSparsePaths: append([]string(nil), worktree.SparsePaths...),
		WorktreeSymlinkDirs: append([]string(nil), worktree.SymlinkDirectories...),
		Description:         input.Description,
		AgentPath:           agent.Path,
		AgentPrompt:         agent.Prompt,
		AgentModel:          agent.Model,
		AgentPermissionMode: string(agent.PermissionMode),
		AgentAllowedTools:   append([]string(nil), agent.AllowedTools...),
	})
	if err != nil {
		if worktree.Owned {
			_ = removePreparedTaskWorktree(ctx, worktree.Path)
		}
		return contracts.ToolResult{}, err
	}
	if hasAgent && agent.Prompt != "" {
		agentMessage := msgs.SystemText("agent_prompt", agent.Prompt)
		agentMessage.SessionID = ctx.SessionID
		if err := runtime.Append(run, session.TranscriptMessage{
			Type:        string(contracts.MessageSystem),
			UUID:        agentMessage.UUID,
			SessionID:   ctx.SessionID,
			Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
			Subtype:     "agent_prompt",
			IsSidechain: true,
			AgentID:     run.ID,
			Message:     &agentMessage,
			Content: map[string]any{
				"agentType":           input.SubagentType,
				"agentPath":           agent.Path,
				"agentPrompt":         agent.Prompt,
				"agentModel":          agent.Model,
				"agentPermissionMode": string(agent.PermissionMode),
				"agentAllowedTools":   append([]string(nil), agent.AllowedTools...),
			},
		}); err != nil {
			return contracts.ToolResult{}, err
		}
	}
	taskMessage := msgs.UserText(input.Prompt)
	taskMessage.SessionID = ctx.SessionID
	if err := runtime.Append(run, session.TranscriptMessage{
		Type:        string(contracts.MessageUser),
		UUID:        taskMessage.UUID,
		SessionID:   ctx.SessionID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		IsSidechain: true,
		AgentID:     run.ID,
		Message:     &taskMessage,
	}); err != nil {
		return contracts.ToolResult{}, err
	}
	structured := map[string]any{
		"success":       true,
		"type":          "task",
		"status":        session.SidechainStatusRunning,
		"run":           input.Run,
		"sidechain_id":  run.ID,
		"subagent_type": input.SubagentType,
		"description":   input.Description,
		"path":          run.Path,
		"worktree_path": worktree.Path,
	}
	if worktree.Owned {
		structured["worktree_owned"] = true
	}
	if len(worktree.SparsePaths) > 0 {
		structured["worktree_sparse_paths"] = append([]string(nil), worktree.SparsePaths...)
	}
	if len(worktree.SymlinkDirectories) > 0 {
		structured["worktree_symlink_directories"] = append([]string(nil), worktree.SymlinkDirectories...)
	}
	if !input.WorktreeSet && requestedWorktree {
		structured["worktree_defaulted"] = true
	}
	if hasAgent && agent.Path != "" {
		structured["agent_path"] = agent.Path
	}
	if hasAgent && agent.Prompt != "" {
		structured["agent_prompt_chars"] = len(agent.Prompt)
	}
	if hasAgent && agent.Model != "" {
		structured["agent_model"] = agent.Model
	}
	if hasAgent && agent.PermissionMode != "" {
		structured["agent_permission_mode"] = string(agent.PermissionMode)
	}
	if hasAgent && len(agent.AllowedTools) > 0 {
		structured["agent_allowed_tools"] = append([]string(nil), agent.AllowedTools...)
	}
	_ = tool.SendProgress(sink, "", "task_started", map[string]any{
		"task_id":        run.ID,
		"sidechain_id":   run.ID,
		"status":         session.SidechainStatusRunning,
		"subagent_type":  input.SubagentType,
		"description":    input.Description,
		"run":            input.Run,
		"path":           run.Path,
		"worktree_path":  worktree.Path,
		"worktree_owned": worktree.Owned,
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Task started: %s\nSubagent type: %s\nSidechain ID: %s", input.Description, input.SubagentType, run.ID),
		StructuredContent: structured,
	}, nil
}

func callTaskOutput(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTaskOutputInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	if input.TaskID == "" {
		states, err := manager.List()
		if err != nil {
			return contracts.ToolResult{}, err
		}
		tasks := make([]map[string]any, 0, len(states))
		for _, state := range states {
			tasks = append(tasks, structuredTaskState(state))
		}
		_ = tool.SendProgress(sink, "", "task_listed", map[string]any{
			"count": len(tasks),
		})
		return contracts.ToolResult{
			Content: formatTaskList(states),
			StructuredContent: map[string]any{
				"type":  "task_output",
				"tasks": tasks,
				"count": len(tasks),
			},
		}, nil
	}
	state, err := findTaskState(manager, input.TaskID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	output, err := taskTranscriptOutput(state, taskOutputTailLines(input))
	if err != nil {
		return contracts.ToolResult{}, err
	}
	structured := structuredTaskState(state)
	structured["type"] = "task_output"
	structured["output"] = output
	if input.TailLines != nil {
		structured["tail_lines"] = *input.TailLines
	}
	_ = tool.SendProgress(sink, "", "task_output", map[string]any{
		"task_id":       state.ID,
		"sidechain_id":  state.ID,
		"status":        state.Status,
		"running":       state.Status == session.SidechainStatusRunning,
		"message_count": state.MessageCount,
		"tail_lines":    taskOutputTailLines(input),
	})
	return contracts.ToolResult{
		Content:           formatTaskOutput(state, output),
		StructuredContent: structured,
	}, nil
}

func callKillTask(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeKillTaskInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	state, err := findTaskState(manager, input.TaskID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	killed := false
	if state.Status == session.SidechainStatusRunning {
		reason := input.Reason
		if reason == "" {
			reason = "cancelled by KillTask"
		}
		if _, err := manager.Cancel(state.ID, reason, time.Now().UTC()); err != nil {
			return contracts.ToolResult{}, err
		}
		killed = true
		state, err = findTaskState(manager, state.ID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
	}
	cleanup, err := CleanupOwnedWorktree(ctx, manager, state, input.Reason)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if cleanup.Attempted {
		state, err = findTaskState(manager, state.ID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
	}
	content := fmt.Sprintf("Task %s is not running.", state.ID)
	if killed {
		content = fmt.Sprintf("Cancel requested for task %s.", state.ID)
	}
	if cleanup.Attempted {
		content += fmt.Sprintf("\nWorktree cleanup: %s.", cleanup.Status)
		if cleanup.Reason != "" {
			content += " " + cleanup.Reason
		}
	}
	structured := structuredTaskState(state)
	structured["type"] = "kill_task"
	structured["killed"] = killed
	structured["cancelled"] = state.Status == session.SidechainStatusCancelled
	if cleanup.Attempted {
		structured["worktree_cleanup_attempted"] = true
		structured["worktree_cleanup_status"] = cleanup.Status
		if cleanup.Reason != "" {
			structured["worktree_cleanup_reason"] = cleanup.Reason
		}
	}
	progressType := "task_not_running"
	if killed {
		progressType = "task_cancelled"
	}
	_ = tool.SendProgress(sink, "", progressType, map[string]any{
		"task_id":      state.ID,
		"sidechain_id": state.ID,
		"status":       state.Status,
		"killed":       killed,
		"cancelled":    state.Status == session.SidechainStatusCancelled,
	})
	if cleanup.Attempted {
		_ = tool.SendProgress(sink, "", "task_worktree_cleanup", map[string]any{
			"task_id":        state.ID,
			"sidechain_id":   state.ID,
			"worktree_path":  state.Metadata.WorktreePath,
			"cleanup_status": cleanup.Status,
			"cleanup_reason": cleanup.Reason,
		})
	}
	return contracts.ToolResult{
		Content:           content,
		StructuredContent: structured,
	}, nil
}

func callSendMessage(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeSendMessageInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	state, err := findTaskState(manager, input.TaskID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if state.Status != session.SidechainStatusRunning {
		return contracts.ToolResult{}, fmt.Errorf("task %s is not running", state.ID)
	}
	message := msgs.UserText(input.Message)
	message.SessionID = ctx.SessionID
	if err := manager.Append(state.ID, session.TranscriptMessage{
		Type:        string(contracts.MessageUser),
		UUID:        message.UUID,
		SessionID:   ctx.SessionID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		IsSidechain: true,
		AgentID:     state.ID,
		Message:     &message,
	}); err != nil {
		return contracts.ToolResult{}, err
	}
	state, err = findTaskState(manager, state.ID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	structured := structuredTaskState(state)
	structured["type"] = "send_message"
	structured["message_uuid"] = string(message.UUID)
	structured["message_chars"] = len(input.Message)
	_ = tool.SendProgress(sink, "", "task_message_sent", map[string]any{
		"task_id":       state.ID,
		"sidechain_id":  state.ID,
		"status":        state.Status,
		"message_uuid":  string(message.UUID),
		"message_chars": len(input.Message),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Message sent to task %s.", state.ID),
		StructuredContent: structured,
	}, nil
}

func callTeamCreate(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTeamCreateInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, manifest, err := manager.CreateTeam(session.TeamOptions{
		ID:                input.TeamID,
		Description:       input.Description,
		CoordinatorTaskID: input.CoordinatorTaskID,
		TaskIDs:           input.TaskIDs,
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	structured := structuredTeamState(team)
	structured["type"] = "team_create"
	structured["team_count"] = len(manifest.Teams)
	_ = tool.SendProgress(sink, "", "team_created", map[string]any{
		"team_id":    team.ID,
		"task_count": len(team.TaskIDs),
		"team_count": len(manifest.Teams),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Team %s created with %d task(s).", team.ID, len(team.TaskIDs)),
		StructuredContent: structured,
	}, nil
}

func callTeamDelete(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTeamDeleteInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, manifest, err := manager.DeleteTeam(input.TeamID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	structured := structuredTeamState(team)
	structured["type"] = "team_delete"
	structured["deleted"] = true
	structured["team_count"] = len(manifest.Teams)
	_ = tool.SendProgress(sink, "", "team_deleted", map[string]any{
		"team_id":    team.ID,
		"task_count": len(team.TaskIDs),
		"team_count": len(manifest.Teams),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Team %s deleted.", team.ID),
		StructuredContent: structured,
	}, nil
}

func callTeamOutput(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTeamOutputInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	manifest, err := manager.TeamManifest()
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if input.TeamID == "" {
		teams := make([]map[string]any, 0, len(manifest.Teams))
		for _, team := range manifest.Teams {
			teams = append(teams, structuredTeamState(team))
		}
		_ = tool.SendProgress(sink, "", "team_listed", map[string]any{
			"team_count": len(teams),
		})
		return contracts.ToolResult{
			Content: formatTeamList(manifest.Teams),
			StructuredContent: map[string]any{
				"type":       "team_output",
				"teams":      teams,
				"team_count": len(teams),
			},
		}, nil
	}
	team, ok := findTeamState(manifest, input.TeamID)
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("team not found: %s", input.TeamID)
	}
	coordinator := structuredTeamCoordinatorState(manager, team)
	tasks := structuredTeamTaskStates(manager, team)
	structured := structuredTeamState(team)
	structured["type"] = "team_output"
	if coordinator != nil {
		structured["coordinator"] = coordinator
	}
	structured["tasks"] = tasks
	_ = tool.SendProgress(sink, "", "team_output", map[string]any{
		"team_id":    team.ID,
		"task_count": len(team.TaskIDs),
	})
	return contracts.ToolResult{
		Content:           formatTeamOutput(team, coordinator, tasks),
		StructuredContent: structured,
	}, nil
}

func callTeamSendMessage(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTeamSendMessageInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	target := resolvedTeamSendTarget(input.Target)
	taskIDs, err := teamSendMessageTaskIDs(team, target)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return contracts.ToolResult{}, err
	}
	sent := make([]map[string]any, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		message := msgs.UserText(input.Message)
		message.SessionID = ctx.SessionID
		if err := manager.Append(taskID, session.TranscriptMessage{
			Type:        string(contracts.MessageUser),
			UUID:        message.UUID,
			SessionID:   ctx.SessionID,
			Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
			IsSidechain: true,
			AgentID:     taskID,
			Message:     &message,
		}); err != nil {
			return contracts.ToolResult{}, err
		}
		sent = append(sent, map[string]any{
			"task_id":      taskID,
			"sidechain_id": taskID,
			"message_uuid": string(message.UUID),
		})
	}
	structured := structuredTeamState(team)
	structured["type"] = "team_send_message"
	structured["target"] = target
	structured["message_chars"] = len(input.Message)
	structured["sent_count"] = len(sent)
	structured["sent"] = sent
	_ = tool.SendProgress(sink, "", "team_message_sent", map[string]any{
		"team_id":       team.ID,
		"target":        target,
		"task_count":    len(team.TaskIDs),
		"sent_count":    len(sent),
		"message_chars": len(input.Message),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Message sent to %d task(s) in team %s.", len(sent), team.ID),
		StructuredContent: structured,
	}, nil
}

func callTeamDispatch(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTeamDispatchInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	taskIDs, err := teamDispatchTaskIDs(team, input.Assignments)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return contracts.ToolResult{}, err
	}
	now := time.Now().UTC()
	assignments := make([]map[string]any, 0, len(input.Assignments))
	for _, assignment := range input.Assignments {
		dispatchMessage := formatTeamDispatchMessage(team, assignment)
		message := msgs.UserText(dispatchMessage)
		message.SessionID = ctx.SessionID
		if err := manager.Append(assignment.TaskID, session.TranscriptMessage{
			Type:        string(contracts.MessageUser),
			UUID:        message.UUID,
			SessionID:   ctx.SessionID,
			Timestamp:   now.Format(time.RFC3339Nano),
			IsSidechain: true,
			AgentID:     assignment.TaskID,
			Message:     &message,
		}); err != nil {
			return contracts.ToolResult{}, err
		}
		assignments = append(assignments, map[string]any{
			"task_id":       assignment.TaskID,
			"sidechain_id":  assignment.TaskID,
			"message_uuid":  string(message.UUID),
			"message_chars": len(assignment.Message),
		})
	}
	structured := structuredTeamState(team)
	structured["type"] = "team_dispatch"
	structured["assignment_count"] = len(assignments)
	structured["assignments"] = assignments
	_ = tool.SendProgress(sink, "", "team_dispatched", map[string]any{
		"team_id":          team.ID,
		"task_count":       len(team.TaskIDs),
		"assignment_count": len(assignments),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Dispatched %d assignment(s) in team %s.", len(assignments), team.ID),
		StructuredContent: structured,
	}, nil
}

func callTeamSchedule(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTeamScheduleInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	taskIDs, err := teamScheduleTaskIDs(team)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return contracts.ToolResult{}, err
	}
	tasks := structuredTeamTaskStates(manager, team)
	now := time.Now().UTC()
	assignments := make([]map[string]any, 0, len(taskIDs))
	for i, taskID := range taskIDs {
		scheduleMessage := formatTeamScheduleMessage(team, tasks, input.Objective, taskID, i+1, len(taskIDs))
		message := msgs.UserText(scheduleMessage)
		message.SessionID = ctx.SessionID
		if err := manager.Append(taskID, session.TranscriptMessage{
			Type:        string(contracts.MessageUser),
			UUID:        message.UUID,
			SessionID:   ctx.SessionID,
			Timestamp:   now.Format(time.RFC3339Nano),
			IsSidechain: true,
			AgentID:     taskID,
			Message:     &message,
		}); err != nil {
			return contracts.ToolResult{}, err
		}
		assignments = append(assignments, map[string]any{
			"task_id":       taskID,
			"sidechain_id":  taskID,
			"member_index":  i + 1,
			"member_count":  len(taskIDs),
			"message_uuid":  string(message.UUID),
			"message_chars": len(scheduleMessage),
		})
	}
	structured := structuredTeamState(team)
	structured["type"] = "team_schedule"
	structured["objective"] = input.Objective
	structured["objective_chars"] = len(input.Objective)
	structured["assignment_count"] = len(assignments)
	structured["assignments"] = assignments
	structured["tasks"] = tasks
	_ = tool.SendProgress(sink, "", "team_scheduled", map[string]any{
		"team_id":          team.ID,
		"task_count":       len(team.TaskIDs),
		"assignment_count": len(assignments),
		"objective_chars":  len(input.Objective),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Scheduled %d assignment(s) in team %s.", len(assignments), team.ID),
		StructuredContent: structured,
	}, nil
}

func callTeamAutoSchedule(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTeamAutoScheduleInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	taskIDs, err := teamAutoScheduleTaskIDs(team, input)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return contracts.ToolResult{}, err
	}
	scheduleSource := teamAutoScheduleSourceDeterministic
	if len(input.Assignments) > 0 {
		scheduleSource = teamAutoScheduleSourceCoordinatorPlan
	}
	var coordinator session.SidechainState
	if team.CoordinatorTaskID != "" {
		coordinator, err = runningTeamCoordinatorState(manager, team)
		if err != nil {
			return contracts.ToolResult{}, err
		}
	}
	tasks := structuredTeamTaskStates(manager, team)
	now := time.Now().UTC()
	structured := structuredTeamState(team)
	structured["type"] = "team_auto_schedule"
	structured["objective"] = input.Objective
	structured["objective_chars"] = len(input.Objective)
	structured["schedule_source"] = scheduleSource
	structured["tasks"] = tasks
	if coordinator.ID != "" {
		briefing := formatTeamAutoCoordinateMessage(team, tasks, input.Objective, input.Assignments)
		message, err := appendTaskUserMessage(manager, ctx.SessionID, coordinator.ID, briefing, now)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		coordinator, err = findTaskState(manager, coordinator.ID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		structured["coordinator"] = structuredTaskState(coordinator)
		structured["coordinator_message"] = map[string]any{
			"task_id":        coordinator.ID,
			"sidechain_id":   coordinator.ID,
			"message_uuid":   string(message.UUID),
			"message_chars":  len(input.Objective),
			"briefing_chars": len(briefing),
		}
	}
	assignments := make([]map[string]any, 0, len(taskIDs))
	if len(input.Assignments) > 0 {
		for _, assignment := range input.Assignments {
			plannedMessage := formatTeamPlannedAssignmentMessage(team, input.Objective, assignment)
			message, err := appendTaskUserMessage(manager, ctx.SessionID, assignment.TaskID, plannedMessage, now)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			assignments = append(assignments, map[string]any{
				"task_id":               assignment.TaskID,
				"sidechain_id":          assignment.TaskID,
				"message_uuid":          string(message.UUID),
				"message_chars":         len(assignment.Message),
				"planned_message_chars": len(plannedMessage),
				"schedule_source":       scheduleSource,
			})
		}
	} else {
		for i, taskID := range taskIDs {
			scheduleMessage := formatTeamScheduleMessage(team, tasks, input.Objective, taskID, i+1, len(taskIDs))
			message, err := appendTaskUserMessage(manager, ctx.SessionID, taskID, scheduleMessage, now)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			assignments = append(assignments, map[string]any{
				"task_id":         taskID,
				"sidechain_id":    taskID,
				"member_index":    i + 1,
				"member_count":    len(taskIDs),
				"message_uuid":    string(message.UUID),
				"message_chars":   len(scheduleMessage),
				"schedule_source": scheduleSource,
			})
		}
	}
	structured["assignment_count"] = len(assignments)
	structured["assignments"] = assignments
	_ = tool.SendProgress(sink, "", "team_auto_scheduled", map[string]any{
		"team_id":             team.ID,
		"task_count":          len(team.TaskIDs),
		"assignment_count":    len(assignments),
		"has_coordinator":     coordinator.ID != "",
		"coordinator_task_id": coordinator.ID,
		"objective_chars":     len(input.Objective),
		"schedule_source":     scheduleSource,
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Auto-scheduled %d assignment(s) in team %s using %s.", len(assignments), team.ID, scheduleSource),
		StructuredContent: structured,
	}, nil
}

func callTeamCoordinate(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTeamCoordinateInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	coordinator, err := runningTeamCoordinatorState(manager, team)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	tasks := structuredTeamTaskStates(manager, team)
	briefing := formatTeamCoordinateMessage(team, tasks, input.Message)
	message := msgs.UserText(briefing)
	message.SessionID = ctx.SessionID
	if err := manager.Append(coordinator.ID, session.TranscriptMessage{
		Type:        string(contracts.MessageUser),
		UUID:        message.UUID,
		SessionID:   ctx.SessionID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		IsSidechain: true,
		AgentID:     coordinator.ID,
		Message:     &message,
	}); err != nil {
		return contracts.ToolResult{}, err
	}
	coordinator, err = findTaskState(manager, coordinator.ID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	structured := structuredTeamState(team)
	structured["type"] = "team_coordinate"
	structured["coordinator"] = structuredTaskState(coordinator)
	structured["tasks"] = tasks
	structured["message_uuid"] = string(message.UUID)
	structured["message_chars"] = len(input.Message)
	structured["briefing_chars"] = len(briefing)
	_ = tool.SendProgress(sink, "", "team_coordinated", map[string]any{
		"team_id":        team.ID,
		"coordinator_id": coordinator.ID,
		"task_count":     len(team.TaskIDs),
		"message_chars":  len(input.Message),
		"briefing_chars": len(briefing),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Coordination request sent to coordinator %s for team %s.", coordinator.ID, team.ID),
		StructuredContent: structured,
	}, nil
}

func callResumeTask(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeResumeTaskInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	if _, err := findTaskState(manager, input.TaskID); err != nil {
		return contracts.ToolResult{}, err
	}
	resumeContext, err := manager.ResumeContext(input.TaskID, resumeTaskLimit(input))
	if err != nil {
		return contracts.ToolResult{}, err
	}
	structured := structuredTaskState(resumeContext.State)
	structured["type"] = "task_resume"
	structured["can_resume"] = resumeContext.CanResume
	structured["truncated"] = resumeContext.Truncated
	structured["message_limit"] = resumeContext.MessageLimit
	structured["resume_messages"] = structuredResumeMessages(resumeContext.Messages)
	_ = tool.SendProgress(sink, "", "task_resume_context", map[string]any{
		"task_id":       resumeContext.State.ID,
		"sidechain_id":  resumeContext.State.ID,
		"status":        resumeContext.State.Status,
		"can_resume":    resumeContext.CanResume,
		"truncated":     resumeContext.Truncated,
		"message_limit": resumeContext.MessageLimit,
	})
	return contracts.ToolResult{
		Content:           formatTaskResume(resumeContext),
		StructuredContent: structured,
	}, nil
}

func callSleep(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeSleepInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	duration, err := sleepDuration(input)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	waitCtx := ctx.Context
	if waitCtx == nil {
		waitCtx = context.Background()
	}
	started := time.Now().UTC()
	_ = tool.SendProgress(sink, "", "sleep_started", map[string]any{
		"duration_ms": duration.Milliseconds(),
	})
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-waitCtx.Done():
		ended := time.Now().UTC()
		structured := map[string]any{
			"type":        "sleep",
			"duration_ms": duration.Milliseconds(),
			"started_at":  started.Format(time.RFC3339Nano),
			"ended_at":    ended.Format(time.RFC3339Nano),
			"cancelled":   true,
		}
		return contracts.ToolResult{
			Content:           "Sleep cancelled.",
			StructuredContent: structured,
		}, waitCtx.Err()
	}
	ended := time.Now().UTC()
	structured := map[string]any{
		"type":        "sleep",
		"duration_ms": duration.Milliseconds(),
		"started_at":  started.Format(time.RFC3339Nano),
		"ended_at":    ended.Format(time.RFC3339Nano),
		"cancelled":   false,
	}
	_ = tool.SendProgress(sink, "", "sleep_completed", map[string]any{
		"duration_ms": duration.Milliseconds(),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Slept for %dms.", duration.Milliseconds()),
		StructuredContent: structured,
	}, nil
}

func callBrief(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeBriefInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	content := formatBrief(input)
	structured := map[string]any{
		"type":             "brief",
		"title":            input.Title,
		"status":           input.Status,
		"summary":          input.Summary,
		"details":          append([]string(nil), input.Details...),
		"next_steps":       append([]string(nil), input.NextSteps...),
		"risks":            append([]string(nil), input.Risks...),
		"detail_count":     len(input.Details),
		"next_step_count":  len(input.NextSteps),
		"risk_count":       len(input.Risks),
		"content_chars":    len(content),
		"brief_body_chars": briefCharCount(input),
	}
	_ = tool.SendProgress(sink, "", "brief_created", map[string]any{
		"title":           input.Title,
		"status":          input.Status,
		"detail_count":    len(input.Details),
		"next_step_count": len(input.NextSteps),
		"risk_count":      len(input.Risks),
		"content_chars":   len(content),
	})
	return contracts.ToolResult{
		Content:           content,
		StructuredContent: structured,
	}, nil
}

func RunDueSchedules(ctx tool.Context, scheduleID string, now time.Time, sink tool.ProgressSink) (contracts.ToolResult, error) {
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	manifest, err := manager.ScheduleManifest()
	if err != nil {
		return contracts.ToolResult{}, err
	}
	dueSchedules := session.DueSchedulesAt(manifest, now)
	filterID := sanitizeTaskLikeID(scheduleID)
	selectedDue := make([]session.ScheduleState, 0, len(dueSchedules))
	for _, schedule := range dueSchedules {
		if filterID == "" || schedule.ID == filterID {
			selectedDue = append(selectedDue, schedule)
		}
	}
	triggered := make([]map[string]any, 0, len(selectedDue))
	runErrors := make([]map[string]any, 0)
	for _, schedule := range selectedDue {
		sent, triggerMessageChars, err := triggerScheduleToTeam(ctx, manager, schedule, now)
		if err != nil {
			updated, _, recordErr := manager.RecordScheduleRun(schedule.ID, session.ScheduleRunOptions{
				Timestamp: now,
				Error:     err.Error(),
			})
			if recordErr == nil {
				schedule = updated
			}
			runError := map[string]any{
				"schedule_id": schedule.ID,
				"error":       err.Error(),
			}
			if recordErr != nil {
				runError["record_error"] = recordErr.Error()
			}
			runErrors = append(runErrors, runError)
			continue
		}
		updated, _, err := manager.RecordScheduleRun(schedule.ID, session.ScheduleRunOptions{
			Timestamp: now,
			SentCount: len(sent),
		})
		if err != nil {
			runErrors = append(runErrors, map[string]any{
				"schedule_id": schedule.ID,
				"error":       err.Error(),
			})
			continue
		}
		triggeredSchedule := structuredScheduleState(updated)
		triggeredSchedule["sent_count"] = len(sent)
		triggeredSchedule["sent"] = sent
		triggeredSchedule["trigger_message_chars"] = triggerMessageChars
		triggered = append(triggered, triggeredSchedule)
	}
	_ = tool.SendProgress(sink, "", "schedule_due_run", map[string]any{
		"checked_at":      now.UTC().Format(time.RFC3339Nano),
		"due_count":       len(selectedDue),
		"triggered_count": len(triggered),
		"error_count":     len(runErrors),
	})
	return contracts.ToolResult{
		Content: fmt.Sprintf("Triggered %d due schedule(s); %d error(s).", len(triggered), len(runErrors)),
		StructuredContent: map[string]any{
			"type":            "schedule_cron",
			"action":          scheduleCronActionRunDue,
			"checked_at":      now.UTC().Format(time.RFC3339Nano),
			"due_count":       len(selectedDue),
			"triggered_count": len(triggered),
			"error_count":     len(runErrors),
			"triggered":       triggered,
			"errors":          runErrors,
		},
	}, nil
}

func callScheduleCron(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeScheduleCronInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	action := resolvedScheduleCronAction(input.Action)
	switch action {
	case scheduleCronActionList:
		manifest, err := manager.ScheduleManifest()
		if err != nil {
			return contracts.ToolResult{}, err
		}
		schedules := structuredScheduleStates(manifest.Schedules)
		_ = tool.SendProgress(sink, "", "schedule_listed", map[string]any{
			"schedule_count": len(schedules),
		})
		return contracts.ToolResult{
			Content: formatScheduleList(manifest.Schedules),
			StructuredContent: map[string]any{
				"type":           "schedule_cron",
				"action":         action,
				"schedules":      schedules,
				"schedule_count": len(schedules),
			},
		}, nil
	case scheduleCronActionDelete:
		deleted, manifest, err := manager.DeleteSchedule(input.ScheduleID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		structured := structuredScheduleState(deleted)
		structured["type"] = "schedule_cron"
		structured["action"] = action
		structured["deleted"] = true
		structured["schedule_count"] = len(manifest.Schedules)
		_ = tool.SendProgress(sink, "", "schedule_deleted", map[string]any{
			"schedule_id":    deleted.ID,
			"schedule_count": len(manifest.Schedules),
		})
		return contracts.ToolResult{
			Content:           fmt.Sprintf("Schedule %s deleted.", deleted.ID),
			StructuredContent: structured,
		}, nil
	case scheduleCronActionTrigger:
		schedule, ok, err := findScheduleState(manager, input.ScheduleID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		if !ok {
			return contracts.ToolResult{}, fmt.Errorf("schedule not found: %s", sanitizeTaskLikeID(input.ScheduleID))
		}
		now, err := parseScheduleCronNow(input.Now)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		sent, triggerMessageChars, err := triggerScheduleToTeam(ctx, manager, schedule, now)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		schedule, _, err = manager.RecordScheduleRun(schedule.ID, session.ScheduleRunOptions{
			Timestamp: now,
			SentCount: len(sent),
		})
		if err != nil {
			return contracts.ToolResult{}, err
		}
		structured := structuredScheduleState(schedule)
		structured["type"] = "schedule_cron"
		structured["action"] = action
		structured["sent_count"] = len(sent)
		structured["sent"] = sent
		structured["trigger_message_chars"] = triggerMessageChars
		_ = tool.SendProgress(sink, "", "schedule_triggered", map[string]any{
			"schedule_id": schedule.ID,
			"team_id":     schedule.TeamID,
			"target":      schedule.Target,
			"sent_count":  len(sent),
		})
		return contracts.ToolResult{
			Content:           fmt.Sprintf("Schedule %s triggered for %d task(s).", schedule.ID, len(sent)),
			StructuredContent: structured,
		}, nil
	case scheduleCronActionRunDue:
		now, err := parseScheduleCronNow(input.Now)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		return RunDueSchedules(ctx, input.ScheduleID, now, sink)
	default:
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		target := resolvedTeamSendTarget(input.Target)
		schedule, manifest, err := manager.UpsertSchedule(session.ScheduleOptions{
			ID:          input.ScheduleID,
			Description: input.Description,
			Cron:        input.Cron,
			Message:     input.Message,
			TeamID:      input.TeamID,
			Target:      target,
			Enabled:     enabled,
		})
		if err != nil {
			return contracts.ToolResult{}, err
		}
		structured := structuredScheduleState(schedule)
		structured["type"] = "schedule_cron"
		structured["action"] = action
		structured["schedule_count"] = len(manifest.Schedules)
		_ = tool.SendProgress(sink, "", "schedule_saved", map[string]any{
			"schedule_id":    schedule.ID,
			"cron":           schedule.Cron,
			"enabled":        schedule.Enabled,
			"schedule_count": len(manifest.Schedules),
		})
		return contracts.ToolResult{
			Content:           fmt.Sprintf("Schedule %s saved for %s.", schedule.ID, schedule.Cron),
			StructuredContent: structured,
		}, nil
	}
}

func triggerScheduleToTeam(ctx tool.Context, manager session.SidechainManager, schedule session.ScheduleState, now time.Time) ([]map[string]any, int, error) {
	if !schedule.Enabled {
		return nil, 0, fmt.Errorf("schedule %s is disabled", schedule.ID)
	}
	if schedule.TeamID == "" {
		return nil, 0, fmt.Errorf("schedule %s has no team", schedule.ID)
	}
	team, err := loadTeamForMessage(manager, schedule.TeamID)
	if err != nil {
		return nil, 0, err
	}
	taskIDs, err := teamSendMessageTaskIDs(team, schedule.Target)
	if err != nil {
		return nil, 0, err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return nil, 0, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	triggerMessage := formatScheduleTriggerMessage(schedule)
	sent := make([]map[string]any, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		message := msgs.UserText(triggerMessage)
		message.SessionID = ctx.SessionID
		if err := manager.Append(taskID, session.TranscriptMessage{
			Type:        string(contracts.MessageUser),
			UUID:        message.UUID,
			SessionID:   ctx.SessionID,
			Timestamp:   now.UTC().Format(time.RFC3339Nano),
			IsSidechain: true,
			AgentID:     taskID,
			Message:     &message,
		}); err != nil {
			return nil, 0, err
		}
		sent = append(sent, map[string]any{
			"task_id":      taskID,
			"sidechain_id": taskID,
			"message_uuid": string(message.UUID),
		})
	}
	return sent, len(triggerMessage), nil
}

func RunRemoteTrigger(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	if err := validateRemoteTrigger(ctx, raw); err != nil {
		return contracts.ToolResult{}, err
	}
	return callRemoteTrigger(ctx, raw, sink)
}

func callRemoteTrigger(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeRemoteTriggerInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	if input.EventID != "" {
		receipt, ok, err := manager.RemoteTriggerReceipt(input.EventID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		if ok {
			receipt, _, err = manager.RecordRemoteTriggerDuplicate(input.EventID, time.Now().UTC())
			if err != nil {
				return contracts.ToolResult{}, err
			}
			structured := structuredRemoteTriggerReceipt(receipt)
			structured["type"] = "remote_trigger"
			structured["duplicate"] = true
			structured["sent_count"] = 0
			_ = tool.SendProgress(sink, "", "remote_trigger_duplicate", map[string]any{
				"event_id":        receipt.EventID,
				"team_id":         receipt.TeamID,
				"duplicate_count": receipt.DuplicateCount,
			})
			return contracts.ToolResult{
				Content:           fmt.Sprintf("Remote trigger %s already processed.", receipt.EventID),
				StructuredContent: structured,
			}, nil
		}
	}
	team, err := loadTeamForMessage(manager, input.TeamID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	target := resolvedRemoteTriggerTarget(team, input.Target)
	taskIDs, err := teamSendMessageTaskIDs(team, target)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if err := validateTaskIDsRunning(manager, taskIDs); err != nil {
		return contracts.ToolResult{}, err
	}
	triggerMessage := formatRemoteTriggerMessage(input)
	sent := make([]map[string]any, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		message := msgs.UserText(triggerMessage)
		message.SessionID = ctx.SessionID
		if err := manager.Append(taskID, session.TranscriptMessage{
			Type:        string(contracts.MessageUser),
			UUID:        message.UUID,
			SessionID:   ctx.SessionID,
			Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
			IsSidechain: true,
			AgentID:     taskID,
			Message:     &message,
		}); err != nil {
			return contracts.ToolResult{}, err
		}
		sent = append(sent, map[string]any{
			"task_id":      taskID,
			"sidechain_id": taskID,
			"message_uuid": string(message.UUID),
		})
	}
	var receipt session.RemoteTriggerReceipt
	if input.EventID != "" {
		var err error
		receipt, _, err = manager.RecordRemoteTriggerReceipt(session.RemoteTriggerReceiptOptions{
			EventID:      input.EventID,
			Source:       input.Source,
			Event:        input.Event,
			TeamID:       team.ID,
			Target:       target,
			SentCount:    len(sent),
			MessageChars: len(input.Message),
		})
		if err != nil {
			return contracts.ToolResult{}, err
		}
	}
	structured := structuredTeamState(team)
	structured["type"] = "remote_trigger"
	structured["target"] = target
	structured["event_id"] = input.EventID
	structured["source"] = input.Source
	structured["event"] = input.Event
	structured["duplicate"] = false
	structured["message_chars"] = len(input.Message)
	structured["trigger_message_chars"] = len(triggerMessage)
	structured["sent_count"] = len(sent)
	structured["sent"] = sent
	if input.EventID != "" {
		structured["receipt"] = structuredRemoteTriggerReceipt(receipt)
	}
	_ = tool.SendProgress(sink, "", "remote_trigger_sent", map[string]any{
		"team_id":       team.ID,
		"target":        target,
		"event_id":      input.EventID,
		"source":        input.Source,
		"event":         input.Event,
		"sent_count":    len(sent),
		"message_chars": len(input.Message),
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Remote trigger sent to %d task(s) in team %s.", len(sent), team.ID),
		StructuredContent: structured,
	}, nil
}

func decodeTaskInput(raw json.RawMessage) (taskInput, error) {
	var input taskInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskInput{}, err
	}
	input.WorktreeSet = rawHasAnyField(raw, "worktree", "isolated_worktree", "isolatedWorktree", "worktree_isolation", "worktreeIsolation", "create_worktree", "createWorktree")
	input.ID = strings.TrimSpace(input.ID)
	input.Description = strings.TrimSpace(input.Description)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.SubagentType = strings.TrimSpace(input.SubagentType)
	return input, nil
}

func rawHasAnyField(raw json.RawMessage, keys ...string) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	for _, key := range keys {
		if _, ok := obj[key]; ok {
			return true
		}
	}
	return false
}

func decodeTaskOutputInput(raw json.RawMessage) (taskOutputInput, error) {
	var input taskOutputInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskOutputInput{}, err
	}
	input.TaskID = strings.TrimSpace(input.TaskID)
	return input, nil
}

func decodeKillTaskInput(raw json.RawMessage) (taskKillInput, error) {
	var input taskKillInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskKillInput{}, err
	}
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.Reason = strings.TrimSpace(input.Reason)
	return input, nil
}

func decodeSendMessageInput(raw json.RawMessage) (taskSendMessageInput, error) {
	var input taskSendMessageInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskSendMessageInput{}, err
	}
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.Message = strings.TrimSpace(input.Message)
	return input, nil
}

func decodeTeamCreateInput(raw json.RawMessage) (teamCreateInput, error) {
	var input teamCreateInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return teamCreateInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	input.Description = strings.TrimSpace(input.Description)
	input.CoordinatorTaskID = strings.TrimSpace(input.CoordinatorTaskID)
	input.TaskIDs = cleanTaskAgentStrings(input.TaskIDs)
	return input, nil
}

func decodeTeamDeleteInput(raw json.RawMessage) (teamDeleteInput, error) {
	var input teamDeleteInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return teamDeleteInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	return input, nil
}

func decodeTeamOutputInput(raw json.RawMessage) (teamOutputInput, error) {
	var input teamOutputInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return teamOutputInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	return input, nil
}

func decodeTeamSendMessageInput(raw json.RawMessage) (teamSendMessageInput, error) {
	var input teamSendMessageInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return teamSendMessageInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	input.Message = strings.TrimSpace(input.Message)
	input.Target = normalizeTeamSendTarget(input.Target)
	return input, nil
}

func decodeTeamDispatchInput(raw json.RawMessage) (teamDispatchInput, error) {
	var input teamDispatchInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return teamDispatchInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	for i := range input.Assignments {
		input.Assignments[i].TaskID = sanitizeTaskLikeID(input.Assignments[i].TaskID)
		input.Assignments[i].Message = strings.TrimSpace(input.Assignments[i].Message)
	}
	return input, nil
}

func decodeTeamScheduleInput(raw json.RawMessage) (teamScheduleInput, error) {
	var input teamScheduleInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return teamScheduleInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	input.Objective = strings.TrimSpace(input.Objective)
	return input, nil
}

func decodeTeamAutoScheduleInput(raw json.RawMessage) (teamAutoScheduleInput, error) {
	var input teamAutoScheduleInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return teamAutoScheduleInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	input.Objective = strings.TrimSpace(input.Objective)
	for i := range input.Assignments {
		input.Assignments[i].TaskID = sanitizeTaskLikeID(input.Assignments[i].TaskID)
		input.Assignments[i].Message = strings.TrimSpace(input.Assignments[i].Message)
	}
	return input, nil
}

func decodeTeamCoordinateInput(raw json.RawMessage) (teamCoordinateInput, error) {
	var input teamCoordinateInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return teamCoordinateInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	input.Message = strings.TrimSpace(input.Message)
	return input, nil
}

func decodeResumeTaskInput(raw json.RawMessage) (taskResumeInput, error) {
	var input taskResumeInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskResumeInput{}, err
	}
	input.TaskID = strings.TrimSpace(input.TaskID)
	return input, nil
}

func decodeSleepInput(raw json.RawMessage) (sleepInput, error) {
	var input sleepInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return sleepInput{}, err
	}
	input.Duration = strings.TrimSpace(input.Duration)
	return input, nil
}

func decodeBriefInput(raw json.RawMessage) (briefInput, error) {
	var input briefInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return briefInput{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Status = strings.TrimSpace(input.Status)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Details = cleanBriefStrings(input.Details)
	input.NextSteps = cleanBriefStrings(input.NextSteps)
	input.Risks = cleanBriefStrings(input.Risks)
	return input, nil
}

func decodeScheduleCronInput(raw json.RawMessage) (scheduleCronInput, error) {
	var input scheduleCronInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scheduleCronInput{}, err
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.ScheduleID = strings.TrimSpace(input.ScheduleID)
	input.Description = strings.TrimSpace(input.Description)
	input.Cron = strings.TrimSpace(input.Cron)
	input.Message = strings.TrimSpace(input.Message)
	input.TeamID = strings.TrimSpace(input.TeamID)
	input.Target = normalizeTeamSendTarget(input.Target)
	return input, nil
}

func decodeRemoteTriggerInput(raw json.RawMessage) (remoteTriggerInput, error) {
	var input remoteTriggerInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return remoteTriggerInput{}, err
	}
	input.TeamID = strings.TrimSpace(input.TeamID)
	input.Target = normalizeTeamSendTarget(input.Target)
	input.EventID = strings.TrimSpace(input.EventID)
	input.Source = strings.TrimSpace(input.Source)
	input.Event = strings.TrimSpace(input.Event)
	input.Message = strings.TrimSpace(input.Message)
	return input, nil
}

func normalizeTaskOutputInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "task_id", "taskId", "sidechain_id", "sidechainId", "id", "tail_lines", "tailLines":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "task_id", "taskId", "sidechain_id", "sidechainId", "id"); ok {
		normalized["task_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "tail_lines", "tailLines"); ok {
		normalized["tail_lines"] = value
	}
	coerceTaskSemanticNumberStrings(normalized, "tail_lines")
	return json.Marshal(normalized)
}

func normalizeKillTaskInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "task_id", "taskId", "sidechain_id", "sidechainId", "id", "reason", "summary", "message":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "task_id", "taskId", "sidechain_id", "sidechainId", "id"); ok {
		normalized["task_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "reason", "summary", "message"); ok {
		normalized["reason"] = value
	}
	return json.Marshal(normalized)
}

func normalizeSendMessageInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "task_id", "taskId", "sidechain_id", "sidechainId", "id", "message", "text", "content", "prompt", "input":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "task_id", "taskId", "sidechain_id", "sidechainId", "id"); ok {
		normalized["task_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "message", "text", "content", "prompt", "input"); ok {
		normalized["message"] = value
	}
	return json.Marshal(normalized)
}

func normalizeTeamCreateInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "id", "name", "description", "desc", "summary", "coordinator_task_id", "coordinatorTaskId", "coordinator", "coordinator_id", "coordinatorId", "task_ids", "taskIds", "tasks", "members", "agents":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "id", "name"); ok {
		normalized["team_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "description", "desc", "summary"); ok {
		normalized["description"] = value
	}
	if value, ok := firstRawTaskField(obj, "coordinator_task_id", "coordinatorTaskId", "coordinator", "coordinator_id", "coordinatorId"); ok {
		normalized["coordinator_task_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "task_ids", "taskIds", "tasks", "members", "agents"); ok {
		normalized["task_ids"] = value
	}
	return json.Marshal(normalized)
}

func normalizeTeamDeleteInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "id", "name":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "id", "name"); ok {
		normalized["team_id"] = value
	}
	return json.Marshal(normalized)
}

func normalizeTeamOutputInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "id", "name":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "id", "name"); ok {
		normalized["team_id"] = value
	}
	return json.Marshal(normalized)
}

func normalizeTeamSendMessageInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "id", "name", "message", "text", "content", "prompt", "input", "target", "recipient", "recipients", "audience", "scope":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "id", "name"); ok {
		normalized["team_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "message", "text", "content", "prompt", "input"); ok {
		normalized["message"] = value
	}
	if value, ok := firstRawTaskField(obj, "target", "recipient", "recipients", "audience", "scope"); ok {
		normalized["target"] = value
	}
	return json.Marshal(normalized)
}

func normalizeTeamDispatchInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "id", "name", "assignments", "tasks", "dispatches", "messages":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "id", "name"); ok {
		normalized["team_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "assignments", "tasks", "dispatches", "messages"); ok {
		normalized["assignments"] = value
	}
	return json.Marshal(normalized)
}

func normalizeTeamScheduleInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "id", "name", "objective", "message", "text", "content", "prompt", "input", "instruction", "request", "goal":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "id", "name"); ok {
		normalized["team_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "objective", "message", "text", "content", "prompt", "input", "instruction", "request", "goal"); ok {
		normalized["objective"] = value
	}
	return json.Marshal(normalized)
}

func normalizeTeamAutoScheduleInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "id", "name", "objective", "message", "text", "content", "prompt", "input", "instruction", "request", "goal", "assignments", "tasks", "dispatches", "plan", "member_assignments", "memberAssignments", "messages":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "id", "name"); ok {
		normalized["team_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "objective", "message", "text", "content", "prompt", "input", "instruction", "request", "goal"); ok {
		normalized["objective"] = value
	}
	if value, ok := firstRawTaskField(obj, "assignments", "tasks", "dispatches", "plan", "member_assignments", "memberAssignments", "messages"); ok {
		normalized["assignments"] = value
	}
	return json.Marshal(normalized)
}

func normalizeTeamCoordinateInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "id", "name", "message", "text", "content", "prompt", "input", "objective", "instruction", "request":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "id", "name"); ok {
		normalized["team_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "message", "text", "content", "prompt", "input", "objective", "instruction", "request"); ok {
		normalized["message"] = value
	}
	return json.Marshal(normalized)
}

func normalizeResumeTaskInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "task_id", "taskId", "sidechain_id", "sidechainId", "id", "limit", "message_limit", "messageLimit":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "task_id", "taskId", "sidechain_id", "sidechainId", "id"); ok {
		normalized["task_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "limit", "message_limit", "messageLimit"); ok {
		normalized["limit"] = value
	}
	coerceTaskSemanticNumberStrings(normalized, "limit")
	return json.Marshal(normalized)
}

func normalizeSleepInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "duration_ms", "durationMs", "milliseconds", "millis", "ms", "seconds", "secs", "sec", "duration", "wait", "for":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "duration_ms", "durationMs", "milliseconds", "millis", "ms"); ok {
		normalized["duration_ms"] = value
	}
	if value, ok := firstRawTaskField(obj, "seconds", "secs", "sec"); ok {
		normalized["seconds"] = value
	}
	if value, ok := firstRawTaskField(obj, "duration", "wait", "for"); ok {
		normalized["duration"] = value
	}
	coerceTaskSemanticNumberStrings(normalized, "duration_ms")
	return json.Marshal(normalized)
}

func normalizeBriefInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "title", "name", "topic", "status", "state", "summary", "message", "content", "body", "brief", "details", "detail", "bullets", "context", "next_steps", "nextSteps", "actions", "action_items", "actionItems", "todos", "risks", "risk", "blockers", "blocker":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "title", "name", "topic"); ok {
		normalized["title"] = value
	}
	if value, ok := firstRawTaskField(obj, "status", "state"); ok {
		normalized["status"] = value
	}
	if value, ok := firstRawTaskField(obj, "summary", "message", "content", "body", "brief"); ok {
		normalized["summary"] = value
	}
	if value, ok := firstRawTaskField(obj, "details", "detail", "bullets", "context"); ok {
		normalized["details"] = value
	}
	if value, ok := firstRawTaskField(obj, "next_steps", "nextSteps", "actions", "action_items", "actionItems", "todos"); ok {
		normalized["next_steps"] = value
	}
	if value, ok := firstRawTaskField(obj, "risks", "risk", "blockers", "blocker"); ok {
		normalized["risks"] = value
	}
	for _, key := range []string{"details", "next_steps", "risks"} {
		if err := normalizeBriefStringList(normalized, key); err != nil {
			return nil, err
		}
	}
	return json.Marshal(normalized)
}

func normalizeScheduleCronInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "action", "op", "operation", "schedule_id", "scheduleId", "id", "name", "description", "desc", "summary", "cron", "expression", "schedule", "message", "text", "content", "prompt", "input", "team_id", "teamId", "team", "target", "recipient", "recipients", "audience", "scope", "enabled", "enable", "now", "at", "time":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "action", "op", "operation"); ok {
		normalized["action"] = value
	}
	if value, ok := firstRawTaskField(obj, "schedule_id", "scheduleId", "id", "name"); ok {
		normalized["schedule_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "description", "desc", "summary"); ok {
		normalized["description"] = value
	}
	if value, ok := firstRawTaskField(obj, "cron", "expression", "schedule"); ok {
		normalized["cron"] = value
	}
	if value, ok := firstRawTaskField(obj, "message", "text", "content", "prompt", "input"); ok {
		normalized["message"] = value
	}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "team"); ok {
		normalized["team_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "target", "recipient", "recipients", "audience", "scope"); ok {
		normalized["target"] = value
	}
	if value, ok := firstRawTaskField(obj, "enabled", "enable"); ok {
		normalized["enabled"] = value
	}
	if value, ok := firstRawTaskField(obj, "now", "at", "time"); ok {
		normalized["now"] = value
	}
	return json.Marshal(normalized)
}

func normalizeRemoteTriggerInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "team_id", "teamId", "team", "target", "recipient", "recipients", "audience", "scope", "event_id", "eventId", "remote_event_id", "remoteEventId", "delivery_id", "deliveryId", "source", "remote", "origin", "event", "event_type", "eventType", "type", "message", "text", "content", "prompt", "input", "payload":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "team_id", "teamId", "team"); ok {
		normalized["team_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "target", "recipient", "recipients", "audience", "scope"); ok {
		normalized["target"] = value
	}
	if value, ok := firstRawTaskField(obj, "event_id", "eventId", "remote_event_id", "remoteEventId", "delivery_id", "deliveryId"); ok {
		normalized["event_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "source", "remote", "origin"); ok {
		normalized["source"] = value
	}
	if value, ok := firstRawTaskField(obj, "event", "event_type", "eventType", "type"); ok {
		normalized["event"] = value
	}
	if value, ok := firstRawTaskField(obj, "message", "text", "content", "prompt", "input", "payload"); ok {
		normalized["message"] = value
	}
	return json.Marshal(normalized)
}

func decodeRawTaskObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}
	return obj, nil
}

func firstRawTaskField(obj map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func coerceTaskSemanticNumberStrings(obj map[string]json.RawMessage, keys ...string) {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok || len(raw) == 0 || raw[0] != '"' {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			continue
		}
		if _, err := strconv.Atoi(text); err == nil {
			obj[key] = json.RawMessage(text)
		}
	}
}

func normalizeBriefStringList(obj map[string]json.RawMessage, key string) error {
	raw, ok := obj[key]
	if !ok || len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		encoded, err := json.Marshal([]string{text})
		if err != nil {
			return err
		}
		obj[key] = encoded
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("%s must be a string or string array", key)
	}
	return nil
}

func sessionPathFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if value, ok := metadata[tool.MetadataSessionPathKey].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func appendTaskUserMessage(manager session.SidechainManager, sessionID contracts.ID, taskID string, text string, now time.Time) (contracts.Message, error) {
	message := msgs.UserText(text)
	message.SessionID = sessionID
	err := manager.Append(taskID, session.TranscriptMessage{
		Type:        string(contracts.MessageUser),
		UUID:        message.UUID,
		SessionID:   sessionID,
		Timestamp:   now.Format(time.RFC3339Nano),
		IsSidechain: true,
		AgentID:     taskID,
		Message:     &message,
	})
	if err != nil {
		return contracts.Message{}, err
	}
	return message, nil
}

func findTaskState(manager session.SidechainManager, taskID string) (session.SidechainState, error) {
	state, err := session.FindSidechainState(manager.Runtime.SessionPath, manager.Runtime.SessionID, taskID)
	if err != nil {
		return session.SidechainState{}, err
	}
	if state.MessageCount == 0 && state.Metadata.Empty() {
		return session.SidechainState{}, fmt.Errorf("task not found: %s", strings.TrimSpace(taskID))
	}
	if state.SessionID == "" {
		state.SessionID = manager.Runtime.SessionID
	}
	return state, nil
}

func structuredTaskState(state session.SidechainState) map[string]any {
	structured := map[string]any{
		"task_id":       state.ID,
		"sidechain_id":  state.ID,
		"status":        state.Status,
		"running":       state.Status == session.SidechainStatusRunning,
		"summary":       state.Summary,
		"started_at":    state.StartedAt,
		"ended_at":      state.EndedAt,
		"message_count": state.MessageCount,
		"path":          state.Path,
	}
	if state.Metadata.AgentType != "" {
		structured["subagent_type"] = state.Metadata.AgentType
	}
	if state.Metadata.Description != "" {
		structured["description"] = state.Metadata.Description
	}
	if state.Metadata.WorktreePath != "" {
		structured["worktree_path"] = state.Metadata.WorktreePath
	}
	if state.Metadata.WorktreeOwned {
		structured["worktree_owned"] = true
	}
	if len(state.Metadata.WorktreeSparsePaths) > 0 {
		structured["worktree_sparse_paths"] = append([]string(nil), state.Metadata.WorktreeSparsePaths...)
	}
	if len(state.Metadata.WorktreeSymlinkDirs) > 0 {
		structured["worktree_symlink_directories"] = append([]string(nil), state.Metadata.WorktreeSymlinkDirs...)
	}
	if state.Metadata.WorktreeCleanupStatus != "" {
		structured["worktree_cleanup_status"] = state.Metadata.WorktreeCleanupStatus
	}
	if state.Metadata.WorktreeCleanupReason != "" {
		structured["worktree_cleanup_reason"] = state.Metadata.WorktreeCleanupReason
	}
	if state.Metadata.WorktreeCleanupAt != "" {
		structured["worktree_cleanup_at"] = state.Metadata.WorktreeCleanupAt
	}
	if state.Metadata.AgentPath != "" {
		structured["agent_path"] = state.Metadata.AgentPath
	}
	if state.Metadata.AgentModel != "" {
		structured["agent_model"] = state.Metadata.AgentModel
	}
	if state.Metadata.AgentPermissionMode != "" {
		structured["agent_permission_mode"] = state.Metadata.AgentPermissionMode
	}
	if len(state.Metadata.AgentAllowedTools) > 0 {
		structured["agent_allowed_tools"] = append([]string(nil), state.Metadata.AgentAllowedTools...)
	}
	return structured
}

func structuredTeamState(team session.TeamState) map[string]any {
	structured := map[string]any{
		"team_id":     team.ID,
		"session_id":  string(team.SessionID),
		"description": team.Description,
		"task_ids":    append([]string(nil), team.TaskIDs...),
		"task_count":  len(team.TaskIDs),
		"created_at":  team.CreatedAt,
		"updated_at":  team.UpdatedAt,
	}
	if team.CoordinatorTaskID != "" {
		structured["coordinator_task_id"] = team.CoordinatorTaskID
	}
	return structured
}

func findTeamState(manifest session.TeamManifest, teamID string) (session.TeamState, bool) {
	id := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "").Replace(strings.TrimSpace(teamID))
	for _, team := range manifest.Teams {
		if team.ID == id {
			return team, true
		}
	}
	return session.TeamState{}, false
}

func loadTeamForMessage(manager session.SidechainManager, teamID string) (session.TeamState, error) {
	manifest, err := manager.TeamManifest()
	if err != nil {
		return session.TeamState{}, err
	}
	team, ok := findTeamState(manifest, teamID)
	if !ok {
		return session.TeamState{}, fmt.Errorf("team not found: %s", teamID)
	}
	return team, nil
}

func runningTeamCoordinatorState(manager session.SidechainManager, team session.TeamState) (session.SidechainState, error) {
	if team.CoordinatorTaskID == "" {
		return session.SidechainState{}, fmt.Errorf("team %s has no coordinator task", team.ID)
	}
	state, err := findTaskState(manager, team.CoordinatorTaskID)
	if err != nil {
		return session.SidechainState{}, err
	}
	if state.Status != session.SidechainStatusRunning {
		return session.SidechainState{}, fmt.Errorf("coordinator task %s is not running", state.ID)
	}
	return state, nil
}

func validateTaskIDsRunning(manager session.SidechainManager, taskIDs []string) error {
	for _, taskID := range taskIDs {
		state, err := findTaskState(manager, taskID)
		if err != nil {
			return err
		}
		if state.Status != session.SidechainStatusRunning {
			return fmt.Errorf("task %s is not running", state.ID)
		}
	}
	return nil
}

func normalizeTeamSendTarget(target string) string {
	return strings.ToLower(strings.TrimSpace(target))
}

func resolvedTeamSendTarget(target string) string {
	target = normalizeTeamSendTarget(target)
	if target == "" {
		return teamSendTargetMembers
	}
	return target
}

func validateTeamSendTarget(target string) error {
	switch resolvedTeamSendTarget(target) {
	case teamSendTargetMembers, teamSendTargetCoordinator, teamSendTargetAll:
		return nil
	default:
		return fmt.Errorf("target must be one of %s, %s, %s", teamSendTargetMembers, teamSendTargetCoordinator, teamSendTargetAll)
	}
}

func teamSendMessageTaskIDs(team session.TeamState, target string) ([]string, error) {
	switch resolvedTeamSendTarget(target) {
	case teamSendTargetMembers:
		if len(team.TaskIDs) == 0 {
			return nil, fmt.Errorf("team %s has no tasks", team.ID)
		}
		return append([]string(nil), team.TaskIDs...), nil
	case teamSendTargetCoordinator:
		if team.CoordinatorTaskID == "" {
			return nil, fmt.Errorf("team %s has no coordinator task", team.ID)
		}
		return []string{team.CoordinatorTaskID}, nil
	case teamSendTargetAll:
		taskIDs := make([]string, 0, len(team.TaskIDs)+1)
		seen := map[string]struct{}{}
		if team.CoordinatorTaskID != "" {
			seen[team.CoordinatorTaskID] = struct{}{}
			taskIDs = append(taskIDs, team.CoordinatorTaskID)
		}
		for _, taskID := range team.TaskIDs {
			if _, ok := seen[taskID]; ok {
				continue
			}
			seen[taskID] = struct{}{}
			taskIDs = append(taskIDs, taskID)
		}
		if len(taskIDs) == 0 {
			return nil, fmt.Errorf("team %s has no tasks", team.ID)
		}
		return taskIDs, nil
	default:
		return nil, fmt.Errorf("target must be one of %s, %s, %s", teamSendTargetMembers, teamSendTargetCoordinator, teamSendTargetAll)
	}
}

func teamDispatchTaskIDs(team session.TeamState, assignments []teamDispatchAssignmentInput) ([]string, error) {
	members := map[string]struct{}{}
	for _, taskID := range team.TaskIDs {
		members[taskID] = struct{}{}
	}
	seen := map[string]struct{}{}
	taskIDs := make([]string, 0, len(assignments))
	for i, assignment := range assignments {
		if assignment.TaskID == "" {
			return nil, fmt.Errorf("assignments[%d].task_id is required", i)
		}
		if assignment.Message == "" {
			return nil, fmt.Errorf("assignments[%d].message is required", i)
		}
		if _, ok := members[assignment.TaskID]; !ok {
			return nil, fmt.Errorf("task %s is not a member of team %s", assignment.TaskID, team.ID)
		}
		if _, ok := seen[assignment.TaskID]; ok {
			return nil, fmt.Errorf("duplicate assignment for task %s", assignment.TaskID)
		}
		seen[assignment.TaskID] = struct{}{}
		taskIDs = append(taskIDs, assignment.TaskID)
	}
	return taskIDs, nil
}

func teamAutoScheduleTaskIDs(team session.TeamState, input teamAutoScheduleInput) ([]string, error) {
	if len(input.Assignments) == 0 {
		return teamScheduleTaskIDs(team)
	}
	if len(input.Assignments) > 32 {
		return nil, fmt.Errorf("assignments must include <= 32 items")
	}
	return teamDispatchTaskIDs(team, input.Assignments)
}

func teamScheduleTaskIDs(team session.TeamState) ([]string, error) {
	if len(team.TaskIDs) == 0 {
		return nil, fmt.Errorf("team %s has no tasks", team.ID)
	}
	taskIDs := make([]string, 0, len(team.TaskIDs))
	seen := map[string]struct{}{}
	for _, taskID := range team.TaskIDs {
		if taskID == "" {
			continue
		}
		if _, ok := seen[taskID]; ok {
			continue
		}
		seen[taskID] = struct{}{}
		taskIDs = append(taskIDs, taskID)
	}
	if len(taskIDs) == 0 {
		return nil, fmt.Errorf("team %s has no tasks", team.ID)
	}
	return taskIDs, nil
}

func resolvedRemoteTriggerTarget(team session.TeamState, target string) string {
	target = normalizeTeamSendTarget(target)
	if target != "" {
		return target
	}
	if team.CoordinatorTaskID != "" {
		return teamSendTargetCoordinator
	}
	return teamSendTargetMembers
}

func resolvedScheduleCronAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return scheduleCronActionCreate
	}
	switch action {
	case "run", "fire", "execute":
		return scheduleCronActionTrigger
	case "due", "tick", "run-due", "rundue":
		return scheduleCronActionRunDue
	}
	return action
}

func validateScheduleCronAction(action string) error {
	switch resolvedScheduleCronAction(action) {
	case scheduleCronActionCreate, scheduleCronActionList, scheduleCronActionDelete, scheduleCronActionTrigger, scheduleCronActionRunDue:
		return nil
	default:
		return fmt.Errorf("action must be one of %s, %s, %s, %s, %s", scheduleCronActionCreate, scheduleCronActionList, scheduleCronActionDelete, scheduleCronActionTrigger, scheduleCronActionRunDue)
	}
}

func parseScheduleCronNow(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Now().UTC(), nil
	}
	now, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("now must be RFC3339")
	}
	return now.UTC(), nil
}

func validScheduleCronSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	switch spec {
	case "@hourly", "@daily", "@weekly", "@monthly", "@yearly", "@annually":
		return true
	}
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return false
	}
	for _, field := range fields {
		if field == "" {
			return false
		}
		for _, r := range field {
			if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				continue
			}
			switch r {
			case '*', '/', ',', '-', '?', '#', 'L', 'W':
				continue
			default:
				return false
			}
		}
	}
	return true
}

func sanitizeTaskLikeID(id string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "").Replace(strings.TrimSpace(id))
}

func findScheduleState(manager session.SidechainManager, scheduleID string) (session.ScheduleState, bool, error) {
	manifest, err := manager.ScheduleManifest()
	if err != nil {
		return session.ScheduleState{}, false, err
	}
	id := sanitizeTaskLikeID(scheduleID)
	for _, schedule := range manifest.Schedules {
		if schedule.ID == id {
			return schedule, true, nil
		}
	}
	return session.ScheduleState{}, false, nil
}

func structuredTeamTaskStates(manager session.SidechainManager, team session.TeamState) []map[string]any {
	out := make([]map[string]any, 0, len(team.TaskIDs))
	for _, taskID := range team.TaskIDs {
		state, err := findTaskState(manager, taskID)
		if err != nil {
			out = append(out, map[string]any{
				"task_id":      taskID,
				"sidechain_id": taskID,
				"status":       "missing",
				"error":        err.Error(),
			})
			continue
		}
		out = append(out, structuredTaskState(state))
	}
	return out
}

func structuredTeamCoordinatorState(manager session.SidechainManager, team session.TeamState) map[string]any {
	if team.CoordinatorTaskID == "" {
		return nil
	}
	state, err := findTaskState(manager, team.CoordinatorTaskID)
	if err != nil {
		return map[string]any{
			"task_id":      team.CoordinatorTaskID,
			"sidechain_id": team.CoordinatorTaskID,
			"status":       "missing",
			"error":        err.Error(),
		}
	}
	return structuredTaskState(state)
}

func formatTeamList(teams []session.TeamState) string {
	if len(teams) == 0 {
		return "No teams found."
	}
	var builder strings.Builder
	builder.WriteString("Teams:\n")
	for _, team := range teams {
		fmt.Fprintf(&builder, "- %s (%d tasks)", team.ID, len(team.TaskIDs))
		if team.CoordinatorTaskID != "" {
			fmt.Fprintf(&builder, " coordinator %s", team.CoordinatorTaskID)
		}
		if team.Description != "" {
			fmt.Fprintf(&builder, ": %s", team.Description)
		}
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

func structuredScheduleState(schedule session.ScheduleState) map[string]any {
	return map[string]any{
		"schedule_id":         schedule.ID,
		"session_id":          string(schedule.SessionID),
		"description":         schedule.Description,
		"cron":                schedule.Cron,
		"message":             schedule.Message,
		"team_id":             schedule.TeamID,
		"target":              schedule.Target,
		"enabled":             schedule.Enabled,
		"created_at":          schedule.CreatedAt,
		"updated_at":          schedule.UpdatedAt,
		"last_run_at":         schedule.LastRunAt,
		"last_run_status":     schedule.LastRunStatus,
		"last_run_error":      schedule.LastRunError,
		"last_run_sent_count": schedule.LastRunSentCount,
		"run_count":           schedule.RunCount,
	}
}

func structuredScheduleStates(schedules []session.ScheduleState) []map[string]any {
	out := make([]map[string]any, 0, len(schedules))
	for _, schedule := range schedules {
		out = append(out, structuredScheduleState(schedule))
	}
	return out
}

func structuredRemoteTriggerReceipt(receipt session.RemoteTriggerReceipt) map[string]any {
	return map[string]any{
		"event_id":          receipt.EventID,
		"session_id":        string(receipt.SessionID),
		"source":            receipt.Source,
		"event":             receipt.Event,
		"team_id":           receipt.TeamID,
		"target":            receipt.Target,
		"received_at":       receipt.ReceivedAt,
		"sent_count":        receipt.SentCount,
		"message_chars":     receipt.MessageChars,
		"duplicate_count":   receipt.DuplicateCount,
		"last_duplicate_at": receipt.LastDuplicateAt,
	}
}

func formatScheduleList(schedules []session.ScheduleState) string {
	if len(schedules) == 0 {
		return "No schedules found."
	}
	var builder strings.Builder
	builder.WriteString("Schedules:")
	for _, schedule := range schedules {
		fmt.Fprintf(&builder, "\n- %s: %s", schedule.ID, schedule.Cron)
		if !schedule.Enabled {
			builder.WriteString(" (disabled)")
		}
		if schedule.Description != "" {
			fmt.Fprintf(&builder, " - %s", schedule.Description)
		}
	}
	return builder.String()
}

func formatTeamOutput(team session.TeamState, coordinator map[string]any, tasks []map[string]any) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Team %s has %d task(s).", team.ID, len(team.TaskIDs))
	if team.Description != "" {
		fmt.Fprintf(&builder, "\nDescription: %s", team.Description)
	}
	if coordinator != nil {
		taskID, _ := coordinator["task_id"].(string)
		status, _ := coordinator["status"].(string)
		if status == "" {
			status = "unknown"
		}
		fmt.Fprintf(&builder, "\nCoordinator: %s: %s", taskID, status)
		if summary, _ := coordinator["summary"].(string); summary != "" {
			fmt.Fprintf(&builder, " - %s", summary)
		}
	}
	if len(tasks) > 0 {
		builder.WriteString("\nTasks:")
		for _, task := range tasks {
			taskID, _ := task["task_id"].(string)
			status, _ := task["status"].(string)
			if status == "" {
				status = "unknown"
			}
			fmt.Fprintf(&builder, "\n- %s: %s", taskID, status)
			if summary, _ := task["summary"].(string); summary != "" {
				fmt.Fprintf(&builder, " - %s", summary)
			}
		}
	}
	return builder.String()
}

func formatTeamCoordinateMessage(team session.TeamState, tasks []map[string]any, objective string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Team coordination request for %s.", team.ID)
	if team.Description != "" {
		fmt.Fprintf(&builder, "\nDescription: %s", team.Description)
	}
	if len(tasks) > 0 {
		builder.WriteString("\nMembers:")
		for _, task := range tasks {
			taskID, _ := task["task_id"].(string)
			status, _ := task["status"].(string)
			if status == "" {
				status = "unknown"
			}
			fmt.Fprintf(&builder, "\n- %s: %s", taskID, status)
			if summary, _ := task["summary"].(string); summary != "" {
				fmt.Fprintf(&builder, " - %s", summary)
			}
		}
	}
	builder.WriteString("\nObjective:\n")
	builder.WriteString(objective)
	return builder.String()
}

func formatTeamAutoCoordinateMessage(team session.TeamState, tasks []map[string]any, objective string, assignments []teamDispatchAssignmentInput) string {
	briefing := formatTeamCoordinateMessage(team, tasks, objective)
	if len(assignments) == 0 {
		return briefing
	}
	var builder strings.Builder
	builder.WriteString(briefing)
	builder.WriteString("\nPlanned assignments:")
	for _, assignment := range assignments {
		fmt.Fprintf(&builder, "\n- %s: %s", assignment.TaskID, assignment.Message)
	}
	return builder.String()
}

func formatTeamScheduleMessage(team session.TeamState, tasks []map[string]any, objective string, taskID string, index int, total int) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Team scheduled assignment for %s.", team.ID)
	if team.Description != "" {
		fmt.Fprintf(&builder, "\nDescription: %s", team.Description)
	}
	fmt.Fprintf(&builder, "\nAssigned member: %s (%d/%d)", taskID, index, total)
	if len(tasks) > 0 {
		builder.WriteString("\nCurrent members:")
		for _, task := range tasks {
			memberID, _ := task["task_id"].(string)
			status, _ := task["status"].(string)
			if status == "" {
				status = "unknown"
			}
			fmt.Fprintf(&builder, "\n- %s: %s", memberID, status)
			if summary, _ := task["summary"].(string); summary != "" {
				fmt.Fprintf(&builder, " - %s", summary)
			}
		}
	}
	builder.WriteString("\nObjective:\n")
	builder.WriteString(objective)
	builder.WriteString("\nInstruction:\nWork on the part of the objective that best matches your role and current context. Use team communication tools when coordination is needed.")
	return builder.String()
}

func formatTeamPlannedAssignmentMessage(team session.TeamState, objective string, assignment teamDispatchAssignmentInput) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Team planned assignment for %s.", team.ID)
	if team.Description != "" {
		fmt.Fprintf(&builder, "\nDescription: %s", team.Description)
	}
	builder.WriteString("\nObjective:\n")
	builder.WriteString(objective)
	builder.WriteString("\nAssignment:\n")
	builder.WriteString(assignment.Message)
	builder.WriteString("\nInstruction:\nExecute this coordinator/model plan for your assigned scope. Use team communication tools when coordination is needed.")
	return builder.String()
}

func formatTeamDispatchMessage(team session.TeamState, assignment teamDispatchAssignmentInput) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Team dispatch assignment for %s.", team.ID)
	if team.Description != "" {
		fmt.Fprintf(&builder, "\nDescription: %s", team.Description)
	}
	builder.WriteString("\nAssignment:\n")
	builder.WriteString(assignment.Message)
	return builder.String()
}

func formatBrief(input briefInput) string {
	var builder strings.Builder
	if input.Title != "" {
		fmt.Fprintf(&builder, "Brief: %s", input.Title)
	} else {
		builder.WriteString("Brief")
	}
	if input.Status != "" {
		fmt.Fprintf(&builder, "\nStatus: %s", input.Status)
	}
	fmt.Fprintf(&builder, "\nSummary: %s", input.Summary)
	writeBriefSection(&builder, "Details", input.Details)
	writeBriefSection(&builder, "Next steps", input.NextSteps)
	writeBriefSection(&builder, "Risks", input.Risks)
	return builder.String()
}

func formatRemoteTriggerMessage(input remoteTriggerInput) string {
	var builder strings.Builder
	builder.WriteString("Remote trigger received.")
	if input.Source != "" {
		fmt.Fprintf(&builder, "\nSource: %s", input.Source)
	}
	if input.Event != "" {
		fmt.Fprintf(&builder, "\nEvent: %s", input.Event)
	}
	if input.EventID != "" {
		fmt.Fprintf(&builder, "\nEvent ID: %s", input.EventID)
	}
	builder.WriteString("\nMessage:\n")
	builder.WriteString(input.Message)
	return builder.String()
}

func formatScheduleTriggerMessage(schedule session.ScheduleState) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Scheduled cron trigger received.\nSchedule: %s\nCron: %s", schedule.ID, schedule.Cron)
	if schedule.Description != "" {
		fmt.Fprintf(&builder, "\nDescription: %s", schedule.Description)
	}
	builder.WriteString("\nMessage:\n")
	builder.WriteString(schedule.Message)
	return builder.String()
}

func writeBriefSection(builder *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(builder, "\n%s:", label)
	for _, value := range values {
		fmt.Fprintf(builder, "\n- %s", value)
	}
}

func taskOutputTailLines(input taskOutputInput) int {
	if input.TailLines == nil {
		return 0
	}
	return *input.TailLines
}

func resumeTaskLimit(input taskResumeInput) int {
	if input.Limit == nil {
		return 0
	}
	return *input.Limit
}

func sleepDuration(input sleepInput) (time.Duration, error) {
	set := 0
	var duration time.Duration
	if input.DurationMS != nil {
		set++
		if *input.DurationMS <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		duration = time.Duration(*input.DurationMS) * time.Millisecond
	}
	if input.Seconds != nil {
		set++
		if *input.Seconds <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		if *input.Seconds > maxSleepDuration.Seconds() {
			return 0, fmt.Errorf("duration must be <= %dms", maxSleepDuration.Milliseconds())
		}
		duration = time.Duration(*input.Seconds * float64(time.Second))
	}
	if input.Duration != "" {
		set++
		parsed, err := time.ParseDuration(input.Duration)
		if err != nil {
			return 0, fmt.Errorf("duration must be a valid Go duration")
		}
		duration = parsed
	}
	if set == 0 {
		return 0, fmt.Errorf("duration is required")
	}
	if set > 1 {
		return 0, fmt.Errorf("provide exactly one of duration_ms, seconds, or duration")
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	if duration > maxSleepDuration {
		return 0, fmt.Errorf("duration must be <= %dms", maxSleepDuration.Milliseconds())
	}
	return duration, nil
}

func cleanBriefStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func briefCharCount(input briefInput) int {
	total := len(input.Title) + len(input.Status) + len(input.Summary)
	for _, values := range [][]string{input.Details, input.NextSteps, input.Risks} {
		for _, value := range values {
			total += len(value)
		}
	}
	return total
}

func taskTranscriptOutput(state session.SidechainState, tailLines int) (string, error) {
	transcript, err := session.LoadTranscript(state.Path)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, id := range transcript.Order {
		entry := transcript.Messages[id]
		if entry == nil || skipTaskOutputEntry(entry) {
			continue
		}
		text := taskEntryText(entry)
		if text == "" {
			continue
		}
		label := strings.TrimSpace(entry.Type)
		if entry.Message != nil && entry.Message.Type != "" {
			label = string(entry.Message.Type)
		}
		if entry.Subtype != "" && label == "" {
			label = entry.Subtype
		}
		if label == "" {
			label = "message"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", label, text))
	}
	output := strings.Join(lines, "\n")
	if tailLines > 0 {
		output = tailTaskText(output, tailLines)
	}
	return output, nil
}

func skipTaskOutputEntry(entry *session.TranscriptMessage) bool {
	switch entry.Subtype {
	case "sidechain_start", "agent_prompt":
		return true
	default:
		return false
	}
}

func taskEntryText(entry *session.TranscriptMessage) string {
	if entry.Message != nil {
		return strings.TrimSpace(msgs.TextContent(*entry.Message))
	}
	return strings.TrimSpace(taskVisibleText(entry.Content))
}

func taskVisibleText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(taskVisibleText(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"summary", "finalSummary", "final_summary", "resultText", "result_text", "finalMessage", "final_message", "outputText", "output_text", "message", "text", "body", "output", "value", "content"} {
			if text := strings.TrimSpace(taskVisibleText(typed[key])); text != "" {
				return text
			}
		}
	case map[string]string:
		for _, key := range []string{"summary", "message", "text", "body", "output", "value", "content"} {
			if text := strings.TrimSpace(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func tailTaskText(text string, lines int) string {
	if lines <= 0 {
		return text
	}
	parts := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(parts) <= lines {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}

func formatTaskList(states []session.SidechainState) string {
	if len(states) == 0 {
		return "No subagent tasks recorded for this session."
	}
	lines := make([]string, 0, len(states)+1)
	lines = append(lines, "Subagent tasks:")
	for _, state := range states {
		description := state.Metadata.Description
		if description == "" {
			description = state.ID
		}
		agentType := state.Metadata.AgentType
		if agentType == "" {
			agentType = "unknown"
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s: %s", state.ID, state.Status, agentType, description))
	}
	return strings.Join(lines, "\n")
}

func formatTaskOutput(state session.SidechainState, output string) string {
	status := fmt.Sprintf("Task %s is %s.", state.ID, state.Status)
	var lines []string
	lines = append(lines, status)
	if state.Metadata.AgentType != "" {
		lines = append(lines, "Subagent type: "+state.Metadata.AgentType)
	}
	if state.Metadata.Description != "" {
		lines = append(lines, "Description: "+state.Metadata.Description)
	}
	if state.Summary != "" {
		lines = append(lines, "Summary: "+state.Summary)
	}
	if strings.TrimSpace(output) == "" {
		lines = append(lines, "No task output recorded yet.")
	} else {
		lines = append(lines, "Output:\n"+output)
	}
	return strings.Join(lines, "\n")
}

func structuredResumeMessages(messages []contracts.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{
			"uuid":    message.UUID,
			"type":    message.Type,
			"subtype": message.Subtype,
			"is_meta": message.IsMeta,
			"text":    strings.TrimSpace(msgs.TextContent(message)),
		}
		out = append(out, item)
	}
	return out
}

func formatTaskResume(resumeContext session.SidechainResumeContext) string {
	state := resumeContext.State
	status := "cannot be resumed"
	if resumeContext.CanResume {
		status = "can be resumed"
	}
	lines := []string{fmt.Sprintf("Task %s %s.", state.ID, status)}
	lines = append(lines, "Status: "+state.Status)
	if state.Metadata.AgentType != "" {
		lines = append(lines, "Subagent type: "+state.Metadata.AgentType)
	}
	if resumeContext.Summary != "" {
		lines = append(lines, "Summary: "+resumeContext.Summary)
	}
	if resumeContext.Truncated {
		lines = append(lines, fmt.Sprintf("Resume context truncated to %d messages.", resumeContext.MessageLimit))
	}
	if len(resumeContext.Messages) == 0 {
		lines = append(lines, "No resume messages available.")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "Resume messages:")
	for _, message := range resumeContext.Messages {
		label := string(message.Type)
		if message.Subtype != "" {
			label += ":" + message.Subtype
		}
		text := strings.TrimSpace(msgs.TextContent(message))
		if text == "" {
			text = "(no text content)"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", label, text))
	}
	return strings.Join(lines, "\n")
}

func firstString(obj map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value, true
		}
	}
	return "", false
}

func firstBool(obj map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := obj[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "on":
				return true, true
			case "false", "0", "no", "off":
				return false, true
			}
		}
	}
	return false, false
}

func availableTaskAgents(metadata map[string]any) []tool.AgentInfo {
	agents := []tool.AgentInfo{{
		Name:        builtInGeneralPurposeAgent,
		Description: "General-purpose subagent for researching, searching, and multi-step tasks.",
	}}
	agents = append(agents, taskAgentsFromMetadata(metadata)...)
	return uniqueSortedAgents(agents)
}

func taskAgentsFromMetadata(metadata map[string]any) []tool.AgentInfo {
	if metadata == nil {
		return nil
	}
	switch raw := metadata[tool.MetadataAvailableAgentsKey].(type) {
	case []tool.AgentInfo:
		return cleanTaskAgents(raw)
	case []map[string]string:
		agents := make([]tool.AgentInfo, 0, len(raw))
		for _, item := range raw {
			agents = append(agents, tool.AgentInfo{
				Name:           item["name"],
				Description:    item["description"],
				Path:           item["path"],
				Prompt:         item["prompt"],
				Model:          item["model"],
				PermissionMode: contracts.PermissionMode(item["permission_mode"]),
				AllowedTools:   splitTaskAgentTools(item["allowed_tools"]),
			})
		}
		return cleanTaskAgents(agents)
	case []map[string]any:
		agents := make([]tool.AgentInfo, 0, len(raw))
		for _, item := range raw {
			agents = append(agents, tool.AgentInfo{
				Name:           firstTaskAgentField(item, "name", "id", "agent"),
				Description:    firstTaskAgentField(item, "description", "desc", "summary", "whenToUse", "when_to_use", "when-to-use"),
				Path:           firstTaskAgentField(item, "path", "file", "source"),
				Prompt:         firstTaskAgentField(item, "prompt", "agentPrompt", "agent_prompt", "instructions", "body"),
				Model:          firstTaskAgentField(item, "model", "agentModel", "agent_model"),
				PermissionMode: contracts.PermissionMode(firstTaskAgentField(item, "permissionMode", "permission_mode", "permission-mode", "agentPermissionMode", "agent_permission_mode")),
				AllowedTools:   firstTaskAgentStringList(item, "allowedTools", "allowed_tools", "allowed-tools", "tools", "agentAllowedTools", "agent_allowed_tools"),
			})
		}
		return cleanTaskAgents(agents)
	default:
		return nil
	}
}

func firstTaskAgentField(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := fields[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cleanTaskAgents(agents []tool.AgentInfo) []tool.AgentInfo {
	out := make([]tool.AgentInfo, 0, len(agents))
	for _, agent := range agents {
		agent.Name = strings.TrimSpace(agent.Name)
		agent.Description = strings.TrimSpace(agent.Description)
		agent.Path = strings.TrimSpace(agent.Path)
		agent.Prompt = strings.TrimSpace(agent.Prompt)
		agent.Model = strings.TrimSpace(agent.Model)
		agent.PermissionMode = contracts.PermissionMode(strings.TrimSpace(string(agent.PermissionMode)))
		agent.AllowedTools = cleanTaskAgentStrings(agent.AllowedTools)
		if agent.Name == "" {
			continue
		}
		out = append(out, agent)
	}
	return out
}

func firstTaskAgentStringList(fields map[string]any, keys ...string) []string {
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		switch value := raw.(type) {
		case []string:
			return cleanTaskAgentStrings(value)
		case []any:
			values := make([]string, 0, len(value))
			for _, item := range value {
				if text, ok := item.(string); ok {
					values = append(values, text)
				}
			}
			return cleanTaskAgentStrings(values)
		case string:
			return splitTaskAgentTools(value)
		}
	}
	return nil
}

func splitTaskAgentTools(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var parts []string
	if strings.Contains(raw, ",") {
		parts = strings.Split(raw, ",")
	} else {
		parts = strings.Fields(raw)
	}
	return cleanTaskAgentStrings(parts)
}

func cleanTaskAgentStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func uniqueSortedAgents(agents []tool.AgentInfo) []tool.AgentInfo {
	agents = cleanTaskAgents(agents)
	seen := map[string]struct{}{}
	out := make([]tool.AgentInfo, 0, len(agents))
	for _, agent := range agents {
		key := agent.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, agent)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == builtInGeneralPurposeAgent {
			return true
		}
		if out[j].Name == builtInGeneralPurposeAgent {
			return false
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func taskAgentNames(agents []tool.AgentInfo) []string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		if agent.Name != "" {
			names = append(names, agent.Name)
		}
	}
	return names
}

func taskAgentAllowed(subagentType string, agents []tool.AgentInfo) bool {
	subagentType = strings.TrimSpace(subagentType)
	for _, agent := range agents {
		if subagentType == agent.Name {
			return true
		}
	}
	return false
}

func taskAgentForType(subagentType string, agents []tool.AgentInfo) (tool.AgentInfo, bool) {
	subagentType = strings.TrimSpace(subagentType)
	for _, agent := range agents {
		if subagentType == agent.Name {
			return agent, true
		}
	}
	return tool.AgentInfo{}, false
}
