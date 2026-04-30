package main

import (
	"strings"
	"testing"
)

// ============================================================================
// MiniMax tool_result 序列完整性測試
// ============================================================================
//
// 背景 (e13.log):
//   MiniMax API 嚴格要求 tool_result 必須緊跟在 tool_use 之後，中間不能
//   有任何 user content。違反此約束時 API 返回 400：
//     "minimax invalid tool_result sequence: tool_result must immediately
//      follow tool_use before user content at message 1 part 0"
//
// 觸發場景：
//   喚醒通知（延遲任務完成/失敗）被注入為 user 消息時，如果插入到
//   assistant(tool_use) → tool(tool_result) 鏈路中間，會破壞此約束。
//
// 以下測試覆蓋：
//   1. I3 不變量 — 孤兒 tool 消息檢測
//   2. removeOrphanedToolMessages — user content 截斷 tool 鏈路
//   3. validateAndCleanMessages — 完整清理管線
//   4. e13.log 精確場景重現
//   5. findLegalStart — 前向掃描修復

// --- 輔助函數（與 context_compressor_test.go 保持一致） ---

func mtMakeAssistantWithToolCalls(content string, tcID string, toolNames ...string) Message {
	toolCalls := make([]map[string]interface{}, len(toolNames))
	for i, name := range toolNames {
		toolCalls[i] = map[string]interface{}{
			"id":   tcID + "_" + name,
			"type": "function",
			"function": map[string]interface{}{
				"name":      name,
				"arguments": "{}",
			},
		}
	}
	return Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
}

func mtMakeToolResult(content string, toolCallID string) Message {
	return Message{
		Role:       "tool",
		Content:    content,
		ToolCallID: toolCallID,
	}
}

func mtMakeUser(content string) Message {
	return Message{Role: "user", Content: content}
}

func mtMakeSystem(content string) Message {
	return Message{Role: "system", Content: content}
}

// ============================================================================
// I3 不變量測試：孤兒 tool 消息檢測
// ============================================================================

func TestI3_NoOrphanToolMessages(t *testing.T) {
	t.Run("合法的 assistant→tool→tool→user 序列", func(t *testing.T) {
		ml := NewMessageList([]Message{
			mtMakeSystem("system prompt"),
			mtMakeUser("請執行任務"),
			mtMakeAssistantWithToolCalls("我來執行", "call", "SmartShell", "ReadFile"),
			mtMakeToolResult("result 1", "call_SmartShell"),
			mtMakeToolResult("result 2", "call_ReadFile"),
			mtMakeUser("請繼續"),
		})
		violations := ml.Validate()
		hasI3 := hasViolationCode(violations, "I3")
		if hasI3 {
			t.Errorf("合法序列不應該有 I3 違規，但檢測到: %v", violations)
		}
	})

	t.Run("user 消息插入在 assistant(tool_use) 和 tool 之間 → I3 違規", func(t *testing.T) {
		// 呢個係 MiniMax 400 錯誤嘅精確場景：
		// 喚醒通知（user 消息）被插入喺 tool_use 同 tool_result 之間
		ml := NewMessageList([]Message{
			mtMakeSystem("system prompt"),
			mtMakeUser("請執行任務"),
			mtMakeAssistantWithToolCalls("我來執行", "call", "SmartShell"),
			// ⚠️ 喚醒通知插入喺 tool_use 同 tool_result 之間！
			mtMakeUser("⏰ 任务唤醒通知\n\n❌ 任务ID: task_abc"),
			mtMakeToolResult("command output", "call_SmartShell"),
		})
		violations := ml.Validate()
		if !hasViolationCode(violations, "I3") {
			t.Error("user 插入在 tool_use 和 tool_result 之間必須檢測為 I3 違規（孤兒 tool），但未檢測到")
		}
	})

	t.Run("多個 tool_use→tool_result 鏈路，第二個鏈路有 user 插入 → I3", func(t *testing.T) {
		ml := NewMessageList([]Message{
			mtMakeSystem("system prompt"),
			mtMakeUser("請執行多個任務"),
			mtMakeAssistantWithToolCalls("第一個工具", "call1", "SmartShell"),
			mtMakeToolResult("output 1", "call1_SmartShell"),
			mtMakeAssistantWithToolCalls("第二個工具", "call2", "ReadFile"),
			// ⚠️ user 插入喺第二個 tool_use 同 tool_result 之間
			mtMakeUser("⏰ 任务唤醒通知"),
			mtMakeToolResult("output 2", "call2_ReadFile"),
		})
		violations := ml.Validate()
		if !hasViolationCode(violations, "I3") {
			t.Error("第二個 tool_use→tool 鏈路有 user 插入必須檢測為 I3 違規")
		}
	})

	t.Run("user 在完整 tool 鏈路之後 → 合法", func(t *testing.T) {
		// 修復後嘅行為：喚醒通知合併到最後一條 user 消息
		ml := NewMessageList([]Message{
			mtMakeSystem("system prompt"),
			mtMakeUser("請執行任務"),
			mtMakeAssistantWithToolCalls("我來執行", "call", "SmartShell"),
			mtMakeToolResult("output", "call_SmartShell"),
			mtMakeUser("繼續 + ⏰ 任务唤醒通知"), // 合併喺同一個 user 消息
		})
		violations := ml.Validate()
		if hasViolationCode(violations, "I3") {
			t.Errorf("user 喺完整 tool 鏈路之後應該合法，但檢測到 I3: %v", violations)
		}
	})

	t.Run("空的 tool_call_id 應觸發 I3（無法匹配任何 assistant）", func(t *testing.T) {
		ml := NewMessageList([]Message{
			mtMakeSystem("system"),
			mtMakeUser("request"),
			Message{Role: "tool", Content: "no parent", ToolCallID: ""},
		})
		violations := ml.Validate()
		if !hasViolationCode(violations, "I3") {
			t.Error("空 ToolCallID 的 tool 消息無法匹配任何 assistant，應觸發 I3")
		}
	})

	t.Run("兩個 assistant 之間有 user 令第一個 assistant 的 tool 變成孤兒", func(t *testing.T) {
		// 場景：assistant_A(tool_use) → user → assistant_B(tool_use) → tool(result_for_A)
		// tool(result_for_A) 變成孤兒，因為 user 重置咗 declared set，
		// 而 assistant_B 重新聲明咗新嘅 tool_call set
		ml := NewMessageList([]Message{
			mtMakeSystem("system"),
			mtMakeUser("request"),
			mtMakeAssistantWithToolCalls("tool A", "callA", "SmartShell"),
			mtMakeUser("⏰ 唤醒通知"),
			mtMakeAssistantWithToolCalls("tool B", "callB", "ReadFile"),
			mtMakeToolResult("result for A", "callA_SmartShell"), // 孤兒！
			mtMakeToolResult("result for B", "callB_ReadFile"),
		})
		violations := ml.Validate()
		if !hasViolationCode(violations, "I3") {
			t.Error("assistant_A 的 tool result 在 assistant_B 之後應該檢測為孤兒 (I3)")
		}
	})
}

// ============================================================================
// removeOrphanedToolMessages：user 截斷 tool 鏈路後的清理
// ============================================================================

func TestRemoveOrphanedToolMessages_UserBreaksChain(t *testing.T) {
	t.Run("user 在 assistant 和 tool 之間 → tool 被移除", func(t *testing.T) {
		msgs := []Message{
			mtMakeAssistantWithToolCalls("執行", "call", "SmartShell"),
			mtMakeUser("⏰ 唤醒通知"),
			mtMakeToolResult("result", "call_SmartShell"),
		}
		// removeOrphanedToolMessages 向後搜索匹配的 assistant 時，
		// 遇到 user 會停止搜索 → tool 被判定為孤兒 → 被移除
		result := removeOrphanedToolMessages(msgs)
		for _, msg := range result {
			if msg.Role == "tool" {
				t.Error("user 分離的 tool 消息應該被移除，但仍存在")
			}
		}
	})

	t.Run("合法序列：assistant → tool → user → 所有 tool 保留", func(t *testing.T) {
		msgs := []Message{
			mtMakeAssistantWithToolCalls("執行", "call", "SmartShell"),
			mtMakeToolResult("result", "call_SmartShell"),
			mtMakeUser("繼續"),
		}
		result := removeOrphanedToolMessages(msgs)
		toolCount := 0
		for _, msg := range result {
			if msg.Role == "tool" {
				toolCount++
			}
		}
		if toolCount != 1 {
			t.Errorf("tool 應該保留，但 tool count = %d", toolCount)
		}
	})

	t.Run("多個 tool 都被 user 截斷 → 全部移除", func(t *testing.T) {
		msgs := []Message{
			mtMakeAssistantWithToolCalls("多個工具", "call", "SmartShell", "ReadFile"),
			mtMakeUser("⏰ 唤醒通知"),
			mtMakeToolResult("result 1", "call_SmartShell"),
			mtMakeToolResult("result 2", "call_ReadFile"),
		}
		result := removeOrphanedToolMessages(msgs)
		for _, msg := range result {
			if msg.Role == "tool" {
				t.Errorf("tool %q 被 user 截斷後應被移除但仍存在", msg.ToolCallID)
			}
		}
	})

	t.Run("system 消息同樣截斷向前搜索", func(t *testing.T) {
		msgs := []Message{
			mtMakeSystem("context summary"),
			mtMakeAssistantWithToolCalls("執行", "call", "SmartShell"),
			// system 在 assistant 之前 → tool 向後搜索遇到 system 會停止
			// 但呢個 case 中 assistant 喺 system 之後，tool 喺 assistant 之後
			// 所以 tool 應該可以搵到 assistant
			mtMakeToolResult("result", "call_SmartShell"),
		}
		result := removeOrphanedToolMessages(msgs)
		toolCount := 0
		for _, msg := range result {
			if msg.Role == "tool" {
				toolCount++
			}
		}
		if toolCount != 1 {
			t.Errorf("tool 喺合法位置應該保留，但 tool count = %d (msgs=%d, result=%d)",
				toolCount, len(msgs), len(result))
		}
	})
}

// ============================================================================
// validateAndCleanMessages：完整清理管線
// ============================================================================

func TestValidateAndCleanMessages_MiniMaxConstraint(t *testing.T) {
	t.Run("e13.log 場景：tool_use → tool → user(wake) 序列保持合法", func(t *testing.T) {
		// 合法序列：assistant + tool 結果保持配對，user 在最後
		msgs := []Message{
			mtMakeSystem("system prompt"),
			mtMakeUser("幫我裝 rsync"),
			mtMakeAssistantWithToolCalls("我來執行", "call", "SmartShell"),
			mtMakeToolResult("command output", "call_SmartShell"),
			// 喚醒通知合併到最後一條 user — 修復後嘅行為
			mtMakeUser("⏰ 任务唤醒通知\n\n❌ 任务ID: task_abc\n\n請繼續"),
		}
		result := validateAndCleanMessages(msgs)
		// 確保所有 tool 都有匹配嘅 assistant
		ml := NewMessageList(result)
		violations := ml.Validate()
		if hasViolationCode(violations, "I3") {
			t.Errorf("合法序列不應該有 I3 違規: %v\nresult: %+v", violations, result)
		}
	})

	t.Run("違規序列：assistant → user(wake) → tool → 清理後 tool 被移除", func(t *testing.T) {
		msgs := []Message{
			mtMakeSystem("system prompt"),
			mtMakeUser("幫我裝 rsync"),
			mtMakeAssistantWithToolCalls("我來執行", "call", "SmartShell"),
			mtMakeUser("⏰ 唤醒通知"), // ⚠️ 喺 tool_use 同 tool 之間
			mtMakeToolResult("command output", "call_SmartShell"),
		}
		result := validateAndCleanMessages(msgs)
		// tool 應該被 removeOrphanedToolMessages 移除
		for _, msg := range result {
			if msg.Role == "tool" {
				t.Error("違規序列中的孤兒 tool 應該被移除，但仍存在")
			}
		}
		// 確保清理後序列合法
		ml := NewMessageList(result)
		violations := ml.Validate()
		if hasViolationCode(violations, "I3") {
			t.Errorf("清理後不應該有 I3 違規: %v", violations)
		}
	})

	t.Run("連續兩個 user 消息被合併", func(t *testing.T) {
		// validateAndCleanMessages 會合併連續嘅 user 消息 (line 833-841)
		msgs := []Message{
			mtMakeSystem("system"),
			mtMakeUser("原本請求"),
			mtMakeUser("⏰ 唤醒通知"),
		}
		result := validateAndCleanMessages(msgs)
		userCount := 0
		for _, msg := range result {
			if msg.Role == "user" {
				userCount++
				content, _ := msg.Content.(string)
				if !strings.Contains(content, "原本請求") || !strings.Contains(content, "唤醒通知") {
					t.Errorf("合併後嘅 user 消息應該同時包含兩個內容，got: %s", content)
				}
			}
		}
		if userCount != 1 {
			t.Errorf("兩個連續 user 消息應該被合併為一個，但 user count = %d", userCount)
		}
	})

	t.Run("空內容的 user 消息之後的 tool 仍然合法", func(t *testing.T) {
		msgs := []Message{
			mtMakeSystem("system"),
			mtMakeUser("request"),
			mtMakeAssistantWithToolCalls("exec", "call", "SmartShell"),
			mtMakeToolResult("output", "call_SmartShell"),
			Message{Role: "user", Content: ""}, // 空 user，會被過濾
		}
		result := validateAndCleanMessages(msgs)
		ml := NewMessageList(result)
		violations := ml.Validate()
		if hasViolationCode(violations, "I3") {
			t.Errorf("空 user 之後不應該破壞 tool 鏈路: %v", violations)
		}
	})
}

// ============================================================================
// e13.log 精確場景重現
// ============================================================================

func TestE13LogScenario_Exact(t *testing.T) {
	// 從 e13.log line 197-214 重現：
	//   1. AgentLoop iteration=7 時，喚醒通知被注入 (line 197)
	//   2. Pipeline compress=4→4, repair=4→4, dedup=4→4 (lines 198-200)
	//   3. Final validate: OK (line 201)
	//   4. 但 API 返回 400 "tool_result must immediately follow tool_use
	//      before user content" (line 214)
	//
	// 關鍵問題：壓縮後嘅 4 條消息雖然通過咗 I1-I4 驗證（冇孤兒 tool），
	// 但 MiniMax API 內部轉換消息格式時，如果 tool_result 同 tool_use
	// 之間有任何 user content（即使係喺前一條消息中），都會觸發 400。
	//
	// 呢個測試確保 validateAndCleanMessages 會將所有呢類違規序列清理乾淨。

	t.Run("壓縮後的 4 條消息模擬", func(t *testing.T) {
		// 模擬 pipeline 壓縮後可能產生嘅 4 條消息
		msgs := []Message{
			mtMakeSystem("system prompt"),
			mtMakeUser("⏰ 任务唤醒通知\n\n❌ 任务ID: task_7f6d4102"),
			mtMakeAssistantWithToolCalls("我來修復", "c1", "SshConnect"),
			mtMakeToolResult("connected", "c1_SshConnect"),
		}
		result := validateAndCleanMessages(msgs)
		ml := NewMessageList(result)
		violations := ml.Validate()
		if len(violations) > 0 {
			t.Errorf("合法序列不應該有任何違規: %v", violations)
		}
		for i := 1; i < len(result); i++ {
			if result[i-1].Role == "assistant" && result[i-1].ToolCalls != nil &&
				result[i].Role == "user" {
				t.Errorf("index %d: user 消息緊跟在 assistant(tool_use) 之後，違反 MiniMax 約束", i)
			}
		}
	})

	t.Run("多輪 tool_use → tool 之後注入喚醒通知", func(t *testing.T) {
		// 模擬多次 tool call 之後注入喚醒通知（最易出錯嘅場景）
		msgs := []Message{
			mtMakeSystem("system"),
			mtMakeUser("安裝 rsync"),
			mtMakeAssistantWithToolCalls("round 1", "r1", "SmartShell"),
			mtMakeToolResult("apt-get update output", "r1_SmartShell"),
			mtMakeAssistantWithToolCalls("round 2", "r2", "SmartShell"),
			mtMakeToolResult("apt-get install output", "r2_SmartShell"),
			mtMakeAssistantWithToolCalls("round 3", "r3", "SmartShell"),
			mtMakeToolResult("cat /etc/os-release output", "r3_SmartShell"),
			mtMakeAssistantWithToolCalls("round 4", "r4", "SmartShell"),
			mtMakeToolResult("pkg install output", "r4_SmartShell"),
			// ⚠️ 喚醒通知注入點 — 修復後會合併到最後一條 user
			mtMakeUser("⏰ 任务唤醒通知\n\n❌ 任务ID: task_ab53e361"),
		}
		result := validateAndCleanMessages(msgs)

		// 驗證冇孤兒 tool
		ml := NewMessageList(result)
		violations := ml.Validate()
		if hasViolationCode(violations, "I3") {
			t.Errorf("多輪 tool call 後嘅合法序列不應該有 I3: %v", violations)
		}

		// 驗證冇 user 插入喺 assistant(tool_use) 同 tool 之間
		for i := 1; i < len(result); i++ {
			prev := result[i-1]
			curr := result[i]
			if prev.Role == "assistant" && prev.ToolCalls != nil && curr.Role == "user" {
				t.Errorf("index %d: assistant(tool_use) 之後不能直接跟 user，違反 MiniMax 約束", i)
			}
		}
	})

	t.Run("喚醒通知合併後緊湊的序列", func(t *testing.T) {
		// 修復後的正確行為：喚醒通知合併到現有 user 消息
		msgs := []Message{
			mtMakeSystem("system"),
			mtMakeUser("安裝 rsync\n\n⏰ 任务唤醒通知\n\n❌ 任务ID: task_abc"),
			mtMakeAssistantWithToolCalls("執行", "c1", "SmartShell"),
			mtMakeToolResult("installed", "c1_SmartShell"),
		}
		result := validateAndCleanMessages(msgs)
		ml := NewMessageList(result)
		violations := ml.Validate()
		if len(violations) > 0 {
			t.Errorf("合併喚醒通知後嘅序列不應該有違規: %v", violations)
		}
	})
}

// ============================================================================
// findLegalStart：前向掃描與 MiniMax 約束
// ============================================================================

func TestFindLegalStart_MiniMaxConstraint(t *testing.T) {
	t.Run("開頭係孤兒 tool → 前向掃描跳過", func(t *testing.T) {
		msgs := []Message{
			mtMakeToolResult("orphan", "call_orphan"),
			mtMakeUser("actual request"),
		}
		result := findLegalStart(msgs)
		if len(result) == 0 {
			t.Fatal("應該至少保留一條消息")
		}
		// 應該只保留 "actual request" 之後嘅內容
		if result[0].Role != "user" {
			t.Errorf("開頭孤兒 tool 應該被跳過，第一條應為 user，got role=%s", result[0].Role)
		}
	})

	t.Run("開頭 system + 孤兒 tool → 保留 system", func(t *testing.T) {
		msgs := []Message{
			mtMakeSystem("system"),
			mtMakeToolResult("orphan", "call_x"),
			mtMakeUser("request"),
		}
		result := findLegalStart(msgs)
		if len(result) < 2 {
			t.Fatalf("expected at least 2 messages, got %d", len(result))
		}
		if result[0].Role != "system" {
			t.Errorf("system 應該被保留，got role=%s", result[0].Role)
		}
	})

	t.Run("全部 tool 消息 → 安全回退", func(t *testing.T) {
		msgs := []Message{
			mtMakeToolResult("o1", "c1"),
			mtMakeToolResult("o2", "c2"),
		}
		result := findLegalStart(msgs)
		if len(result) == 0 {
			t.Fatal("安全回退應該至少返回一條消息")
		}
	})
}

// ============================================================================
// 端到端：從注入到 API 請求前的完整鏈路
// ============================================================================

func TestEndToEnd_MiniMaxSafeSequence(t *testing.T) {
	t.Run("完整模擬：tool 鏈路 + 喚醒通知合併 → 所有 API 約束滿足", func(t *testing.T) {
		// Step 1: 正常對話進行到一半，有多輪 tool call
		messages := []Message{
			mtMakeSystem("你係一個 helpful assistant"),
			mtMakeUser("幫我喺遠程伺服器安裝 rsync"),
			mtMakeAssistantWithToolCalls("我來 SSH", "c1", "SmartShell"),
			mtMakeToolResult("ssh connected", "c1_SmartShell"),
			mtMakeAssistantWithToolCalls("檢查 OS", "c2", "SmartShell"),
			mtMakeToolResult("FreeBSD 14.3", "c2_SmartShell"),
		}

		// Step 2: 模擬喚醒通知被合併到最後一條 user 消息（修復後行為）
		// 注意：messages 目前結尾係 tool result，冇 user 消息喺尾部
		// 所以需要向前搵 user 消息合併
		wakeContent := "⏰ 任务唤醒通知\n\n❌ 任务ID: task_abc\n\n請繼續執行"
		merged := false
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				if contentStr, ok := messages[i].Content.(string); ok {
					messages[i].Content = contentStr + "\n\n" + wakeContent
				}
				merged = true
				break
			}
		}

		if !merged {
			t.Fatal("應該搵到 user 消息合併喚醒通知")
		}

		// Step 3: 模型繼續回應新嘅 tool call
		messages = append(messages, mtMakeAssistantWithToolCalls("安裝 rsync", "c3", "SmartShell"))
		messages = append(messages, mtMakeToolResult("rsync installed", "c3_SmartShell"))

		// Step 4: validateAndCleanMessages 處理後應該合法
		result := validateAndCleanMessages(messages)
		ml := NewMessageList(result)
		violations := ml.Validate()

		if hasViolationCode(violations, "I3") {
			t.Errorf("完整鏈路不應該有 I3 違規: %v", violations)
		}

		// Step 5: 確保冇 assistant(tool_use) 後面直接跟 user
		for i := 1; i < len(result); i++ {
			if result[i-1].Role == "assistant" && result[i-1].ToolCalls != nil &&
				result[i].Role == "user" {
				t.Errorf("index %d: MiniMax 約束違反 — user 不能緊跟 assistant(tool_use)", i)
			}
		}
	})

	t.Run("邊界情況：messages 尾部只有 assistant，冇 tool results", func(t *testing.T) {
		// 模型剛剛發出 tool_use 但 tool 未執行完，呢個時候注入喚醒通知
		messages := []Message{
			mtMakeSystem("system"),
			mtMakeUser("original request"),
			mtMakeAssistantWithToolCalls("executing", "c1", "SmartShell"),
		}

		// 喚醒通知合併到最後一條 user
		wakeContent := "⏰ 唤醒通知"
		merged := false
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				if contentStr, ok := messages[i].Content.(string); ok {
					messages[i].Content = contentStr + "\n\n" + wakeContent
				}
				merged = true
				break
			}
		}

		if !merged {
			t.Fatal("應該搵到 user 消息")
		}

		// 然後 tool result 到達
		messages = append(messages, mtMakeToolResult("done", "c1_SmartShell"))

		result := validateAndCleanMessages(messages)
		ml := NewMessageList(result)
		violations := ml.Validate()

		// 關鍵檢查：tool 同 assistant 之間不能有 user
		gotUserBetween := false
		for i := 2; i < len(result); i++ {
			if result[i].Role == "tool" && result[i-1].Role == "user" {
				// 檢查呢個 tool 嘅 parent assistant 係咪被 user 隔開
				for j := i - 2; j >= 0; j-- {
					if result[j].Role == "assistant" && result[j].ToolCalls != nil {
						gotUserBetween = true
						break
					}
					if result[j].Role == "user" || result[j].Role == "system" {
						break
					}
				}
			}
		}
		if gotUserBetween {
			t.Error("tool 同其 parent assistant 之間不應該有 user 消息")
		}
		if hasViolationCode(violations, "I3") {
			t.Errorf("不應該有 I3 違規: %v", violations)
		}
	})
}

// ============================================================================
// hasViolationCode helper
// ============================================================================

func hasViolationCode(violations []InvariantViolation, code string) bool {
	for _, v := range violations {
		if v.Code == code {
			return true
		}
	}
	return false
}
