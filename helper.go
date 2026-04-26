package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

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
