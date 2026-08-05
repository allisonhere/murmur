package obsidian_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/alliebayless/murmur/internal/obsidian"
)

func TestURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		vault string
		path  string
		file  string
	}{
		{"Obsidian Vault", "Projects/Linux/ROG Flow Z13.md", "Projects/Linux/ROG Flow Z13"},
		{"Notes", "Inbox.md", "Inbox"},
		{"My Vault & Co", "A note (draft).md", "A note (draft)"},
	}

	for _, tc := range tests {
		got := obsidian.URI(tc.vault, tc.path)
		if !strings.HasPrefix(got, "obsidian://open?") {
			t.Fatalf("uri = %q", got)
		}

		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("the generated URI does not parse: %v", err)
		}
		q := u.Query()
		if q.Get("vault") != tc.vault {
			t.Errorf("vault = %q, want %q", q.Get("vault"), tc.vault)
		}
		if q.Get("file") != tc.file {
			t.Errorf("file = %q, want %q", q.Get("file"), tc.file)
		}
		if strings.Contains(got, " ") {
			t.Errorf("the URI contains a raw space: %q", got)
		}
	}
}
