//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"syscall"
)

// ==================== 平台相关函数 ====================
func getSysProcAttr() *syscall.SysProcAttr {
	return nil
}

func killProcessGroup(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	return cmd.Run()
}

func terminateProcessGroup(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/PID", strconv.Itoa(pid))
	return cmd.Run()
}