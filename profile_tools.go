package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// handleProfileCheck checks which required bootstrap keys are missing.
func handleProfileCheck(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	missing := GetMissingBootstrapKeys(globalUnifiedMemory)
	if len(missing) == 0 {
		return "All required bootstrap keys are present. No initialization needed.", false
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Missing %d bootstrap key(s):\n\n", len(missing)))
	for _, key := range missing {
		switch key {
		case "user.name":
			sb.WriteString("- **user.name**: employer name/title\n")
		case "user.birth_year":
			sb.WriteString("- **user.birth_year**: employer birth year\n")
		case "user.gender":
			sb.WriteString("- **user.gender**: employer gender\n")
		case "assistant.name":
			sb.WriteString("- **assistant.name**: how the employer wants to call you\n")
		default:
			sb.WriteString(fmt.Sprintf("- **%s**\n", key))
		}
	}
	return sb.String(), false
}

// handleActorIdentitySet writes content to profiles/actors/<actor_name>/IDENTITY.md.
func handleActorIdentitySet(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	actorName, ok := argsMap["actor_name"].(string)
	if !ok || actorName == "" {
		return "Error: missing or invalid 'actor_name' parameter. Example: actor_identity_set(actor_name=\"hero\", content=\"...\")", false
	}

	content, ok := argsMap["content"].(string)
	if !ok {
		return "Error: missing or invalid 'content' parameter.", false
	}

	if globalProfileLoader == nil {
		return "Error: profile loader not initialized.", false
	}

	// Build the path
	actorsDir := filepath.Join(globalExecDir, "profiles", "actors", actorName)
	if err := os.MkdirAll(actorsDir, 0755); err != nil {
		return fmt.Sprintf("Error: failed to create actor directory: %v", err), false
	}

	identityPath := filepath.Join(actorsDir, "IDENTITY.md")
	if err := os.WriteFile(identityPath, []byte(content), 0644); err != nil {
		return fmt.Sprintf("Error: failed to write IDENTITY.md: %v", err), false
	}

	return fmt.Sprintf("Actor identity set for '%s' at %s", actorName, identityPath), false
}

// handleActorIdentityClear deletes profiles/actors/<actor_name>/IDENTITY.md.
func handleActorIdentityClear(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	actorName, ok := argsMap["actor_name"].(string)
	if !ok || actorName == "" {
		return "Error: missing or invalid 'actor_name' parameter. Example: actor_identity_clear(actor_name=\"hero\")", false
	}

	identityPath := filepath.Join(globalExecDir, "profiles", "actors", actorName, "IDENTITY.md")

	if err := os.Remove(identityPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("No IDENTITY.md found for actor '%s'. Nothing to clear.", actorName), false
		}
		return fmt.Sprintf("Error: failed to delete IDENTITY.md: %v", err), false
	}

	return fmt.Sprintf("Actor identity cleared for '%s'.", actorName), false
}

// handleProfileReload forces a profile reload from disk.
func handleProfileReload(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	if globalProfileLoader == nil {
		return "Error: profile loader not initialized.", false
	}

	globalProfileLoader.loadAll()
	profile := globalProfileLoader.GetProfile()

	var sb strings.Builder
	sb.WriteString("Profile reloaded.\n\n")
	if profile.Soul != "" {
		sb.WriteString("- SOUL.md: loaded\n")
	}
	if profile.User != "" {
		sb.WriteString("- USER.md: loaded\n")
	}
	if profile.Agent != "" {
		sb.WriteString("- AGENT.md: loaded\n")
	}
	if profile.ToolsDoc != "" {
		sb.WriteString("- TOOLS.md: loaded\n")
	}
	if len(profile.Actors) > 0 {
		var names []string
		for name := range profile.Actors {
			names = append(names, name)
		}
		sb.WriteString(fmt.Sprintf("- Actor identities: %s\n", strings.Join(names, ", ")))
	}

	return sb.String(), false
}
