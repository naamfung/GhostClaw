package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// SystemInfo 系统信息结构体
type SystemInfo struct {
	// 操作系统信息
	OS           string `json:"os"`            // 操作系统名称 (windows/linux/darwin/freebsd)
	Arch         string `json:"arch"`          // 架构 (amd64/arm64/386/loong64)
	OSVersion    string `json:"os_version"`    // 操作系统版本
	Hostname     string `json:"hostname"`      // 主机名

	// 内存信息
	TotalMemory  uint64 `json:"total_memory"`  // 总内存 (MB)
	FreeMemory   uint64 `json:"free_memory"`   // 可用内存 (MB)

	// CPU 信息
	NumCPU       int    `json:"num_cpu"`       // CPU 核心数
	CPUModel     string `json:"cpu_model"`     // CPU 型号

	// GPU 信息
	GPUInfo      string `json:"gpu_info"`      // GPU 信息
}

// systemInfoCache 系统信息缓存（静态信息在进程生命周期内不变）
var (
	systemInfoCache     *SystemInfo
	systemInfoCacheOnce sync.Once
)

// GetSystemInfo 获取系统信息（带缓存）
func GetSystemInfo() *SystemInfo {
	systemInfoCacheOnce.Do(func() {
		systemInfoCache = collectSystemInfo()
	})
	return systemInfoCache
}

// collectSystemInfo 收集系统信息
func collectSystemInfo() *SystemInfo {
	info := &SystemInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		NumCPU:   runtime.NumCPU(),
		Hostname: getHostname(),
	}

	// 获取内存信息
	totalMem, freeMem := getMemoryInfo()
	info.TotalMemory = totalMem
	info.FreeMemory = freeMem

	// 获取 CPU 型号
	info.CPUModel = getCPUModel()

	// 获取操作系统版本
	info.OSVersion = getOSVersion()

	// 获取 GPU 信息
	info.GPUInfo = getGPUInfo()

	return info
}

// getHostname 获取主机名
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// getMemoryInfo 获取内存信息（MB）
func getMemoryInfo() (total, free uint64) {
	switch runtime.GOOS {
	case "windows":
		return getWindowsMemoryInfo()
	case "linux":
		return getLinuxMemoryInfo()
	case "darwin":
		return getDarwinMemoryInfo()
	case "freebsd":
		return getFreeBSDMemoryInfo()
	default:
		return 0, 0
	}
}

// getWindowsMemoryInfo 获取 Windows 内存信息
func getWindowsMemoryInfo() (total, free uint64) {
	// 使用 wmic 命令获取内存信息
	cmd := exec.Command("wmic", "computersystem", "get", "TotalPhysicalMemory", "/value")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "TotalPhysicalMemory=") {
				valueStr := strings.TrimPrefix(line, "TotalPhysicalMemory=")
				valueStr = strings.TrimSpace(valueStr)
				if value, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
					total = value / 1024 / 1024 // 转换为 MB
				}
			}
		}
	}

	// 获取可用内存
	cmd = exec.Command("wmic", "os", "get", "FreePhysicalMemory", "/value")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "FreePhysicalMemory=") {
				valueStr := strings.TrimPrefix(line, "FreePhysicalMemory=")
				valueStr = strings.TrimSpace(valueStr)
				if value, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
					free = value / 1024 // 转换为 MB (FreePhysicalMemory 返回 KB)
				}
			}
		}
	}

	return total, free
}

// getLinuxMemoryInfo 获取 Linux 内存信息
func getLinuxMemoryInfo() (total, free uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					total = value / 1024 // 转换为 MB
				}
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					free = value / 1024 // 转换为 MB
				}
			}
		}
	}

	return total, free
}

// getDarwinMemoryInfo 获取 macOS 内存信息
func getDarwinMemoryInfo() (total, free uint64) {
	// 获取总内存
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	output, err := cmd.Output()
	if err == nil {
		valueStr := strings.TrimSpace(string(output))
		if value, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
			total = value / 1024 / 1024 // 转换为 MB
		}
	}

	// macOS 获取可用内存较复杂，使用 vm_stat 估算
	cmd = exec.Command("vm_stat")
	output, err = cmd.Output()
	if err == nil {
		// 解析 vm_stat 输出估算可用内存
		// 这是一个简化版本
		free = total / 4 // 粗略估计：假设 25% 可用
	}

	return total, free
}

// getFreeBSDMemoryInfo 获取 FreeBSD 内存信息
func getFreeBSDMemoryInfo() (total, free uint64) {
	// 获取总内存
	cmd := exec.Command("sysctl", "-n", "hw.physmem")
	output, err := cmd.Output()
	if err == nil {
		valueStr := strings.TrimSpace(string(output))
		if value, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
			total = value / 1024 / 1024 // 转换为 MB
		}
	}

	// 获取可用内存
	cmd = exec.Command("sysctl", "-n", "vm.stats.vm.v_free_count")
	output, err = cmd.Output()
	if err == nil {
		valueStr := strings.TrimSpace(string(output))
		if value, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
			// 需要知道页面大小
			cmd = exec.Command("sysctl", "-n", "hw.pagesize")
			out, err := cmd.Output()
			if err == nil {
				pageSizeStr := strings.TrimSpace(string(out))
				if pageSize, err := strconv.ParseUint(pageSizeStr, 10, 64); err == nil {
					free = (value * pageSize) / 1024 / 1024 // 转换为 MB
				}
			}
		}
	}

	return total, free
}

// getCPUModel 获取 CPU 型号
func getCPUModel() string {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("wmic", "cpu", "get", "Name", "/value")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "Name=") {
					model := strings.TrimPrefix(line, "Name=")
					return strings.TrimSpace(model)
				}
			}
		}
	case "linux":
		data, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "model name") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						return strings.TrimSpace(parts[1])
					}
				}
			}
		}
	case "darwin":
		cmd := exec.Command("sysctl", "-n", "machdep.cpu.brand_string")
		output, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(output))
		}
	case "freebsd":
		cmd := exec.Command("sysctl", "-n", "hw.model")
		output, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(output))
		}
	}
	return "Unknown"
}

// getOSVersion 获取操作系统版本
func getOSVersion() string {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("wmic", "os", "get", "Caption", "/value")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "Caption=") {
					version := strings.TrimPrefix(line, "Caption=")
					return strings.TrimSpace(version)
				}
			}
		}
	case "linux":
		// 尝试读取 /etc/os-release
		data, err := os.ReadFile("/etc/os-release")
		if err == nil {
			lines := strings.Split(string(data), "\n")
			var name, version string
			for _, line := range lines {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					name = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
					return name
				}
				if strings.HasPrefix(line, "NAME=") {
					name = strings.Trim(strings.TrimPrefix(line, "NAME="), "\"")
				}
				if strings.HasPrefix(line, "VERSION_ID=") {
					version = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
				}
			}
			if name != "" && version != "" {
				return name + " " + version
			}
			if name != "" {
				return name
			}
		}
		// 尝试读取 /etc/issue
		data, err = os.ReadFile("/etc/issue")
		if err == nil {
			issue := strings.TrimSpace(string(data))
			issue = strings.ReplaceAll(issue, "\\n", "")
			issue = strings.ReplaceAll(issue, "\\l", "")
			if issue != "" {
				return issue
			}
		}
	case "darwin":
		cmd := exec.Command("sw_vers", "-productVersion")
		output, err := cmd.Output()
		if err == nil {
			version := strings.TrimSpace(string(output))
			return "macOS " + version
		}
	case "freebsd":
		cmd := exec.Command("uname", "-r")
		output, err := cmd.Output()
		if err == nil {
			version := strings.TrimSpace(string(output))
			return "FreeBSD " + version
		}
	}
	return runtime.GOOS + " " + runtime.GOARCH
}

// getGPUInfo 获取 GPU 信息
func getGPUInfo() string {
	switch runtime.GOOS {
	case "windows":
		// 使用 wmic 获取显卡信息
		cmd := exec.Command("wmic", "path", "win32_VideoController", "get", "Name", "/value")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			var gpus []string
			for _, line := range lines {
				if strings.HasPrefix(line, "Name=") {
					gpu := strings.TrimPrefix(line, "Name=")
					gpu = strings.TrimSpace(gpu)
					if gpu != "" {
						gpus = append(gpus, gpu)
					}
				}
			}
			if len(gpus) > 0 {
				return strings.Join(gpus, ", ")
			}
		}
	case "linux":
		// 尝试使用 lspci
		cmd := exec.Command("lspci")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			var gpus []string
			for _, line := range lines {
				if strings.Contains(line, "VGA") || strings.Contains(line, "3D") || strings.Contains(line, "Display") {
					// 提取设备名称
					parts := strings.Split(line, ": ")
					if len(parts) >= 2 {
						gpu := parts[1]
						// 简化名称
						gpu = simplifyGPUName(gpu)
						gpus = append(gpus, gpu)
					}
				}
			}
			if len(gpus) > 0 {
				return strings.Join(gpus, ", ")
			}
		}
		// 尝试读取 /sys/class/drm
		entries, err := os.ReadDir("/sys/class/drm")
		if err == nil {
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "card") && !strings.Contains(entry.Name(), "-") {
					devicePath := fmt.Sprintf("/sys/class/drm/%s/device/vendor", entry.Name())
					vendorData, err := os.ReadFile(devicePath)
					if err == nil {
						vendor := strings.TrimSpace(string(vendorData))
						return "GPU (Vendor: " + vendor + ")"
					}
				}
			}
		}
	case "darwin":
		cmd := exec.Command("system_profiler", "SPDisplaysDataType")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			var gpus []string
			for i, line := range lines {
				if strings.Contains(line, "Chipset Model:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						gpu := strings.TrimSpace(parts[1])
						gpus = append(gpus, gpu)
					}
				}
				// 也检查下一行是否有型号信息
				if i+1 < len(lines) && strings.Contains(line, "Chipset Model:") {
					nextLine := lines[i+1]
					if strings.Contains(nextLine, "Model") {
						parts := strings.SplitN(nextLine, ":", 2)
						if len(parts) == 2 {
							gpu := strings.TrimSpace(parts[1])
							if len(gpus) > 0 && gpus[len(gpus)-1] != gpu {
								gpus = append(gpus, gpu)
							}
						}
					}
				}
			}
			if len(gpus) > 0 {
				return strings.Join(gpus, ", ")
			}
		}
	case "freebsd":
		// FreeBSD 使用 pciconf
		cmd := exec.Command("pciconf", "-lv")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			var gpus []string
			for _, line := range lines {
				if strings.Contains(line, "vgapci") || strings.Contains(line, "VGA") {
					// 尝试找到设备描述
					for i := 0; i < 3 && len(lines) > 0; i++ {
						if strings.Contains(line, "device=") {
							parts := strings.Split(line, "'")
							if len(parts) >= 2 {
								gpus = append(gpus, parts[1])
								break
							}
						}
					}
				}
			}
			if len(gpus) > 0 {
				return strings.Join(gpus, ", ")
			}
		}
	}
	return "Unknown"
}

// simplifyGPUName 简化 GPU 名称
func simplifyGPUName(name string) string {
	// 移除常见的厂商前缀，保留核心信息
	replacements := []string{
		"NVIDIA Corporation ", "NVIDIA ",
		"Advanced Micro Devices, Inc. [AMD/ATI] ", "AMD ",
		"Intel Corporation ", "Intel ",
		"Corporation ", "",
	}
	for i := 0; i < len(replacements); i += 2 {
		name = strings.ReplaceAll(name, replacements[i], replacements[i+1])
	}
	return name
}

// FormatSystemInfoForPrompt 格式化系统信息为提示词
func FormatSystemInfoForPrompt(info *SystemInfo, config SystemInfoConfig) string {
	if !config.Enabled {
		return ""
	}

	var parts []string

	// 基础信息始终显示
	parts = append(parts, fmt.Sprintf("- **操作系统**：%s", info.OSVersion))
	parts = append(parts, fmt.Sprintf("- **架构**：%s", info.Arch))
	parts = append(parts, fmt.Sprintf("- **主机名**：%s", info.Hostname))

	// CPU 信息
	if config.IncludeCPU {
		parts = append(parts, fmt.Sprintf("- **处理器**：%s (%d 核心)", info.CPUModel, info.NumCPU))
	}

	// 内存信息
	if config.IncludeMemory && info.TotalMemory > 0 {
		parts = append(parts, fmt.Sprintf("- **内存**：%d MB 总计", info.TotalMemory))
	}

	// GPU 信息
	if config.IncludeGPU && info.GPUInfo != "" && info.GPUInfo != "Unknown" {
		parts = append(parts, fmt.Sprintf("- **图形处理器**：%s", info.GPUInfo))
	}

	if len(parts) == 0 {
		return ""
	}

	return "\n\n# 系统环境\n\n" + strings.Join(parts, "\n")
}
