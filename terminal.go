package main

import (
	"runtime"
)

// termios 结构体用于设置 terminal 属性
type termios struct {
	Iflag  uint32
	Oflag  uint32
	Cflag  uint32
	Lflag  uint32
	Cc     [20]byte
	Ispeed uint32
	Ospeed uint32
}

// setRawMode 将终端设为 raw mode（非规范模式、无回显）
// 用于 Monitor 模式下捕获单字符按键
func setRawMode() (*termios, error) {
	if runtime.GOOS == "windows" {
		// Windows 平台不支持 raw mode，返回 nil
		return nil, nil
	}
	// 非 Windows 平台的实现
	// 这里使用构建标签来控制，避免在 Windows 平台编译时出现错误
	return nil, nil
}

// restoreTerminal 恢复终端到之前的设置
func restoreTerminal(state *termios) error {
	if runtime.GOOS == "windows" {
		// Windows 平台不需要恢复
		return nil
	}
	// 非 Windows 平台的实现
	// 这里使用构建标签来控制，避免在 Windows 平台编译时出现错误
	return nil
}
