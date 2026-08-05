// Package indexer walks the vault and keeps the SQLite index in sync with the
// Markdown files on disk. Staleness is detected by comparing modification time
// and size, so a full re-parse only happens for files that actually changed.
package indexer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/database"
	"github.com/alliebayless/murmur/internal/markdown"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/storage"
)

// ExcerptLimit bounds how much note text is copied into the index. Murmur is a
// router, not a search engine: a short excerpt is enough for keyword overlap
// and keeps the database small and the vault contents mostly on disk.
const ExcerptLimit = 600

// Stats summarises an indexing run.
type Stats struct {
	Scanned   int
	Indexed   int
	Skipped   int
	Removed   int
	Warnings  []string
	Duration  time.Duration
	TotalNote int
}

// Indexer syncs the vault into the database.
type Indexer struct {
	vault *storage.Vault
	repo  *database.Repo
	cfg   config.Config
}

// New creates an Indexer.
func New(v *storage.Vault, repo *database.Repo, cfg config.Config) *Indexer {
	return &Indexer{vault: v, repo: repo, cfg: cfg}
}

// Run scans the vault. When rebuild is true the existing index is discarded
// first and every note is re-parsed.
func (ix *Indexer) Run(rebuild bool) (Stats, error) {
	start := time.Now()
	var st Stats

	if rebuild {
		if err := ix.repo.ClearIndex(); err != nil {
			return st, fmt.Errorf("clear index: %w", err)
		}
	}
	stamps, err := ix.repo.Stamps()
	if err != nil {
		return st, err
	}

	seen := make(map[string]bool, len(stamps))
	root := ix.vault.Root()

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory should not abort the whole scan.
			st.Warnings = append(st.Warnings, fmt.Sprintf("skipped %s: %v", path, err))
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if ix.excluded(rel, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		if ix.excluded(rel, d.Name()) {
			return nil
		}

		st.Scanned++
		seen[rel] = true

		info, err := d.Info()
		if err != nil {
			st.Warnings = append(st.Warnings, fmt.Sprintf("skipped %s: %v", rel, err))
			return nil
		}
		if prev, ok := stamps[rel]; ok &&
			prev.Size == info.Size() && prev.ModTime.Unix() == info.ModTime().Unix() {
			st.Skipped++
			return nil
		}

		note, warn, err := ix.parse(rel, path, info)
		if err != nil {
			st.Warnings = append(st.Warnings, fmt.Sprintf("skipped %s: %v", rel, err))
			return nil
		}
		if warn != "" {
			st.Warnings = append(st.Warnings, warn)
		}
		if err := ix.repo.UpsertNote(note); err != nil {
			return err
		}
		st.Indexed++
		return nil
	})
	if walkErr != nil {
		return st, fmt.Errorf("scan vault: %w", walkErr)
	}

	for path := range stamps {
		if !seen[path] {
			if err := ix.repo.DeleteNote(path); err != nil {
				return st, err
			}
			st.Removed++
		}
	}

	st.TotalNote = len(seen)
	st.Duration = time.Since(start)
	_ = ix.repo.SetMeta("last_index", time.Now().Format(time.RFC3339))
	return st, nil
}

func (ix *Indexer) parse(rel, abs string, info fs.FileInfo) (model.Note, string, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return model.Note{}, "", err
	}
	content := string(data)
	fm := markdown.ParseFrontmatter(content)

	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	note := model.Note{
		RelPath:  rel,
		FileName: base,
		Title:    markdown.Title(content, base),
		Aliases:  fm.Aliases,
		Headings: markdown.ExtractHeadings(content),
		Links:    markdown.ExtractWikilinks(content),
		Excerpt:  markdown.Excerpt(content, ExcerptLimit),
		ModTime:  info.ModTime(),
		Size:     info.Size(),
	}
	note.Tags = mergeTags(fm.Tags, markdown.ExtractInlineTags(content))

	var warn string
	if fm.Err != nil {
		warn = fmt.Sprintf("%s: %v (indexed without frontmatter)", rel, fm.Err)
	}
	return note, warn, nil
}

func mergeTags(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, t := range list {
			key := strings.ToLower(t)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, t)
		}
	}
	return out
}

// excluded reports whether a vault-relative path should be skipped. Hidden
// entries are always skipped; configured exclusions match either a whole path
// segment or a glob against the relative path.
func (ix *Indexer) excluded(rel, name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	lowerRel := strings.ToLower(rel)
	if strings.EqualFold(name, "trash") {
		return true
	}
	for _, ex := range ix.cfg.ExcludedPaths {
		ex = strings.Trim(strings.TrimSpace(ex), "/")
		if ex == "" {
			continue
		}
		lowerEx := strings.ToLower(ex)
		if lowerRel == lowerEx || strings.HasPrefix(lowerRel, lowerEx+"/") {
			return true
		}
		if strings.EqualFold(name, ex) {
			return true
		}
		if ok, err := filepath.Match(strings.ToLower(ex), lowerRel); err == nil && ok {
			return true
		}
	}
	return false
}

// EnsureFresh runs an incremental index when the vault has not been scanned in
// this session. It is cheap enough to call on every launch.
func (ix *Indexer) EnsureFresh() (Stats, error) {
	return ix.Run(false)
}
