package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    "sync"

    "github.com/robfig/cron/v3"
    "github.com/toon-format/toon-go"
)

// CronJob 表示一个定时任务
type CronJob struct {
    Name        string      `toon:"Name" json:"Name"`
    Schedule    string      `toon:"Schedule" json:"Schedule"`
    UserMessage string      `toon:"UserMessage" json:"UserMessage"`
    Channel     ChannelConf `toon:"Channel" json:"Channel"`
    SessionID   string      `toon:"SessionID,omitempty" json:"SessionID,omitempty"`
    Category    string      `toon:"Category,omitempty" json:"Category,omitempty"` // "heartbeat" 或 "scheduled"
}

// ChannelConf 定义输出目标配置
type ChannelConf struct {
    Type        string        `toon:"Type" json:"Type"`
    Recipients  []string      `toon:"Recipients,omitempty" json:"Recipients,omitempty"`
    SubChannels []ChannelConf `toon:"SubChannels,omitempty" json:"SubChannels,omitempty"`
}

// CronFile 包装 cron.toon 的顶层结构
type CronFile struct {
    CronJobs []CronJob `toon:"CronJobs"`
}

// CronManager 管理所有定时任务
type CronManager struct {
    cron        *cron.Cron
    jobs        map[string]cron.EntryID
    jobData     map[string]*CronJob
    config      *CronConfig
    sem         chan struct{}
    mu          sync.RWMutex
    file        string
    stopChan    chan struct{}
    runningJobs map[string]context.CancelFunc // 正在运行的任务及其取消函数
}

// NewCronManager 创建并启动管理器
func NewCronManager(configPath string, cronConfig *CronConfig) (*CronManager, error) {
    if cronConfig == nil {
        cronConfig = &CronConfig{MaxConcurrent: 1}
    }
    if cronConfig.MaxConcurrent <= 0 {
        cronConfig.MaxConcurrent = 1
    }
    cm := &CronManager{
        cron:        cron.New(cron.WithSeconds()),
        jobs:        make(map[string]cron.EntryID),
        jobData:     make(map[string]*CronJob),
        config:      cronConfig,
        sem:         make(chan struct{}, cronConfig.MaxConcurrent),
        file:        configPath,
        stopChan:    make(chan struct{}),
        runningJobs: make(map[string]context.CancelFunc),
    }
    cm.cron.Start()

    // 加载静态任务
    if err := cm.loadJobs(); err != nil && !os.IsNotExist(err) {
        return nil, err
    }
    return cm, nil
}

// loadJobs 从 cron.toon 加载任务
func (cm *CronManager) loadJobs() error {
    data, err := os.ReadFile(cm.file)
    if err != nil {
        return err
    }
    var cf CronFile
    if err := toon.Unmarshal(data, &cf); err != nil {
        return fmt.Errorf("failed to parse cron jobs TOON: %w", err)
    }
    for _, job := range cf.CronJobs {
        if err := cm.AddJob(&job); err != nil {
            log.Printf("Failed to load job %s: %v", job.Name, err)
        }
    }
    return nil
}

// saveJobs 将任务列表写回文件（公开方法，会获取读锁）
func (cm *CronManager) saveJobs() error {
    cm.mu.RLock()
    jobs := make([]CronJob, 0, len(cm.jobData))
    for _, job := range cm.jobData {
        jobs = append(jobs, *job)
    }
    cm.mu.RUnlock()

    return cm.saveJobsWithData(jobs)
}

// saveJobsUnlocked 内部方法，假设锁已持有（用于 AddJob、RemoveJob 等已持有锁的方法）
func (cm *CronManager) saveJobsUnlocked() error {
    jobs := make([]CronJob, 0, len(cm.jobData))
    for _, job := range cm.jobData {
        jobs = append(jobs, *job)
    }
    return cm.saveJobsWithData(jobs)
}

// saveJobsWithData 实际写入文件的逻辑（不涉及锁操作）
func (cm *CronManager) saveJobsWithData(jobs []CronJob) error {
    cf := CronFile{CronJobs: jobs}
    data, err := toon.Marshal(cf)
    if err != nil {
        return fmt.Errorf("failed to marshal jobs to TOON: %w", err)
    }
    tmp := cm.file + ".tmp"
    if err := os.WriteFile(tmp, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, cm.file)
}

// AddJob 添加任务时，若 Category 为空则设为 "scheduled"
func (cm *CronManager) AddJob(job *CronJob) error {
    if job.Name == "" {
        return fmt.Errorf("job name cannot be empty")
    }
    if job.Schedule == "" {
        return fmt.Errorf("schedule cannot be empty")
    }
    if job.UserMessage == "" {
        return fmt.Errorf("user_message cannot be empty")
    }
    if job.Category == "" {
        job.Category = "scheduled"
    }

    cm.mu.Lock()
    defer cm.mu.Unlock()

    if _, exists := cm.jobs[job.Name]; exists {
        cm.removeJobUnlocked(job.Name)
    }

    entryID, err := cm.cron.AddFunc(job.Schedule, func() {
        cm.executeJob(job)
    })
    if err != nil {
        return fmt.Errorf("invalid cron schedule: %w", err)
    }

    cm.jobs[job.Name] = entryID
    jobCopy := *job
    cm.jobData[job.Name] = &jobCopy

    return cm.saveJobsUnlocked()
}

// removeJobUnlocked 假设锁已持有，从调度器中移除
func (cm *CronManager) removeJobUnlocked(name string) {
    if id, ok := cm.jobs[name]; ok {
        cm.cron.Remove(id)
        delete(cm.jobs, name)
        delete(cm.jobData, name)
    }
    // 如果任务正在运行，取消它
    if cancel, ok := cm.runningJobs[name]; ok {
        log.Printf("Cron job %s is running, cancelling...", name)
        cancel()
        delete(cm.runningJobs, name)
    }
}

// RemoveJob 删除任务
func (cm *CronManager) RemoveJob(name string) error {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    if _, ok := cm.jobs[name]; !ok {
        return fmt.Errorf("job %s not found", name)
    }
    // 检查任务是否正在运行
    _, isRunning := cm.runningJobs[name]
    cm.removeJobUnlocked(name)
    // 使用 saveJobsUnlocked，因为锁已持有
    if err := cm.saveJobsUnlocked(); err != nil {
        return err
    }
    // 返回 nil，但调用者可以通过 IsJobRunning 检查状态
    // 这里我们不返回额外信息，保持 API 简洁
    _ = isRunning // 由调用者决定如何处理
    return nil
}

// IsJobRunning 检查任务是否正在运行
func (cm *CronManager) IsJobRunning(name string) bool {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    _, ok := cm.runningJobs[name]
    return ok
}

// ListJobs 返回所有任务列表（只读）
func (cm *CronManager) ListJobs() []CronJob {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    jobs := make([]CronJob, 0, len(cm.jobData))
    for _, job := range cm.jobData {
        jobs = append(jobs, *job)
    }
    return jobs
}

// GetJobStatus 返回任务状态（下次执行时间等）
func (cm *CronManager) GetJobStatus(name string) (map[string]interface{}, error) {
    cm.mu.RLock()
    id, ok := cm.jobs[name]
    if !ok {
        cm.mu.RUnlock()
        return nil, fmt.Errorf("job not found")
    }
    entry := cm.cron.Entry(id)

    // 从 jobData 获取原始 schedule 字符串
    job, jobExists := cm.jobData[name]
    cm.mu.RUnlock()

    if !jobExists {
        return nil, fmt.Errorf("job data not found")
    }

    status := map[string]interface{}{
        "name":     name,
        "next":     entry.Next,
        "prev":     entry.Prev,
        "valid":    entry.Valid,
        "schedule": job.Schedule, // 返回原始的 cron 表达式字符串
    }
    return status, nil
}

// Stop 停止调度器，等待所有运行中任务完成
func (cm *CronManager) Stop() {
    close(cm.stopChan)
    ctx := cm.cron.Stop() // 返回一个 context.Context
    <-ctx.Done()          // 等待所有正在执行的任务完成
}

// executeJob 执行任务（由调度器调用）
func (cm *CronManager) executeJob(job *CronJob) {
    // 获取并发控制信号量
    select {
    case cm.sem <- struct{}{}:
        defer func() { <-cm.sem }()
    case <-cm.stopChan:
        log.Printf("Cron job %s cancelled due to shutdown", job.Name)
        return
    }

    // 1. 创建基础通道（用户配置的目标）
    baseCh, err := createChannelFromConf(job.Name, &job.Channel)
    if err != nil {
        log.Printf("Failed to create base channel for job %s: %v", job.Name, err)
        return
    }

    var ch Channel = baseCh

    // 2. 如果有会话且已连接，将会话通道加入复合输出
	if job.SessionID != "" {
		session := GetGlobalSession()
		// 假设全局会话总是存在的，且判断连接状态可能需要新的字段
		if session.Connected {  // 需要在 GlobalSession 中添加 Connected 字段（bool）
		    sessionCh := NewSessionChannel(session)
		    ch = NewCompositeChannel(baseCh, sessionCh)
		}
	}

    defer ch.Close()

    // 创建可取消的 context
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // 注册 cancel 函数，以便在移除任务时能够终止运行中的任务
    cm.mu.Lock()
    cm.runningJobs[job.Name] = cancel
    cm.mu.Unlock()

    // 任务结束时清理 cancel 函数
    defer func() {
        cm.mu.Lock()
        delete(cm.runningJobs, job.Name)
        cm.mu.Unlock()
    }()

    // 创建一个 done channel，用于通知监听 goroutine 任务已完成
    taskDone := make(chan struct{})
    defer close(taskDone)

    // 启动一个 goroutine 来监听 stopChan 和 taskDone
    go func() {
        select {
        case <-cm.stopChan:
            log.Printf("Cron job %s received shutdown signal, cancelling...", job.Name)
            cancel()
        case <-taskDone:
            // 任务正常结束，无需额外操作
        case <-ctx.Done():
            // 上下文已取消（可能由 removeJob 触发）
        }
    }()

    // 统一处理斜杠命令
    trimmedMsg := strings.TrimSpace(job.UserMessage)
    if strings.HasPrefix(trimmedMsg, "/") {
        if globalRoleManager != nil && globalActorManager != nil && globalStage != nil {
            result := ProcessSlashCommand(trimmedMsg, globalRoleManager, globalActorManager, globalStage)
            if result.Handled {
                if result.Response != "" {
                    ch.WriteChunk(StreamChunk{Content: result.Response, Done: true})
                }
                return
            }
        }
    }

    // 构建历史消息
    history := []Message{
        {Role: "user", Content: job.UserMessage},
    }

    // 启动 AgentLoop
    newHistory, err := AgentLoop(ctx, ch, history, apiType, baseURL, apiKey, modelID,
        temperature, maxTokens, stream, thinking)

    // 如果是心跳任务，发布心跳事件
    if job.Category == "heartbeat" {
        status := "completed"
        output := "任务执行完成"
        if err != nil {
            status = "failed"
            output = err.Error()
        }
        GetBus().NotifyCron(job.Name, status, output)
    }

    if err != nil {
        if err == context.Canceled {
            log.Printf("Cron job %s cancelled", job.Name)
        } else {
            log.Printf("Cron job %s execution error: %v", job.Name, err)
        }
    } else {
        log.Printf("Cron job %s completed.", job.Name)
        _ = newHistory
    }
}
