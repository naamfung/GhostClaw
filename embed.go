package main

import _ "embed"

//go:embed embed/index.html
var indexHTML string

// GetIndexHTML returns the embedded index.html content
func GetIndexHTML() string {
	return indexHTML
}
