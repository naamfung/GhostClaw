package main

import (
        "errors"
        "fmt"
        "sort"
        "sync"
        "time"
)

// ProviderConfig describes a single LLM provider endpoint.
type ProviderConfig struct {
        Name            string        `json:"name"`
        BaseURL         string        `json:"base_url"`
        APIKey          string        `json:"api_key"`
        Priority        int           `json:"priority"`         // lower = higher priority
        Enabled         bool          `json:"enabled"`
        MaxRetries      int           `json:"max_retries"`      // default 3
        CooldownSeconds int           `json:"cooldown_seconds"` // default 60
}

// ProviderHealth captures the live health status of a provider.
type ProviderHealth struct {
        Name               string    `json:"name"`
        Healthy            bool      `json:"healthy"`
        LastError          string    `json:"last_error"`
        LastSuccessTime    time.Time `json:"last_success_time"`
        ConsecutiveFailures int      `json:"consecutive_failures"`
        CooldownUntil      time.Time `json:"cooldown_until"`
}

// ProviderFailoverChain manages multiple LLM provider endpoints with automatic
// failover, cooldown, and health tracking. All methods are safe for concurrent
// use.
type ProviderFailoverChain struct {
        mu        sync.RWMutex
        providers map[string]*ProviderConfig
        health    map[string]*ProviderHealth
}

// NewProviderFailoverChain creates an empty failover chain.
func NewProviderFailoverChain() *ProviderFailoverChain {
        return &ProviderFailoverChain{
                providers: make(map[string]*ProviderConfig),
                health:    make(map[string]*ProviderHealth),
        }
}

// RegisterProvider adds (or replaces) a provider configuration in the chain.
// Sensible defaults are applied when MaxRetries or CooldownSeconds are zero.
func (c *ProviderFailoverChain) RegisterProvider(config ProviderConfig) {
        c.mu.Lock()
        defer c.mu.Unlock()

        if config.MaxRetries <= 0 {
                config.MaxRetries = 3
        }
        if config.CooldownSeconds <= 0 {
                config.CooldownSeconds = 60
        }

        c.providers[config.Name] = &config

        // Preserve existing health state if we already tracked this provider.
        if _, exists := c.health[config.Name]; !exists {
                c.health[config.Name] = &ProviderHealth{
                        Name:    config.Name,
                        Healthy: true,
                }
        }
}

// UnregisterProvider removes a provider from the chain.
func (c *ProviderFailoverChain) UnregisterProvider(name string) {
        c.mu.Lock()
        defer c.mu.Unlock()

        delete(c.providers, name)
        delete(c.health, name)
}

// GetActiveProvider returns the highest-priority provider that is both enabled
// and healthy (not in cooldown). If no provider is available an error is
// returned.
func (c *ProviderFailoverChain) GetActiveProvider() (*ProviderConfig, error) {
        c.mu.RLock()
        defer c.mu.RUnlock()

        now := time.Now()

        // Collect enabled, healthy providers not in cooldown.
        candidates := make([]*ProviderConfig, 0, len(c.providers))
        for name, cfg := range c.providers {
                if !cfg.Enabled {
                        continue
                }
                h := c.health[name]
                if h == nil || !h.Healthy {
                        continue
                }
                // Check cooldown expiry.
                if !h.CooldownUntil.IsZero() && now.Before(h.CooldownUntil) {
                        continue
                }
                candidates = append(candidates, cfg)
        }

        if len(candidates) == 0 {
                return nil, errors.New("no active providers available: all are disabled, unhealthy, or in cooldown")
        }

        // Sort by priority (ascending — lower number = higher priority).
        sort.Slice(candidates, func(i, j int) bool {
                return candidates[i].Priority < candidates[j].Priority
        })

        return candidates[0], nil
}

// ReportSuccess resets the failure counter for the named provider and records
// the current time as the last success.
func (c *ProviderFailoverChain) ReportSuccess(providerName string) {
        c.mu.Lock()
        defer c.mu.Unlock()

        h, ok := c.health[providerName]
        if !ok {
                return
        }

        h.Healthy = true
        h.ConsecutiveFailures = 0
        h.LastError = ""
        h.LastSuccessTime = time.Now()
        h.CooldownUntil = time.Time{}
}

// ReportFailure increments the consecutive failure counter for the named
// provider. Once the counter reaches MaxRetries the provider is marked
// unhealthy and placed in cooldown for CooldownSeconds.
func (c *ProviderFailoverChain) ReportFailure(providerName string, err error) {
        c.mu.Lock()
        defer c.mu.Unlock()

        h, ok := c.health[providerName]
        if !ok {
                return
        }

        h.ConsecutiveFailures++
        h.LastError = ""

        if err != nil {
                h.LastError = err.Error()
        }

        cfg, cfgOK := c.providers[providerName]
        if !cfgOK {
                return
        }

        // Enter cooldown when failures exceed allowed retries.
        if h.ConsecutiveFailures >= cfg.MaxRetries {
                h.Healthy = false
                h.CooldownUntil = time.Now().Add(time.Duration(cfg.CooldownSeconds) * time.Second)
        }
}

// IsHealthy returns whether the named provider is currently considered healthy.
// A provider in active cooldown is reported as unhealthy.
func (c *ProviderFailoverChain) IsHealthy(providerName string) bool {
        c.mu.RLock()
        defer c.mu.RUnlock()

        h, ok := c.health[providerName]
        if !ok {
                return false
        }
        if !h.Healthy {
                return false
        }
        if !h.CooldownUntil.IsZero() && time.Now().Before(h.CooldownUntil) {
                return false
        }
        return true
}

// GetAllHealth returns a snapshot of health status for every registered provider.
func (c *ProviderFailoverChain) GetAllHealth() []ProviderHealth {
        c.mu.RLock()
        defer c.mu.RUnlock()

        result := make([]ProviderHealth, 0, len(c.health))
        for _, h := range c.health {
                snapshot := *h

                // If cooldown has elapsed, report healthy status even if the flag
                // hasn't been explicitly reset yet (lazy recovery).
                if !snapshot.Healthy && !snapshot.CooldownUntil.IsZero() && time.Now().After(snapshot.CooldownUntil) {
                        snapshot.Healthy = true
                }

                result = append(result, snapshot)
        }

        // Sort by provider name for deterministic output.
        sort.Slice(result, func(i, j int) bool {
                return result[i].Name < result[j].Name
        })

        return result
}

// GetProviderConfig returns the configuration for a named provider, or nil if
// the provider is not registered.
func (c *ProviderFailoverChain) GetProviderConfig(name string) *ProviderConfig {
        c.mu.RLock()
        defer c.mu.RUnlock()
        return c.providers[name]
}

// ProviderCount returns the total number of registered providers.
func (c *ProviderFailoverChain) ProviderCount() int {
        c.mu.RLock()
        defer c.mu.RUnlock()
        return len(c.providers)
}

// ResetHealth manually resets a provider's health state, clearing failures and
// cooldown. Useful for administrative overrides.
func (c *ProviderFailoverChain) ResetHealth(providerName string) error {
        c.mu.Lock()
        defer c.mu.Unlock()

        if _, ok := c.providers[providerName]; !ok {
                return fmt.Errorf("provider %q not registered", providerName)
        }

        c.health[providerName] = &ProviderHealth{
                Name:    providerName,
                Healthy: true,
        }
        return nil
}

// Providers returns a sorted list of all registered provider names.
func (c *ProviderFailoverChain) Providers() []string {
        c.mu.RLock()
        defer c.mu.RUnlock()

        names := make([]string, 0, len(c.providers))
        for name := range c.providers {
                names = append(names, name)
        }
        sort.Strings(names)
        return names
}

// ---------------------------------------------------------------------------
// Global provider failover chain
// ---------------------------------------------------------------------------

var globalProviderFailover *ProviderFailoverChain
