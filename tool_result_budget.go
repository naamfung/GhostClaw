package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Default threshold constants (in characters)
const (
	DefaultToolResultThreshold = 10000
	ShellToolThreshold         = 15000
	BrowserToolThreshold       = 20000
	FileToolThreshold          = 10000
	PreviewHeadChars           = 2000 // number of characters for head preview
	PreviewTailChars           = 500  // number of characters for tail preview
)

// ToolResultBudget manages per-tool result size thresholds and caches
// oversized results to disk, returning a preview snippet instead.
type ToolResultBudget struct {
	mu           sync.RWMutex
	thresholds   map[string]int // per-tool name -> max chars threshold
	defaultLimit int            // fallback threshold for unconfigured tools
	cacheDir     string         // directory where oversized results are persisted
}

// toolCategoryPrefixes maps keyword prefixes to their tool category thresholds.
// A tool name is matched against these prefixes in order; the first match wins.
var toolCategoryPrefixes = []struct {
	prefix   string
	threshold int
}{
	{"shell", ShellToolThreshold},
	{"smart_shell", ShellToolThreshold},
	{"ssh", ShellToolThreshold},
	{"browser", BrowserToolThreshold},
	{"read_file", FileToolThreshold},
	{"read_all", FileToolThreshold},
	{"write_file", FileToolThreshold},
	{"write_all", FileToolThreshold},
	{"append_file", FileToolThreshold},
	{"mcp", 10000},
}

// NewToolResultBudget creates a new ToolResultBudget with sensible defaults.
// cacheDir is the base directory for storing oversized tool results.
// If cacheDir is empty it defaults to "tool_results_cache" relative to the
// current working directory.
func NewToolResultBudget(cacheDir string) *ToolResultBudget {
	if cacheDir == "" {
		cacheDir = "tool_results_cache"
	}
	b := &ToolResultBudget{
		thresholds:   make(map[string]int),
		defaultLimit: DefaultToolResultThreshold,
		cacheDir:     cacheDir,
	}
	b.applyDefaultCategoryThresholds()
	return b
}

// applyDefaultCategoryThresholds pre-populates thresholds for well-known tool
// name patterns.  Explicit calls to SetToolThreshold override these.
func (b *ToolResultBudget) applyDefaultCategoryThresholds() {
	for _, entry := range toolCategoryPrefixes {
		b.thresholds[entry.prefix] = entry.threshold
	}
}

// SetToolThreshold sets a per-tool character threshold.  Pass a maxChars value
// of 0 or negative to remove the override and fall back to category/default.
func (b *ToolResultBudget) SetToolThreshold(toolName string, maxChars int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if maxChars <= 0 {
		delete(b.thresholds, toolName)
		return
	}
	b.thresholds[toolName] = maxChars
}

// SetDefaultThreshold changes the fallback threshold used when no specific
// tool or category threshold matches.
func (b *ToolResultBudget) SetDefaultThreshold(maxChars int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.defaultLimit = maxChars
}

// resolveThreshold returns the effective character threshold for the given tool.
func (b *ToolResultBudget) resolveThreshold(toolName string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// 1. Exact tool-name override
	if t, ok := b.thresholds[toolName]; ok {
		return t
	}

	// 2. Category prefix match (longest prefix wins)
	toolLower := strings.ToLower(toolName)
	bestLen := 0
	bestThreshold := 0
	for _, entry := range toolCategoryPrefixes {
		if strings.HasPrefix(toolLower, entry.prefix) && len(entry.prefix) > bestLen {
			bestLen = len(entry.prefix)
			bestThreshold = entry.threshold
		}
	}
	if bestLen > 0 {
		return bestThreshold
	}

	// 3. Global default
	return b.defaultLimit
}

// CheckAndPersistResult inspects a tool result.  If its rune count exceeds the
// configured threshold for the tool, the full result is persisted to disk under
// b.cacheDir and a preview snippet with metadata is returned.  Otherwise the
// original result is returned unchanged.
func (b *ToolResultBudget) CheckAndPersistResult(toolName, result string) string {
	threshold := b.resolveThreshold(toolName)
	runes := []rune(result)
	charCount := len(runes)

	if charCount <= threshold {
		return result
	}

	// Persist the full result to disk
	filePath, err := b.persistResult(toolName, result)
	if err != nil {
		// If persistence fails, fall back to an in-memory truncation so we
		// still respect the budget.
		return b.truncateInMemory(result, charCount, threshold, err)
	}

	return b.buildPreview(toolName, charCount, filePath, result)
}

// persistResult atomically writes the full result to a uniquely-named file
// inside cacheDir and returns the relative file path.
func (b *ToolResultBudget) persistResult(toolName, result string) (string, error) {
	// Ensure cache directory exists
	if err := os.MkdirAll(b.cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory %q: %w", b.cacheDir, err)
	}

	// Generate a unique file name: <tool_name>_<unix_nano>.txt
	sanitised := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, toolName)
	if sanitised == "" {
		sanitised = "unknown_tool"
	}

	fileName := fmt.Sprintf("%s_%d.txt", sanitised, time.Now().UnixNano())
	fullPath := filepath.Join(b.cacheDir, fileName)

	// Write to a temporary file first, then rename for atomicity
	tmpPath := fullPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("failed to write temp file %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		os.Remove(tmpPath) // best-effort cleanup
		return "", fmt.Errorf("failed to rename temp file to %q: %w", fullPath, err)
	}

	return fullPath, nil
}

// buildPreview creates a preview snippet containing metadata and head/tail
// excerpts of the original result.
func (b *ToolResultBudget) buildPreview(toolName string, charCount int, filePath, result string) string {
	runes := []rune(result)

	var sb strings.Builder

	sb.WriteString("[TOOL_RESULT_TRUNCATED]\n")
	sb.WriteString(fmt.Sprintf("Tool: %s\n", toolName))
	sb.WriteString(fmt.Sprintf("Original size: %d chars\n", charCount))
	sb.WriteString(fmt.Sprintf("Threshold: %d chars\n", b.resolveThreshold(toolName)))
	sb.WriteString(fmt.Sprintf("Cached to: %s\n", filePath))
	sb.WriteString("\n")

	// Head preview
	sb.WriteString(fmt.Sprintf("--- Preview (first %d chars) ---\n", PreviewHeadChars))
	if len(runes) > PreviewHeadChars {
		sb.WriteString(string(runes[:PreviewHeadChars]))
	} else {
		sb.WriteString(result)
	}
	sb.WriteString("\n\n")

	// Tail preview
	tailStart := len(runes) - PreviewTailChars
	if tailStart < PreviewHeadChars {
		tailStart = PreviewHeadChars // avoid overlap
	}
	if tailStart < len(runes) {
		sb.WriteString(fmt.Sprintf("--- Preview (last %d chars) ---\n", PreviewTailChars))
		sb.WriteString(string(runes[tailStart:]))
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\nFull result available at: %s\n", filePath))

	return sb.String()
}

// truncateInMemory is the fallback when disk persistence fails.
func (b *ToolResultBudget) truncateInMemory(result string, charCount, threshold int, persistErr error) string {
	runes := []rune(result)

	var sb strings.Builder
	sb.WriteString("[TOOL_RESULT_TRUNCATED (disk persist failed)]\n")
	sb.WriteString(fmt.Sprintf("Tool result size: %d chars (threshold: %d)\n", charCount, threshold))
	sb.WriteString(fmt.Sprintf("Persist error: %v\n", persistErr))
	sb.WriteString(fmt.Sprintf("\n--- Preview (first %d chars) ---\n", PreviewHeadChars))

	if len(runes) > PreviewHeadChars {
		sb.WriteString(string(runes[:PreviewHeadChars]))
	} else {
		sb.WriteString(result)
	}

	sb.WriteString("\n\n... (content truncated due to budget)\n")
	return sb.String()
}

// GetCachedResult reads and returns the full tool result that was previously
// persisted to the given file path.
func GetCachedResult(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read cached result from %q: %w", filePath, err)
	}
	return string(data), nil
}

// CleanOldCache removes cached tool result files older than maxAge from the
// default cache directory ("tool_results_cache").
// It returns the number of files removed and any error encountered.
func CleanOldCache(maxAge time.Duration) (int, error) {
	return CleanOldCacheDir("tool_results_cache", maxAge)
}

// CleanOldCacheDir removes cached tool result files older than maxAge from the
// specified directory.  It returns the count of removed files and any error.
func CleanOldCacheDir(cacheDir string, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // nothing to clean
		}
		return 0, fmt.Errorf("failed to read cache directory %q: %w", cacheDir, err)
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	var firstErr error

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			fp := filepath.Join(cacheDir, entry.Name())
			if err := os.Remove(fp); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to remove %q: %w", fp, err)
				}
				continue
			}
			removed++
		}
	}

	return removed, firstErr
}

// --- global singleton ---

// globalToolResultBudget is the process-wide ToolResultBudget instance.
// It is lazily initialised on first use.
var (
	globalToolResultBudget     *ToolResultBudget
	globalToolResultBudgetOnce sync.Once
)

// GetGlobalToolResultBudget returns the global ToolResultBudget singleton.
func GetGlobalToolResultBudget() *ToolResultBudget {
	globalToolResultBudgetOnce.Do(func() {
		globalToolResultBudget = NewToolResultBudget("tool_results_cache")
	})
	return globalToolResultBudget
}

// --- cache directory size inspection ---

// CacheDirInfo holds statistics about the tool result cache directory.
type CacheDirInfo struct {
	TotalFiles   int
	TotalBytes   int64
	OldestFile   time.Time
	NewestFile   time.Time
	TopFiles     []FileCacheEntry // largest files first
}

// FileCacheEntry describes a single cached file.
type FileCacheEntry struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// GetCacheDirInfo scans the cache directory and returns usage statistics.
func (b *ToolResultBudget) GetCacheDirInfo() *CacheDirInfo {
	info := &CacheDirInfo{}

	entries, err := os.ReadDir(b.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return info
		}
		return info
	}

	var allFiles []FileCacheEntry

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fi, err := entry.Info()
		if err != nil {
			continue
		}
		fe := FileCacheEntry{
			Path:    filepath.Join(b.cacheDir, entry.Name()),
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		}
		allFiles = append(allFiles, fe)
		info.TotalFiles++
		info.TotalBytes += fi.Size()

		if info.OldestFile.IsZero() || fi.ModTime().Before(info.OldestFile) {
			info.OldestFile = fi.ModTime()
		}
		if fi.ModTime().After(info.NewestFile) {
			info.NewestFile = fi.ModTime()
		}
	}

	// Sort by size descending, keep top 10
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].Size > allFiles[j].Size
	})
	limit := len(allFiles)
	if limit > 10 {
		limit = 10
	}
	info.TopFiles = allFiles[:limit]

	return info
}
