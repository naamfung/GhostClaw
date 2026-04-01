package main

import (
        "log"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"

        "github.com/fsnotify/fsnotify"
        "github.com/toon-format/toon-go"
)

// RoleHotLoader 角色热加载器
type RoleHotLoader struct {
        mu            sync.RWMutex
        rolesDir   string
        manager       *RoleManager
        watcher       *fsnotify.Watcher
        stopCh        chan struct{}
        fileModTimes  map[string]time.Time
        checkInterval time.Duration
}

// NewRoleHotLoader 创建角色热加载器
func NewRoleHotLoader(rolesDir string, manager *RoleManager) (*RoleHotLoader, error) {
        loader := &RoleHotLoader{
                rolesDir:   rolesDir,
                manager:       manager,
                stopCh:        make(chan struct{}),
                fileModTimes:  make(map[string]time.Time),
                checkInterval: 2 * time.Second,
        }

        // 记录初始文件修改时间
        loader.recordInitialModTimes()

        // 尝试创建 fsnotify watcher
        watcher, err := fsnotify.NewWatcher()
        if err != nil {
                log.Printf("Warning: fsnotify not available, using polling: %v", err)
                // 使用轮询模式
                go loader.pollLoop()
        } else {
                loader.watcher = watcher
                // 添加目录监控
                if err := watcher.Add(rolesDir); err != nil {
                        log.Printf("Warning: failed to watch roles dir: %v", err)
                }
                // 监控子目录
                filepath.Walk(rolesDir, func(path string, info os.FileInfo, err error) error {
                        if err == nil && info.IsDir() && path != rolesDir {
                                watcher.Add(path)
                        }
                        return nil
                })
                go loader.watchLoop()
        }

        return loader, nil
}

// recordInitialModTimes 记录初始文件修改时间
func (l *RoleHotLoader) recordInitialModTimes() {
        filepath.Walk(l.rolesDir, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return nil
                }
                if !info.IsDir() && (filepath.Ext(path) == ".toon" || filepath.Ext(path) == ".md") {
                        l.fileModTimes[path] = info.ModTime()
                }
                return nil
        })
}

// watchLoop 使用 fsnotify 监控文件变化
func (l *RoleHotLoader) watchLoop() {
        for {
                select {
                case <-l.stopCh:
                        return
                case event, ok := <-l.watcher.Events:
                        if !ok {
                                return
                        }
                        if event.Op&fsnotify.Write == fsnotify.Write || 
                           event.Op&fsnotify.Create == fsnotify.Create ||
                           event.Op&fsnotify.Remove == fsnotify.Remove {
                                if filepath.Ext(event.Name) == ".toon" || filepath.Ext(event.Name) == ".md" {
                                        log.Printf("Role file changed: %s", event.Name)
                                        l.handleFileChange(event.Name, event.Op)
                                }
                        }
                case err, ok := <-l.watcher.Errors:
                        if !ok {
                                return
                        }
                        log.Printf("Watcher error: %v", err)
                }
        }
}

// pollLoop 使用轮询检查文件变化（备用方案）
func (l *RoleHotLoader) pollLoop() {
        ticker := time.NewTicker(l.checkInterval)
        defer ticker.Stop()

        for {
                select {
                case <-l.stopCh:
                        return
                case <-ticker.C:
                        l.checkForChanges()
                }
        }
}

// checkForChanges 检查文件变化
func (l *RoleHotLoader) checkForChanges() {
        l.mu.Lock()
        defer l.mu.Unlock()

        currentTimes := make(map[string]time.Time)
        changedFiles := make([]string, 0)
        deletedFiles := make([]string, 0)

        // 检查现有文件
        filepath.Walk(l.rolesDir, func(path string, info os.FileInfo, err error) error {
                if err != nil || info.IsDir() {
                        return nil
                }
                if filepath.Ext(path) != ".toon" && filepath.Ext(path) != ".md" {
                        return nil
                }

                currentTimes[path] = info.ModTime()
                oldTime, exists := l.fileModTimes[path]
                if !exists || !oldTime.Equal(info.ModTime()) {
                        changedFiles = append(changedFiles, path)
                }
                return nil
        })

        // 检查已删除的文件
        for path := range l.fileModTimes {
                if _, exists := currentTimes[path]; !exists {
                        deletedFiles = append(deletedFiles, path)
                }
        }

        // 更新文件时间记录
        l.fileModTimes = currentTimes

        // 处理变化
        if len(changedFiles) > 0 || len(deletedFiles) > 0 {
                log.Printf("Detected changes: %d changed, %d deleted", len(changedFiles), len(deletedFiles))
                l.reloadAll()
        }
}

// handleFileChange 处理单个文件变化
func (l *RoleHotLoader) handleFileChange(path string, op fsnotify.Op) {
        l.mu.Lock()
        defer l.mu.Unlock()

        if op&fsnotify.Remove == fsnotify.Remove {
                // 文件被删除，需要重新加载全部
                log.Printf("Role file removed: %s, reloading all", path)
                l.reloadAll()
                return
        }

        // 文件被修改或创建
        log.Printf("Reloading role file: %s", path)
        l.reloadFile(path)
}

// reloadFile 重新加载单个文件
func (l *RoleHotLoader) reloadFile(path string) {
        // 读取并解析文件
        data, err := os.ReadFile(path)
        if err != nil {
                log.Printf("Failed to read role file %s: %v", path, err)
                return
        }

        var role Role
        if err := toon.Unmarshal(data, &role); err != nil {
                log.Printf("Failed to parse role file %s: %v", path, err)
                return
        }

        // 确定角色名
        relPath, err := filepath.Rel(l.rolesDir, path)
        if err != nil {
                relPath = path
        }
        roleName := strings.TrimSuffix(relPath, filepath.Ext(relPath))
        roleName = strings.ReplaceAll(roleName, string(filepath.Separator), "/")

        if role.Name == "" {
                role.Name = roleName
        }

        // 更新管理器
        l.manager.mu.Lock()
        l.manager.roles[role.Name] = &role
        l.manager.mu.Unlock()

        log.Printf("Reloaded role: %s", role.Name)
}

// reloadAll 重新加载所有角色
func (l *RoleHotLoader) reloadAll() {
        // 清空现有角色（保留预置角色）
        l.manager.mu.Lock()
        presets := GetPresetRoles()
        l.manager.roles = make(map[string]*Role)
        for _, p := range presets {
                l.manager.roles[p.Name] = p
        }
        l.manager.mu.Unlock()

        // 重新从目录加载
        if err := l.manager.loadFromDirectory(); err != nil {
                log.Printf("Failed to reload roles from directory: %v", err)
        }

        log.Printf("Reloaded all roles, total: %d", l.manager.Count())
}

// Stop 停止热加载
func (l *RoleHotLoader) Stop() {
        close(l.stopCh)
        if l.watcher != nil {
                l.watcher.Close()
        }
}

// ForceReload 强制重新加载所有角色
func (l *RoleHotLoader) ForceReload() {
        l.mu.Lock()
        defer l.mu.Unlock()
        l.reloadAll()
}

// GetStatus 获取热加载状态
func (l *RoleHotLoader) GetStatus() map[string]interface{} {
        l.mu.RLock()
        defer l.mu.RUnlock()

        status := map[string]interface{}{
                "roles_dir":   l.rolesDir,
                "file_count":     len(l.fileModTimes),
                "check_interval": l.checkInterval.String(),
                "watcher_active": l.watcher != nil,
        }

        return status
}
