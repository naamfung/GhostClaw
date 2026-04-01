package main

import (
        "context"
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "os"
        "strings"
        "sync/atomic"
        "testing"
        "time"
)

// 测试用的全局邮件配置
var testEmailConfig = &EmailConfig{
        SMTPServer:   "smtp.example.com",
        SMTPPort:     587,
        SMTPUseTLS:   true,
        SMTPUser:     "test@example.com",
        SMTPPassword: "password",
}

var mockServer *httptest.Server

// TestMain 统一管理测试前准备和测试后清理
func TestMain(m *testing.M) {
        // 启动 mock API 服务器
        mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path == "/chat/completions" {
                        resp := map[string]interface{}{
                                "choices": []map[string]interface{}{
                                        {
                                                "message": map[string]interface{}{
                                                        "role":    "assistant",
                                                        "content": "This is a mock response from test server.",
                                                },
                                                "finish_reason": "stop",
                                        },
                                },
                        }
                        w.Header().Set("Content-Type", "application/json")
                        json.NewEncoder(w).Encode(resp)
                        return
                }
                http.NotFound(w, r)
        }))

        // 覆盖全局 API 配置
        apiType = "openai"
        baseURL = mockServer.URL
        apiKey = "test-key"
        modelID = "test-model"
        temperature = 0.0
        maxTokens = 100
        stream = false
        thinking = false
        IsDebug = false // 关闭调试日志，加速测试

        // 运行所有测试
        code := m.Run()

        // 清理
        mockServer.Close()
        os.Exit(code)
}

// TestCronManager_Basic 测试添加、删除、列表、状态等基本功能（任务不实际触发）
func TestCronManager_Basic(t *testing.T) {
        tmpDir := t.TempDir()
        cronFile := tmpDir + "/cron.toon"

        cfg := &CronConfig{
                MaxConcurrent: 2,
        }
        cm, err := NewCronManager(cronFile, cfg)
        if err != nil {
                t.Fatalf("Failed to create CronManager: %v", err)
        }
        defer cm.Stop()

        // 使用 6 字段 cron 表达式，永远不会触发（每年1月1日0点0秒）
        job := &CronJob{
                Name:        "test_job",
                Schedule:    "0 0 0 1 1 *", // 秒 分 时 日 月 周
                UserMessage: "test message",
                Channel:     ChannelConf{Type: "log"},
        }
        err = cm.AddJob(job)
        if err != nil {
                t.Fatalf("AddJob failed: %v", err)
        }

        // 列出任务
        jobs := cm.ListJobs()
        if len(jobs) != 1 {
                t.Fatalf("Expected 1 job, got %d", len(jobs))
        }
        if jobs[0].Name != job.Name {
                t.Errorf("Job name mismatch: expected %s, got %s", job.Name, jobs[0].Name)
        }

        // 获取状态
        status, err := cm.GetJobStatus(job.Name)
        if err != nil {
                t.Fatalf("GetJobStatus failed: %v", err)
        }
        if status["name"] != job.Name {
                t.Errorf("Status name mismatch: expected %s, got %v", job.Name, status["name"])
        }
        if next, ok := status["next"].(time.Time); !ok || next.IsZero() {
                t.Error("Next execution time is zero")
        }
        if schedule, ok := status["schedule"].(string); !ok || schedule != job.Schedule {
                t.Errorf("Schedule mismatch: expected %s, got %v", job.Schedule, schedule)
        }

        // 删除任务
        err = cm.RemoveJob(job.Name)
        if err != nil {
                t.Fatalf("RemoveJob failed: %v", err)
        }
        jobs = cm.ListJobs()
        if len(jobs) != 0 {
                t.Errorf("Expected 0 jobs after removal, got %d", len(jobs))
        }
}

// TestCronManager_Persistence 测试持久化：添加任务后重启管理器应能加载
func TestCronManager_Persistence(t *testing.T) {
        tmpDir := t.TempDir()
        cronFile := tmpDir + "/cron.toon"

        cfg := &CronConfig{
                MaxConcurrent: 1,
        }

        // 第一个管理器，添加任务
        cm1, err := NewCronManager(cronFile, cfg)
        if err != nil {
                t.Fatalf("Failed to create first CronManager: %v", err)
        }
        job := &CronJob{
                Name:        "persist_job",
                Schedule:    "0 0 0 * * *", // 每天0点0秒
                UserMessage: "test",
                Channel:     ChannelConf{Type: "log"},
        }
        err = cm1.AddJob(job)
        if err != nil {
                t.Fatalf("AddJob failed: %v", err)
        }
        cm1.Stop()

        // 第二个管理器，重新加载
        cm2, err := NewCronManager(cronFile, cfg)
        if err != nil {
                t.Fatalf("Failed to create second CronManager: %v", err)
        }
        defer cm2.Stop()

        jobs := cm2.ListJobs()
        if len(jobs) != 1 {
                t.Fatalf("Expected 1 job after reload, got %d", len(jobs))
        }
        if jobs[0].Name != job.Name {
                t.Errorf("Job name mismatch after reload: expected %s, got %s", job.Name, jobs[0].Name)
        }
}

// TestCronManager_ConcurrentControl 测试任务实际执行并自动停止
func TestCronManager_ConcurrentControl(t *testing.T) {
        tmpDir := t.TempDir()
        cronFile := tmpDir + "/cron.toon"

        cfg := &CronConfig{
                MaxConcurrent: 1,
        }
        cm, err := NewCronManager(cronFile, cfg)
        if err != nil {
                t.Fatalf("Failed to create CronManager: %v", err)
        }
        defer cm.Stop()

        // 创建带计数器的 mock 服务器
        var mockCount int32
        countServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path == "/chat/completions" {
                        atomic.AddInt32(&mockCount, 1)
                        resp := map[string]interface{}{
                                "choices": []map[string]interface{}{
                                        {
                                                "message": map[string]interface{}{
                                                        "role":    "assistant",
                                                        "content": "Mock response",
                                                },
                                                "finish_reason": "stop",
                                        },
                                },
                        }
                        w.Header().Set("Content-Type", "application/json")
                        json.NewEncoder(w).Encode(resp)
                        return
                }
                http.NotFound(w, r)
        }))
        defer countServer.Close()

        // 保存原 baseURL，恢复
        oldBaseURL := baseURL
        defer func() { baseURL = oldBaseURL }()
        baseURL = countServer.URL

        // 添加任务
        err = cm.AddJob(&CronJob{
                Name:        "test_count",
                Schedule:    "@every 100ms", // 每100ms触发一次
                UserMessage: "test",
                Channel:     ChannelConf{Type: "log"},
        })
        if err != nil {
                t.Fatalf("AddJob failed: %v", err)
        }

        // 等待计数达到目标次数（例如3次）
        const targetExecutions = 3
        done := make(chan struct{})

        go func() {
                for {
                        if atomic.LoadInt32(&mockCount) >= targetExecutions {
                                // 停止调度器，防止新任务触发
                                cm.Stop()
                                close(done)
                                return
                        }
                        time.Sleep(50 * time.Millisecond)
                }
        }()

        select {
        case <-done:
                // 正常结束
        case <-time.After(5 * time.Second):
                t.Fatal("Test timed out waiting for job executions")
        }

        // 验证计数达到预期
        if atomic.LoadInt32(&mockCount) < targetExecutions {
                t.Errorf("Expected at least %d executions, got %d", targetExecutions, mockCount)
        }
}

// TestCreateChannelFromConf 测试 Channel 工厂函数
func TestCreateChannelFromConf(t *testing.T) {
        oldEmailConfig := globalEmailConfig
        defer func() { globalEmailConfig = oldEmailConfig }()
        globalEmailConfig = testEmailConfig

        tests := []struct {
                name      string
                conf      *ChannelConf
                wantType  string
                wantError bool
        }{
                {
                        name:      "log channel",
                        conf:      &ChannelConf{Type: "log"},
                        wantType:  "log",
                        wantError: false,
                },
                {
                        name: "email channel with recipients",
                        conf: &ChannelConf{
                                Type:       "email",
                                Recipients: []string{"user@example.com", "admin@example.com"},
                        },
                        wantType:  "composite",
                        wantError: false,
                },
                {
                        name: "email channel with single recipient",
                        conf: &ChannelConf{
                                Type:       "email",
                                Recipients: []string{"user@example.com"},
                        },
                        wantType:  "email",
                        wantError: false,
                },
                {
                        name:      "email channel without recipients",
                        conf:      &ChannelConf{Type: "email"},
                        wantError: true,
                },
                {
                        name: "email channel without global config",
                        conf: &ChannelConf{
                                Type:       "email",
                                Recipients: []string{"user@example.com"},
                        },
                        wantError: true,
                },
                {
                        name: "composite channel with subchannels",
                        conf: &ChannelConf{
                                Type: "composite",
                                SubChannels: []ChannelConf{
                                        {Type: "log"},
                                        {Type: "email", Recipients: []string{"user@example.com"}},
                                },
                        },
                        wantType:  "composite",
                        wantError: false,
                },
                {
                        name:      "composite channel without subchannels",
                        conf:      &ChannelConf{Type: "composite"},
                        wantError: true,
                },
                {
                        name:      "unknown type",
                        conf:      &ChannelConf{Type: "unknown"},
                        wantError: true,
                },
        }

        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        if tt.name == "email channel without global config" {
                                globalEmailConfig = nil
                                defer func() { globalEmailConfig = testEmailConfig }()
                        }
                        ch, err := createChannelFromConf("test_job", tt.conf)
                        if tt.wantError {
                                if err == nil {
                                        t.Error("Expected error, got nil")
                                }
                                return
                        }
                        if err != nil {
                                t.Fatalf("Unexpected error: %v", err)
                        }
                        if ch == nil {
                                t.Fatal("Channel is nil")
                        }
                        switch tt.wantType {
                        case "log":
                                if _, ok := ch.(*LogChannel); !ok {
                                        t.Errorf("Expected *LogChannel, got %T", ch)
                                }
                        case "email":
                                if _, ok := ch.(*EmailChannel); !ok {
                                        t.Errorf("Expected *EmailChannel, got %T", ch)
                                }
                        case "composite":
                                if _, ok := ch.(*CompositeChannel); !ok {
                                        t.Errorf("Expected *CompositeChannel, got %T", ch)
                                }
                        }
                })
        }
}

// TestToolHandlers 测试 cron 工具处理函数（需要模拟 globalCronManager）
func TestToolHandlers(t *testing.T) {
        tmpDir := t.TempDir()
        cronFile := tmpDir + "/cron.toon"
        cfg := &CronConfig{
                MaxConcurrent: 1,
        }
        cm, err := NewCronManager(cronFile, cfg)
        if err != nil {
                t.Fatalf("Failed to create CronManager: %v", err)
        }
        defer cm.Stop()

        oldGlobalCronManager := globalCronManager
        defer func() { globalCronManager = oldGlobalCronManager }()
        globalCronManager = cm

        oldEmailConfig := globalEmailConfig
        defer func() { globalEmailConfig = oldEmailConfig }()
        globalEmailConfig = testEmailConfig

        // 添加任务
        addArgs := map[string]interface{}{
                "name":         "tool_test_job",
                "schedule":     "0 0 0 * * *", // 6 字段
                "user_message": "test message",
                "channel":      map[string]interface{}{"type": "log"},
                "timeout_sec":  60,
        }
        content, used := handleCronAdd(context.Background(), addArgs, &dummyChannel{})
        if used {
                t.Error("handleCronAdd returned usedTodo = true")
        }
        if content == "" {
                t.Error("handleCronAdd returned empty content")
        }
        if !strings.Contains(content, "added successfully") {
                t.Errorf("Unexpected success message: %s", content)
        }

        // 验证任务已添加
        jobs := cm.ListJobs()
        if len(jobs) != 1 {
                t.Fatalf("Expected 1 job, got %d", len(jobs))
        }
        if jobs[0].Name != "tool_test_job" {
                t.Errorf("Job name mismatch: expected tool_test_job, got %s", jobs[0].Name)
        }

        // 列出任务
        listArgs := map[string]interface{}{}
        content, used = handleCronList(context.Background(), listArgs, &dummyChannel{})
        if used {
                t.Error("handleCronList returned usedTodo = true")
        }
        if !strings.Contains(content, "tool_test_job") {
                t.Errorf("Job not found in list: %s", content)
        }

        // 查询状态
        statusArgs := map[string]interface{}{"name": "tool_test_job"}
        content, used = handleCronStatus(context.Background(), statusArgs, &dummyChannel{})
        if used {
                t.Error("handleCronStatus returned usedTodo = true")
        }
        var status map[string]interface{}
        if err := json.Unmarshal([]byte(content), &status); err != nil {
                t.Errorf("Failed to parse status JSON: %v, content=%s", err, content)
        }
        if status["name"] != "tool_test_job" {
                t.Errorf("Status name mismatch: expected tool_test_job, got %v", status["name"])
        }

        // 删除任务
        removeArgs := map[string]interface{}{"name": "tool_test_job"}
        content, used = handleCronRemove(context.Background(), removeArgs, &dummyChannel{})
        if used {
                t.Error("handleCronRemove returned usedTodo = true")
        }
        if !strings.Contains(content, "removed") {
                t.Errorf("Unexpected removal message: %s", content)
        }

        // 验证任务已删除
        jobs = cm.ListJobs()
        if len(jobs) != 0 {
                t.Errorf("Expected 0 jobs after removal, got %d", len(jobs))
        }
}

// TestParseChannelConf 测试配置解析函数
func TestParseChannelConf(t *testing.T) {
        tests := []struct {
                name     string
                input    map[string]interface{}
                expected ChannelConf
        }{
                {
                        name:  "log channel",
                        input: map[string]interface{}{"type": "log"},
                        expected: ChannelConf{Type: "log"},
                },
                {
                        name: "email channel",
                        input: map[string]interface{}{
                                "type":       "email",
                                "recipients": []interface{}{"a@b.com", "c@d.com"},
                        },
                        expected: ChannelConf{
                                Type:       "email",
                                Recipients: []string{"a@b.com", "c@d.com"},
                        },
                },
                {
                        name: "composite channel",
                        input: map[string]interface{}{
                                "type": "composite",
                                "sub_channels": []interface{}{
                                        map[string]interface{}{"type": "log"},
                                        map[string]interface{}{
                                                "type":       "email",
                                                "recipients": []interface{}{"user@example.com"},
                                        },
                                },
                        },
                        expected: ChannelConf{
                                Type: "composite",
                                SubChannels: []ChannelConf{
                                        {Type: "log"},
                                        {Type: "email", Recipients: []string{"user@example.com"}},
                                },
                        },
                },
        }

        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        result := parseChannelConf(tt.input)
                        expectedJSON, _ := json.Marshal(tt.expected)
                        resultJSON, _ := json.Marshal(result)
                        if string(expectedJSON) != string(resultJSON) {
                                t.Errorf("Parse result mismatch\nExpected: %s\nGot: %s", expectedJSON, resultJSON)
                        }
                })
        }
}
