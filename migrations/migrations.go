// Package migrations embeds Murmur's SQL schema migrations so the binary is
// self-contained: no migration files need to ship alongside it.
package migrations

import "embed"

// FS holds every numbered .sql migration, applied in lexical order.
//
//go:embed *.sql
var FS embed.FS
