package memory

import (
	"bufio"
	"strings"
)

func ParseFrontmatter(content string) (map[string]string, string) {
	out := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return out, content
	}

	var bodyStart int
	offset := len(scanner.Text()) + 1
	for scanner.Scan() {
		line := scanner.Text()
		nextOffset := offset + len(line) + 1
		if strings.TrimSpace(line) == "---" {
			bodyStart = nextOffset
			break
		}
		if key, value, ok := strings.Cut(line, ":"); ok {
			key = strings.TrimSpace(key)
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			if key != "" {
				out[key] = value
			}
		}
		offset = nextOffset
	}
	if bodyStart <= 0 || bodyStart > len(content) {
		return out, ""
	}
	return out, content[bodyStart:]
}

func firstLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) >= maxLines {
			break
		}
	}
	return strings.Join(lines, "\n")
}
