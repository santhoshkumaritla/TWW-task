package main

import "embed"

//go:embed web
var webFS embed.FS

//go:embed seed/events.json
var seedJSON []byte
