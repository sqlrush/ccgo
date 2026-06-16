package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

type ScheduleState struct {
	ID               string       `json:"id"`
	SessionID        contracts.ID `json:"sessionId,omitempty"`
	Description      string       `json:"description,omitempty"`
	Cron             string       `json:"cron,omitempty"`
	Message          string       `json:"message,omitempty"`
	TeamID           string       `json:"teamId,omitempty"`
	Target           string       `json:"target,omitempty"`
	Enabled          bool         `json:"enabled"`
	CreatedAt        string       `json:"createdAt,omitempty"`
	UpdatedAt        string       `json:"updatedAt,omitempty"`
	LastRunAt        string       `json:"lastRunAt,omitempty"`
	LastRunStatus    string       `json:"lastRunStatus,omitempty"`
	LastRunError     string       `json:"lastRunError,omitempty"`
	LastRunSentCount int          `json:"lastRunSentCount,omitempty"`
	RunCount         int          `json:"runCount,omitempty"`
}

type ScheduleManifest struct {
	SessionID contracts.ID    `json:"sessionId,omitempty"`
	Schedules []ScheduleState `json:"schedules,omitempty"`
}

type ScheduleOptions struct {
	ID          string
	Description string
	Cron        string
	Message     string
	TeamID      string
	Target      string
	Enabled     bool
	Timestamp   time.Time
}

type ScheduleRunOptions struct {
	Timestamp time.Time
	SentCount int
	Error     string
}

func ScheduleManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "schedules.json")
}

func LoadScheduleManifest(sessionPath string, sessionID contracts.ID) (ScheduleManifest, error) {
	path := ScheduleManifestPath(sessionPath, sessionID)
	if path == "" {
		return ScheduleManifest{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ScheduleManifest{SessionID: sessionID}, nil
	}
	if err != nil {
		return ScheduleManifest{}, err
	}
	var manifest ScheduleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ScheduleManifest{}, err
	}
	manifest.SessionID = sessionID
	for i := range manifest.Schedules {
		manifest.Schedules[i].SessionID = sessionID
		manifest.Schedules[i].ID = sanitizeSidechainID(manifest.Schedules[i].ID)
		manifest.Schedules[i].Description = strings.TrimSpace(manifest.Schedules[i].Description)
		manifest.Schedules[i].Cron = strings.TrimSpace(manifest.Schedules[i].Cron)
		manifest.Schedules[i].Message = strings.TrimSpace(manifest.Schedules[i].Message)
		manifest.Schedules[i].TeamID = sanitizeSidechainID(manifest.Schedules[i].TeamID)
		manifest.Schedules[i].Target = strings.TrimSpace(manifest.Schedules[i].Target)
		manifest.Schedules[i].LastRunAt = strings.TrimSpace(manifest.Schedules[i].LastRunAt)
		manifest.Schedules[i].LastRunStatus = strings.TrimSpace(manifest.Schedules[i].LastRunStatus)
		manifest.Schedules[i].LastRunError = strings.TrimSpace(manifest.Schedules[i].LastRunError)
	}
	sort.SliceStable(manifest.Schedules, func(i, j int) bool {
		return manifest.Schedules[i].ID < manifest.Schedules[j].ID
	})
	return manifest, nil
}

func SaveScheduleManifest(sessionPath string, manifest ScheduleManifest) error {
	path := ScheduleManifestPath(sessionPath, manifest.SessionID)
	if path == "" {
		return os.ErrInvalid
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return platform.AtomicWriteFile(path, data, 0o644)
}

func UpsertSchedule(sessionPath string, sessionID contracts.ID, options ScheduleOptions) (ScheduleState, ScheduleManifest, error) {
	manifest, err := LoadScheduleManifest(sessionPath, sessionID)
	if err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	id := sanitizeSidechainID(options.ID)
	if id == "" {
		id = string(contracts.NewID())
	}
	now := options.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	schedule := ScheduleState{
		ID:          id,
		SessionID:   sessionID,
		Description: strings.TrimSpace(options.Description),
		Cron:        strings.TrimSpace(options.Cron),
		Message:     strings.TrimSpace(options.Message),
		TeamID:      sanitizeSidechainID(options.TeamID),
		Target:      strings.TrimSpace(options.Target),
		Enabled:     options.Enabled,
		CreatedAt:   now.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   now.UTC().Format(time.RFC3339Nano),
	}
	replaced := false
	for i := range manifest.Schedules {
		if manifest.Schedules[i].ID != id {
			continue
		}
		if manifest.Schedules[i].CreatedAt != "" {
			schedule.CreatedAt = manifest.Schedules[i].CreatedAt
		}
		schedule.LastRunAt = manifest.Schedules[i].LastRunAt
		schedule.LastRunStatus = manifest.Schedules[i].LastRunStatus
		schedule.LastRunError = manifest.Schedules[i].LastRunError
		schedule.LastRunSentCount = manifest.Schedules[i].LastRunSentCount
		schedule.RunCount = manifest.Schedules[i].RunCount
		manifest.Schedules[i] = schedule
		replaced = true
		break
	}
	if !replaced {
		manifest.Schedules = append(manifest.Schedules, schedule)
	}
	sort.SliceStable(manifest.Schedules, func(i, j int) bool {
		return manifest.Schedules[i].ID < manifest.Schedules[j].ID
	})
	if err := SaveScheduleManifest(sessionPath, manifest); err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	return schedule, manifest, nil
}

func DeleteSchedule(sessionPath string, sessionID contracts.ID, scheduleID string) (ScheduleState, ScheduleManifest, error) {
	manifest, err := LoadScheduleManifest(sessionPath, sessionID)
	if err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	id := sanitizeSidechainID(scheduleID)
	if id == "" {
		return ScheduleState{}, ScheduleManifest{}, fmt.Errorf("schedule_id is required")
	}
	next := manifest.Schedules[:0]
	var deleted ScheduleState
	found := false
	for _, schedule := range manifest.Schedules {
		if schedule.ID == id {
			deleted = schedule
			found = true
			continue
		}
		next = append(next, schedule)
	}
	if !found {
		return ScheduleState{}, ScheduleManifest{}, fmt.Errorf("schedule not found: %s", id)
	}
	manifest.Schedules = next
	if err := SaveScheduleManifest(sessionPath, manifest); err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	return deleted, manifest, nil
}

func RecordScheduleRun(sessionPath string, sessionID contracts.ID, scheduleID string, options ScheduleRunOptions) (ScheduleState, ScheduleManifest, error) {
	manifest, err := LoadScheduleManifest(sessionPath, sessionID)
	if err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	id := sanitizeSidechainID(scheduleID)
	if id == "" {
		return ScheduleState{}, ScheduleManifest{}, fmt.Errorf("schedule_id is required")
	}
	now := options.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	runError := strings.TrimSpace(options.Error)
	for i := range manifest.Schedules {
		if manifest.Schedules[i].ID != id {
			continue
		}
		schedule := manifest.Schedules[i]
		schedule.LastRunAt = now.UTC().Format(time.RFC3339Nano)
		schedule.LastRunSentCount = options.SentCount
		schedule.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
		if runError != "" {
			schedule.LastRunStatus = "error"
			schedule.LastRunError = runError
		} else {
			schedule.LastRunStatus = "success"
			schedule.LastRunError = ""
			schedule.RunCount++
		}
		manifest.Schedules[i] = schedule
		if err := SaveScheduleManifest(sessionPath, manifest); err != nil {
			return ScheduleState{}, ScheduleManifest{}, err
		}
		return schedule, manifest, nil
	}
	return ScheduleState{}, ScheduleManifest{}, fmt.Errorf("schedule not found: %s", id)
}

func DueSchedulesAt(manifest ScheduleManifest, now time.Time) []ScheduleState {
	due := make([]ScheduleState, 0, len(manifest.Schedules))
	for _, schedule := range manifest.Schedules {
		if ScheduleDueAt(schedule, now) {
			due = append(due, schedule)
		}
	}
	return due
}

func ScheduleDueAt(schedule ScheduleState, now time.Time) bool {
	if !schedule.Enabled {
		return false
	}
	if !cronSpecMatchesAt(schedule.Cron, now) {
		return false
	}
	if schedule.LastRunAt == "" {
		return true
	}
	lastRunAt, err := time.Parse(time.RFC3339Nano, schedule.LastRunAt)
	if err != nil {
		return true
	}
	return !lastRunAt.UTC().Truncate(time.Minute).Equal(now.UTC().Truncate(time.Minute))
}

func cronSpecMatchesAt(spec string, now time.Time) bool {
	now = now.UTC()
	spec = strings.ToLower(strings.TrimSpace(spec))
	switch spec {
	case "@hourly":
		return now.Minute() == 0
	case "@daily":
		return now.Hour() == 0 && now.Minute() == 0
	case "@weekly":
		return now.Weekday() == time.Sunday && now.Hour() == 0 && now.Minute() == 0
	case "@monthly":
		return now.Day() == 1 && now.Hour() == 0 && now.Minute() == 0
	case "@yearly", "@annually":
		return now.Month() == time.January && now.Day() == 1 && now.Hour() == 0 && now.Minute() == 0
	}
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return false
	}
	minuteMatch, _, minuteOK := cronFieldMatches(fields[0], now.Minute(), 0, 59, nil)
	hourMatch, _, hourOK := cronFieldMatches(fields[1], now.Hour(), 0, 23, nil)
	dayMatch, dayAny, dayOK := cronFieldMatches(fields[2], now.Day(), 1, 31, nil)
	monthMatch, _, monthOK := cronFieldMatches(fields[3], int(now.Month()), 1, 12, cronMonthNames())
	weekday := int(now.Weekday())
	weekdayMatch, weekdayAny, weekdayOK := cronFieldMatches(fields[4], weekday, 0, 7, cronWeekdayNames())
	if weekday == 0 && !weekdayMatch {
		weekdayMatch, weekdayAny, weekdayOK = cronFieldMatches(fields[4], 7, 0, 7, cronWeekdayNames())
	}
	if !minuteOK || !hourOK || !dayOK || !monthOK || !weekdayOK {
		return false
	}
	if !minuteMatch || !hourMatch || !monthMatch {
		return false
	}
	switch {
	case dayAny && weekdayAny:
		return true
	case dayAny:
		return weekdayMatch
	case weekdayAny:
		return dayMatch
	default:
		return dayMatch || weekdayMatch
	}
}

func cronFieldMatches(field string, value, min, max int, names map[string]int) (bool, bool, bool) {
	field = strings.ToUpper(strings.TrimSpace(field))
	if field == "" {
		return false, false, false
	}
	matched := false
	any := false
	for _, rawPart := range strings.Split(field, ",") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			return false, false, false
		}
		partMatch, partAny, ok := cronPartMatches(part, value, min, max, names)
		if !ok {
			return false, false, false
		}
		matched = matched || partMatch
		any = any || partAny
	}
	return matched, any, true
}

func cronPartMatches(part string, value, min, max int, names map[string]int) (bool, bool, bool) {
	base := part
	step := 1
	if before, after, ok := strings.Cut(part, "/"); ok {
		base = strings.TrimSpace(before)
		parsedStep, err := strconv.Atoi(strings.TrimSpace(after))
		if err != nil || parsedStep <= 0 {
			return false, false, false
		}
		step = parsedStep
	}
	start, end, any, ok := cronPartRange(base, min, max, names, step != 1)
	if !ok {
		return false, false, false
	}
	if value < start || value > end {
		return false, any, true
	}
	return (value-start)%step == 0, any, true
}

func cronPartRange(base string, min, max int, names map[string]int, stepped bool) (int, int, bool, bool) {
	base = strings.TrimSpace(base)
	if base == "*" || base == "?" {
		return min, max, true, true
	}
	if before, after, ok := strings.Cut(base, "-"); ok {
		start, ok := parseCronFieldValue(strings.TrimSpace(before), names)
		if !ok {
			return 0, 0, false, false
		}
		end, ok := parseCronFieldValue(strings.TrimSpace(after), names)
		if !ok {
			return 0, 0, false, false
		}
		if start < min || end > max || start > end {
			return 0, 0, false, false
		}
		return start, end, false, true
	}
	value, ok := parseCronFieldValue(base, names)
	if !ok || value < min || value > max {
		return 0, 0, false, false
	}
	if stepped {
		return value, max, false, true
	}
	return value, value, false, true
}

func parseCronFieldValue(value string, names map[string]int) (int, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return 0, false
	}
	if names != nil {
		if named, ok := names[value]; ok {
			return named, true
		}
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func cronMonthNames() map[string]int {
	return map[string]int{
		"JAN": 1,
		"FEB": 2,
		"MAR": 3,
		"APR": 4,
		"MAY": 5,
		"JUN": 6,
		"JUL": 7,
		"AUG": 8,
		"SEP": 9,
		"OCT": 10,
		"NOV": 11,
		"DEC": 12,
	}
}

func cronWeekdayNames() map[string]int {
	return map[string]int{
		"SUN": 0,
		"MON": 1,
		"TUE": 2,
		"WED": 3,
		"THU": 4,
		"FRI": 5,
		"SAT": 6,
	}
}
