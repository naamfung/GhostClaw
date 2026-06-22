package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	colorRed    = "\033[0;31m"
	colorGreen  = "\033[0;32m"
	colorYellow = "\033[0;33m"
	colorBlue   = "\033[0;34m"
	colorNC     = "\033[0m"
)

func printColored(color, msg string) {
	fmt.Print(color + msg + colorNC)
}

func printInfo(msg string)    { printColored(colorBlue, msg) }
func printSuccess(msg string) { printColored(colorGreen, msg) }
func printWarning(msg string) { printColored(colorYellow, msg) }
func printError(msg string)   { printColored(colorRed, msg) }

// platform 定义目标平台
type platform struct {
	Name       string // 用户友好的名称（命令行参数用）
	GOOS       string
	GOARCH     string
	OutputName string // 最终文件名（不含后缀）
	Suffix     string // 可执行文件后缀（Windows 为 .exe）
	Static     bool   // 是否静态链接（CGO_ENABLED=0）
}

// 预定义所有支持的目标平台
// 注意：freebsd-amd64 和 ghostbsd-amd64 共享同一个输出文件名，构建时会自动去重
var platforms = []platform{
	{Name: "linux-amd64", GOOS: "linux", GOARCH: "amd64", OutputName: "ghostclaw-linux-amd64", Suffix: "", Static: true},
	{Name: "linux-arm64", GOOS: "linux", GOARCH: "arm64", OutputName: "ghostclaw-linux-arm64", Suffix: "", Static: true},
	{Name: "loong64", GOOS: "linux", GOARCH: "loong64", OutputName: "ghostclaw-linux-loong64", Suffix: "", Static: true},
	{Name: "darwin-amd64", GOOS: "darwin", GOARCH: "amd64", OutputName: "ghostclaw-darwin-amd64", Suffix: "", Static: true},
	{Name: "darwin-arm64", GOOS: "darwin", GOARCH: "arm64", OutputName: "ghostclaw-darwin-arm64", Suffix: "", Static: true},
	{Name: "windows-amd64", GOOS: "windows", GOARCH: "amd64", OutputName: "ghostclaw-windows-amd64", Suffix: ".exe", Static: true},
	// FreeBSD 和 GhostBSD 本质上是同一个二进制文件，统一命名为 ghostclaw-ghostbsd-or-freebsd-amd64
	{Name: "freebsd-amd64", GOOS: "freebsd", GOARCH: "amd64", OutputName: "ghostclaw-ghostbsd-or-freebsd-amd64", Suffix: "", Static: true},
	{Name: "ghostbsd-amd64", GOOS: "freebsd", GOARCH: "amd64", OutputName: "ghostclaw-ghostbsd-or-freebsd-amd64", Suffix: "", Static: true},
}

func main() {
	progName := filepath.Base(os.Args[0])

	// 切换到脚本所在目录
	exePath, err := os.Executable()
	if err != nil {
		exePath = os.Args[0]
	}
	scriptDir := filepath.Dir(exePath)
	if err := os.Chdir(scriptDir); err != nil {
		fmt.Printf("切换目录失败: %v\n", err)
		os.Exit(1)
	}

	// 处理帮助命令
	if len(os.Args) >= 2 {
		arg := os.Args[1]
		if arg == "help" || arg == "--help" || arg == "-h" {
			printHelp(progName)
			os.Exit(0)
		}
	}

	// 处理 clean 命令
	if len(os.Args) >= 2 && (os.Args[1] == "clean" || os.Args[1] == "--clean" || os.Args[1] == "-clean") {
		clean()
		os.Exit(0)
	}

	// 处理 cross 命令（跨平台构建）
	if len(os.Args) >= 2 && (os.Args[1] == "cross" || os.Args[1] == "--cross" || os.Args[1] == "-cross") {
		crossBuild(os.Args[2:])
		os.Exit(0)
	}

	// 默认行为：构建当前平台
	printInfo("=== GhostClaw 构建脚本 ===\n\n")

	// 清理旧文件（构建前常规清理）
	removeGlob("ghostclaw", "ghostclaw.exe", "ghostclaw_*")
	removeAll("webui/node_modules", "webui/.svelte-kit")

	copyFileIgnoreMissing("webui/tsconfig.json", "webui/tsconfig.env.json")
	os.Remove("webui/tsconfig.json")

	osInfo := detectOS()
	osName := osInfo.name
	isWindows := osInfo.isWindows
	fmt.Printf("检测到系统: %s\n", osName)

	if len(os.Args) >= 2 && (os.Args[1] == "--check-deps" || os.Args[1] == "-check-deps") {
		checkDependencies(osName)
		os.Exit(0)
	}

	pkgManager := checkDependencies(osName)
	pkgManager = resolvePackageManager(pkgManager)
	if pkgManager == "" {
		printError("错误: 未找到可用的包管理器（bun/pnpm/npm 均未安装）\n")
		fmt.Printf("请安装 bun (https://bun.sh/) 或 Node.js + pnpm (https://nodejs.org/)\n")
		fmt.Printf("或运行: ./%s --check-deps 查看详细安装指南\n", progName)
		os.Exit(1)
	}
	fmt.Printf("使用包管理器: ")
	printSuccess(pkgManager + "\n\n")

	checkPackageJSON()

	printInfo("[1/2] 构建前端...\n")
	if err := buildFrontend(pkgManager); err != nil {
		fmt.Printf("前端构建失败: %v\n", err)
		os.Exit(1)
	}

	copyFileIgnoreMissing("webui/tsconfig.env.json", "webui/tsconfig.json")
	os.Remove("webui/tsconfig.env.json")

	printInfo("\n[2/2] 编译后端...\n")
	if err := buildBackend(isWindows); err != nil {
		fmt.Printf("后端编译失败: %v\n", err)
		os.Exit(1)
	}

	printSuccess("\n=== 构建完成 ===\n")
	outputName := "ghostclaw"
	if isWindows {
		outputName = "ghostclaw.exe"
	}
	fmt.Printf("可执行文件: ./%s\n\n", outputName)
	fmt.Printf("运行程序: ./%s\n", outputName)
	fmt.Println("访问地址: http://localhost:10086")

	tags := getBuildTags()
	if tags != "" {
		fmt.Printf("\n已启用扩展渠道:%s\n", tags)
	}
	printExtensionsHelp(progName)
}

// printHelp 显示帮助信息
func printHelp(progName string) {
	fmt.Printf(`GhostClaw 构建脚本

用法:
  %s                    构建当前平台的可执行文件（默认）
  %s cross [选项]       跨平台构建所有或指定平台（无需 Docker）
  %s clean              清理构建缓存和生成文件
  %s --check-deps       检查并显示依赖安装状态

选项（cross 命令）:
  --platforms, -p <列表>   指定要构建的平台，逗号分隔（例如：linux-amd64,windows-amd64）
                           如果不指定，则构建所有平台
  --help, -h               显示此帮助

支持平台列表（cross）:
  linux-amd64      - Linux x86_64
  linux-arm64      - Linux ARM64
  loong64          - Loong64 龙芯64位 
  darwin-amd64     - macOS Intel
  darwin-arm64     - macOS Apple Silicon
  windows-amd64    - Windows x86_64
  freebsd-amd64    - FreeBSD x86_64 (与 GhostBSD 共用同一二进制)
  ghostbsd-amd64   - GhostBSD x86_64 (与 FreeBSD 共用同一二进制)

示例:
  %s                               # 构建当前平台
  %s cross                         # 构建所有平台到 dist/ 目录
  %s cross --platforms linux-amd64,windows-amd64
  %s clean                         # 清理
  %s --check-deps                  # 检查依赖
  ENABLE_ALL_CHANNELS=1 %s cross   # 启用所有扩展渠道并构建所有平台

环境变量（扩展渠道）:
  ENABLE_TELEGRAM=1   启用 Telegram 渠道
  ENABLE_DISCORD=1    启用 Discord 渠道
  ENABLE_SLACK=1      启用 Slack 渠道
  ENABLE_FEISHU=1     启用 飞书 渠道
  ENABLE_IRC=1        启用 IRC 渠道
  ENABLE_WEBHOOK=1    启用 Webhook 渠道
  ENABLE_XMPP=1       启用 XMPP 渠道
  ENABLE_MATRIX=1     启用 Matrix 渠道
  ENABLE_ALL_CHANNELS=1 启用所有扩展渠道

环境变量（包管理器）:
  GHOSTCLAW_PKG_MANAGER=bun|pnpm|npm  指定包管理器（默认自动检测，优先 bun）
`,
		progName, progName, progName, progName, // 用法部分4个
		progName, progName, progName, progName, progName, progName) // 示例部分6个，共10个
}

// clean 清理所有构建缓存和生成文件
func clean() {
	printInfo("=== GhostClaw 清理脚本 ===\n\n")
	removeGlob("ghostclaw", "ghostclaw.exe", "ghostclaw_*")
	removeAll("webui/node_modules", "webui/.svelte-kit", "webui/.vite", "webui/build", "webui/dist")
	os.Remove("webui/tsconfig.env.json")
	removeAll("dist")
	fmt.Println("清理完成！")
}

// crossBuild 跨平台构建（无需 Docker）
func crossBuild(args []string) {
	printInfo("=== GhostClaw 跨平台构建（无需 Docker） ===\n\n")

	buildAll := true
	var selectedPlatforms []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			printHelp(filepath.Base(os.Args[0]))
			return
		case "--platforms", "-p":
			if i+1 < len(args) {
				buildAll = false
				selectedPlatforms = strings.Split(args[i+1], ",")
				i++
			} else {
				printError("错误: --platforms 需要指定平台列表\n")
				os.Exit(1)
			}
		default:
			printError(fmt.Sprintf("未知参数: %s\n", args[i]))
			printHelp(filepath.Base(os.Args[0]))
			os.Exit(1)
		}
	}

	// 检查依赖（前端构建需要 bun/pnpm/npm 之一）
	osInfo := detectOS()
	osName := osInfo.name
	pkgManager := checkDependencies(osName)
	pkgManager = resolvePackageManager(pkgManager)
	if pkgManager == "" {
		printError("错误: 未找到可用的包管理器（bun/pnpm/npm 均未安装）\n")
		fmt.Printf("请安装 bun (https://bun.sh/) 或 Node.js + pnpm (https://nodejs.org/)\n")
		os.Exit(1)
	}
	fmt.Printf("使用包管理器: ")
	printSuccess(pkgManager + "\n\n")

	// 1. 构建前端（只一次）
	printInfo("[1/3] 构建前端...\n")
	if err := buildFrontend(pkgManager); err != nil {
		fmt.Printf("前端构建失败: %v\n", err)
		os.Exit(1)
	}
	// 恢复 tsconfig.json
	copyFileIgnoreMissing("webui/tsconfig.env.json", "webui/tsconfig.json")
	os.Remove("webui/tsconfig.env.json")

	// 2. 准备版本信息
	version := getVersion()
	gitCommit := getGitCommit()
	buildTimeBase := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	tags := getBuildTags()

	printInfo("\n[2/3] 开始交叉编译...\n")
	fmt.Printf("版本号: %s\n", version)
	fmt.Printf("Git Commit: %s\n", gitCommit)
	if tags != "" {
		fmt.Printf("启用扩展渠道:%s\n", tags)
	}
	fmt.Println()

	// 创建输出目录
	distDir := "dist"
	if err := os.MkdirAll(distDir, 0755); err != nil {
		printError(fmt.Sprintf("无法创建输出目录: %v\n", err))
		os.Exit(1)
	}

	// 确定要构建的平台列表
	var targets []platform
	if buildAll {
		targets = platforms
	} else {
		for _, name := range selectedPlatforms {
			found := false
			for _, p := range platforms {
				if p.Name == name {
					targets = append(targets, p)
					found = true
					break
				}
			}
			if !found {
				printWarning(fmt.Sprintf("警告: 未知平台 '%s'，已跳过\n", name))
			}
		}
		if len(targets) == 0 {
			printError("错误: 没有有效的平台可供构建\n")
			os.Exit(1)
		}
	}

	// 根据 OutputName 去重，避免重复构建 FreeBSD/GhostBSD
	seen := make(map[string]bool)
	var dedupTargets []platform
	for _, p := range targets {
		if !seen[p.OutputName] {
			seen[p.OutputName] = true
			dedupTargets = append(dedupTargets, p)
		}
	}
	targets = dedupTargets

	successCount := 0
	for _, p := range targets {
		printInfo(fmt.Sprintf("→ 构建 %s (%s/%s) ...\n", p.Name, p.GOOS, p.GOARCH))
		outputPath := filepath.Join(distDir, p.OutputName+p.Suffix)

		// 构建 ldflags
		ldflags := fmt.Sprintf("-X 'main.Version=%s' -X 'main.GitCommit=%s' -X 'main.BuildTime=%s'",
			version, gitCommit, buildTimeBase)

		// 执行 go build
		cmd := exec.Command("go", "build")
		if tags != "" {
			cmd.Args = append(cmd.Args, "-tags", tags)
		}
		cmd.Args = append(cmd.Args, "-ldflags", ldflags, "-o", outputPath, ".")

		// 设置交叉编译环境变量
		cmd.Env = append(os.Environ(),
			"GOOS="+p.GOOS,
			"GOARCH="+p.GOARCH,
		)
		// 默认禁用 CGO 以保证最大兼容性
		cmd.Env = append(cmd.Env, "CGO_ENABLED=0")

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			printError(fmt.Sprintf("  ✗ 构建失败: %v\n", err))
		} else {
			printSuccess(fmt.Sprintf("  ✓ 成功: %s\n", outputPath))
			successCount++
		}
		fmt.Println()
	}

	// 3. 完成
	printInfo("[3/3] 构建完成\n")
	fmt.Printf("成功构建 %d / %d 个平台\n", successCount, len(targets))
	if successCount > 0 {
		fmt.Printf("输出目录: %s\n", distDir)
		// 列出文件
		files, _ := filepath.Glob(filepath.Join(distDir, "*"))
		for _, f := range files {
			if info, err := os.Stat(f); err == nil {
				fmt.Printf("  - %s (%d MB)\n", filepath.Base(f), info.Size()/1024/1024)
			}
		}
	}
}

// ========== 辅助函数 ==========

func removeGlob(patterns ...string) {
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			os.RemoveAll(m)
		}
	}
}

func removeAll(paths ...string) {
	for _, p := range paths {
		os.RemoveAll(p)
	}
}

func copyFileIgnoreMissing(src, dst string) {
	data, err := os.ReadFile(src)
	if err == nil {
		os.WriteFile(dst, data, 0644)
	}
}

type osInfo struct {
	name      string
	isWindows bool
}

func detectOS() osInfo {
	if runtime.GOOS == "windows" {
		return osInfo{name: "windows", isWindows: true}
	}
	if unameOut, err := exec.Command("uname", "-s").Output(); err == nil {
		unameStr := strings.TrimSpace(string(unameOut))
		if strings.Contains(unameStr, "MINGW64_NT") || strings.Contains(unameStr, "MSYS_NT") || strings.Contains(unameStr, "CYGWIN_NT") {
			return osInfo{name: "windows", isWindows: true}
		}
	}
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "ID=") {
				id := strings.TrimPrefix(line, "ID=")
				id = strings.Trim(id, "\"")
				return osInfo{name: id, isWindows: false}
			}
		}
	}
	if unameOut, err := exec.Command("uname", "-s").Output(); err == nil {
		unameStr := strings.TrimSpace(string(unameOut))
		switch unameStr {
		case "Darwin":
			return osInfo{name: "macos", isWindows: false}
		case "Linux":
			return osInfo{name: "linux", isWindows: false}
		default:
			return osInfo{name: strings.ToLower(unameStr), isWindows: false}
		}
	}
	return osInfo{name: "unknown", isWindows: false}
}

// checkDependencies 检查构建所需的所有依赖项，返回检测到的包管理器名称。
//
// 包管理器优先级：bun > pnpm > npm
//   - bun: 新版本已兼容 SvelteKit/Vite，且速度最快，自带 Node.js 运行时
//   - pnpm: 成熟稳定，需要 Node.js
//   - npm: 随 Node.js 安装，兼容性最好
//
// Node.js 检测：
//   - 有 Node.js：bun/pnpm/npm 任一即可
//   - 无 Node.js：仅 bun 可用（bun 自带 Node.js 兼容层）
func checkDependencies(osName string) string {
	printInfo("检查依赖...\n\n")

	// Go 是必需依赖
	if _, err := exec.LookPath("go"); err == nil {
		version, _ := exec.Command("go", "version").Output()
		versionStr := strings.TrimSpace(string(version))
		if versionStr == "" {
			versionStr = "installed"
		}
		fmt.Printf("  \033[0;32m✓\033[0m Go: %s\n", versionStr)
	} else {
		fmt.Printf("  \033[0;31m✗\033[0m Go: 未安装\n")
		printInstallGuide(osName)
		os.Exit(1)
	}

	// Node.js（可选，bun 可替代）
	hasNode := false
	if _, err := exec.LookPath("node"); err == nil {
		version, _ := exec.Command("node", "--version").Output()
		versionStr := strings.TrimSpace(string(version))
		if versionStr == "" {
			versionStr = "installed"
		}
		fmt.Printf("  \033[0;32m✓\033[0m Node.js: %s\n", versionStr)
		hasNode = true
	} else {
		fmt.Printf("  \033[0;31m✗\033[0m Node.js: 未安装\n")
	}

	// 检测包管理器（优先级：bun > pnpm > npm）
	pkgManager := ""
	for _, pm := range []string{"bun", "pnpm", "npm"} {
		if _, err := exec.LookPath(pm); err == nil {
			version, _ := exec.Command(pm, "--version").Output()
			versionStr := strings.TrimSpace(string(version))
			if versionStr == "" {
				versionStr = "installed"
			}
			fmt.Printf("  \033[0;32m✓\033[0m %s: %s\n", pm, versionStr)
			if pkgManager == "" {
				pkgManager = pm
			}
		} else {
			fmt.Printf("  - %s: 未安装\n", pm)
		}
	}

	// 如果 pnpm 未安装但 npm 可用，尝试自动安装 pnpm（保持向后兼容）
	if pkgManager == "npm" && hasNode {
		if _, err := exec.LookPath("pnpm"); err != nil {
			printInfo("\n尝试自动安装 pnpm...\n")
			installCmd := exec.Command("npm", "install", "-g", "pnpm")
			installCmd.Stdout = os.Stdout
			installCmd.Stderr = os.Stderr
			if installCmd.Run() == nil {
				if _, err := exec.LookPath("pnpm"); err == nil {
					printSuccess("pnpm 安装成功，将使用 pnpm 进行构建。\n")
					pkgManager = "pnpm"
				}
			} else {
				printWarning("pnpm 自动安装失败，继续使用 npm。\n")
			}
		}
	}

	fmt.Println()

	// 无包管理器 → 报错
	if pkgManager == "" {
		printError("错误: 未找到包管理器（bun/pnpm/npm 均未安装）\n\n")
		printInstallGuide(osName)
		os.Exit(1)
	}

	// 无 Node.js 且非 bun → 报错（pnpm/npm 需要 Node.js 运行时）
	if !hasNode && pkgManager != "bun" {
		printError("错误: Node.js 未安装，pnpm/npm 需要 Node.js 运行时\n")
		fmt.Printf("请安装 Node.js (https://nodejs.org/) 或 bun (https://bun.sh/)\n")
		os.Exit(1)
	}

	// 无 Node.js 但使用 bun → 仅警告
	if !hasNode && pkgManager == "bun" {
		fmt.Printf("  \033[0;33m⚠\033[0m Node.js 未安装，使用 bun 替代（bun 自带 Node.js 兼容层）\n\n")
	}

	return pkgManager
}

// resolvePackageManager 确定最终使用的包管理器。
// 优先级：环境变量 GHOSTCLAW_PKG_MANAGER > 传入参数（checkDependencies 检测结果）
func resolvePackageManager(detected string) string {
	// 环境变量优先
	if env := os.Getenv("GHOSTCLAW_PKG_MANAGER"); env != "" {
		env = strings.ToLower(strings.TrimSpace(env))
		if _, err := exec.LookPath(env); err == nil {
			return env
		}
		printWarning(fmt.Sprintf("警告: 环境变量 GHOSTCLAW_PKG_MANAGER=%s 不可用，将使用自动检测结果\n", env))
	}
	return detected
}

// printInstallGuide 打印依赖安装指南
func printInstallGuide(osName string) {
	fmt.Println("请安装缺少的依赖：")
	fmt.Println()
	switch osName {
	case "ghostbsd", "freebsd":
		fmt.Println("  # 安装 Go")
		fmt.Println("  sudo pkg install go")
		fmt.Println()
		fmt.Println("  # 安装 Node.js 和 npm")
		fmt.Println("  sudo pkg install node npm-node24")
		fmt.Println()
		fmt.Println("  # 可选：安装 pnpm")
		fmt.Println("  sudo npm install -g pnpm")
		fmt.Println()
		fmt.Println("  # 可选：安装 bun（推荐，最快）")
		fmt.Println("  curl -fsSL https://bun.sh/install | bash")
	case "macos":
		fmt.Println("  # 使用 Homebrew")
		fmt.Println("  brew install go node")
		fmt.Println()
		fmt.Println("  # 可选：安装 pnpm")
		fmt.Println("  npm install -g pnpm")
		fmt.Println()
		fmt.Println("  # 可选：安装 bun（推荐，最快）")
		fmt.Println("  brew install bun")
	default:
		fmt.Println("  # Debian/Ubuntu")
		fmt.Println("  sudo apt install golang-go nodejs npm")
		fmt.Println()
		fmt.Println("  # Fedora/RHEL")
		fmt.Println("  sudo dnf install golang nodejs npm")
		fmt.Println()
		fmt.Println("  # Arch Linux")
		fmt.Println("  sudo pacman -S go nodejs npm")
		fmt.Println()
		fmt.Println("  # 可选：安装 pnpm")
		fmt.Println("  npm install -g pnpm")
		fmt.Println()
		fmt.Println("  # 可选：安装 bun（推荐，最快）")
		fmt.Println("  curl -fsSL https://bun.sh/install | bash")
	}
	fmt.Println()
}

func checkPackageJSON() {
	data, err := os.ReadFile("webui/package.json")
	if err != nil {
		return
	}
	if bytes.Contains(data, []byte("sass-embedded")) {
		printWarning("警告: package.json 中仍使用 sass-embedded\n")
		fmt.Println("建议将 sass-embedded 替换为 sass 以提高兼容性")
	}
}

func buildFrontend(pkgManager string) error {
	if err := os.Chdir("webui"); err != nil {
		return err
	}
	defer os.Chdir("..")

	// 清理前端编译缓存，确保干净构建
	cleanWebUICache()

	if _, err := os.Stat("node_modules"); os.IsNotExist(err) {
		fmt.Println("安装依赖...")
		if err := installDeps(pkgManager); err != nil {
			return err
		}
	}

	// 同步 SvelteKit 配置（生成 .svelte-kit/tsconfig.json 等）
	// bun 使用 bunx 替代 npx
	fmt.Println("同步 SvelteKit 配置...")
	syncTool := "npx"
	if pkgManager == "bun" {
		syncTool = "bunx"
	}
	syncCmd := exec.Command(syncTool, "svelte-kit", "sync")
	syncCmd.Stdout = os.Stdout
	syncCmd.Stderr = os.Stderr
	if err := syncCmd.Run(); err != nil {
		printWarning(fmt.Sprintf("svelte-kit sync 失败: %v，继续构建...\n", err))
	}

	fmt.Println("构建中...")
	if err := runBuildScript(pkgManager); err != nil {
		return err
	}
	return nil
}

// cleanWebUICache 删除前端构建缓存目录，确保干净构建
func cleanWebUICache() {
	cacheDirs := []struct {
		path string
		desc string
	}{
		{".svelte-kit", ".svelte-kit/ 编译缓存"},
		{".vite", ".vite/ Vite 缓存"},
	}
	for _, d := range cacheDirs {
		if _, err := os.Stat(d.path); err == nil {
			os.RemoveAll(d.path)
			printSuccess(fmt.Sprintf("  ✓ 已清理 %s (%s)\n", d.path, d.desc))
		}
	}
}

// installDeps 安装前端依赖，处理各包管理器的特性差异。
//
// pnpm v10+ 新安全模型兼容：先試正常 install，失败则 approve-builds + 清旧 node_modules 重試。
// bun 使用 bun install（自带 lockfile 处理）。
// npm 使用 npm install。
func installDeps(pkgManager string) error {
	switch pkgManager {
	case "pnpm":
		// 1) 先試正常 install
		cmd := exec.Command("pnpm", "install")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return nil
		}
		// 2) 失敗 → approve native build scripts + 清舊 node_modules 重試
		printWarning("pnpm install 失败，尝试 approve-builds 後重試...\n")
		os.RemoveAll("node_modules")
		approveCmd := exec.Command("pnpm", "approve-builds", "esbuild", "@parcel/watcher")
		approveCmd.Stdout = os.Stdout
		approveCmd.Stderr = os.Stderr
		approveCmd.Run()

		cmd2 := exec.Command("pnpm", "install")
		cmd2.Stdout = os.Stdout
		cmd2.Stderr = os.Stderr
		return cmd2.Run()

	case "bun":
		cmd := exec.Command("bun", "install")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()

	case "npm":
		cmd := exec.Command("npm", "install")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("unknown package manager: %s", pkgManager)
}

// runBuildScript 执行前端构建脚本，处理各包管理器的命令差异。
//
// 命令差异：
//   - pnpm: `pnpm build`（pnpm 支持 script 简写，也支持 `pnpm run build`）
//   - npm:  `npm run build`
//   - bun:  `bun run build`
func runBuildScript(pkgManager string) error {
	args := []string{"run", "build"}
	if pkgManager == "pnpm" {
		args = []string{"build"}
	}
	cmd := exec.Command(pkgManager, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildBackend(isWindows bool) error {
	version := getVersion()
	gitCommit := getGitCommit()
	buildTime := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	tags := getBuildTags()

	fmt.Printf("版本号: %s\n", version)
	fmt.Printf("Git Commit: %s\n", gitCommit)
	fmt.Printf("构建时间: %s\n", buildTime)
	if tags != "" {
		fmt.Printf("启用扩展渠道:%s\n", tags)
	}

	outputName := "ghostclaw"
	if isWindows {
		outputName = "ghostclaw.exe"
	}

	ldflags := fmt.Sprintf("-X 'main.Version=%s' -X 'main.GitCommit=%s' -X 'main.BuildTime=%s'", version, gitCommit, buildTime)

	var cmd *exec.Cmd
	if tags != "" {
		cmd = exec.Command("go", "build", "-tags", tags, "-ldflags", ldflags, "-o", outputName, ".")
	} else {
		cmd = exec.Command("go", "build", "-ldflags", ldflags, "-o", outputName, ".")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		printError("Go 编译失败\n")
		fmt.Println("请确保已安装 Go 编译器")
		return err
	}
	return nil
}

func getVersion() string {
	version := "dev"
	if data, err := os.ReadFile("version.go"); err == nil {
		re := regexp.MustCompile(`Version\s*=\s*"([^"]+)"`)
		matches := re.FindSubmatch(data)
		if len(matches) >= 2 {
			version = string(matches[1])
		}
	}
	return version
}

func getGitCommit() string {
	gitCommit := "unknown"
	if _, err := os.Stat(".git"); err == nil {
		if out, err := exec.Command("git", "rev-parse", "--short=7", "HEAD").Output(); err == nil {
			if len(out) > 0 {
				gitCommit = strings.TrimSpace(string(out))
			}
		}
	}
	return gitCommit
}

func getBuildTags() string {
	all := os.Getenv("ENABLE_ALL_CHANNELS") == "1"
	tags := []string{}
	if all || os.Getenv("ENABLE_TELEGRAM") == "1" {
		tags = append(tags, "telegram")
	}
	if all || os.Getenv("ENABLE_DISCORD") == "1" {
		tags = append(tags, "discord")
	}
	if all || os.Getenv("ENABLE_SLACK") == "1" {
		tags = append(tags, "slack")
	}
	if all || os.Getenv("ENABLE_FEISHU") == "1" {
		tags = append(tags, "feishu")
	}
	if all || os.Getenv("ENABLE_IRC") == "1" {
		tags = append(tags, "irc")
	}
	if all || os.Getenv("ENABLE_WEBHOOK") == "1" {
		tags = append(tags, "webhook")
	}
	if all || os.Getenv("ENABLE_XMPP") == "1" {
		tags = append(tags, "xmpp")
	}
	if all || os.Getenv("ENABLE_MATRIX") == "1" {
		tags = append(tags, "matrix")
	}
	if len(tags) == 0 {
		return ""
	}
	return strings.Join(tags, ",")
}

// printExtensionsHelp 显示扩展渠道构建选项，使用实际的程序名称
func printExtensionsHelp(progName string) {
	fmt.Printf("\n扩展渠道构建选项:\n")
	fmt.Printf("  ENABLE_TELEGRAM=1 ./%s  - 启用 Telegram 渠道\n", progName)
	fmt.Printf("  ENABLE_DISCORD=1 ./%s   - 启用 Discord 渠道\n", progName)
	fmt.Printf("  ENABLE_SLACK=1 ./%s     - 启用 Slack 渠道\n", progName)
	fmt.Printf("  ENABLE_FEISHU=1 ./%s    - 启用飞书渠道\n", progName)
	fmt.Printf("  ENABLE_IRC=1 ./%s       - 启用 IRC 渠道\n", progName)
	fmt.Printf("  ENABLE_WEBHOOK=1 ./%s   - 启用 Webhook 渠道\n", progName)
	fmt.Printf("  ENABLE_XMPP=1 ./%s      - 启用 XMPP 渠道\n", progName)
	fmt.Printf("  ENABLE_MATRIX=1 ./%s    - 启用 Matrix 渠道\n", progName)
	fmt.Printf("  ENABLE_ALL_CHANNELS=1 ./%s - 启用所有扩展渠道\n\n", progName)
}
