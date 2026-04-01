package main

import (
	"os"
	"syscall"
	"unsafe"
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

const (
	tcgets  = 0x5403
	tcsets  = 0x5402
	isig    = 0x00000001
	icanon  = 0x00000002
	echo    = 0x00000008
)

// setRawMode 将终端设为 raw mode（非规范模式、无回显）
// 用于 Monitor 模式下捕获单字符按键
func setRawMode() (*termios, error) {
	fd := int(os.Stdin.Fd())
	var oldState termios

	_, _, errNo := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		tcgets, uintptr(unsafe.Pointer(&oldState)), 0, 0, 0)
	if errNo != 0 {
		return nil, errNo
	}

	newState := oldState
	// 关闭规范模式和回显
	newState.Lflag &^= icanon | echo | isig

	_, _, errNo = syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		tcsets, uintptr(unsafe.Pointer(&newState)), 0, 0, 0)
	if errNo != 0 {
		return nil, errNo
	}

	return &oldState, nil
}

// restoreTerminal 恢复终端到之前的设置
func restoreTerminal(state *termios) error {
	fd := int(os.Stdin.Fd())
	_, _, errNo := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		tcsets, uintptr(unsafe.Pointer(state)), 0, 0, 0)
	if errNo != 0 {
		return errNo
	}
	return nil
}
