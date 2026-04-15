package main

import (
	"fmt"
	"log"
	"sync"
)

// MemoryProvider defines the abstraction layer for all memory backends.
// Any memory system (built-in SQLite, external HTTP service, etc.) must
// implement this interface to be usable by GhostClaw.
type MemoryProvider interface {
	// ── Memory CRUD ───────────────────────────────────────────────
	SaveEntry(category MemoryCategory, key, value string, tags []string, scope MemoryScope) error
	GetEntry(category MemoryCategory, key string) (MemoryEntry, bool)
	DeleteEntry(category MemoryCategory, key string) error
	UpdateEntry(category MemoryCategory, key, newValue string, newTags []string) error
	SearchEntries(category MemoryCategory, query string, limit int) []MemoryEntry

	// ── Experience tracking ───────────────────────────────────────
	RecordExperience(taskDesc string, actions []ExperienceAction, result bool, sessionID string) error
	RetrieveExperiences(taskDesc string, limit int) []MemoryEntry
	UpdateExperienceRating(expID string, success bool)

	// ── Session tracking ──────────────────────────────────────────
	RecordSession(sessionID, channel, summary string, messageCount int, tags []string)
	GetRecentSessions(limit int) []SessionRecord

	// ── Prompt context assembly ───────────────────────────────────
	GetContextForPrompt(taskDesc string) string

	// ── Provider lifecycle ────────────────────────────────────────
	// Name returns a human-readable identifier for this provider.
	Name() string
	// Ping verifies connectivity. Returns nil if the provider is reachable.
	Ping() error
}

// ─── BuiltinMemoryProvider ───────────────────────────────────────────────────
// Wraps the existing UnifiedMemory (GORM/SQLite) so it satisfies MemoryProvider.
// This is the default provider shipped with GhostClaw.

type BuiltinMemoryProvider struct {
	inner *UnifiedMemory
}

// NewBuiltinMemoryProvider creates a built-in provider backed by UnifiedMemory.
func NewBuiltinMemoryProvider(workDir string) (*BuiltinMemoryProvider, error) {
	um, err := NewUnifiedMemory(workDir)
	if err != nil {
		return nil, fmt.Errorf("builtin provider init failed: %w", err)
	}
	return &BuiltinMemoryProvider{inner: um}, nil
}

// Name returns the provider identifier.
func (p *BuiltinMemoryProvider) Name() string { return "builtin" }

// Ping always succeeds for the built-in provider (DB already open).
func (p *BuiltinMemoryProvider) Ping() error { return nil }

func (p *BuiltinMemoryProvider) SaveEntry(category MemoryCategory, key, value string, tags []string, scope MemoryScope) error {
	return p.inner.SaveEntry(category, key, value, tags, scope)
}

func (p *BuiltinMemoryProvider) GetEntry(category MemoryCategory, key string) (MemoryEntry, bool) {
	return p.inner.GetEntry(category, key)
}

func (p *BuiltinMemoryProvider) DeleteEntry(category MemoryCategory, key string) error {
	return p.inner.DeleteEntry(category, key)
}

func (p *BuiltinMemoryProvider) UpdateEntry(category MemoryCategory, key, newValue string, newTags []string) error {
	return p.inner.UpdateEntry(category, key, newValue, newTags)
}

func (p *BuiltinMemoryProvider) SearchEntries(category MemoryCategory, query string, limit int) []MemoryEntry {
	return p.inner.SearchEntries(category, query, limit)
}

func (p *BuiltinMemoryProvider) RecordExperience(taskDesc string, actions []ExperienceAction, result bool, sessionID string) error {
	return p.inner.RecordExperience(taskDesc, actions, result, sessionID)
}

func (p *BuiltinMemoryProvider) RetrieveExperiences(taskDesc string, limit int) []MemoryEntry {
	return p.inner.RetrieveExperiences(taskDesc, limit)
}

func (p *BuiltinMemoryProvider) UpdateExperienceRating(expID string, success bool) {
	p.inner.UpdateExperienceRating(expID, success)
}

func (p *BuiltinMemoryProvider) RecordSession(sessionID, channel, summary string, messageCount int, tags []string) {
	p.inner.RecordSession(sessionID, channel, summary, messageCount, tags)
}

func (p *BuiltinMemoryProvider) GetRecentSessions(limit int) []SessionRecord {
	return p.inner.GetRecentSessions(limit)
}

func (p *BuiltinMemoryProvider) GetContextForPrompt(taskDesc string) string {
	return p.inner.GetContextForPrompt(taskDesc)
}

// Inner exposes the underlying UnifiedMemory for code that has not yet been
// migrated to the provider interface.
func (p *BuiltinMemoryProvider) Inner() *UnifiedMemory { return p.inner }

// ─── ExternalProviderConfig ──────────────────────────────────────────────────
// Holds the configuration needed to connect to an external memory service.

type ExternalProviderConfig struct {
	// Name is a human-readable label (e.g. "redis-memory").
	Name string `json:"name" yaml:"name"`
	// EndpointURL is the base URL of the external memory service.
	EndpointURL string `json:"endpoint_url" yaml:"endpoint_url"`
	// APIKey is an optional bearer token or API key.
	APIKey string `json:"api_key" yaml:"api_key"`
	// TimeoutSeconds controls HTTP request timeout (0 = no deadline).
	TimeoutSeconds int `json:"timeout_seconds" yaml:"timeout_seconds"`
	// Enabled allows the provider to be registered but disabled.
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// ─── ExternalMemoryProvider ──────────────────────────────────────────────────
// A stub provider that logs every call but does not perform real I/O yet.
// When the external service becomes unavailable the manager transparently
// falls back to the next registered provider.

type ExternalMemoryProvider struct {
	config ExternalProviderConfig
}

// NewExternalMemoryProvider creates an external memory provider from config.
func NewExternalMemoryProvider(cfg ExternalProviderConfig) *ExternalMemoryProvider {
	return &ExternalMemoryProvider{config: cfg}
}

// Name returns the configured provider name, defaulting to "external".
func (p *ExternalMemoryProvider) Name() string {
	if p.config.Name != "" {
		return p.config.Name
	}
	return "external"
}

// Ping logs and returns an error indicating the provider is a stub.
func (p *ExternalMemoryProvider) Ping() error {
	log.Printf("[MemoryProvider:%s] Ping — stub provider, endpoint=%s", p.Name(), p.config.EndpointURL)
	return fmt.Errorf("external memory provider %q is a stub (no HTTP integration yet)", p.Name())
}

func (p *ExternalMemoryProvider) SaveEntry(category MemoryCategory, key, value string, tags []string, scope MemoryScope) error {
	log.Printf("[MemoryProvider:%s] SaveEntry category=%q key=%q scope=%q — stub, no-op", p.Name(), category, key, scope)
	return fmt.Errorf("stub: not implemented")
}

func (p *ExternalMemoryProvider) GetEntry(category MemoryCategory, key string) (MemoryEntry, bool) {
	log.Printf("[MemoryProvider:%s] GetEntry category=%q key=%q — stub, returning empty", p.Name(), category, key)
	return MemoryEntry{}, false
}

func (p *ExternalMemoryProvider) DeleteEntry(category MemoryCategory, key string) error {
	log.Printf("[MemoryProvider:%s] DeleteEntry category=%q key=%q — stub, no-op", p.Name(), category, key)
	return fmt.Errorf("stub: not implemented")
}

func (p *ExternalMemoryProvider) UpdateEntry(category MemoryCategory, key, newValue string, newTags []string) error {
	log.Printf("[MemoryProvider:%s] UpdateEntry category=%q key=%q — stub, no-op", p.Name(), category, key)
	return fmt.Errorf("stub: not implemented")
}

func (p *ExternalMemoryProvider) SearchEntries(category MemoryCategory, query string, limit int) []MemoryEntry {
	log.Printf("[MemoryProvider:%s] SearchEntries category=%q query=%q limit=%d — stub, returning empty", p.Name(), category, query, limit)
	return nil
}

func (p *ExternalMemoryProvider) RecordExperience(taskDesc string, actions []ExperienceAction, result bool, sessionID string) error {
	log.Printf("[MemoryProvider:%s] RecordExperience taskDesc=%q result=%v sessionID=%q — stub, no-op", p.Name(), taskDesc, result, sessionID)
	return fmt.Errorf("stub: not implemented")
}

func (p *ExternalMemoryProvider) RetrieveExperiences(taskDesc string, limit int) []MemoryEntry {
	log.Printf("[MemoryProvider:%s] RetrieveExperiences taskDesc=%q limit=%d — stub, returning empty", p.Name(), taskDesc, limit)
	return nil
}

func (p *ExternalMemoryProvider) UpdateExperienceRating(expID string, success bool) {
	log.Printf("[MemoryProvider:%s] UpdateExperienceRating expID=%q success=%v — stub, no-op", p.Name(), expID, success)
}

func (p *ExternalMemoryProvider) RecordSession(sessionID, channel, summary string, messageCount int, tags []string) {
	log.Printf("[MemoryProvider:%s] RecordSession sessionID=%q channel=%q — stub, no-op", p.Name(), sessionID, channel)
}

func (p *ExternalMemoryProvider) GetRecentSessions(limit int) []SessionRecord {
	log.Printf("[MemoryProvider:%s] GetRecentSessions limit=%d — stub, returning empty", p.Name(), limit)
	return nil
}

func (p *ExternalMemoryProvider) GetContextForPrompt(taskDesc string) string {
	log.Printf("[MemoryProvider:%s] GetContextForPrompt taskDesc=%q — stub, returning empty", p.Name(), taskDesc)
	return ""
}

// Config returns a copy of the provider's configuration.
func (p *ExternalMemoryProvider) Config() ExternalProviderConfig {
	return p.config
}

// ─── MemoryProviderManager ───────────────────────────────────────────────────
// Manages one primary MemoryProvider and an ordered list of fallback providers.
// Every write/read call goes to the primary first; on error it iterates through
// the fallback chain.

type MemoryProviderManager struct {
	mu        sync.RWMutex
	providers map[string]MemoryProvider // name -> provider
	primary   string                    // name of the active primary provider
	fallbacks []string                  // ordered fallback names (after primary)
}

// NewMemoryProviderManager creates an empty manager.
func NewMemoryProviderManager() *MemoryProviderManager {
	return &MemoryProviderManager{
		providers: make(map[string]MemoryProvider),
	}
}

// RegisterProvider adds a named provider to the manager.
func (m *MemoryProviderManager) RegisterProvider(name string, provider MemoryProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[name] = provider
	// If this is the very first provider, make it primary automatically.
	if m.primary == "" {
		m.primary = name
		log.Printf("[MemoryProviderManager] auto-set primary=%q", name)
	}
	log.Printf("[MemoryProviderManager] registered provider %q", name)
}

// SetPrimary promotes a registered provider to the primary slot.
// Returns an error if the name is not registered.
func (m *MemoryProviderManager) SetPrimary(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.providers[name]; !ok {
		return fmt.Errorf("provider %q not registered", name)
	}
	m.primary = name
	log.Printf("[MemoryProviderManager] primary set to %q", name)
	return nil
}

// GetProvider returns a named provider and whether it exists.
func (m *MemoryProviderManager) GetProvider(name string) (MemoryProvider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[name]
	return p, ok
}

// PrimaryName returns the name of the current primary provider.
func (m *MemoryProviderManager) PrimaryName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.primary
}

// AddFallback appends a provider name to the fallback chain.
func (m *MemoryProviderManager) AddFallback(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.providers[name]; !ok {
		return fmt.Errorf("provider %q not registered", name)
	}
	m.fallbacks = append(m.fallbacks, name)
	log.Printf("[MemoryProviderManager] added fallback %q (chain: %v)", name, m.fallbacks)
	return nil
}

// SetFallbacks replaces the entire fallback chain.
func (m *MemoryProviderManager) SetFallbacks(names []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range names {
		if _, ok := m.providers[n]; !ok {
			return fmt.Errorf("fallback provider %q not registered", n)
		}
	}
	m.fallbacks = append([]string{}, names...)
	log.Printf("[MemoryProviderManager] fallback chain set to %v", m.fallbacks)
	return nil
}

// ProviderNames returns all registered provider names.
func (m *MemoryProviderManager) ProviderNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.providers))
	for n := range m.providers {
		names = append(names, n)
	}
	return names
}

// resolveChain returns the primary followed by fallbacks, all validated.
func (m *MemoryProviderManager) resolveChain() []MemoryProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	chain := make([]MemoryProvider, 0, 1+len(m.fallbacks))
	if p, ok := m.providers[m.primary]; ok {
		chain = append(chain, p)
	}
	for _, n := range m.fallbacks {
		if p, ok := m.providers[n]; ok {
			chain = append(chain, p)
		}
	}
	return chain
}

// ── Prefetch / Sync lifecycle ────────────────────────────────────────────────

// Prefetch signals all registered providers to warm their caches. Errors are
// logged but do not stop the remaining providers.
func (m *MemoryProviderManager) Prefetch() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	log.Println("[MemoryProviderManager] Prefetch() called")
	for name, p := range m.providers {
		if err := p.Ping(); err != nil {
			log.Printf("[MemoryProviderManager] Prefetch: provider %q ping failed: %v", name, err)
		} else {
			log.Printf("[MemoryProviderManager] Prefetch: provider %q ready", name)
		}
	}
}

// Sync pushes any local state to external providers and vice-versa.
// For the built-in provider this is a no-op; external providers can use
// this to upload queued writes.
func (m *MemoryProviderManager) Sync() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	log.Println("[MemoryProviderManager] Sync() called")
	for name, p := range m.providers {
		if err := p.Ping(); err != nil {
			log.Printf("[MemoryProviderManager] Sync: provider %q not reachable: %v", name, err)
		}
	}
	return nil
}

// ── Memory CRUD delegation ───────────────────────────────────────────────────

func (m *MemoryProviderManager) SaveEntry(category MemoryCategory, key, value string, tags []string, scope MemoryScope) error {
	for _, p := range m.resolveChain() {
		if err := p.SaveEntry(category, key, value, tags, scope); err != nil {
			log.Printf("[MemoryProviderManager] SaveEntry failed on %q: %v", p.Name(), err)
			continue
		}
		return nil
	}
	return fmt.Errorf("SaveEntry: all providers failed for category=%q key=%q", category, key)
}

func (m *MemoryProviderManager) GetEntry(category MemoryCategory, key string) (MemoryEntry, bool) {
	for _, p := range m.resolveChain() {
		if entry, ok := p.GetEntry(category, key); ok {
			return entry, true
		}
	}
	return MemoryEntry{}, false
}

func (m *MemoryProviderManager) DeleteEntry(category MemoryCategory, key string) error {
	for _, p := range m.resolveChain() {
		if err := p.DeleteEntry(category, key); err != nil {
			log.Printf("[MemoryProviderManager] DeleteEntry failed on %q: %v", p.Name(), err)
			continue
		}
		return nil
	}
	return fmt.Errorf("DeleteEntry: all providers failed for category=%q key=%q", category, key)
}

func (m *MemoryProviderManager) UpdateEntry(category MemoryCategory, key, newValue string, newTags []string) error {
	for _, p := range m.resolveChain() {
		if err := p.UpdateEntry(category, key, newValue, newTags); err != nil {
			log.Printf("[MemoryProviderManager] UpdateEntry failed on %q: %v", p.Name(), err)
			continue
		}
		return nil
	}
	return fmt.Errorf("UpdateEntry: all providers failed for category=%q key=%q", category, key)
}

func (m *MemoryProviderManager) SearchEntries(category MemoryCategory, query string, limit int) []MemoryEntry {
	for _, p := range m.resolveChain() {
		if entries := p.SearchEntries(category, query, limit); len(entries) > 0 {
			return entries
		}
	}
	return nil
}

// ── Experience delegation ────────────────────────────────────────────────────

func (m *MemoryProviderManager) RecordExperience(taskDesc string, actions []ExperienceAction, result bool, sessionID string) error {
	for _, p := range m.resolveChain() {
		if err := p.RecordExperience(taskDesc, actions, result, sessionID); err != nil {
			log.Printf("[MemoryProviderManager] RecordExperience failed on %q: %v", p.Name(), err)
			continue
		}
		return nil
	}
	return fmt.Errorf("RecordExperience: all providers failed for taskDesc=%q", taskDesc)
}

func (m *MemoryProviderManager) RetrieveExperiences(taskDesc string, limit int) []MemoryEntry {
	for _, p := range m.resolveChain() {
		if entries := p.RetrieveExperiences(taskDesc, limit); len(entries) > 0 {
			return entries
		}
	}
	return nil
}

func (m *MemoryProviderManager) UpdateExperienceRating(expID string, success bool) {
	// Best-effort: call every provider in the chain.
	for _, p := range m.resolveChain() {
		p.UpdateExperienceRating(expID, success)
	}
}

// ── Session delegation ───────────────────────────────────────────────────────

func (m *MemoryProviderManager) RecordSession(sessionID, channel, summary string, messageCount int, tags []string) {
	// Best-effort across all providers.
	for _, p := range m.resolveChain() {
		p.RecordSession(sessionID, channel, summary, messageCount, tags)
	}
}

func (m *MemoryProviderManager) GetRecentSessions(limit int) []SessionRecord {
	for _, p := range m.resolveChain() {
		if records := p.GetRecentSessions(limit); len(records) > 0 {
			return records
		}
	}
	return nil
}

// ── Prompt context delegation ────────────────────────────────────────────────

func (m *MemoryProviderManager) GetContextForPrompt(taskDesc string) string {
	for _, p := range m.resolveChain() {
		if ctx := p.GetContextForPrompt(taskDesc); ctx != "" {
			return ctx
		}
	}
	return ""
}

// ── MemoryProvider interface compliance for the manager itself ───────────────
// The manager also satisfies MemoryProvider so it can be dropped in wherever a
// MemoryProvider is expected.

// Ensure compile-time check.
var _ MemoryProvider = (*MemoryProviderManager)(nil)

func (m *MemoryProviderManager) Name() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return "manager(primary=" + m.primary + ")"
}

func (m *MemoryProviderManager) Ping() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.primary == "" {
		return fmt.Errorf("no primary provider configured")
	}
	p, ok := m.providers[m.primary]
	if !ok {
		return fmt.Errorf("primary provider %q not found", m.primary)
	}
	return p.Ping()
}
