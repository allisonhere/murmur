// Package model contains the core domain types shared across Murmur.
//
// Nothing in this package may import other Murmur packages: it is the leaf of
// the dependency graph so that every other package can depend on it freely.
package model

import (
	"strings"
	"time"
)

// ContentType describes the shape a captured thought will take in Markdown.
type ContentType string

// The supported content types.
const (
	TypeTask      ContentType = "task"
	TypeIdea      ContentType = "idea"
	TypeJournal   ContentType = "journal"
	TypeProject   ContentType = "project"
	TypeReference ContentType = "reference"
	TypeQuestion  ContentType = "question"
	TypeBookmark  ContentType = "bookmark"
	TypeNote      ContentType = "note"
)

// AllContentTypes lists every supported content type in display order.
func AllContentTypes() []ContentType {
	return []ContentType{
		TypeTask, TypeIdea, TypeJournal, TypeProject,
		TypeReference, TypeQuestion, TypeBookmark, TypeNote,
	}
}

// Label returns a human readable name for the content type.
func (c ContentType) Label() string {
	switch c {
	case TypeProject:
		return "Project note"
	case "":
		return "Note"
	default:
		return strings.ToUpper(string(c)[:1]) + string(c)[1:]
	}
}

// Valid reports whether the content type is one Murmur knows how to format.
func (c ContentType) Valid() bool {
	for _, t := range AllContentTypes() {
		if t == c {
			return true
		}
	}
	return false
}

// ParseContentType converts a free-form string into a ContentType. It accepts a
// few common synonyms so that hint parsing and AI providers can be forgiving.
func ParseContentType(s string) (ContentType, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "task", "todo", "tasks":
		return TypeTask, true
	case "idea", "ideas":
		return TypeIdea, true
	case "journal", "diary", "daily", "log":
		return TypeJournal, true
	case "project", "project note", "project-note", "projectnote":
		return TypeProject, true
	case "reference", "ref", "resource":
		return TypeReference, true
	case "question", "q", "research":
		return TypeQuestion, true
	case "bookmark", "link", "url":
		return TypeBookmark, true
	case "note", "plain", "plain note":
		return TypeNote, true
	}
	return "", false
}

// Heading is a Markdown heading discovered inside a note.
type Heading struct {
	Level int    // 1-6, matching the number of leading '#' characters
	Text  string // heading text with the '#' markers and surrounding space removed
	Line  int    // zero-based line index within the file
}

// Note is an indexed vault file. It deliberately stores only a bounded excerpt
// of the note body rather than the whole file.
type Note struct {
	ID       int64
	RelPath  string // vault-relative path, always slash separated, e.g. "Projects/Tidemail.md"
	FileName string // base name without the .md extension
	Title    string // frontmatter title, first H1, or FileName
	Aliases  []string
	Tags     []string
	Headings []Heading
	Links    []string // wikilink targets found in the note
	Excerpt  string   // bounded plain-text excerpt used for keyword matching
	ModTime  time.Time
	Size     int64
	Indexed  time.Time
}

// HeadingTexts returns the note's heading texts in document order.
func (n Note) HeadingTexts() []string {
	out := make([]string, 0, len(n.Headings))
	for _, h := range n.Headings {
		out = append(out, h.Text)
	}
	return out
}

// Candidate is a scored destination note produced by the routing engine.
type Candidate struct {
	Note    Note
	Score   float64
	Reasons []string
}

// Reason renders the candidate's match explanation as a single line.
func (c Candidate) Reason() string {
	if len(c.Reasons) == 0 {
		return "General match"
	}
	return strings.Join(c.Reasons, ", ")
}

// InsertMode describes where inside the destination note content is written.
type InsertMode string

// The supported insertion modes.
const (
	// InsertUnderHeading places content inside an existing section.
	InsertUnderHeading InsertMode = "under_heading"
	// InsertCreateHeading appends a new heading and places content beneath it.
	InsertCreateHeading InsertMode = "create_heading"
	// InsertAppendEnd appends content to the end of the file.
	InsertAppendEnd InsertMode = "append_end"
)

// RoutingSource records which stage of the engine decided the destination.
type RoutingSource string

// The routing stages, in precedence order.
const (
	SourceExplicit RoutingSource = "explicit"
	SourceRule     RoutingSource = "rule"
	SourceRanking  RoutingSource = "ranking"
	SourceAI       RoutingSource = "ai"
	SourceFallback RoutingSource = "fallback"
)

// Hints are explicit routing instructions extracted from the raw thought.
type Hints struct {
	Path    string      // ">Projects/Foo" destination path
	Project string      // "@tidemail" project or note hint
	Type    ContentType // "#task" style type hint
	Tags    []string    // remaining "#tags" that are not type hints
}

// Any reports whether the user supplied any explicit hint at all.
func (h Hints) Any() bool {
	return h.Path != "" || h.Project != "" || h.Type != "" || len(h.Tags) > 0
}

// Routing is the complete destination decision for a capture.
type Routing struct {
	NotePath    string // vault-relative destination path
	Section     string // destination heading text; empty means "end of file"
	Mode        InsertMode
	Type        ContentType
	Tags        []string
	Confidence  float64 // normalised 0..1
	Source      RoutingSource
	Candidates  []Candidate // alternatives, best first, excluding the selection
	Explanation string
}

// Draft is a fully prepared capture awaiting user confirmation.
type Draft struct {
	Raw      string // exactly what the user typed
	Cleaned  string // routing hints removed
	Hints    Hints
	Routing  Routing
	Markdown string // the Markdown block that will be inserted
	// FileHashBefore is the hash of the destination file at preview time. It is
	// empty when the destination does not exist yet.
	FileHashBefore string
	CreatedAt      time.Time
}

// CaptureRecord is a historical capture as stored in the database.
type CaptureRecord struct {
	ID          int64
	CreatedAt   time.Time
	Raw         string
	Markdown    string
	NotePath    string
	Section     string
	Type        ContentType
	Tags        []string
	Confidence  float64
	Source      RoutingSource
	Corrected   bool
	Undone      bool
	Transaction int64
}

// WriteTransaction is the undo record written after every successful save.
type WriteTransaction struct {
	ID         int64
	CaptureID  int64
	CreatedAt  time.Time
	Path       string // vault-relative path
	HashBefore string
	HashAfter  string
	Inserted   string
	Section    string
	Mode       InsertMode
	Created    bool   // the destination file did not exist before the write
	Backup     string // full prior file content, empty when Created is true
	Undone     bool
	UndoneAt   time.Time
}
