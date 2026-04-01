package main

import (
        "fmt"
        "os/exec"
        "strings"
)

// 版本信息，通过编译时 -ldflags 注入
var (
        // 版本号，如 v2.6.2
        Version = "v0.1.4"
        // Git 提交哈希，如 abc1234
        GitCommit = "unknown"
        // 构建时间
        BuildTime = "unknown"
)

// GetGitInfo 尝试从 .git 目录获取 git 信息（作为备用）
func GetGitInfo() (commit string, shortCommit string) {
        // 尝试获取 git commit
        cmd := exec.Command("git", "rev-parse", "HEAD")
        output, err := cmd.Output()
        if err == nil {
                commit = strings.TrimSpace(string(output))
                shortCommit = commit
                if len(commit) >= 7 {
                        shortCommit = commit[:7]
                }
        }
        return
}

// PrintBanner 打印启动横幅，包含版本信息
func PrintBanner() {
        // 获取备用 git 信息
        _, gitShort := GetGitInfo()

        // 如果编译时注入了信息，使用注入的；否则使用动态获取的
        displayCommit := GitCommit
        if GitCommit == "unknown" && gitShort != "" {
                displayCommit = gitShort
        }

        // 打印简洁的启动信息
        fmt.Println()
        fmt.Printf("  GhostClaw %s", Version)
        if displayCommit != "unknown" {
                fmt.Printf(" (%s)", displayCommit)
        }
        fmt.Println()
        fmt.Println()
}

func init() {
        // 打印启动横幅（版本信息）
        PrintBanner()
}
