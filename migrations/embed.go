// Package migrations embeds all SQL migration files so the binary can run
// migrations without depending on the filesystem layout at runtime.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
