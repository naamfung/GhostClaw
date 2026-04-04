package main

import (
	"testing"
)

func TestCleanControlChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "正常文本",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "包含换行符",
			input:    "Line1\nLine2\nLine3",
			expected: "Line1\nLine2\nLine3",
		},
		{
			name:     "包含制表符",
			input:    "Column1\tColumn2\tColumn3",
			expected: "Column1\tColumn2\tColumn3",
		},
		{
			name:     "包含NULL字符",
			input:    "Hello\x00World",
			expected: "HelloWorld",
		},
		{
			name:     "包含BEL字符",
			input:    "Hello\x07World",
			expected: "HelloWorld",
		},
		{
			name:     "包含BS字符",
			input:    "Hello\x08World",
			expected: "HelloWorld",
		},
		{
			name:     "包含多种控制字符",
			input:    "\x00Hello\x07\x08World\x00\nTest\x07",
			expected: "HelloWorld\nTest",
		},
		{
			name:     "包含回车换行",
			input:    "Line1\r\nLine2",
			expected: "Line1\r\nLine2",
		},
		{
			name:     "空字符串",
			input:    "",
			expected: "",
		},
		{
			name:     "只有控制字符",
			input:    "\x00\x01\x02\x03\x04\x05\x06\x07",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanControlChars(tt.input)
			if result != tt.expected {
				t.Errorf("cleanControlChars() = %q, want %q", result, tt.expected)
			}
		})
	}
}
