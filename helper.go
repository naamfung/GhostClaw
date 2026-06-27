package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// tempDirResolveCounter 统计 resolveTempDir 的调用次数。
// 当计数达到 2 时（即程序启动后第二次解析临时目录），触发一次 <dataDir>/temp 的过期清理。
// 使用 atomic 计数器保证并发安全，无需加锁。
var tempDirResolveCounter int64

// tempDirCleanupOnce 确保过期清理逻辑只触发一次（避免重复扫描磁盘）。
var tempDirCleanupOnce sync.Once

// resolveTempDir 统一解析临时性目录路径。
//
// 策略：
//  1. 优先使用系统临时目录：os.TempDir() + "/ghostclaw-<subdir>"
//  2. 若系统临时目录不可写（权限拒绝、磁盘满等），回退到：
//     globalDataDir + "/temp/<subdir>"（即「程序自身目录/data/temp/<subdir>」）
//
// 此函数用于避免临时文件像 "tool_results_cache" 那样被散落地创建在
// 程序的当前工作目录中，让临时数据有统一、可预测的存放位置。
//
// 注意：调用方应在 globalDataDir 已初始化后使用；若 globalDataDir 为空
// （例如在单元测试的早期路径中），将退化为 execDir/data/temp/<subdir>。
//
// 副作用：当程序启动后第二次调用此函数时（计数=2），会异步触发一次
// <dataDir>/temp 的过期清理（删除 24 小时前的文件），避免 data/temp
// 长期堆积。系统临时目录（/tmp）由 OS 自身的 tmpfiles.d / reboot 自动清理，
// 无需程序干预。
func resolveTempDir(subdir string) string {
	if subdir == "" {
		subdir = "tmp"
	}

	// 1. 尝试系统临时目录
	sysTmp := filepath.Join(os.TempDir(), "ghostclaw-"+subdir)
	if isDirWritable(sysTmp) {
		maybeTriggerDataTempCleanup()
		return sysTmp
	}

	// 2. 回退到 data/temp/<subdir>
	base := globalDataDir
	if base == "" {
		// 极少数情况：globalDataDir 尚未初始化，使用 execDir/data 兜底
		base = filepath.Join(getExecDir(), "data")
	}
	fallback := filepath.Join(base, "temp", subdir)
	// 不在此处 MkdirAll，让调用方按需创建（保持函数纯粹、便于测试）

	maybeTriggerDataTempCleanup()
	return fallback
}

// maybeTriggerDataTempCleanup 在 resolveTempDir 被调用第二次时触发一次
// <dataDir>/temp 的过期清理。后续调用不再触发（sync.Once 保证）。
//
// 设计依据：第一次调用通常发生在程序启动早期（globalDataDir 可能未完全初始化），
// 第二次调用时基本可以保证 globalDataDir 已就绪，此时触发清理最安全。
// 清理逻辑异步执行（go routine），不阻塞主调用路径。
func maybeTriggerDataTempCleanup() {
	count := atomic.AddInt64(&tempDirResolveCounter, 1)
	if count != 2 {
		return
	}
	tempDirCleanupOnce.Do(func() {
		go cleanupDataTempDir()
	})
}

// cleanupDataTempDir 遍历 <dataDir>/temp 目录，删除修改时间在 24 小时之前的文件。
//
// 作用域：仅限 <dataDir>/temp 子树，不影响 <dataDir> 下其他子目录
// （如 memory、plugins、skills 等持久化数据）。
//
// 策略：
//   - 一日（24h）之前的文件自动删除
//   - 空目录自动删除（保持 temp 目录干净）
//   - 单个文件删除失败不影响其他文件
//   - 任何错误只记录日志，不向上抛出（清理是 best-effort）
//
// 注意：系统临时目录（os.TempDir()，如 /tmp）由 OS 自身的 tmpfiles.d
// 或重启自动清理，本函数不处理。
func cleanupDataTempDir() {
	base := globalDataDir
	if base == "" {
		base = filepath.Join(getExecDir(), "data")
	}
	tempRoot := filepath.Join(base, "temp")

	// 如果 temp 目录不存在，无需清理
	info, err := os.Stat(tempRoot)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[TempCleanup] stat %s failed: %v", tempRoot, err)
		}
		return
	}
	if !info.IsDir() {
		return
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	removed := 0
	var errs []string

	// filepath.Walk 遍历整棵 temp 子树
	err = filepath.Walk(tempRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			errs = append(errs, fmt.Sprintf("walk %s: %v", path, err))
			return nil // 跳过此项，继续遍历其他
		}

		// 跳过 tempRoot 本身（最后单独处理空目录删除）
		if path == tempRoot {
			return nil
		}

		// 文件：检查修改时间，过期则删除
		if !info.IsDir() {
			if info.ModTime().Before(cutoff) {
				if rmErr := os.Remove(path); rmErr != nil {
					errs = append(errs, fmt.Sprintf("remove %s: %v", path, rmErr))
				} else {
					removed++
				}
			}
			return nil
		}

		// 目录：先递归处理其内容（Walk 默认行为），稍后删除空目录
		return nil
	})

	// 第二轮：自底向上删除空目录（保持 temp 目录干净）
	// 必须在 Walk 完成后做，因为 Walk 期间不能删除当前遍历的目录
	if err == nil {
		emptyRemoved := removeEmptyDirsUnder(tempRoot)
		if IsDebug && emptyRemoved > 0 {
			log.Printf("[TempCleanup] removed %d empty directories under %s", emptyRemoved, tempRoot)
		}
	}

	if removed > 0 || len(errs) > 0 || IsDebug {
		log.Printf("[TempCleanup] %s: removed %d expired file(s), %d error(s)",
			tempRoot, removed, len(errs))
		for _, e := range errs {
			log.Printf("[TempCleanup] error: %s", e)
		}
	}
}

// removeEmptyDirsUnder 自底向上删除 root 下所有空目录（不删除 root 本身）。
// 返回删除的目录数。失败静默忽略（best-effort）。
func removeEmptyDirsUnder(root string) int {
	removed := 0
	// 收集所有子目录路径
	var dirs []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || path == root {
			return nil
		}
		dirs = append(dirs, path)
		return nil
	})
	// 自底向上排序（路径深的先处理）
	// 简单做法：按路径长度降序
	for i := len(dirs) - 1; i >= 0; i-- {
		for j := i; j < len(dirs)-1; j++ {
			if len(dirs[j]) < len(dirs[j+1]) {
				dirs[j], dirs[j+1] = dirs[j+1], dirs[j]
			}
		}
	}
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			if err := os.Remove(d); err == nil {
				removed++
			}
		}
	}
	return removed
}

// isDirWritable 判断 dir 是否可写：尝试创建目录并写入测试文件，成功则返回 true。
// 失败（权限拒绝、磁盘满、路径非法等）返回 false。
func isDirWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".writetest")
	f, err := os.Create(probe)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}

// TruncateString 安全地截断字符串（按字节长度），确保不会在多字节 UTF-8 字符中间切断。
// 适用于需要控制输出字节大小的场景（如日志、API 响应）。
func TruncateString(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// 反向扫描，找到最后一个合法 UTF-8 起始字节
	for i := maxBytes; i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			return s[:i] + "..."
		}
	}
	return "..."
}

// TruncateRunes 按字符（rune）数截断字符串，保留完整的 UTF-8 字符。
// 适用于需要控制可见字符数量的场景（如中文摘要、UI 显示）。
func TruncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// TailRunes 返回字符串末尾最多 maxRunes 个字符，安全处理 UTF-8。
// 适用于需要显示尾部内容的场景（如过长输出的末尾预览）。
func TailRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[len(runes)-maxRunes:])
}

// TruncateAny 接受任意类型输入（string / []byte / 其他），转为字符串后安全截断。
// 适用于消息内容等 interface{} 类型字段的截断场景。
func TruncateAny(content interface{}, maxBytes int) string {
	var str string
	switch v := content.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		str = fmt.Sprintf("%v", content)
	}
	return TruncateString(str, maxBytes)
}

// 清理文件名
func cleanFileName(name string) string {
	invalidChars := regexp.MustCompile(`[<>:"/\|?*]`)
	cleaned := invalidChars.ReplaceAllString(name, "_")
	cleaned = regexp.MustCompile(`_+`).ReplaceAllString(cleaned, "_")
	cleaned = strings.Trim(cleaned, "_")
	return cleaned
}
