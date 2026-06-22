package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

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
func resolveTempDir(subdir string) string {
	if subdir == "" {
		subdir = "tmp"
	}

	// 1. 尝试系统临时目录
	sysTmp := filepath.Join(os.TempDir(), "ghostclaw-"+subdir)
	if isDirWritable(sysTmp) {
		return sysTmp
	}

	// 2. 回退到 data/temp/<subdir>
	base := globalDataDir
	if base == "" {
		// 极少数情况：globalDataDir 尚未初始化，使用 execDir/data 兜底
		execPath, err := os.Executable()
		if err == nil {
			base = filepath.Join(filepath.Dir(execPath), "data")
		} else {
			base = filepath.Join(".", "data")
		}
	}
	fallback := filepath.Join(base, "temp", subdir)
	// 不在此处 MkdirAll，让调用方按需创建（保持函数纯粹、便于测试）
	return fallback
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
