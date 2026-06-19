package conversation

import (
	"context"

	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	telemetrypkg "ccgo/internal/telemetry"
)

func (r Runner) recordTelemetry(event Event) {
	if !r.telemetryEnabled() {
		return
	}
	path := telemetrypkg.SessionPath(r.SessionPath, r.SessionID)
	if path == "" {
		return
	}
	summary := telemetrypkg.PrepareEvent(r.telemetryEvent(event))
	_ = telemetrypkg.Append(path, summary)
	_, _ = telemetrypkg.ExportEvent(context.Background(), r.telemetryExportTarget(), summary)
}

func (r Runner) telemetryEnabled() bool {
	if r.MCP == nil {
		return false
	}
	settings := r.MCP.MergedSettings()
	return settings.Advanced != nil && settings.Advanced.Telemetry != nil && *settings.Advanced.Telemetry
}

func (r Runner) telemetryEvent(event Event) telemetrypkg.Event {
	out := telemetrypkg.Event{
		SessionID: r.SessionID,
		Type:      string(event.Type),
		Model:     event.Model,
	}
	if event.Message != nil {
		out.MessageType = string(event.Message.Type)
		out.MessageUUID = event.Message.UUID
		if out.Model == "" {
			out.Model = event.Message.Model
		}
	}
	if event.ToolUse != nil {
		out.ToolUseID = event.ToolUse.ID
		out.ToolName = event.ToolUse.Name
	}
	if event.ToolResult != nil {
		if out.ToolUseID == "" {
			out.ToolUseID = event.ToolResult.ToolUseID
		}
		out.ToolResultErr = event.ToolResult.IsError
	}
	if event.ToolProgress != nil {
		out.ToolUseID = event.ToolProgress.ToolUseID
		out.ProgressType = event.ToolProgress.Type
		out.ProgressKeys = telemetrypkg.SortedMapKeys(event.ToolProgress.Data)
	}
	if event.Retry != nil {
		out.RetryAttempt = event.Retry.Attempt
		out.RetryMax = event.Retry.MaxAttempts
		out.RetryFailed = event.Retry.FailedModel
		out.RetryNext = event.Retry.NextModel
		out.RetryFallback = event.Retry.Fallback
		if out.Model == "" {
			out.Model = event.Retry.FailedModel
		}
	}
	if event.TokenWarning != nil {
		out.TokenUsage = event.TokenWarning.TokenUsage
		out.TokenState = telemetryWarningState(event.TokenWarning.State)
	}
	if event.Compact != nil {
		out.CompactTrigger = event.Compact.Plan.Metadata.Trigger
		if out.Model == "" && event.Compact.Response != nil {
			out.Model = event.Compact.Response.Model
		}
	}
	if event.DeferredToolsPoolChange != nil {
		out.DeferredToolAddedCount = event.DeferredToolsPoolChange.AddedCount
		out.DeferredToolRemovedCount = event.DeferredToolsPoolChange.RemovedCount
		out.DeferredToolPriorAnnounced = event.DeferredToolsPoolChange.PriorAnnouncedCount
		out.DeferredToolMessagesLength = event.DeferredToolsPoolChange.MessagesLength
		out.DeferredToolAttachmentCount = event.DeferredToolsPoolChange.AttachmentCount
		out.DeferredToolsDeltaCount = event.DeferredToolsPoolChange.DeferredToolsDeltaCount
		out.DeferredToolsDeltaCallSite = event.DeferredToolsPoolChange.CallSite
		out.DeferredToolsDeltaQuerySource = event.DeferredToolsPoolChange.QuerySource
		out.DeferredToolAttachmentTypes = event.DeferredToolsPoolChange.AttachmentTypesSeen
	}
	if event.ToolSearchModeDecision != nil {
		enabled := event.ToolSearchModeDecision.Enabled
		out.ToolSearchEnabled = &enabled
		out.ToolSearchMode = event.ToolSearchModeDecision.Mode
		out.ToolSearchReason = event.ToolSearchModeDecision.Reason
		out.ToolSearchCheckedModel = event.ToolSearchModeDecision.CheckedModel
		out.ToolSearchMCPToolCount = event.ToolSearchModeDecision.MCPToolCount
		out.ToolSearchUserType = event.ToolSearchModeDecision.UserType
		out.ToolSearchDeferredToolTokens = event.ToolSearchModeDecision.DeferredToolTokens
		out.ToolSearchThreshold = event.ToolSearchModeDecision.Threshold
		out.ToolSearchDeferredToolDescriptionChars = event.ToolSearchModeDecision.DeferredToolDescriptionChars
		out.ToolSearchCharThreshold = event.ToolSearchModeDecision.CharThreshold
		if out.Model == "" {
			out.Model = event.ToolSearchModeDecision.CheckedModel
		}
	}
	if event.Error != nil {
		out.Error = event.Error.Error()
	}
	return out
}

func (r Runner) telemetryExportTarget() telemetrypkg.ExportTarget {
	settings := r.mergedSettings()
	return telemetryExportTargetFromSettings(settings)
}

func telemetryExportTargetFromSettings(settings contracts.Settings) telemetrypkg.ExportTarget {
	if settings.TelemetryExport == nil {
		return telemetrypkg.ExportTarget{}
	}
	return telemetrypkg.ExportTarget{
		Path:    settings.TelemetryExport.Path,
		URL:     settings.TelemetryExport.URL,
		Headers: settings.TelemetryExport.Headers,
	}
}

func telemetryWarningState(state compactpkg.WarningState) string {
	switch {
	case state.IsAtBlockingLimit:
		return "blocking"
	case state.IsAboveErrorThreshold:
		return "error"
	case state.IsAboveAutoCompactThreshold:
		return "auto_compact"
	case state.IsAboveWarningThreshold:
		return "warning"
	default:
		return "ok"
	}
}
