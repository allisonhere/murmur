package router

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alliebayless/murmur/internal/model"
)

// SearchResult is one fuzzy-search hit with an explanation of why it matched.
type SearchResult struct {
	Note   model.Note
	Score  int
	Reason string
}

// FuzzySearch ranks notes against a free-text query. It searches the path, file
// name, title, aliases, tags and headings, and reports which of those matched.
func FuzzySearch(notes []model.Note, query string, limit int) []SearchResult {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 50
	}
	if query == "" {
		out := make([]SearchResult, 0, limit)
		// With no query, show the most recently modified notes first.
		sorted := append([]model.Note(nil), notes...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].ModTime.After(sorted[j].ModTime)
		})
		for i, n := range sorted {
			if i >= limit {
				break
			}
			out = append(out, SearchResult{Note: n, Score: 1, Reason: describe(n)})
		}
		return out
	}

	lower := strings.ToLower(query)
	var results []SearchResult
	for _, n := range notes {
		best := 0
		reason := ""
		consider := func(field, label string, weight int) {
			if field == "" {
				return
			}
			if s, ok := fuzzyScore(strings.ToLower(field), lower); ok && s*weight/10 > best {
				best = s * weight / 10
				reason = label
			}
		}
		consider(n.FileName, "file name", 14)
		consider(n.Title, "title", 13)
		consider(n.RelPath, "path", 10)
		for _, a := range n.Aliases {
			consider(a, "alias "+a, 12)
		}
		for _, t := range n.Tags {
			consider(t, "tag #"+t, 11)
		}
		for _, h := range n.Headings {
			consider(h.Text, "heading "+h.Text, 8)
		}
		if best > 0 {
			results = append(results, SearchResult{Note: n, Score: best, Reason: combineReason(n, reason)})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Note.RelPath < results[j].Note.RelPath
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func combineReason(n model.Note, reason string) string {
	if len(n.Tags) > 0 {
		return "Matched " + reason + ", tags: " + strings.Join(n.Tags[:minInt(len(n.Tags), 3)], ", ")
	}
	return "Matched " + reason
}

func describe(n model.Note) string {
	if len(n.Tags) > 0 {
		return "tags: " + strings.Join(n.Tags[:minInt(len(n.Tags), 3)], ", ")
	}
	if len(n.Headings) > 0 {
		return fmt.Sprintf("%d headings", len(n.Headings))
	}
	return "recently modified"
}

// fuzzyScore implements subsequence matching with bonuses for consecutive runs
// and matches at word boundaries, which is what makes "prtd" find
// "Projects/Tidemail.md" while still ranking exact prefixes highest.
func fuzzyScore(target, query string) (int, bool) {
	if query == "" {
		return 1, true
	}
	if strings.Contains(target, query) {
		// A literal substring is always the strongest signal.
		score := 100 - strings.Index(target, query)
		if strings.HasPrefix(target, query) {
			score += 40
		}
		return score, true
	}

	ti, score, streak := 0, 0, 0
	for _, qr := range query {
		found := false
		for ti < len(target) {
			tr := rune(target[ti])
			ti++
			if tr == qr {
				found = true
				score += 3 + streak
				streak += 2
				if ti >= 2 {
					prev := target[ti-2]
					if prev == ' ' || prev == '/' || prev == '-' || prev == '_' {
						score += 5
					}
				} else {
					score += 5 // matched the very first character
				}
				break
			}
			streak = 0
		}
		if !found {
			return 0, false
		}
	}
	return score, true
}
