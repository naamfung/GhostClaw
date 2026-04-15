package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
)

// RewardFunction defines the interface for computing reward scores on trajectories.
type RewardFunction interface {
	ComputeReward(traj Trajectory) float64
}

// RewardBreakdown provides a per-component breakdown of how a reward was computed.
type RewardBreakdown struct {
	SuccessWeight     float64 `json:"success_weight"`
	SuccessScore      float64 `json:"success_score"`
	FeedbackWeight    float64 `json:"feedback_weight"`
	FeedbackScore     float64 `json:"feedback_score"`
	ToolRateWeight    float64 `json:"tool_rate_weight"`
	ToolRateScore     float64 `json:"tool_rate_score"`
	EfficiencyWeight  float64 `json:"efficiency_weight"`
	EfficiencyScore   float64 `json:"efficiency_score"`
	Total             float64 `json:"total"`
}

// DefaultRewardFunction computes a composite reward from multiple trajectory signals.
//
// Reward composition (sum to 1.0 max):
//   - Success:           +0.5 if traj.Success, 0.0 otherwise
//   - UserFeedback:      normalized 0 – 0.3 based on UserFeedback (1-5 scale)
//   - ToolSuccessRate:   0 – 0.2 based on fraction of successful tool calls
//   - TokenEfficiency:   0 – 0.0 bonus (reserved for future tuning)
type DefaultRewardFunction struct{}

// ComputeReward evaluates the composite reward for a single trajectory.
func (rf *DefaultRewardFunction) ComputeReward(traj Trajectory) float64 {
	reward := 0.0

	// Success component: flat +0.5 or 0.0
	if traj.Success {
		reward += 0.5
	}

	// UserFeedback component: normalize 1-5 → 0.0-0.3
	if traj.UserFeedback > 0 {
		// Linear mapping: 1→0.0, 3→0.15, 5→0.3
		reward += float64(traj.UserFeedback-1) / 4.0 * 0.3
	}

	// ToolSuccessRate component: 0-0.2
	if len(traj.ToolCalls) > 0 {
		successCount := 0
		for _, tc := range traj.ToolCalls {
			if tc.Success {
				successCount++
			}
		}
		rate := float64(successCount) / float64(len(traj.ToolCalls))
		reward += rate * 0.2
	}

	// TokenEfficiency component: 0.0 (reserved for future tuning)
	// Currently unused — structure in place to add e.g. a penalty for
	// excessive token usage without penalising normal conversations.

	return reward
}

// ComputeRewardBreakdown returns both the total score and per-component details.
func (rf *DefaultRewardFunction) ComputeRewardBreakdown(traj Trajectory) (float64, RewardBreakdown) {
	bd := RewardBreakdown{
		SuccessWeight:    0.5,
		FeedbackWeight:   0.3,
		ToolRateWeight:   0.2,
		EfficiencyWeight: 0.0,
	}

	// Success
	if traj.Success {
		bd.SuccessScore = 0.5
	}

	// Feedback
	if traj.UserFeedback > 0 {
		bd.FeedbackScore = float64(traj.UserFeedback-1) / 4.0 * 0.3
	}

	// Tool rate
	if len(traj.ToolCalls) > 0 {
		successCount := 0
		for _, tc := range traj.ToolCalls {
			if tc.Success {
				successCount++
			}
		}
		rate := float64(successCount) / float64(len(traj.ToolCalls))
		bd.ToolRateScore = rate * 0.2
	}

	bd.Total = bd.SuccessScore + bd.FeedbackScore + bd.ToolRateScore + bd.EfficiencyScore

	return bd.Total, bd
}

// ScoredTrajectory pairs a trajectory with its computed reward.
type ScoredTrajectory struct {
	Trajectory       Trajectory       `json:"trajectory"`
	Score            float64          `json:"score"`
	RewardBreakdown  RewardBreakdown  `json:"reward_breakdown"`
}

// ScoreTrajectories scores all trajectories managed by tm using the provided
// RewardFunction. Results are returned sorted by score descending
// (highest reward first).
func ScoreTrajectories(tm *TrajectoryManager, rf RewardFunction) ([]ScoredTrajectory, error) {
	trajectories, err := tm.GetTrajectories()
	if err != nil {
		return nil, fmt.Errorf("failed to load trajectories for scoring: %w", err)
	}

	scored := make([]ScoredTrajectory, 0, len(trajectories))

	// Use the concrete DefaultRewardFunction when available for breakdown.
	drf, hasBreakdown := rf.(*DefaultRewardFunction)

	for _, traj := range trajectories {
		score := rf.ComputeReward(traj)
		st := ScoredTrajectory{
			Trajectory: traj,
			Score:      score,
		}
		if hasBreakdown {
			_, bd := drf.ComputeRewardBreakdown(traj)
			st.RewardBreakdown = bd
		}
		scored = append(scored, st)
	}

	// Sort descending by score; break ties with timestamp (most recent first).
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Trajectory.Timestamp.After(scored[j].Trajectory.Timestamp)
	})

	return scored, nil
}

// ExportSFTToFile writes SFTSample entries to a JSONL file at filepath.
// Each line is a single valid JSON object.
func ExportSFTToFile(samples []SFTSample, filepath string) error {
	f, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create SFT export file %q: %w", filepath, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	for _, sample := range samples {
		if err := enc.Encode(sample); err != nil {
			return fmt.Errorf("failed to encode SFT sample: %w", err)
		}
	}

	return nil
}

// ExportRLToFile writes RLTrainingItem entries to a JSONL file at filepath.
func ExportRLToFile(items []RLTrainingItem, filepath string) error {
	f, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create RL export file %q: %w", filepath, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return fmt.Errorf("failed to encode RL training item: %w", err)
		}
	}

	return nil
}

// ExportSFTToJSONL is a convenience function that loads SFT samples from the
// TrajectoryManager and writes them to outputPath in JSONL format.
// If limit > 0 only the top `limit` samples are exported.
func ExportSFTToJSONL(tm *TrajectoryManager, outputPath string, limit int) error {
	samples, err := tm.ExportSFTSamples(limit)
	if err != nil {
		return fmt.Errorf("failed to export SFT samples: %w", err)
	}
	if len(samples) == 0 {
		return fmt.Errorf("no SFT samples to export")
	}
	return ExportSFTToFile(samples, outputPath)
}

// ExportRLToJSONL is a convenience function that loads RL training items from
// the TrajectoryManager and writes them to outputPath in JSONL format.
// If limit > 0 only the top `limit` items are exported.
func ExportRLToJSONL(tm *TrajectoryManager, outputPath string, limit int) error {
	items, err := tm.ExportRLData(limit)
	if err != nil {
		return fmt.Errorf("failed to export RL data: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("no RL training items to export")
	}
	return ExportRLToFile(items, outputPath)
}

// ExportTrajectoryStatsToFile writes turn-level and trajectory-level statistics
// to a JSON file at filepath.
func ExportTrajectoryStatsToFile(tm *TrajectoryManager, filepath string) error {
	turnStats := tm.GetTurnStats()
	trajStats := tm.GetTrajectoryStats()

	combined := map[string]interface{}{
		"turn_stats":       turnStats,
		"trajectory_stats": trajStats,
		"generated_at":     fmt.Sprintf("%s", tm.lastSaveTime),
	}

	// Ensure directory exists.
	if dir := parentDir(filepath); dir != "" && dir != "." {
		os.MkdirAll(dir, 0755)
	}

	data, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal trajectory stats: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write trajectory stats to %q: %w", filepath, err)
	}

	return nil
}

// ExportScoredTrajectoriesToFile writes scored trajectories to a JSONL file.
func ExportScoredTrajectoriesToFile(scored []ScoredTrajectory, filepath string) error {
	f, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create scored trajectories file %q: %w", filepath, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	for _, s := range scored {
		if err := enc.Encode(s); err != nil {
			return fmt.Errorf("failed to encode scored trajectory: %w", err)
		}
	}

	return nil
}

// LoadSFTSamplesFromFile reads SFTSample entries from a JSONL file.
func LoadSFTSamplesFromFile(filepath string) ([]SFTSample, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open SFT file %q: %w", filepath, err)
	}
	defer f.Close()

	var samples []SFTSample
	dec := json.NewDecoder(bufio.NewReader(f))
	for dec.More() {
		var sample SFTSample
		if err := dec.Decode(&sample); err != nil {
			return nil, fmt.Errorf("failed to decode SFT sample: %w", err)
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

// LoadRLItemsFromFile reads RLTrainingItem entries from a JSONL file.
func LoadRLItemsFromFile(filepath string) ([]RLTrainingItem, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open RL file %q: %w", filepath, err)
	}
	defer f.Close()

	var items []RLTrainingItem
	dec := json.NewDecoder(bufio.NewReader(f))
	for dec.More() {
		var item RLTrainingItem
		if err := dec.Decode(&item); err != nil {
			return nil, fmt.Errorf("failed to decode RL training item: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

// FilterScoredTrajectories returns only the scored trajectories whose score
// is at or above minScore, preserving the input order.
func FilterScoredTrajectories(scored []ScoredTrajectory, minScore float64) []ScoredTrajectory {
	filtered := make([]ScoredTrajectory, 0, len(scored))
	for _, s := range scored {
		if s.Score >= minScore {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// RewardPercentile returns the score at the given percentile (0-100) from a
// pre-sorted (descending) scored slice. Returns NaN if the slice is empty.
func RewardPercentile(scored []ScoredTrajectory, percentile float64) float64 {
	if len(scored) == 0 {
		return math.NaN()
	}
	if percentile <= 0 {
		return scored[0].Score
	}
	if percentile >= 100 {
		return scored[len(scored)-1].Score
	}
	idx := percentile / 100.0 * float64(len(scored)-1)
	lo := int(math.Floor(idx))
	hi := lo + 1
	if hi >= len(scored) {
		return scored[lo].Score
	}
	frac := idx - float64(lo)
	return scored[lo].Score + frac*(scored[hi].Score-scored[lo].Score)
}

// parentDir returns the directory portion of a path, similar to filepath.Dir
// but avoids importing path/filepath to keep the dependency surface flat.
func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
