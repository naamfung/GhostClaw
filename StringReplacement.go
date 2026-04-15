package main

import "fmt"

// 定义键值对结构体，用于排序
type StringReplacement struct {
	Key   string
	Value string
}

// 定义排序后的字符串替换映射
type SortedStringReplacements struct {
	Replacements []StringReplacement
}

// 全局排序后的字符串替换映射
var sortedStringsReplacements SortedStringReplacements

// 初始化函数
func init() {
	// 初始化排序后的字符串替换映射
	sortedStringsReplacements = SortedStringReplacements{
		Replacements: make([]StringReplacement, 0, len(stringsReplacements)),
	}

	// 将 map 转换为切片
	for key, value := range stringsReplacements {
		sortedStringsReplacements.Replacements = append(sortedStringsReplacements.Replacements, StringReplacement{
			Key:   key,
			Value: value,
		})
	}

	// 按字符串长度从长到短排序
	for i := 0; i < len(sortedStringsReplacements.Replacements); i++ {
		for j := i + 1; j < len(sortedStringsReplacements.Replacements); j++ {
			if len(sortedStringsReplacements.Replacements[i].Key) < len(sortedStringsReplacements.Replacements[j].Key) {
				// 交换位置
				sortedStringsReplacements.Replacements[i], sortedStringsReplacements.Replacements[j] = sortedStringsReplacements.Replacements[j], sortedStringsReplacements.Replacements[i]
			}
		}
	}

	// 打印排序结果（调试用）
	if IsDebug {
		fmt.Println("字符串替换映射排序完成，前5项：")
		for i := 0; i < min(5, len(sortedStringsReplacements.Replacements)); i++ {
			fmt.Printf("%d: %s -> %s (长度: %d)\n", i+1, sortedStringsReplacements.Replacements[i].Key, sortedStringsReplacements.Replacements[i].Value, len(sortedStringsReplacements.Replacements[i].Key))
		}
	}
}

// 获取排序后的字符串替换映射长度
func (s *SortedStringReplacements) Len() int {
	return len(s.Replacements)
}

// 遍历排序后的字符串替换映射
func (s *SortedStringReplacements) ForEach(f func(key, value string)) {
	for _, item := range s.Replacements {
		f(item.Key, item.Value)
	}
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}