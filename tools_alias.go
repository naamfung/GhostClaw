package main

import (
	"log"
	"os"

	"github.com/toon-format/toon-go"
)

// AliasEntry represents a single command alias.
type AliasEntry struct {
	Name    string `toon:"Name"`
	Command string `toon:"Command"`
}

// ToolsAliasConfig is the top-level config structure for tools.toon.
type ToolsAliasConfig struct {
	Aliases []AliasEntry `toon:"Aliases"`
}

// globalToolsAliases stores the loaded alias map.
var globalToolsAliases map[string]string

// LoadToolsAliases loads alias definitions from a TOON file and returns a map
// from alias name to expanded command.
func LoadToolsAliases(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}

	var config ToolsAliasConfig
	if err := toon.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	aliases := make(map[string]string)
	for _, entry := range config.Aliases {
		if entry.Name != "" && entry.Command != "" {
			aliases[entry.Name] = entry.Command
		}
	}

	log.Printf("Loaded %d tool alias(es) from %s", len(aliases), path)
	return aliases, nil
}

