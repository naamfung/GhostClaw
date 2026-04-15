package main

import (
        "encoding/json"
        "fmt"
        "math/rand"
        "os"
        "sync"
        "time"
)

// ToolsetDistribution defines a named group of tools with a sampling weight.
type ToolsetDistribution struct {
        Name        string   `json:"name"`
        ToolNames   []string `json:"tool_names"`
        Weight      float64  `json:"weight"`
        Description string   `json:"description"`
}

// ToolDistributionManager manages weighted distributions of toolsets,
// supporting random sampling and exclusion-based selection.
type ToolDistributionManager struct {
        mu            sync.RWMutex
        distributions []*ToolsetDistribution
        totalWeight   float64
        rng           *rand.Rand
}

// NewToolDistributionManager creates a new empty ToolDistributionManager
// with a seeded random source.
func NewToolDistributionManager() *ToolDistributionManager {
        return &ToolDistributionManager{
                distributions: make([]*ToolsetDistribution, 0),
                rng:           rand.New(rand.NewSource(time.Now().UnixNano())),
        }
}

// AddDistribution registers a new toolset distribution with the given weight.
// If a distribution with the same name already exists it is replaced.
func (tdm *ToolDistributionManager) AddDistribution(name string, toolNames []string, weight float64, description string) {
        tdm.mu.Lock()
        defer tdm.mu.Unlock()

        // Replace existing distribution with same name
        for i, d := range tdm.distributions {
                if d.Name == name {
                        tdm.totalWeight -= d.Weight
                        tdm.distributions[i] = &ToolsetDistribution{
                                Name:        name,
                                ToolNames:   append([]string{}, toolNames...),
                                Weight:      weight,
                                Description: description,
                        }
                        tdm.totalWeight += weight
                        return
                }
        }

        tdm.distributions = append(tdm.distributions, &ToolsetDistribution{
                Name:        name,
                ToolNames:   append([]string{}, toolNames...),
                Weight:      weight,
                Description: description,
        })
        tdm.totalWeight += weight
}

// RemoveDistribution removes a distribution by name.
func (tdm *ToolDistributionManager) RemoveDistribution(name string) {
        tdm.mu.Lock()
        defer tdm.mu.Unlock()

        for i, d := range tdm.distributions {
                if d.Name == name {
                        tdm.totalWeight -= d.Weight
                        tdm.distributions = append(tdm.distributions[:i], tdm.distributions[i+1:]...)
                        return
                }
        }
}

// SampleToolset performs weighted random sampling to select a toolset.
// Returns nil if no distributions are registered.
func (tdm *ToolDistributionManager) SampleToolset() *ToolsetDistribution {
        tdm.mu.RLock()
        defer tdm.mu.RUnlock()

        if len(tdm.distributions) == 0 || tdm.totalWeight <= 0 {
                return nil
        }

        r := tdm.rng.Float64() * tdm.totalWeight
        cumulative := 0.0
        for _, d := range tdm.distributions {
                cumulative += d.Weight
                if r <= cumulative {
                        return d
                }
        }

        // Fallback to last distribution (floating point edge case)
        return tdm.distributions[len(tdm.distributions)-1]
}

// SampleToolsetWithExclusion performs weighted random sampling but excludes
// distributions whose names appear in excludeNames.
// Returns nil if no distributions remain after exclusion.
func (tdm *ToolDistributionManager) SampleToolsetWithExclusion(excludeNames []string) *ToolsetDistribution {
        tdm.mu.RLock()
        defer tdm.mu.RUnlock()

        if len(tdm.distributions) == 0 {
                return nil
        }

        // Build exclusion set
        exclude := make(map[string]bool, len(excludeNames))
        for _, name := range excludeNames {
                exclude[name] = true
        }

        // Compute effective weight and collect eligible distributions
        effectiveWeight := 0.0
        eligible := make([]*ToolsetDistribution, 0, len(tdm.distributions))
        for _, d := range tdm.distributions {
                if !exclude[d.Name] {
                        eligible = append(eligible, d)
                        effectiveWeight += d.Weight
                }
        }

        if len(eligible) == 0 || effectiveWeight <= 0 {
                return nil
        }

        r := tdm.rng.Float64() * effectiveWeight
        cumulative := 0.0
        for _, d := range eligible {
                cumulative += d.Weight
                if r <= cumulative {
                        return d
                }
        }

        return eligible[len(eligible)-1]
}

// GetDistribution retrieves a distribution by name.
func (tdm *ToolDistributionManager) GetDistribution(name string) (*ToolsetDistribution, bool) {
        tdm.mu.RLock()
        defer tdm.mu.RUnlock()

        for _, d := range tdm.distributions {
                if d.Name == name {
                        return d, true
                }
        }
        return nil, false
}

// ListDistributions returns a copy of all registered distributions.
func (tdm *ToolDistributionManager) ListDistributions() []*ToolsetDistribution {
        tdm.mu.RLock()
        defer tdm.mu.RUnlock()

        result := make([]*ToolsetDistribution, len(tdm.distributions))
        for i, d := range tdm.distributions {
                cp := *d
                cp.ToolNames = append([]string{}, d.ToolNames...)
                result[i] = &cp
        }
        return result
}

// distributionConfigFile is the JSON schema for loading distributions from file.
type distributionConfigFile struct {
        Distributions []struct {
                Name        string   `json:"name"`
                ToolNames   []string `json:"tool_names"`
                Weight      float64  `json:"weight"`
                Description string   `json:"description"`
        } `json:"distributions"`
}

// LoadDistributionConfig loads toolset distributions from a JSON file.
// The file should contain a JSON object with a "distributions" array.
// Example:
//
//      {
//        "distributions": [
//          {"name": "core", "tool_names": ["shell", "read_file"], "weight": 1.0, "description": "Core tools"},
//          {"name": "web", "tool_names": ["browser_search", "browser_visit"], "weight": 0.5, "description": "Web tools"}
//        ]
//      }
func (tdm *ToolDistributionManager) LoadDistributionConfig(configPath string) error {
        data, err := os.ReadFile(configPath)
        if err != nil {
                return fmt.Errorf("failed to read distribution config %q: %w", configPath, err)
        }

        var cfg distributionConfigFile
        if err := json.Unmarshal(data, &cfg); err != nil {
                return fmt.Errorf("failed to parse distribution config %q: %w", configPath, err)
        }

        tdm.mu.Lock()
        // Clear existing distributions
        tdm.distributions = make([]*ToolsetDistribution, 0)
        tdm.totalWeight = 0
        tdm.mu.Unlock()

        for _, d := range cfg.Distributions {
                tdm.AddDistribution(d.Name, d.ToolNames, d.Weight, d.Description)
        }

        return nil
}

// AutoGenerateDistributions automatically creates balanced toolset distributions
// from the full tool registry. Tools are split into groups of at least
// minToolsPerSet, producing at most numSets distributions with equal weight.
//
// If minToolsPerSet is 0 or negative, a sensible default of 3 is used.
// If numSets is 0 or negative, the number of sets is derived from the tool count.
func (tdm *ToolDistributionManager) AutoGenerateDistributions(toolRegistry []*ToolDef, minToolsPerSet int, numSets int) {
        tdm.mu.Lock()
        // Clear existing distributions
        tdm.distributions = make([]*ToolsetDistribution, 0)
        tdm.totalWeight = 0
        tdm.mu.Unlock()

        if len(toolRegistry) == 0 {
                return
        }

        if minToolsPerSet <= 0 {
                minToolsPerSet = 3
        }

        // Derive numSets if not specified
        if numSets <= 0 {
                numSets = len(toolRegistry) / minToolsPerSet
                if numSets < 1 {
                        numSets = 1
                }
        }

        // Ensure numSets is reasonable
        if numSets > len(toolRegistry) {
                numSets = len(toolRegistry)
        }

        // Distribute tools across sets using round-robin by category
        // First, sort tools by category for balanced grouping
        tools := make([]*ToolDef, len(toolRegistry))
        copy(tools, toolRegistry)

        // Group tools by category for balanced distribution
        categoryTools := make(map[string][]*ToolDef)
        var order []string
        for _, t := range tools {
                cat := t.Category
                if cat == "" {
                        cat = "misc"
                }
                if _, exists := categoryTools[cat]; !exists {
                        order = append(order, cat)
                }
                categoryTools[cat] = append(categoryTools[cat], t)
        }

        // Flatten tools spread across categories into interleaved order
        interleaved := make([]*ToolDef, 0, len(tools))
        for {
                anyRemaining := false
                for _, cat := range order {
                        bucket := categoryTools[cat]
                        if len(bucket) > 0 {
                                interleaved = append(interleaved, bucket[0])
                                categoryTools[cat] = bucket[1:]
                                anyRemaining = true
                        }
                }
                if !anyRemaining {
                        break
                }
        }

        // Split into numSets groups
        perSet := len(interleaved) / numSets
        remainder := len(interleaved) % numSets
        weight := 1.0 / float64(numSets)

        idx := 0
        for i := 0; i < numSets; i++ {
                size := perSet
                if i < remainder {
                        size++
                }
                if size <= 0 {
                        continue
                }
                end := idx + size
                if end > len(interleaved) {
                        end = len(interleaved)
                }

                names := make([]string, 0, size)
                categories := make(map[string]bool)
                for _, t := range interleaved[idx:end] {
                        names = append(names, t.Name)
                        if t.Category != "" {
                                categories[t.Category] = true
                        }
                }

                // Build description from categories
                desc := fmt.Sprintf("Auto-generated set %d with %d tools", i+1, size)
                if len(categories) > 0 {
                        catList := ""
                        for c := range categories {
                                if catList != "" {
                                        catList += ", "
                                }
                                catList += c
                        }
                        desc += fmt.Sprintf(" (%s)", catList)
                }

                tdm.mu.Lock()
                tdm.distributions = append(tdm.distributions, &ToolsetDistribution{
                        Name:        fmt.Sprintf("auto_set_%d", i+1),
                        ToolNames:   names,
                        Weight:      weight,
                        Description: desc,
                })
                tdm.totalWeight += weight
                tdm.mu.Unlock()

                idx = end
        }
}

// ---------------------------------------------------------------------------
// Global tool distribution manager
// ---------------------------------------------------------------------------

var globalToolDistributionMgr *ToolDistributionManager
