package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/alliebayless/murmur/internal/database"
	"github.com/alliebayless/murmur/internal/markdown"
	"github.com/alliebayless/murmur/internal/model"
)

// UndoStrategy is how a write will be reversed.
type UndoStrategy string

// The available undo strategies.
const (
	// UndoRestore rewrites the file with its exact prior content.
	UndoRestore UndoStrategy = "restore"
	// UndoDelete removes a note Murmur created.
	UndoDelete UndoStrategy = "delete"
	// UndoPatch removes just the inserted block, preserving edits made since.
	UndoPatch UndoStrategy = "patch"
)

// UndoPlan describes what `murmur undo` is about to do.
type UndoPlan struct {
	Tx       model.WriteTransaction
	Strategy UndoStrategy
	// Conflict is true when the file changed after Murmur wrote it. The plan is
	// still safe (it only removes Murmur's own block) but needs confirmation.
	Conflict bool
	Detail   string
}

// ErrNothingToUndo is re-exported for callers.
var ErrNothingToUndo = database.ErrNothingToUndo

// UndoConflictError refuses an undo that cannot be performed safely.
type UndoConflictError struct {
	Path   string
	Reason string
}

func (e *UndoConflictError) Error() string {
	return fmt.Sprintf("refusing to undo %s: %s", e.Path, e.Reason)
}

// PlanUndo inspects the most recent write and works out how to reverse it.
func (a *App) PlanUndo() (UndoPlan, error) {
	tx, err := a.Repo.LatestTransaction()
	if err != nil {
		return UndoPlan{}, err
	}
	plan := UndoPlan{Tx: tx}

	st, err := a.Vault.Read(tx.Path)
	if err != nil {
		return plan, err
	}

	if !st.Exists {
		return plan, &UndoConflictError{
			Path:   tx.Path,
			Reason: "the note no longer exists, so there is nothing to reverse",
		}
	}

	if st.Hash == tx.HashAfter {
		if tx.Created {
			plan.Strategy = UndoDelete
			plan.Detail = "the note was created by this capture and will be deleted"
		} else {
			plan.Strategy = UndoRestore
			plan.Detail = "the note is unchanged since Murmur wrote it and will be restored exactly"
		}
		return plan, nil
	}

	// The file moved on. Only remove Murmur's own block, never newer content.
	if _, ok := markdown.RemoveBlock(st.Content, tx.Inserted); ok {
		plan.Strategy = UndoPatch
		plan.Conflict = true
		plan.Detail = "the note changed after Murmur wrote it; only the inserted block will be removed"
		return plan, nil
	}

	return plan, &UndoConflictError{
		Path:   tx.Path,
		Reason: "the note changed and the inserted text is no longer present. Edit it by hand instead of overwriting newer content",
	}
}

// ApplyUndo executes a plan produced by PlanUndo.
func (a *App) ApplyUndo(plan UndoPlan) error {
	tx := plan.Tx
	switch plan.Strategy {
	case UndoRestore:
		if err := a.Vault.Write(tx.Path, tx.Backup); err != nil {
			return err
		}
	case UndoDelete:
		abs, err := a.Vault.Resolve(tx.Path)
		if err != nil {
			return err
		}
		if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", tx.Path, err)
		}
	case UndoPatch:
		st, err := a.Vault.Read(tx.Path)
		if err != nil {
			return err
		}
		updated, ok := markdown.RemoveBlock(st.Content, tx.Inserted)
		if !ok {
			return &UndoConflictError{Path: tx.Path, Reason: "the inserted text is no longer present"}
		}
		if err := a.Vault.Write(tx.Path, updated); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown undo strategy %q", plan.Strategy)
	}

	if err := a.Repo.MarkUndone(tx.ID, tx.CaptureID); err != nil {
		return fmt.Errorf("undo applied but not recorded: %w", err)
	}
	if err := a.refreshIndex(); err != nil {
		a.Debugf("could not refresh index after undo: %v", err)
	}
	return nil
}
