package main

import _ "embed"

//go:embed embed/index.html
var indexHTML string

// GetIndexHTML returns the embedded index.html content
func GetIndexHTML() string {
	/*
		export PATH="/path/to/go/bin:$PATH"
		sh /path/to/GhostClaw/builder.sh
		/path/to/GhostClaw/builder.app
	*/
	return indexHTML
}
