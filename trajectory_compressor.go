package main

import (
        "encoding/json"
        "fmt"
        "log"
        "strings"
        "sync"
        "time"
)

// TrajectoryCompressor 軌跡壓縮管線 (Trajectory Compression Pipeline)
//
// Protects head/tail messages and compresses the middle portion into a
// structured summary that preserves key context: tools used, time range,
// topics discussed, and per-role message distribution.
type TrajectoryCompressor struct {
        protectFirstNTurns int // protect first N messages (default: 2)
        protectLastNTurns  int // protect last N messages  (default: 2)
        maxTokens          int // target max tokens for compressed middle (default: 4000)
        compressionCount   int // number of compressions performed
        mu                 sync.Mutex
}

// DefaultTrajectoryCompressor creates a TrajectoryCompressor with sensible defaults.
func DefaultTrajectoryCompressor() *TrajectoryCompressor {
        return &TrajectoryCompressor{
                protectFirstNTurns: 2,
                protectLastNTurns:  2,
                maxTokens:          4000,
        }
}

// NewTrajectoryCompressor creates a TrajectoryCompressor with custom settings.
// Any value <= 0 falls back to its default.
func NewTrajectoryCompressor(protectFirst, protectLast, maxTokens int) *TrajectoryCompressor {
        tc := DefaultTrajectoryCompressor()
        if protectFirst > 0 {
                tc.protectFirstNTurns = protectFirst
        }
        if protectLast > 0 {
                tc.protectLastNTurns = protectLast
        }
        if maxTokens > 0 {
                tc.maxTokens = maxTokens
        }
        return tc
}

// CompressionStats holds statistics about a single compression operation.
type CompressionStats struct {
        OriginalMessageCount int       `json:"original_message_count"`
        CompressedMessageCount int     `json:"compressed_message_count"`
        MessagesCompressed   int       `json:"messages_compressed"`
        ToolsUsed            []string  `json:"tools_used"`
        TimeRange            string    `json:"time_range"`
        KeyTopics            []string  `json:"key_topics"`
        CompressionRatio     float64   `json:"compression_ratio"`
        Timestamp            time.Time `json:"timestamp"`
}

// CompressTrajectory compresses a trajectory by protecting the first N and last N
// messages and replacing the middle with a structured summary.
//
// The summary includes: message count compressed, tools used, time range, and key
// topics extracted from the compressed section.  All other Trajectory metadata
// (ID, SessionID, ToolCalls, TokenUsage, Turns, etc.) is preserved verbatim.
func (tc *TrajectoryCompressor) CompressTrajectory(traj Trajectory) (Trajectory, error) {
        messages := traj.Messages

        totalProtected := tc.protectFirstNTurns + tc.protectLastNTurns

        // Not enough messages to warrant compression — return a shallow copy.
        if len(messages) <= totalProtected {
                result := traj
                result.Messages = make([]Message, len(messages))
                copy(result.Messages, messages)
                return result, nil
        }

        // Split into head / middle / tail.
        head := messages[:tc.protectFirstNTurns]
        tail := messages[len(messages)-tc.protectLastNTurns:]
        middle := messages[tc.protectFirstNTurns : len(messages)-tc.protectLastNTurns]

        // Build the structured summary.
        stats := tc.analyseMiddle(middle)
        summaryMessage := tc.buildSummaryMessage(stats)

        // Assemble compressed message list: head + summary + tail.
        compressedMessages := make([]Message, 0, len(head)+1+len(tail))
        compressedMessages = append(compressedMessages, head...)
        compressedMessages = append(compressedMessages, summaryMessage)
        compressedMessages = append(compressedMessages, tail...)

        // Copy metadata map so we never mutate the original.
        metadata := make(map[string]interface{}, len(traj.Metadata)+5)
        for k, v := range traj.Metadata {
                metadata[k] = v
        }
        metadata["original_message_count"] = len(messages)
        metadata["compressed_message_count"] = len(compressedMessages)
        metadata["compressed_turns"] = len(middle)
        metadata["compression_applied"] = true
        metadata["compression_ratio"] = stats.CompressionRatio

        result := Trajectory{
                ID:           traj.ID,
                SessionID:    traj.SessionID,
                Messages:     compressedMessages,
                Success:      traj.Success,
                UserFeedback: traj.UserFeedback,
                ToolCalls:    traj.ToolCalls,
                Timestamp:    traj.Timestamp,
                Duration:     traj.Duration,
                ModelUsed:    traj.ModelUsed,
                TokenUsage:   traj.TokenUsage,
                Turns:        traj.Turns,
                Metadata:     metadata,
        }

        // Atomically increment the global counter.
        tc.mu.Lock()
        tc.compressionCount++
        tc.mu.Unlock()

        return result, nil
}

// analyseMiddle extracts structured information from the middle message slice.
func (tc *TrajectoryCompressor) analyseMiddle(messages []Message) CompressionStats {
        stats := CompressionStats{
                MessagesCompressed:   len(messages),
                OriginalMessageCount: len(messages),
                CompressedMessageCount: 1, // the summary itself
                Timestamp:           time.Now(),
        }

        // Collect tool names used in this section.
        toolSet := make(map[string]bool)
        for _, msg := range messages {
                extractToolsFromMessage(msg, toolSet)
        }
        stats.ToolsUsed = sortedKeys(toolSet)

        // Extract time range.
        stats.TimeRange = extractTimeRange(messages)

        // Extract key topics from user messages.
        stats.KeyTopics = extractKeyTopics(messages, 5)

        // Compute compression ratio.
        if stats.OriginalMessageCount > 0 {
                stats.CompressionRatio = float64(stats.MessagesCompressed) / float64(stats.OriginalMessageCount)
        }

        return stats
}

// buildSummaryMessage renders the CompressionStats into a single system-level Message.
func (tc *TrajectoryCompressor) buildSummaryMessage(stats CompressionStats) Message {
        var sb strings.Builder

        sb.WriteString("[Trajectory Compression Summary]\n")
        sb.WriteString(fmt.Sprintf("Compressed %d messages into this summary.\n", stats.MessagesCompressed))
        sb.WriteString(fmt.Sprintf("Time range: %s\n", stats.TimeRange))

        if len(stats.ToolsUsed) > 0 {
                sb.WriteString(fmt.Sprintf("Tools used: %s\n", strings.Join(stats.ToolsUsed, ", ")))
        } else {
                sb.WriteString("Tools used: none\n")
        }

        if len(stats.KeyTopics) > 0 {
                sb.WriteString(fmt.Sprintf("Key topics: %s\n", strings.Join(stats.KeyTopics, "; ")))
        }

        sb.WriteString(fmt.Sprintf("Compression ratio: %.2f\n", stats.CompressionRatio))
        summaryText := sb.String()

        // Enforce the maxTokens budget (~4 chars per token as a rough heuristic).
        maxChars := tc.maxTokens * 4
        if len(summaryText) > maxChars {
                runes := []rune(summaryText)
                summaryText = string(runes[:maxChars]) + "\n[...truncated to fit token budget]"
        }

        return Message{
                Role:      "system",
                Content:   summaryText,
                Timestamp: stats.Timestamp.Unix(),
        }
}

// extractToolsFromMessage scans a single Message for tool call references and
// inserts unique names into toolSet.
func extractToolsFromMessage(msg Message, toolSet map[string]bool) {
        if msg.ToolCalls == nil {
                return
        }

        switch v := msg.ToolCalls.(type) {
        case []ToolCall:
                for _, tc := range v {
                        if tc.FunctionName != "" {
                                toolSet[tc.FunctionName] = true
                        }
                }
        case []interface{}:
                for _, item := range v {
                        if tcMap, ok := item.(map[string]interface{}); ok {
                                if fn, ok := tcMap["function"].(map[string]interface{}); ok {
                                        if name, ok := fn["name"].(string); ok && name != "" {
                                                toolSet[name] = true
                                        }
                                }
                        }
                }
        case []map[string]interface{}:
                for _, tcMap := range v {
                        if fn, ok := tcMap["function"].(map[string]interface{}); ok {
                                if name, ok := fn["name"].(string); ok && name != "" {
                                        toolSet[name] = true
                                }
                        }
                }
        }
}

// extractTimeRange returns a human-readable time span covering the first and
// last messages that carry a non-zero timestamp.
func extractTimeRange(messages []Message) string {
        var first, last int64
        found := false
        for _, msg := range messages {
                if msg.Timestamp > 0 {
                        if !found {
                                first = msg.Timestamp
                                last = msg.Timestamp
                                found = true
                        } else {
                                if msg.Timestamp < first {
                                        first = msg.Timestamp
                                }
                                if msg.Timestamp > last {
                                        last = msg.Timestamp
                                }
                        }
                }
        }
        if !found {
                return "unknown"
        }
        t0 := time.Unix(first, 0).Format("2006-01-02 15:04:05")
        t1 := time.Unix(last, 0).Format("2006-01-02 15:04:05")
        if t0 == t1 {
                return t0
        }
        return t0 + " ~ " + t1
}

// extractKeyTopics pulls short topic phrases from user messages.
// Up to maxTopics are returned, deduplicated.
func extractKeyTopics(messages []Message, maxTopics int) []string {
        seen := make(map[string]bool)
        var topics []string
        for _, msg := range messages {
                if msg.Role != "user" {
                        continue
                }
                content := extractStringContent(msg)
                if content == "" {
                        continue
                }
                // Take the first sentence / line up to 80 chars as a topic hint.
                topic := firstSentence(content, 80)
                topic = strings.TrimSpace(topic)
                if topic == "" {
                        continue
                }
                if !seen[topic] {
                        seen[topic] = true
                        topics = append(topics, topic)
                        if len(topics) >= maxTopics {
                                break
                        }
                }
        }
        return topics
}

// firstSentence returns the first sentence-like fragment of s, truncated to
// maxRunes runes.  A "sentence" ends at the first '.', '!', '?', '\n' or at
// the rune limit.
func firstSentence(s string, maxRunes int) string {
        runes := []rune(s)
        end := len(runes)
        if end > maxRunes {
                end = maxRunes
        }
        for i := 0; i < end; i++ {
                ch := runes[i]
                if ch == '.' || ch == '!' || ch == '?' || ch == '\n' {
                        return string(runes[:i+1])
                }
        }
        return string(runes[:end])
}

// sortedKeys returns the keys of m in lexicographic order.
func sortedKeys(m map[string]bool) []string {
        keys := make([]string, 0, len(m))
        for k := range m {
                keys = append(keys, k)
        }
        // Simple insertion sort — these sets are tiny.
        for i := 1; i < len(keys); i++ {
                for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
                        keys[j], keys[j-1] = keys[j-1], keys[j]
                }
        }
        return keys
}

// BatchCompress compresses multiple trajectories concurrently.
// If concurrency <= 0 it defaults to the number of trajectories (no limit).
// Errors on individual trajectories are logged but do not abort the batch;
// the corresponding slot in the result slice is set to the original trajectory.
func (tc *TrajectoryCompressor) BatchCompress(trajectories []Trajectory, concurrency int) ([]Trajectory, error) {
        if len(trajectories) == 0 {
                return nil, nil
        }
        if concurrency <= 0 {
                concurrency = len(trajectories)
        }

        results := make([]Trajectory, len(trajectories))
        errs := make([]error, len(trajectories))

        // Worker pool.
        type job struct {
                index int
                traj  Trajectory
        }
        jobs := make(chan job, len(trajectories))
        var wg sync.WaitGroup

        // Launch workers.
        workerCount := concurrency
        if workerCount > len(trajectories) {
                workerCount = len(trajectories)
        }
        for w := 0; w < workerCount; w++ {
                wg.Add(1)
                go func() {
                        defer wg.Done()
                        for j := range jobs {
                                compressed, err := tc.CompressTrajectory(j.traj)
                                if err != nil {
                                        log.Printf("[TrajectoryCompressor] BatchCompress error at index %d: %v", j.index, err)
                                        results[j.index] = j.traj
                                        errs[j.index] = err
                                        continue
                                }
                                results[j.index] = compressed
                        }
                }()
        }

        // Enqueue all jobs.
        for i, t := range trajectories {
                jobs <- job{index: i, traj: t}
        }
        close(jobs)
        wg.Wait()

        // Check if any individual errors occurred.
        var firstErr error
        for _, e := range errs {
                if e != nil {
                        firstErr = e
                        break
                }
        }
        return results, firstErr
}

// CompressedRatio calculates the compression ratio between original and
// compressed trajectories based on message count.
//
// Returns a value in [0, 1] where:
//   - 1.0 means no compression (same size)
//   - 0.0 means maximum compression
//
// If either trajectory has zero messages the ratio is 0.0.
func CompressedRatio(original, compressed Trajectory) float64 {
        origLen := len(original.Messages)
        if origLen == 0 {
                return 0.0
        }
        compLen := len(compressed.Messages)
        ratio := float64(compLen) / float64(origLen)
        if ratio > 1.0 {
                ratio = 1.0
        }
        return ratio
}

// TokenEstimate provides a rough token count for a trajectory by estimating
// ~4 characters per token across all message content.  This is used for
// logging and diagnostics only — it is not used for model API calls.
func TokenEstimate(traj Trajectory) int {
        total := 0
        for _, msg := range traj.Messages {
                content := extractStringContent(msg)
                total += len(content)
        }
        return total / 4
}

// MarshalCompressionStats serialises CompressionStats to JSON.
func MarshalCompressionStats(stats CompressionStats) ([]byte, error) {
        return json.Marshal(stats)
}

// UnmarshalCompressionStats deserialises CompressionStats from JSON.
func UnmarshalCompressionStats(data []byte) (CompressionStats, error) {
        var stats CompressionStats
        err := json.Unmarshal(data, &stats)
        return stats, err
}
