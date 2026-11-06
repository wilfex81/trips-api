package migrations

import "embed"

// FS holds the .sql files inside the compiled binary.
//
//go:embed *.sql
var FS embed.FS
