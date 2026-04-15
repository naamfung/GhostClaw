package main

import (
        "crypto/rand"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "os"
        "strings"
        "sync"
        "time"
)

// ---------------------------------------------------------------------------
// ManagedProfile – per-profile isolation for model / memory configuration
// ---------------------------------------------------------------------------

// ProfileModelSettings holds model parameters scoped to a single profile.
type ProfileModelSettings struct {
        ModelID     string  `json:"model_id"`
        APIType     string  `json:"api_type"`
        BaseURL     string  `json:"base_url"`
        APIKey      string  `json:"api_key"`
        Temperature float64 `json:"temperature"`
        MaxTokens   int     `json:"max_tokens"`
}

// ProfileMemorySettings controls memory behaviour scoped to a profile.
type ProfileMemorySettings struct {
        Enabled bool   `json:"enabled"`
        Scope   string `json:"scope"` // "profile" or "global"
}

// ManagedProfile is a named, self-contained configuration for a user or
// scenario.  It carries its own model and memory settings and can be
// persisted to / loaded from a JSON file.
type ManagedProfile struct {
        ID          string                 `json:"id"`
        Name        string                 `json:"name"`
        Description string                 `json:"description"`
        Model       ProfileModelSettings   `json:"model_config"`
        Memory      ProfileMemorySettings  `json:"memory_config"`
        CreatedAt   time.Time              `json:"created_at"`
        UpdatedAt   time.Time              `json:"updated_at"`
        Tags        []string               `json:"tags"`
}

// ---------------------------------------------------------------------------
// ProfileManager – thread-safe CRUD + persistence
// ---------------------------------------------------------------------------

// ProfileManager manages a set of ManagedProfile instances, tracks the
// currently active profile, and handles JSON-file persistence.
type ProfileManager struct {
        mu               sync.RWMutex
        profiles         map[string]*ManagedProfile
        activeProfileID  string
        storagePath      string
}

// NewProfileManager creates a ProfileManager backed by the JSON file at
// storagePath.  If the file does not exist or is empty a "default" profile
// is auto-created.
func NewProfileManager(storagePath string) *ProfileManager {
        pm := &ProfileManager{
                profiles:    make(map[string]*ManagedProfile),
                storagePath: storagePath,
        }

        // Attempt to load existing data; ignore errors (will auto-create default).
        if err := pm.Load(); err != nil {
                // File missing / corrupt – fall through to default creation.
        }

        // Ensure at least one profile exists.
        if len(pm.profiles) == 0 {
                now := time.Now()
                defaultProfile := &ManagedProfile{
                        ID:          "default",
                        Name:        "default",
                        Description: "Default profile – created on first run",
                        Model: ProfileModelSettings{
                                Temperature: 0.7,
                                MaxTokens:   4096,
                        },
                        Memory: ProfileMemorySettings{
                                Enabled: true,
                                Scope:   "profile",
                        },
                        CreatedAt: now,
                        UpdatedAt: now,
                        Tags:      []string{"default"},
                }
                pm.profiles[defaultProfile.ID] = defaultProfile
                pm.activeProfileID = defaultProfile.ID
                _ = pm.Save() // best-effort
        }

        // Ensure activeProfileID refers to a valid profile.
        if _, ok := pm.profiles[pm.activeProfileID]; !ok {
                for id := range pm.profiles {
                        pm.activeProfileID = id
                        break
                }
        }

        return pm
}

// CreateProfile validates and stores a new profile.  The ID field is
// auto-generated when empty.  Name must be non-empty.
func (pm *ProfileManager) CreateProfile(profile *ManagedProfile) error {
        pm.mu.Lock()
        defer pm.mu.Unlock()

        if profile.Name == "" {
                return fmt.Errorf("profile name must not be empty")
        }

        now := time.Now()
        profile.CreatedAt = now
        profile.UpdatedAt = now

        if profile.ID == "" {
                profile.ID = generateProfileID()
        }

        if _, exists := pm.profiles[profile.ID]; exists {
                return fmt.Errorf("profile with id %q already exists", profile.ID)
        }

        // Validate memory scope.
        if profile.Memory.Scope != "" &&
                profile.Memory.Scope != "profile" &&
                profile.Memory.Scope != "global" {
                return fmt.Errorf("invalid memory scope %q, must be \"profile\" or \"global\"", profile.Memory.Scope)
        }

        // Default memory scope.
        if profile.Memory.Scope == "" {
                profile.Memory.Scope = "profile"
        }

        pm.profiles[profile.ID] = profile

        // Auto-activate if this is the first profile.
        if pm.activeProfileID == "" {
                pm.activeProfileID = profile.ID
        }

        return pm.saveLocked()
}

// GetProfile returns a profile by ID.  The returned pointer is a copy;
// callers may mutate it without affecting the stored profile.
func (pm *ProfileManager) GetProfile(id string) (*ManagedProfile, bool) {
        pm.mu.RLock()
        defer pm.mu.RUnlock()

        p, ok := pm.profiles[id]
        if !ok {
                return nil, false
        }
        cp := *p
        cp.Tags = make([]string, len(p.Tags))
        copy(cp.Tags, p.Tags)
        return &cp, true
}

// UpdateProfile applies a set of key-value updates to the profile identified
// by id.  Supported keys (all optional):
//
//      "name", "description", "model_config", "memory_config", "tags"
//
// The "model_config" and "memory_config" values must be map[string]interface{}.
func (pm *ProfileManager) UpdateProfile(id string, updates map[string]interface{}) error {
        pm.mu.Lock()
        defer pm.mu.Unlock()

        p, ok := pm.profiles[id]
        if !ok {
                return fmt.Errorf("profile %q not found", id)
        }

        if v, ok := updates["name"].(string); ok {
                if v == "" {
                        return fmt.Errorf("profile name must not be empty")
                }
                p.Name = v
        }

        if v, ok := updates["description"].(string); ok {
                p.Description = v
        }

        if v, ok := updates["model_config"].(map[string]interface{}); ok {
                if val, ok := v["model_id"].(string); ok {
                        p.Model.ModelID = val
                }
                if val, ok := v["api_type"].(string); ok {
                        p.Model.APIType = val
                }
                if val, ok := v["base_url"].(string); ok {
                        p.Model.BaseURL = val
                }
                if val, ok := v["api_key"].(string); ok {
                        p.Model.APIKey = val
                }
                if val, ok := v["temperature"].(float64); ok {
                        p.Model.Temperature = val
                }
                if val, ok := v["max_tokens"].(float64); ok {
                        p.Model.MaxTokens = int(val)
                }
        }

        if v, ok := updates["memory_config"].(map[string]interface{}); ok {
                if val, ok := v["enabled"].(bool); ok {
                        p.Memory.Enabled = val
                }
                if val, ok := v["scope"].(string); ok {
                        if val != "profile" && val != "global" {
                                return fmt.Errorf("invalid memory scope %q", val)
                        }
                        p.Memory.Scope = val
                }
        }

        if v, ok := updates["tags"].([]string); ok {
                p.Tags = v
        }

        p.UpdatedAt = time.Now()
        return pm.saveLocked()
}

// DeleteProfile removes a profile by ID.  The default profile ("default")
// cannot be deleted.  If the active profile is deleted the active profile
// falls back to the remaining first profile.
func (pm *ProfileManager) DeleteProfile(id string) error {
        pm.mu.Lock()
        defer pm.mu.Unlock()

        if id == "default" {
                return fmt.Errorf("cannot delete the default profile")
        }

        if _, ok := pm.profiles[id]; !ok {
                return fmt.Errorf("profile %q not found", id)
        }

        delete(pm.profiles, id)

        if pm.activeProfileID == id {
                for pid := range pm.profiles {
                        pm.activeProfileID = pid
                        break
                }
        }

        return pm.saveLocked()
}

// ListProfiles returns all profiles (copies).
func (pm *ProfileManager) ListProfiles() []*ManagedProfile {
        pm.mu.RLock()
        defer pm.mu.RUnlock()

        out := make([]*ManagedProfile, 0, len(pm.profiles))
        for _, p := range pm.profiles {
                cp := *p
                cp.Tags = make([]string, len(p.Tags))
                copy(cp.Tags, p.Tags)
                out = append(out, &cp)
        }
        return out
}

// SetActiveProfile changes the currently active profile.
func (pm *ProfileManager) SetActiveProfile(id string) error {
        pm.mu.Lock()
        defer pm.mu.Unlock()

        if _, ok := pm.profiles[id]; !ok {
                return fmt.Errorf("profile %q not found", id)
        }
        pm.activeProfileID = id
        return nil
}

// GetActiveProfile returns the currently active profile.
func (pm *ProfileManager) GetActiveProfile() (*ManagedProfile, error) {
        pm.mu.RLock()
        defer pm.mu.RUnlock()

        p, ok := pm.profiles[pm.activeProfileID]
        if !ok {
                return nil, fmt.Errorf("no active profile")
        }
        cp := *p
        cp.Tags = make([]string, len(p.Tags))
        copy(cp.Tags, p.Tags)
        return &cp, nil
}

// Save persists all profiles to the JSON storage file.
func (pm *ProfileManager) Save() error {
        pm.mu.Lock()
        defer pm.mu.Unlock()
        return pm.saveLocked()
}

// saveLocked writes the profile store to disk.  Caller must hold the write lock.
func (pm *ProfileManager) saveLocked() error {
        data := struct {
                Profiles        map[string]*ManagedProfile `json:"profiles"`
                ActiveProfileID string                    `json:"active_profile_id"`
        }{
                Profiles:        pm.profiles,
                ActiveProfileID: pm.activeProfileID,
        }

        raw, err := json.MarshalIndent(data, "", "  ")
        if err != nil {
                return fmt.Errorf("failed to marshal profiles: %w", err)
        }

        if err := os.WriteFile(pm.storagePath, raw, 0600); err != nil {
                return fmt.Errorf("failed to write profile storage: %w", err)
        }
        return nil
}

// Load reads profiles from the JSON storage file.
func (pm *ProfileManager) Load() error {
        pm.mu.Lock()
        defer pm.mu.Unlock()

        raw, err := os.ReadFile(pm.storagePath)
        if err != nil {
                return err
        }

        var data struct {
                Profiles        map[string]*ManagedProfile `json:"profiles"`
                ActiveProfileID string                    `json:"active_profile_id"`
        }
        if err := json.Unmarshal(raw, &data); err != nil {
                return fmt.Errorf("failed to parse profile storage: %w", err)
        }

        pm.profiles = data.Profiles
        if pm.profiles == nil {
                pm.profiles = make(map[string]*ManagedProfile)
        }
        pm.activeProfileID = data.ActiveProfileID
        return nil
}

// ExportProfile serialises a profile to JSON with the API key masked.
func (pm *ProfileManager) ExportProfile(id string) (string, error) {
        pm.mu.RLock()
        defer pm.mu.RUnlock()

        p, ok := pm.profiles[id]
        if !ok {
                return "", fmt.Errorf("profile %q not found", id)
        }

        export := *p
        export.Model.APIKey = maskProfileAPIKey(p.Model.APIKey)
        if export.Tags == nil {
                export.Tags = []string{}
        }

        raw, err := json.MarshalIndent(export, "", "  ")
        if err != nil {
                return "", fmt.Errorf("failed to export profile: %w", err)
        }
        return string(raw), nil
}

// ImportProfile parses a JSON document into a new ManagedProfile.  The API
// key field is preserved as-is (callers should ensure the source is trusted).
// If the ID is empty a new one is generated.  The profile is stored
// immediately.
func (pm *ProfileManager) ImportProfile(jsonData string) (*ManagedProfile, error) {
        var p ManagedProfile
        if err := json.Unmarshal([]byte(jsonData), &p); err != nil {
                return nil, fmt.Errorf("failed to parse profile JSON: %w", err)
        }

        if p.Name == "" {
                return nil, fmt.Errorf("profile name must not be empty")
        }

        if p.ID == "" {
                p.ID = generateProfileID()
        }

        if err := pm.CreateProfile(&p); err != nil {
                return nil, err
        }

        // Return a copy of the stored profile.
        if stored, ok := pm.GetProfile(p.ID); ok {
                return stored, nil
        }
        return nil, fmt.Errorf("profile %q not found after creation", p.ID)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// generateProfileID produces a random 16-char hex string.
func generateProfileID() string {
        b := make([]byte, 8)
        _, _ = rand.Read(b)
        return hex.EncodeToString(b)
}

// maskProfileAPIKey reveals the first 8 and last 4 characters of an API key;
// everything in between is replaced with asterisks.  Short keys are fully
// masked.
func maskProfileAPIKey(key string) string {
        if key == "" {
                return ""
        }
        if len(key) <= 12 {
                return strings.Repeat("*", len(key))
        }
        return key[:8] + strings.Repeat("*", len(key)-12) + key[len(key)-4:]
}

// ---------------------------------------------------------------------------
// Global profile manager
// ---------------------------------------------------------------------------

var globalProfileManager *ProfileManager
