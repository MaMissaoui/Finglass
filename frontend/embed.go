// Package frontend embeds the built Svelte application.
package frontend

import "embed"

// Dist holds the Vite build output. The dist directory always contains at
// least .keep, so the package compiles before the first frontend build.
//
//go:embed all:dist
var Dist embed.FS
