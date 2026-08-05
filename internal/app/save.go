package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alliebayless/murmur/internal/markdown"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/router"
	"github.com/alliebayless/murmur/internal/storage"
)

// SaveResult describes a completed write.
type SaveResult struct {
	Path      string
	Section   string
	Mode      model.InsertMode
	Created   bool
	CaptureID int64
	TxID      int64
	URI       string
}

// Summary renders the "Saved to ..." line printed after a successful capture.
func (r SaveResult) Summary() string {
	if r.Section == "" {
		return fmt.Sprintf("Saved to %s", r.Path)
	}
	return fmt.Sprintf("Saved to %s under ## %s", r.Path, r.Section)
}

// Save writes the draft into the vault, records undo information and updates
// the local learning signal.
func (d *Draft) Save() (SaveResult, error) {
	var res SaveResult
	if strings.TrimSpace(d.Markdown) == "" {
		return res, errors.New("nothing to save: the preview is empty")
	}

	st, err := d.stateForWrite()
	if err != nil {
		return res, err
	}

	base := st.Content
	created := !st.Exists
	if created {
		base = d.newNoteContent()
	}

	mode := d.Routing.Mode
	if mode == model.InsertUnderHeading {
		if _, ok := markdown.FindHeading(base, d.Routing.Section); !ok {
			mode = model.InsertCreateHeading
		}
	}

	updated, err := markdown.Insert(markdown.InsertRequest{
		Content: base,
		Block:   d.Markdown,
		Section: d.Routing.Section,
		Mode:    mode,
	})
	if err != nil {
		return res, err
	}

	if err := d.app.Vault.Write(d.Routing.NotePath, updated); err != nil {
		return res, err
	}

	res = SaveResult{
		Path:    d.Routing.NotePath,
		Section: d.Routing.Section,
		Mode:    mode,
		Created: created,
		URI:     d.app.ObsidianURI(d.Routing.NotePath),
	}

	// Everything below is bookkeeping: a failure here must not make the user
	// think their thought was lost, because it is already on disk.
	corrected := d.Corrected()
	capture := model.CaptureRecord{
		CreatedAt:  time.Now(),
		Raw:        d.Raw,
		Markdown:   d.Markdown,
		NotePath:   d.Routing.NotePath,
		Section:    d.Routing.Section,
		Type:       d.Routing.Type,
		Tags:       d.Routing.Tags,
		Confidence: d.Routing.Confidence,
		Source:     d.Routing.Source,
		Corrected:  corrected,
	}
	captureID, err := d.app.Repo.InsertCapture(capture)
	if err != nil {
		d.app.Warnf("saved to %s but could not record history: %v", res.Path, err)
		return res, nil
	}
	res.CaptureID = captureID

	if err := d.app.Repo.InsertCandidates(captureID, allCandidates(d), d.Routing.NotePath); err != nil {
		d.app.Debugf("could not record candidates: %v", err)
	}

	txID, err := d.app.Repo.InsertWriteTransaction(model.WriteTransaction{
		CaptureID:  captureID,
		CreatedAt:  time.Now(),
		Path:       d.Routing.NotePath,
		HashBefore: st.Hash,
		HashAfter:  storage.Hash(updated),
		Inserted:   d.Markdown,
		Section:    d.Routing.Section,
		Mode:       mode,
		Created:    created,
		Backup:     st.Content,
	})
	if err != nil {
		d.app.Warnf("saved to %s but could not record undo information: %v", res.Path, err)
		return res, nil
	}
	res.TxID = txID

	if err := d.app.Repo.RecordRouting(router.Tokenize(d.Cleaned),
		d.Routing.NotePath, d.Routing.Section, d.Routing.Type, corrected); err != nil {
		d.app.Debugf("could not record learning signal: %v", err)
	}

	// Keep the index in step so the next capture sees the new heading.
	if err := d.app.refreshIndex(); err != nil {
		d.app.Debugf("could not refresh index for %s: %v", res.Path, err)
	}
	return res, nil
}

func allCandidates(d *Draft) []model.Candidate {
	cands := make([]model.Candidate, 0, len(d.Routing.Candidates)+1)
	if n, ok := d.app.Engine.Note(d.Routing.NotePath); ok {
		cands = append(cands, model.Candidate{Note: n, Score: d.Routing.Confidence, Reasons: []string{d.Routing.Explanation}})
	} else {
		cands = append(cands, model.Candidate{
			Note:    model.Note{RelPath: d.Routing.NotePath},
			Score:   d.Routing.Confidence,
			Reasons: []string{d.Routing.Explanation},
		})
	}
	return append(cands, d.Routing.Candidates...)
}

// newNoteContent builds the starting content for a note Murmur has to create.
// Daily notes use the configured template when one exists.
func (d *Draft) newNoteContent() string {
	cfg := d.app.Cfg
	if cfg.DailyTemplatePath == "" {
		return ""
	}
	if d.Routing.NotePath != d.app.Engine.DailyPath(d.now) {
		return ""
	}
	tpl, err := d.app.readTemplate(cfg.DailyTemplatePath)
	if err != nil {
		d.app.Warnf("could not read daily template %s: %v", cfg.DailyTemplatePath, err)
		return ""
	}
	return router.ExpandTemplate(tpl, d.now, cfg.DateFormat, cfg.TimeFormat)
}

// readTemplate loads a template from the vault, or from an absolute path
// outside it when the user configured one.
func (a *App) readTemplate(path string) (string, error) {
	if filepath.IsAbs(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	st, err := a.Vault.Read(path)
	if err != nil {
		return "", err
	}
	if !st.Exists {
		return "", fmt.Errorf("template %s does not exist", path)
	}
	return st.Content, nil
}

// refreshIndex brings the index back in step after a write. The scan is
// incremental (modification time and size), so this is cheap.
func (a *App) refreshIndex() error {
	if _, err := a.Index.Run(false); err != nil {
		return err
	}
	return a.reloadEngine()
}
