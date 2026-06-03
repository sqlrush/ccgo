package memory

import (
	"fmt"
	"time"
)

const dayDuration = 24 * time.Hour

func MemoryAgeDays(mtime time.Time, now time.Time) int {
	if now.IsZero() {
		now = time.Now()
	}
	if mtime.IsZero() || mtime.After(now) {
		return 0
	}
	return int(now.Sub(mtime) / dayDuration)
}

func MemoryAge(mtime time.Time, now time.Time) string {
	days := MemoryAgeDays(mtime, now)
	switch days {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

func MemoryFreshnessText(mtime time.Time, now time.Time) string {
	days := MemoryAgeDays(mtime, now)
	if days <= 1 {
		return ""
	}
	return fmt.Sprintf("This memory is %d days old. Memories are point-in-time observations, not live state - claims about code behavior or file:line citations may be outdated. Verify against current code before asserting as fact.", days)
}

func MemoryFreshnessNote(mtime time.Time, now time.Time) string {
	text := MemoryFreshnessText(mtime, now)
	if text == "" {
		return ""
	}
	return "<system-reminder>" + text + "</system-reminder>\n"
}
