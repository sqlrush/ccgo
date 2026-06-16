package session

import (
	"testing"
	"time"
)

func TestScheduleDueAtMatchesCronAndRunMinute(t *testing.T) {
	now := mustParseScheduleTestTime(t, "2026-06-22T09:00:00Z")
	schedule := ScheduleState{
		Cron:    "0 9 * * MON-FRI",
		Enabled: true,
	}
	if !ScheduleDueAt(schedule, now) {
		t.Fatalf("expected weekday schedule to be due")
	}

	schedule.LastRunAt = now.Add(30 * time.Second).Format(time.RFC3339Nano)
	if ScheduleDueAt(schedule, now) {
		t.Fatalf("expected schedule to skip a duplicate run in the same minute")
	}

	schedule.LastRunAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	if !ScheduleDueAt(schedule, now) {
		t.Fatalf("expected schedule to be due after the previous minute")
	}

	schedule.Cron = "@daily"
	if ScheduleDueAt(schedule, now) {
		t.Fatalf("expected daily schedule to be due only at midnight")
	}
}

func mustParseScheduleTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
