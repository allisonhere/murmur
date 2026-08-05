package router

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alliebayless/murmur/internal/model"
)

// stopwords are ignored when tokenising a thought: they carry no routing signal
// and would otherwise match every note in the vault.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "that": true, "this": true, "with": true,
	"from": true, "into": true, "about": true, "should": true, "would": true,
	"could": true, "have": true, "has": true, "had": true, "was": true, "were": true,
	"are": true, "is": true, "be": true, "been": true, "it": true, "its": true,
	"you": true, "your": true, "our": true, "their": true, "they": true, "them": true,
	"but": true, "not": true, "why": true, "how": true, "what": true, "when": true,
	"where": true, "who": true, "which": true, "does": true, "did": true, "do": true,
	"to": true, "of": true, "in": true, "on": true, "at": true, "an": true, "a": true,
	"as": true, "if": true, "or": true, "so": true, "up": true, "out": true,
	"remember": true, "need": true, "want": true, "maybe": true, "today": true,
	"just": true, "really": true, "still": true, "some": true, "more": true,
	"only": true, "also": true, "then": true, "than": true, "there": true, "here": true,
	"can": true, "will": true, "get": true, "got": true, "make": true, "made": true,
	"i": true, "me": true, "my": true, "we": true, "us": true,
}

var tokenSplit = regexp.MustCompile(`[^\p{L}\p{N}_]+`)

// Tokenize lower-cases a string and returns its meaningful words.
func Tokenize(s string) []string {
	raw := tokenSplit.Split(strings.ToLower(s), -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if len(t) < 2 || stopwords[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// tokenSet returns a lookup set, keeping duplicates out.
func tokenSet(items ...string) map[string]bool {
	set := map[string]bool{}
	for _, item := range items {
		for _, t := range tokenSplit.Split(strings.ToLower(item), -1) {
			if len(t) >= 2 {
				set[t] = true
			}
		}
	}
	return set
}

// RankInput carries everything the ranker needs.
type RankInput struct {
	Text    string
	Tokens  []string
	Notes   []model.Note
	Learned map[string]LearnedWeight
	Now     time.Time
	// Project is the "@name" hint, weighted heavily when present.
	Project string
}

// LearnedWeight is the accumulated signal for a destination.
type LearnedWeight struct {
	Weight  float64
	Section string
	Type    model.ContentType
}

// Weights are the ranking coefficients. They are exported so the behaviour is
// documented and testable rather than hidden in magic numbers.
var Weights = struct {
	ExactName    float64
	NameToken    float64
	AliasToken   float64
	TagToken     float64
	HeadingToken float64
	ExcerptToken float64
	PathToken    float64
	Learned      float64
	Recency      float64
	Wikilink     float64
	ProjectHint  float64
}{
	ExactName:    6.0,
	NameToken:    3.0,
	AliasToken:   2.5,
	TagToken:     2.5,
	HeadingToken: 1.5,
	ExcerptToken: 0.7,
	PathToken:    1.2,
	Learned:      1.0,
	Recency:      0.6,
	Wikilink:     0.4,
	ProjectHint:  8.0,
}

// Rank scores every indexed note against the thought and returns the best
// candidates, highest score first.
func Rank(in RankInput, limit int) []model.Candidate {
	if limit <= 0 {
		limit = 3
	}
	lowerText := strings.ToLower(in.Text)
	if in.Now.IsZero() {
		in.Now = time.Now()
	}

	cands := make([]model.Candidate, 0, len(in.Notes))
	for _, n := range in.Notes {
		c := scoreNote(n, lowerText, in)
		if c.Score > 0 {
			cands = append(cands, c)
		}
	}

	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].Note.RelPath < cands[j].Note.RelPath
	})

	// Notes linked from the strongest match get a small boost: if the thought
	// clearly belongs to project X, a note X links to is a plausible home too.
	if len(cands) > 1 {
		applyLinkBoost(cands)
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].Score != cands[j].Score {
				return cands[i].Score > cands[j].Score
			}
			return cands[i].Note.RelPath < cands[j].Note.RelPath
		})
	}

	if len(cands) > limit {
		cands = cands[:limit]
	}
	return cands
}

func scoreNote(n model.Note, lowerText string, in RankInput) model.Candidate {
	c := model.Candidate{Note: n}

	nameSet := tokenSet(n.FileName, n.Title)
	aliasSet := tokenSet(n.Aliases...)
	tagSet := map[string]bool{}
	for _, t := range n.Tags {
		tagSet[strings.ToLower(t)] = true
		for _, part := range strings.Split(strings.ToLower(t), "/") {
			tagSet[part] = true
		}
	}
	pathSet := tokenSet(strings.Split(path.Dir(n.RelPath), "/")...)
	excerpt := strings.ToLower(n.Excerpt)

	// Whole-name match: the note's name appears verbatim in the thought.
	for _, name := range append([]string{n.FileName, n.Title}, n.Aliases...) {
		lname := strings.ToLower(strings.TrimSpace(name))
		if len(lname) >= 3 && strings.Contains(lowerText, lname) {
			c.Score += Weights.ExactName
			c.Reasons = append(c.Reasons, fmt.Sprintf("matched note name %q", name))
			break
		}
	}

	if in.Project != "" {
		p := strings.ToLower(in.Project)
		if nameSet[p] || aliasSet[p] || strings.Contains(strings.ToLower(n.FileName), p) {
			c.Score += Weights.ProjectHint
			c.Reasons = append(c.Reasons, "matched project hint")
		}
	}

	var nameHits, aliasHits, excerptHits, pathHits int
	var tagHits []string
	for _, tok := range in.Tokens {
		switch {
		case nameSet[tok]:
			nameHits++
		case aliasSet[tok]:
			aliasHits++
		}
		if tagSet[tok] {
			tagHits = append(tagHits, tok)
		}
		if pathSet[tok] {
			pathHits++
		}
		if excerpt != "" && strings.Contains(excerpt, tok) {
			excerptHits++
		}
	}

	c.Score += Weights.NameToken * float64(minInt(nameHits, 3))
	c.Score += Weights.AliasToken * float64(minInt(aliasHits, 2))
	c.Score += Weights.TagToken * float64(minInt(len(tagHits), 3))
	c.Score += Weights.PathToken * float64(minInt(pathHits, 2))
	c.Score += Weights.ExcerptToken * float64(minInt(excerptHits, 4))

	if nameHits > 0 && len(c.Reasons) == 0 {
		c.Reasons = append(c.Reasons, "matched note name")
	}
	if len(tagHits) > 0 {
		c.Reasons = append(c.Reasons, "tags: "+strings.Join(tagHits, ", "))
	}
	if pathHits > 0 {
		c.Reasons = append(c.Reasons, "matched folder")
	}

	// Headings give a bonus and, more importantly, tell the caller which
	// section to suggest later.
	headingHits := 0
	for _, h := range n.Headings {
		hset := tokenSet(h.Text)
		for _, tok := range in.Tokens {
			if hset[tok] {
				headingHits++
				break
			}
		}
	}
	if headingHits > 0 {
		c.Score += Weights.HeadingToken * float64(minInt(headingHits, 2))
		c.Reasons = append(c.Reasons, "matched a heading")
	}

	if lw, ok := in.Learned[n.RelPath]; ok && lw.Weight > 0 {
		c.Score += Weights.Learned * math.Min(lw.Weight, 4)
		c.Reasons = append(c.Reasons, "you have routed similar thoughts here")
	}

	if excerptHits > 0 && len(c.Reasons) == 0 {
		c.Reasons = append(c.Reasons, "keyword overlap")
	}

	// Recency is a tie-breaker, never a driver.
	if c.Score > 0 {
		days := in.Now.Sub(n.ModTime).Hours() / 24
		if days < 0 {
			days = 0
		}
		c.Score += Weights.Recency * math.Exp(-days/30.0)
	}
	return c
}

func applyLinkBoost(cands []model.Candidate) {
	top := cands[0].Note
	linked := map[string]bool{}
	for _, l := range top.Links {
		linked[strings.ToLower(l)] = true
	}
	if len(linked) == 0 {
		return
	}
	for i := 1; i < len(cands); i++ {
		n := cands[i].Note
		if linked[strings.ToLower(n.FileName)] || linked[strings.ToLower(n.Title)] {
			cands[i].Score += Weights.Wikilink
			cands[i].Reasons = append(cands[i].Reasons, "linked from "+top.FileName)
		}
	}
}

// Confidence normalises the top score into 0..1, discounting results where the
// runner-up is nearly as good.
func Confidence(cands []model.Candidate) float64 {
	if len(cands) == 0 {
		return 0
	}
	top := cands[0].Score
	if top <= 0 {
		return 0
	}
	base := top / (top + 5.0) // saturating: big scores approach 1 slowly
	if len(cands) > 1 && cands[1].Score > 0 {
		margin := (top - cands[1].Score) / top
		base *= 0.75 + 0.25*margin
	}
	return clamp(base, 0.05, 0.99)
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SuggestHeading picks the heading inside a note whose text best overlaps the
// thought. It returns an empty string when nothing matches well.
func SuggestHeading(n model.Note, tokens []string) string {
	best := ""
	bestScore := 0
	for _, h := range n.Headings {
		if h.Level == 1 {
			continue // an H1 is usually the note title, not a section
		}
		hset := tokenSet(h.Text)
		score := 0
		for _, tok := range tokens {
			if hset[tok] {
				score++
			}
		}
		if score > bestScore {
			bestScore, best = score, h.Text
		}
	}
	return best
}

// SuggestTags proposes tags from explicit hints, vault vocabulary mentioned in
// the thought, and the destination note's own tags.
func SuggestTags(text string, hintTags []string, dest model.Note, vocab map[string]string, limit int) []string {
	if limit <= 0 {
		limit = 5
	}
	out := make([]string, 0, limit)
	seen := map[string]bool{}
	add := func(t string) {
		t = strings.TrimPrefix(strings.TrimSpace(t), "#")
		key := strings.ToLower(t)
		if t == "" || seen[key] || len(out) >= limit {
			return
		}
		seen[key] = true
		out = append(out, t)
	}

	for _, t := range hintTags {
		add(t)
	}
	for _, tok := range Tokenize(text) {
		if canonical, ok := vocab[tok]; ok {
			add(canonical)
		}
	}
	for _, t := range dest.Tags {
		add(t)
	}
	return out
}
