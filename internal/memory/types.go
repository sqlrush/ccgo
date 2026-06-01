package memory

import "time"

type Type string

const (
	TypeProject Type = "project"
	TypeUser    Type = "user"
	TypeTeam    Type = "team"
	TypeAuto    Type = "auto"
	TypeSession Type = "session"
)

type Header struct {
	Filename    string
	Path        string
	Mtime       time.Time
	Description string
	Type        Type
}

type Document struct {
	Header
	Content string
}

type ScanOptions struct {
	MaxFiles             int
	FrontmatterMaxLines  int
	IncludeMemoryDotFile bool
}

func ParseType(raw string) Type {
	switch Type(raw) {
	case TypeProject, TypeUser, TypeTeam, TypeAuto, TypeSession:
		return Type(raw)
	default:
		return ""
	}
}
