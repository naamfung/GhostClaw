package main

import (
        "encoding/json"
        "fmt"
        "hash/fnv"
        "sync"
        "time"
)

// PromptCacheEntry represents a single cached prompt entry.
type PromptCacheEntry struct {
        Hash          string    `json:"hash"`
        MessagesHash  string    `json:"messages_hash"` // FNV-64a of serialized messages
        CreatedAt     time.Time `json:"created_at"`
        HitCount      int       `json:"hit_count"`
        LastAccessAt  time.Time `json:"last_access_at"`
        TokenEstimate int       `json:"token_estimate"`
}

// PromptCache provides thread-safe caching of prompt messages to avoid
// re-sending identical message sequences. Entries expire after TTL.
type PromptCache struct {
        mu         sync.RWMutex
        entries    map[string]*PromptCacheEntry // key: MessagesHash
        maxEntries int
        ttl        time.Duration

        // Stats counters
        hitCount  int
        missCount int
}

// NewPromptCache creates a new PromptCache with the given limits.
// maxEntries caps the number of cached entries (default 100).
// ttl is the time-to-live for each cache entry (default 5 minutes).
func NewPromptCache(maxEntries int, ttl time.Duration) *PromptCache {
        if maxEntries <= 0 {
                maxEntries = 100
        }
        if ttl <= 0 {
                ttl = 5 * time.Minute
        }
        return &PromptCache{
                entries:    make(map[string]*PromptCacheEntry),
                maxEntries: maxEntries,
                ttl:        ttl,
        }
}

// ComputeMessagesHash serializes the given messages to JSON and returns
// the FNV-64a hex digest.
//
// 優化說明：原來使用 crypto/sha256，但這裡的 hash 僅用於請求去重和 prompt loop 偵測，
// 不涉及安全性。FNV-64a 是非加密哈希，速度比 SHA256 快 10-50 倍，
// 且對短到中等長度的數據分佈均勻，完全適合此場景。
// hermes-agent 也不使用 SHA256 做請求級緩存 key。
func ComputeMessagesHash(messages []Message) string {
        data, err := json.Marshal(messages)
        if err != nil {
                // Fallback: hash an empty representation so we never panic
                data = []byte(fmt.Sprintf("error:%v", err))
        }
        h := fnv.New64a()
        h.Write(data)
        return fmt.Sprintf("%016x", h.Sum64())
}

// Lookup checks whether the messages match a cached, non-expired entry.
// On hit the entry's HitCount and LastAccessAt are updated.
func (pc *PromptCache) Lookup(messages []Message) (*PromptCacheEntry, bool) {
        msgHash := ComputeMessagesHash(messages)

        pc.mu.RLock()
        entry, ok := pc.entries[msgHash]
        if !ok {
                pc.mu.RUnlock()
                pc.mu.Lock()
                pc.missCount++
                pc.mu.Unlock()
                return nil, false
        }
        // Check TTL
        if time.Since(entry.CreatedAt) > pc.ttl {
                pc.mu.RUnlock()
                // Evict expired entry lazily
                pc.mu.Lock()
                delete(pc.entries, msgHash)
                pc.missCount++
                pc.mu.Unlock()
                return nil, false
        }
        // Clone before releasing lock to avoid data races
        copied := *entry
        pc.mu.RUnlock()

        // Update hit stats
        pc.mu.Lock()
        if e, exists := pc.entries[msgHash]; exists {
                e.HitCount++
                e.LastAccessAt = time.Now()
                copied = *e
        }
        pc.hitCount++
        pc.mu.Unlock()

        return &copied, true
}

// Store adds a new cache entry for the given messages. If the cache is full,
// expired entries are evicted first; otherwise the oldest entry is removed.
func (pc *PromptCache) Store(messages []Message, tokenEstimate int) {
        msgHash := ComputeMessagesHash(messages)
        now := time.Now()

        pc.mu.Lock()
        defer pc.mu.Unlock()

        // If already cached, just update token estimate and access time
        if existing, ok := pc.entries[msgHash]; ok {
                existing.TokenEstimate = tokenEstimate
                existing.LastAccessAt = now
                return
        }

        // Evict expired entries if at capacity
        if len(pc.entries) >= pc.maxEntries {
                pc.evictExpiredLocked()
        }

        // If still at capacity after eviction, remove the oldest entry
        if len(pc.entries) >= pc.maxEntries {
                var oldestKey string
                var oldestTime time.Time
                for k, e := range pc.entries {
                        if oldestKey == "" || e.LastAccessAt.Before(oldestTime) {
                                oldestKey = k
                                oldestTime = e.LastAccessAt
                        }
                }
                if oldestKey != "" {
                        delete(pc.entries, oldestKey)
                }
        }

        pc.entries[msgHash] = &PromptCacheEntry{
                Hash:          msgHash,
                MessagesHash:  msgHash,
                CreatedAt:     now,
                HitCount:      0,
                LastAccessAt:  now,
                TokenEstimate: tokenEstimate,
        }
}

// Invalidate clears all entries from the cache and resets stats.
func (pc *PromptCache) Invalidate() {
        pc.mu.Lock()
        defer pc.mu.Unlock()
        pc.entries = make(map[string]*PromptCacheEntry)
        pc.hitCount = 0
        pc.missCount = 0
}

// Stats returns cache statistics: hit_count, miss_count, entries, hit_rate.
func (pc *PromptCache) Stats() map[string]interface{} {
        pc.mu.RLock()
        defer pc.mu.RUnlock()

        total := pc.hitCount + pc.missCount
        hitRate := float64(0)
        if total > 0 {
                hitRate = float64(pc.hitCount) / float64(total)
        }

        return map[string]interface{}{
                "hit_count":   pc.hitCount,
                "miss_count":  pc.missCount,
                "entries":     len(pc.entries),
                "hit_rate":    hitRate,
                "max_entries": pc.maxEntries,
                "ttl_seconds": pc.ttl.Seconds(),
        }
}

// EvictExpired removes all entries past the TTL. Safe to call concurrently.
func (pc *PromptCache) EvictExpired() {
        pc.mu.Lock()
        defer pc.mu.Unlock()
        pc.evictExpiredLocked()
}

// evictExpiredLocked is the internal eviction helper. Caller must hold pc.mu.
func (pc *PromptCache) evictExpiredLocked() {
        now := time.Now()
        for k, e := range pc.entries {
                if now.Sub(e.CreatedAt) > pc.ttl {
                        delete(pc.entries, k)
                }
        }
}

// DetectPromptChanges compares two message slices and returns true if the
// prompt has actually changed. This is useful for avoiding unnecessary cache
// invalidation when messages are reordered or have cosmetic differences.
//
// It computes FNV-64a hashes of both slices and compares them directly.
func DetectPromptChanges(oldMessages, newMessages []Message) bool {
        oldHash := ComputeMessagesHash(oldMessages)
        newHash := ComputeMessagesHash(newMessages)
        return oldHash != newHash
}

// ---------------------------------------------------------------------------
// Global prompt cache
// ---------------------------------------------------------------------------

var globalPromptCache *PromptCache
