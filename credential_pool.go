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
// Credential – a single API key with health-tracking metadata
// ---------------------------------------------------------------------------

// Credential represents a single API key with priority-based routing,
// rate-limit tracking, and automatic exponential cooldown on failures.
type Credential struct {
        ID                  string    `json:"id"`
        Key                 string    `json:"key"`
        Provider            string    `json:"provider"`
        Priority            int       `json:"priority"`
        Enabled             bool      `json:"enabled"`
        RateLimit           int       `json:"rate_limit"`             // requests per minute (0 = unlimited)
        UsageCount          int       `json:"usage_count"`
        LastUsedAt          time.Time `json:"last_used_at"`
        LastError           string    `json:"last_error"`
        CooldownUntil       time.Time `json:"cooldown_until"`
        RateLimitCooldown   time.Time `json:"rate_limit_cooldown,omitempty"` // separate cooldown for HTTP 429
        consecutiveFailures int       `json:"-"`                          // unexported – not persisted
        requestTimestamps   []time.Time `json:"-"`                        // RPM tracking (circular buffer, not persisted)
}

// ---------------------------------------------------------------------------
// CredentialPool – thread-safe credential rotation
// ---------------------------------------------------------------------------

// CredentialPool manages a collection of credentials for a single provider.
// It automatically rotates to the highest-priority healthy credential and
// applies exponential backoff on failures.  Supports round-robin within
// the same priority level and proactive RPM-aware deprioritization.
type CredentialPool struct {
        mu          sync.RWMutex
        credentials []*Credential
        provider    string
        lastUsedIndex int // round-robin index (unexported, not persisted)
}

// NewCredentialPool returns a pool for the given provider name.
func NewCredentialPool(provider string) *CredentialPool {
        return &CredentialPool{
                provider:    provider,
                credentials: make([]*Credential, 0),
        }
}

// AddCredential inserts a new credential and returns it.  The ID is
// auto-generated.
func (cp *CredentialPool) AddCredential(key string, priority int) *Credential {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        cred := &Credential{
                ID:       generateCredentialID(),
                Key:      key,
                Provider: cp.provider,
                Priority: priority,
                Enabled:  true,
        }
        cp.credentials = append(cp.credentials, cred)
        return cred
}

// RemoveCredential deletes a credential by ID.
func (cp *CredentialPool) RemoveCredential(id string) error {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        for i, c := range cp.credentials {
                if c.ID == id {
                        cp.credentials = append(cp.credentials[:i], cp.credentials[i+1:]...)
                        // Adjust lastUsedIndex if it would now point past the slice.
                        if cp.lastUsedIndex >= len(cp.credentials) && len(cp.credentials) > 0 {
                                cp.lastUsedIndex = 0
                        }
                        return nil
                }
        }
        return fmt.Errorf("credential %q not found", id)
}

// ---------------------------------------------------------------------------
// isHealthy – checks whether a credential is usable right now
// ---------------------------------------------------------------------------

// isHealthy returns true when the credential is enabled and not in any
// cooldown (general or rate-limit).
func isHealthy(c *Credential, now time.Time) bool {
        if !c.Enabled {
                return false
        }
        if !c.CooldownUntil.IsZero() && c.CooldownUntil.After(now) {
                return false
        }
        if !c.RateLimitCooldown.IsZero() && c.RateLimitCooldown.After(now) {
                return false
        }
        return true
}

// ---------------------------------------------------------------------------
// RPM helpers (must be called with cp.mu held)
// ---------------------------------------------------------------------------

// cleanOldTimestamps removes request timestamps older than 60 seconds.
func cleanOldTimestamps(c *Credential, now time.Time) {
        cutoff := now.Add(-60 * time.Second)
        writeIdx := 0
        for _, ts := range c.requestTimestamps {
                if ts.After(cutoff) {
                        c.requestTimestamps[writeIdx] = ts
                        writeIdx++
                }
        }
        c.requestTimestamps = c.requestTimestamps[:writeIdx]
}

// currentRPM returns the number of requests recorded in the last 60 seconds.
func currentRPM(c *Credential, now time.Time) int {
        cleanOldTimestamps(c, now)
        return len(c.requestTimestamps)
}

// rpmProximity returns a value in [0, 1] indicating how close the credential
// is to its rate limit.  0 means no load, 1 means at limit.
// If RateLimit is 0 (unlimited), returns 0.
func rpmProximity(c *Credential, now time.Time) float64 {
        if c.RateLimit <= 0 {
                return 0
        }
        rpm := currentRPM(c, now)
        prox := float64(rpm) / float64(c.RateLimit)
        if prox > 1.0 {
                prox = 1.0
        }
        return prox
}

// ---------------------------------------------------------------------------
// collectCandidates – gathers healthy credentials sorted by (priority desc, rpmProximity asc)
// ---------------------------------------------------------------------------

// credentialScore is used to sort candidates: higher priority is better,
// lower RPM proximity breaks ties.
type credentialScore struct {
        cred       *Credential
        priority   int
        proximity  float64
}

// collectCandidates returns all healthy credentials scored for selection.
func (cp *CredentialPool) collectCandidates(now time.Time, excludeID string) []credentialScore {
        var candidates []credentialScore
        for _, c := range cp.credentials {
                if !isHealthy(c, now) {
                        continue
                }
                if excludeID != "" && c.ID == excludeID {
                        continue
                }
                candidates = append(candidates, credentialScore{
                        cred:      c,
                        priority:  c.Priority,
                        proximity: rpmProximity(c, now),
                })
        }
        return candidates
}

// ---------------------------------------------------------------------------
// GetCredential – returns the best credential (round-robin + RPM-aware)
// ---------------------------------------------------------------------------

// GetCredential returns the highest-priority, non-cooldown, enabled
// credential.  On success it increments UsageCount and updates LastUsedAt.
// Selection uses round-robin within the same priority level and
// deprioritises credentials whose RPM is above 80% of their limit.
// Returns an error when no healthy credential is available.
func (cp *CredentialPool) GetCredential() (*Credential, error) {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        now := time.Now()
        candidates := cp.collectCandidates(now, "")
        if len(candidates) == 0 {
                return nil, fmt.Errorf("no healthy credential available for provider %q", cp.provider)
        }

        best := cp.selectBestCandidate(candidates, now)
        if best == nil {
                return nil, fmt.Errorf("no healthy credential available for provider %q", cp.provider)
        }

        best.UsageCount++
        best.LastUsedAt = now
        // RPM 追蹤由 RecordRequest 在 API 調用成功後統一記錄，
        // 不在此處 append requestTimestamps，避免雙重計數。
        return best, nil
}

// selectBestCandidate picks the best credential from the candidate list.
// Strategy: round-robin within the highest priority group; if all in that
// group are at >80% RPM proximity, fall to the next priority group.
func (cp *CredentialPool) selectBestCandidate(candidates []credentialScore, now time.Time) *Credential {
        // Find the highest priority level.
        maxPriority := 0
        for _, cs := range candidates {
                if cs.priority > maxPriority {
                        maxPriority = cs.priority
                }
        }

        // Collect candidates at the highest priority.
        var topGroup []credentialScore
        for _, cs := range candidates {
                if cs.priority == maxPriority {
                        topGroup = append(topGroup, cs)
                }
        }

        // Try to find one at <80% RPM proximity.
        var coolCreds []credentialScore
        for _, cs := range topGroup {
                if cs.proximity < 0.8 {
                        coolCreds = append(coolCreds, cs)
                }
        }

        selection := topGroup
        if len(coolCreds) > 0 {
                selection = coolCreds
        }

        if len(selection) == 0 {
                return nil
        }

        // Round-robin within the selection.
        if cp.lastUsedIndex >= len(selection) {
                cp.lastUsedIndex = 0
        }
        chosen := selection[cp.lastUsedIndex].cred
        cp.lastUsedIndex = (cp.lastUsedIndex + 1) % len(selection)
        return chosen
}

// ---------------------------------------------------------------------------
// GetCredentialWithFallback – best + fallback credential
// ---------------------------------------------------------------------------

// GetCredentialWithFallback returns the best credential AND a fallback
// credential (second priority).  The fallback may be nil if only one
// credential is available.  This is useful for pre-fetching a backup key.
func (cp *CredentialPool) GetCredentialWithFallback() (*Credential, *Credential, error) {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        now := time.Now()
        candidates := cp.collectCandidates(now, "")
        if len(candidates) == 0 {
                return nil, nil, fmt.Errorf("no healthy credential available for provider %q", cp.provider)
        }

        best := cp.selectBestCandidate(candidates, now)
        if best == nil {
                return nil, nil, fmt.Errorf("no healthy credential available for provider %q", cp.provider)
        }
        best.UsageCount++
        best.LastUsedAt = now
        // RPM 追蹤由 RecordRequest 統一記錄，不在 Get 中雙重計數。

        // Fallback: best candidate excluding the one just picked.
        var fallbackCandidates []credentialScore
        for _, cs := range candidates {
                if cs.cred.ID != best.ID {
                        fallbackCandidates = append(fallbackCandidates, cs)
                }
        }

        var fallback *Credential
        if len(fallbackCandidates) > 0 {
                fallback = cp.selectBestCandidate(fallbackCandidates, now)
        }

        return best, fallback, nil
}

// ---------------------------------------------------------------------------
// RecordRequest – track a successful request for RPM accounting
// ---------------------------------------------------------------------------

// RecordRequest is called after a successful credential use to track RPM.
// It appends the current timestamp and cleans entries older than 60 seconds.
func (cp *CredentialPool) RecordRequest(credentialID string) {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        now := time.Now()
        for _, c := range cp.credentials {
                if c.ID == credentialID {
                        c.requestTimestamps = append(c.requestTimestamps, now)
                        cleanOldTimestamps(c, now)
                        return
                }
        }
}

// ---------------------------------------------------------------------------
// ReportRateLimit – aggressive cooldown for HTTP 429
// ---------------------------------------------------------------------------

// ReportRateLimit is called when a 429 (Too Many Requests) is received.
// It sets RateLimitCooldown based on the Retry-After duration, clamped to
// [30 s, 5 min].  This is more aggressive than the general ReportFailure
// cooldown and does NOT increment consecutiveFailures (rate-limiting is
// not a credential fault).
func (cp *CredentialPool) ReportRateLimit(credentialID string, retryAfter time.Duration) {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        now := time.Now()
        if retryAfter < 30*time.Second {
                retryAfter = 30 * time.Second
        }
        if retryAfter > 5*time.Minute {
                retryAfter = 5 * time.Minute
        }

        for _, c := range cp.credentials {
                if c.ID == credentialID {
                        c.RateLimitCooldown = now.Add(retryAfter)
                        c.LastError = fmt.Sprintf("rate limited (429), cooldown %v", retryAfter)
                        return
                }
        }
}

// ---------------------------------------------------------------------------
// GetCredentialForRetry – get an alternate credential excluding one ID
// ---------------------------------------------------------------------------

// GetCredentialForRetry returns the next available credential excluding the
// one identified by excludeID.  Used for immediate retry with a different
// key after a 429 rate-limit response.
func (cp *CredentialPool) GetCredentialForRetry(excludeID string) (*Credential, error) {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        now := time.Now()
        candidates := cp.collectCandidates(now, excludeID)
        if len(candidates) == 0 {
                return nil, fmt.Errorf("no alternate credential available (excluding %q) for provider %q", excludeID, cp.provider)
        }

        best := cp.selectBestCandidate(candidates, now)
        if best == nil {
                return nil, fmt.Errorf("no alternate credential available (excluding %q) for provider %q", excludeID, cp.provider)
        }

        best.UsageCount++
        best.LastUsedAt = now
        // RPM 追蹤由 RecordRequest 統一記錄，不在 Get 中雙重計數。
        return best, nil
}

// ---------------------------------------------------------------------------
// GetCredentialByID – lookup a credential by its ID
// ---------------------------------------------------------------------------

// GetCredentialByID returns the credential with the given ID, or an error
// if not found.  The returned credential is a direct reference (caller
// must hold no assumptions about thread-safety of the returned pointer).
func (cp *CredentialPool) GetCredentialByID(id string) (*Credential, error) {
        cp.mu.RLock()
        defer cp.mu.RUnlock()

        for _, c := range cp.credentials {
                if c.ID == id {
                        return c, nil
                }
        }
        return nil, fmt.Errorf("credential %q not found", id)
}

// ---------------------------------------------------------------------------
// PoolSize – total number of credentials (healthy or not)
// ---------------------------------------------------------------------------

// PoolSize returns the total number of credentials in the pool.
func (cp *CredentialPool) PoolSize() int {
        cp.mu.RLock()
        defer cp.mu.RUnlock()
        return len(cp.credentials)
}

// ReportSuccess records that a credential was used successfully.
func (cp *CredentialPool) ReportSuccess(credentialID string) {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        for _, c := range cp.credentials {
                if c.ID == credentialID {
                        c.LastUsedAt = time.Now()
                        c.consecutiveFailures = 0
                        c.LastError = ""
                        return
                }
        }
}

// ReportFailure records a failure for a credential and applies exponential
// backoff: 30 s, 60 s, 120 s, 240 s, …  capped at 30 minutes.
func (cp *CredentialPool) ReportFailure(credentialID string, errMsg string) {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        now := time.Now()
        for _, c := range cp.credentials {
                if c.ID == credentialID {
                        c.consecutiveFailures++
                        c.LastError = errMsg

                        // Exponential backoff: 30s * 2^(n-1), max 30 min.
                        // 封頂 consecutiveFailures 到 30，防止 64 次以上失敗時 1<<63 溢出。
                        if c.consecutiveFailures > 30 {
                                c.consecutiveFailures = 30
                        }
                        delay := 30 * (1 << (c.consecutiveFailures - 1))
                        if delay > 1800 {
                                delay = 1800
                        }
                        c.CooldownUntil = now.Add(time.Duration(delay) * time.Second)
                        return
                }
        }
}

// GetAllCredentials returns copies of all credentials with masked keys.
func (cp *CredentialPool) GetAllCredentials() []*Credential {
        cp.mu.RLock()
        defer cp.mu.RUnlock()

        out := make([]*Credential, len(cp.credentials))
        for i, c := range cp.credentials {
                cpCopy := *c
                cpCopy.Key = MaskAPIKey(c.Key)
                out[i] = &cpCopy
        }
        return out
}

// GetHealthyCredentialCount returns the number of credentials that are
// enabled and not currently in any cooldown (general or rate-limit).
func (cp *CredentialPool) GetHealthyCredentialCount() int {
        cp.mu.RLock()
        defer cp.mu.RUnlock()

        now := time.Now()
        count := 0
        for _, c := range cp.credentials {
                if isHealthy(c, now) {
                        count++
                }
        }
        return count
}

// SetRateLimit updates the per-minute rate limit for a credential.
func (cp *CredentialPool) SetRateLimit(credentialID string, rateLimit int) error {
        cp.mu.Lock()
        defer cp.mu.Unlock()

        for _, c := range cp.credentials {
                if c.ID == credentialID {
                        c.RateLimit = rateLimit
                        return nil
                }
        }
        return fmt.Errorf("credential %q not found", credentialID)
}

// ---------------------------------------------------------------------------
// MaskAPIKey – exported utility
// ---------------------------------------------------------------------------

// MaskAPIKey reveals the first 8 and last 4 characters of an API key and
// replaces everything in between with asterisks.  Short keys (<= 12 chars)
// are fully masked.
func MaskAPIKey(key string) string {
        if key == "" {
                return ""
        }
        if len(key) <= 12 {
                return strings.Repeat("*", len(key))
        }
        return key[:8] + strings.Repeat("*", len(key)-12) + key[len(key)-4:]
}

// ---------------------------------------------------------------------------
// Config persistence (keys are masked on write)
// ---------------------------------------------------------------------------

// credentialFileFormat is the on-disk representation for credential pools.
type credentialFileFormat struct {
        Provider    string       `json:"provider"`
        Credentials []*Credential `json:"credentials"`
}

// LoadFromConfig reads credentials from a JSON file.  Existing in-memory
// credentials are replaced.
func (cp *CredentialPool) LoadFromConfig(configPath string) error {
        raw, err := os.ReadFile(configPath)
        if err != nil {
                return fmt.Errorf("failed to read credential config: %w", err)
        }

        var fileData credentialFileFormat
        if err := json.Unmarshal(raw, &fileData); err != nil {
                return fmt.Errorf("failed to parse credential config: %w", err)
        }

        cp.mu.Lock()
        defer cp.mu.Unlock()

        cp.provider = fileData.Provider
        cp.credentials = make([]*Credential, 0, len(fileData.Credentials))
        for _, c := range fileData.Credentials {
                copy := *c
                // Ensure requestTimestamps is initialised for loaded credentials.
                if copy.requestTimestamps == nil {
                        copy.requestTimestamps = make([]time.Time, 0)
                }
                cp.credentials = append(cp.credentials, &copy)
        }
        return nil
}

// SaveConfig persists the current credentials to a JSON file.  API keys
// are masked before writing.
func (cp *CredentialPool) SaveConfig(configPath string) error {
        cp.mu.RLock()
        defer cp.mu.RUnlock()

        fileData := credentialFileFormat{
                Provider:    cp.provider,
                Credentials: make([]*Credential, len(cp.credentials)),
        }
        for i, c := range cp.credentials {
                cpCopy := *c
                cpCopy.Key = MaskAPIKey(c.Key)
                fileData.Credentials[i] = &cpCopy
        }

        raw, err := json.MarshalIndent(fileData, "", "  ")
        if err != nil {
                return fmt.Errorf("failed to marshal credentials: %w", err)
        }

        if err := os.WriteFile(configPath, raw, 0600); err != nil {
                return fmt.Errorf("failed to write credential config: %w", err)
        }
        return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// generateCredentialID produces a random 12-char hex string.
func generateCredentialID() string {
        b := make([]byte, 6)
        _, _ = rand.Read(b)
        return "cred_" + hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// Global credential pool
// ---------------------------------------------------------------------------

var globalCredentialPool *CredentialPool
