package formatter

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/alliebayless/murmur/internal/model"
)

// Options controls Markdown rendering.
type Options struct {
	Now              time.Time
	DateFormat       string
	IncludeDate      bool
	UseCallouts      bool
	TaskDateProperty string
	// AppendTags writes the suggested tags into the note content. Off by
	// default: most users prefer tags to stay out of the inserted line.
	AppendTags bool
	Tags       []string
	Caser      *TermCaser
}

// Defaults returns Options with Murmur's standard formatting behaviour.
func Defaults() Options {
	return Options{
		Now:              time.Now(),
		DateFormat:       "2006-01-02",
		IncludeDate:      true,
		UseCallouts:      true,
		TaskDateProperty: "Added",
	}
}

// Format renders a thought as the Markdown block that will be inserted.
func Format(text string, ctype model.ContentType, opts Options) string {
	if opts.DateFormat == "" {
		opts.DateFormat = "2006-01-02"
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.TaskDateProperty == "" {
		opts.TaskDateProperty = "Added"
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	switch ctype {
	case model.TypeTask, model.TypeQuestion:
		return formatTask(text, ctype, opts)
	case model.TypeIdea:
		return formatIdea(text, opts)
	case model.TypeJournal:
		return bullet(sentence(text, opts), opts)
	case model.TypeBookmark:
		return formatBookmark(text, opts)
	case model.TypeReference:
		return bullet(sentence(text, opts), opts)
	case model.TypeProject:
		return formatParagraph(text, opts)
	default:
		return bullet(sentence(text, opts), opts)
	}
}

// sentence normalises a thought into one clean sentence.
func sentence(text string, opts Options) string {
	s := CollapseWhitespace(StripFiller(text))
	s = opts.Caser.Apply(s)
	return EnsureTerminator(Capitalize(s))
}

func bullet(s string, opts Options) string {
	return "- " + withTags(s, opts)
}

func withTags(s string, opts Options) string {
	if !opts.AppendTags || len(opts.Tags) == 0 {
		return s
	}
	parts := make([]string, 0, len(opts.Tags))
	for _, t := range opts.Tags {
		parts = append(parts, "#"+strings.TrimPrefix(t, "#"))
	}
	return s + " " + strings.Join(parts, " ")
}

func formatTask(text string, ctype model.ContentType, opts Options) string {
	body := CollapseWhitespace(StripFiller(text))
	body = opts.Caser.Apply(body)
	if ctype == model.TypeQuestion {
		// Turn the open question into something actionable while keeping the
		// user's own words intact.
		body = strings.TrimRight(body, "?")
		body = "Research: " + strings.TrimSpace(body) + "?"
	} else {
		body = EnsureTerminatorless(Capitalize(body))
	}
	line := "- [ ] " + withTags(body, opts)
	if opts.IncludeDate {
		line += fmt.Sprintf("\n  - %s: %s", opts.TaskDateProperty, opts.Now.Format(opts.DateFormat))
	}
	return line
}

// EnsureTerminatorless leaves task text without a trailing full stop: checkbox
// items read better as short imperatives.
func EnsureTerminatorless(s string) string {
	return strings.TrimRight(s, ". \t")
}

func formatIdea(text string, opts Options) string {
	body := sentence(text, opts)
	if !opts.UseCallouts {
		return bullet(body, opts)
	}
	var b strings.Builder
	b.WriteString("> [!idea]\n")
	for i, line := range strings.Split(withTags(body, opts), "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("> " + line)
	}
	return b.String()
}

func formatBookmark(text string, opts Options) string {
	link := URLRe.FindString(text)
	rest := strings.TrimSpace(URLRe.ReplaceAllString(text, " "))
	rest = CollapseWhitespace(StripFiller(rest))
	rest = opts.Caser.Apply(rest)
	title := Capitalize(EnsureTerminatorless(rest))
	if title == "" {
		title = linkTitle(link)
	}
	if link == "" {
		return bullet(sentence(text, opts), opts)
	}
	return "- " + withTags(fmt.Sprintf("[%s](%s)", title, link), opts)
}

func linkTitle(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return link
	}
	return strings.TrimPrefix(u.Host, "www.")
}

func formatParagraph(text string, opts Options) string {
	paras := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")
	out := make([]string, 0, len(paras))
	for _, p := range paras {
		p = CollapseWhitespace(p)
		if p == "" {
			continue
		}
		p = opts.Caser.Apply(p)
		out = append(out, EnsureTerminator(Capitalize(p)))
	}
	joined := strings.Join(out, "\n\n")
	if opts.AppendTags && len(opts.Tags) > 0 {
		joined = withTags(joined, opts)
	}
	if opts.IncludeDate {
		joined += fmt.Sprintf("\n\n*Captured %s*", opts.Now.Format(opts.DateFormat))
	}
	return joined
}
