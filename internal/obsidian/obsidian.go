// Package obsidian builds obsidian:// URIs and opens them with the system
// handler. Obsidian never needs to be running for Murmur to capture or save.
package obsidian

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// URI builds an obsidian://open link for a note inside a named vault. The
// ".md" extension is dropped because Obsidian addresses notes by name.
func URI(vaultName, relPath string) string {
	file := strings.TrimSuffix(relPath, ".md")
	q := url.Values{}
	q.Set("vault", vaultName)
	q.Set("file", file)
	return "obsidian://open?" + q.Encode()
}

// Open hands a URI to the desktop's URL handler.
func Open(uri string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", uri)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", uri)
	default:
		cmd = exec.Command("xdg-open", uri)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open Obsidian: %w", err)
	}
	// Do not wait: the handler may be a long-lived GUI process.
	go func() { _ = cmd.Wait() }()
	return nil
}
