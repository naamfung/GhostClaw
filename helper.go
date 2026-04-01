package main

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// TruncateString 安全地截断 UTF-8 字符串，确保不会在字符中间切断
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// 确保我们不会在 UTF-8 字符的中间截断
	for i := maxLen; i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			return s[:i] + "..."
		}
	}

	return "..."
}

// 清理文件名
func cleanFileName(name string) string {
	invalidChars := regexp.MustCompile(`[<>:"/\|?*]`)
	cleaned := invalidChars.ReplaceAllString(name, "_")
	cleaned = regexp.MustCompile(`_+`).ReplaceAllString(cleaned, "_")
	cleaned = strings.Trim(cleaned, "_")
	return cleaned
}
