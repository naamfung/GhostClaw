package main

import (
        "bufio"
        "crypto/md5"
        "fmt"
        "io"
        "log"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "regexp"
        "runtime"
        "strconv"
        "strings"
        "sync"
        "time"

        "github.com/go-rod/rod"
        "github.com/go-rod/rod/lib/launcher"
)

// 正则匹配 Chrome 输出的 DevTools WebSocket URL
// 例如: DevTools listening on ws://127.0.0.1:12345/devtools/browser/xxx-xxx
var devToolsURLPattern = regexp.MustCompile(`DevTools listening on (ws://[\d.:]+/devtools/browser/[\w-]+)`)

// readDevToolsActivePort 从 Chrome 用户数据目录读取 DevToolsActivePort 文件
// Chrome 在使用 --remote-debugging-port 启动时，会在 user-data-dir 下写入此文件
// 文件格式：第一行是端口号，第二行是浏览器 WebSocket 调试路径
// 示例内容：
//   37712
//   ws/browser-guid-here
// 返回完整的 WebSocket URL，例如 ws://127.0.0.1:37712/devtools/browser/ws/browser-guid-here
func readDevToolsActivePort(userDataDir string) (string, error) {
        filePath := filepath.Join(userDataDir, "DevToolsActivePort")

        // 重试读取，因为 Chrome 写入文件可能需要一小段时间
        for i := 0; i < 30; i++ {
                data, err := os.ReadFile(filePath)
                if err != nil {
                        if os.IsNotExist(err) {
                                time.Sleep(200 * time.Millisecond)
                                continue
                        }
                        return "", fmt.Errorf("读取 DevToolsActivePort 文件失败: %w", err)
                }

                lines := strings.Split(strings.TrimSpace(string(data)), "\n")
                if len(lines) >= 2 {
                        port := strings.TrimSpace(lines[0])
                        wsPath := strings.TrimSpace(lines[1])
                        if port != "" && wsPath != "" {
                                url := fmt.Sprintf("ws://127.0.0.1:%s/%s", port, wsPath)
                                log.Printf("从 DevToolsActivePort 文件获取到 DevTools URL: %s", url)
                                return url, nil
                        }
                }
                // 文件内容不完整，稍后重试
                time.Sleep(200 * time.Millisecond)
        }

        return "", fmt.Errorf("DevToolsActivePort 文件不存在或内容无效 (目录: %s)", userDataDir)
}

var (
        isWindows   = runtime.GOOS == "windows"
        browserPath = "" // 记录找到的浏览器路径
)

func init() {
        // 检测是否为 Alpine Linux（仅用于路径提示，不影响 rod）
        if !isWindows {
                osRelease, err := os.ReadFile("/etc/os-release")
                if err == nil && strings.Contains(string(osRelease), "Alpine") {
                        // Alpine 下可能缺少浏览器，但 rod 仍会尝试查找
                        log.Println("Alpine Linux detected, browser may need to be installed separately")
                }
        }

        // 检测系统是否已有可用的 Chromium/Chrome
        detectBrowser()
}

// isOpenCLIAvailable 检测 opencli 是否可用（结果缓存，进程生命周期内只执行一次子进程）
// 原实现每次调用都 exec.Command("opencli", "doctor", "--no-live")（~350ms/次），
// filterToolsByConfig 中每个请求调用 ~21 次，导致 prepareRequestData 耗时 7 秒。
var (
        opencliAvailableResult bool
        opencliAvailableOnce  sync.Once
)

func isOpenCLIAvailable() bool {
        opencliAvailableOnce.Do(func() {
                /*
                        root@~/GhostClaw <master># opencli doctor
                        opencli v1.6.1 doctor (node v20.11.1)

                        [OK] Daemon: running on port 19825
                        [OK] Extension: connected (v1.5.5)
                        [OK] Connectivity: connected in 0.3s

                        Everything looks good!
                        root@~/GhostClaw <master>#
                */
                cmd := exec.Command("opencli", "doctor", "--no-live")
                output, err := cmd.CombinedOutput()
                if err != nil {
                        opencliAvailableResult = false
                        return
                }
                opencliAvailableResult = strings.Contains(string(output), "Everything looks good!")
        })
        return opencliAvailableResult
}

// detectBrowser 尝试查找系统安装的 Chromium/Chrome 浏览器
func detectBrowser() {
        // 常见浏览器可执行文件名称（优先搜索 Chrome/Chromium）
        browserNames := []string{
                "chrome", "google-chrome", "chromium", "chromium-browser",
                "brave-browser", "microsoft-edge", "edge",
        }

        // 常见安装路径
        commonPaths := []string{
                "/usr/bin/",
                "/usr/local/bin/",
                "/snap/bin/",
                "/opt/google/chrome/",
                "/opt/chromium/",
        }

        // 先在 PATH 中查找
        for _, name := range browserNames {
                if path, err := exec.LookPath(name); err == nil {
                        browserPath = path
                        log.Printf("找到浏览器: %s", path)
                        return
                }
        }

        // 在常见路径中查找
        if !isWindows {
                for _, dir := range commonPaths {
                        for _, name := range browserNames {
                                fullPath := filepath.Join(dir, name)
                                if info, err := os.Stat(fullPath); err == nil && info.Mode()&0111 != 0 {
                                        browserPath = fullPath
                                        log.Printf("找到浏览器: %s", fullPath)
                                        return
                                }
                        }
                }
        } else {
                // Windows 上的查找逻辑
                driveLetters := make([]string, 0, 26)
                for i := 'A'; i <= 'Z'; i++ {
                        driveLetters = append(driveLetters, string(i))
                }
                basePaths := []string{
                        // 标准安装路径
                        "Program Files/Google/Chrome/Application/chrome.exe",
                        "Program Files (x86)/Google/Chrome/Application/chrome.exe",
                        "Users/" + os.Getenv("USERNAME") + "/AppData/Local/Google/Chrome/Application/chrome.exe",
                        "Users/" + os.Getenv("USERNAME") + "/AppData/Local/Chromium/Application/chrome.exe",
                        "Program Files/Microsoft/Edge/Application/msedge.exe",
                        "Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
                        // 自定义安装路径（支持用户自定义安装位置）
                        "Chrome/App/chrome.exe",
                        "Chrome/Application/chrome.exe",
                        "Google/Chrome/Application/chrome.exe",
                        "Browser/chrome.exe",
                        "Browser/Chrome/Application/chrome.exe",
                        "Tools/Chrome/Application/chrome.exe",
                        "Software/Chrome/Application/chrome.exe",
                        "Software/Google/Chrome/Application/chrome.exe",
                        // 便携版/绿色版常见路径
                        "Chrome/chrome.exe",
                        "Chromium/chrome.exe",
                        "Chromium/Application/chrome.exe",
                }
                for _, drive := range driveLetters {
                        for _, basePath := range basePaths {
                                fullPath := drive + ":/" + basePath
                                if _, err := os.Stat(fullPath); err == nil {
                                        browserPath = fullPath
                                        log.Printf("找到浏览器: %s", fullPath)
                                        return
                                }
                        }
                }
        }

        if browserPath == "" {
                log.Println("提示：未找到系统浏览器，程序将使用 rod 自动下载的浏览器")
        }
}

// launchBrowserRod 启动浏览器并返回 rod 浏览器实例
func launchBrowserRod() (*rod.Browser, error) {
        // 如果已检测到系统浏览器，绕过 rod launcher 的版本检测
        // rod launcher 在非 Linux 平台（如 FreeBSD）上无法正确查找浏览器二进制文件，
        // 会尝试下载 Linux 版 Chromium 导致启动失败
        if browserPath != "" {
                if UserModeBrowser {
                        return launchBrowserDirectUserMode(browserPath)
                }
                return launchBrowserDirect(browserPath)
        }

        // 未检测到系统浏览器，回退到 rod launcher（自动查找/下载）
        if UserModeBrowser {
                l := launcher.NewUserMode()
                url, err := l.Launch()
                if err != nil {
                        return nil, fmt.Errorf("启动浏览器进程失败: %w", err)
                }
                browser := rod.New().ControlURL(url)
                err = browser.Connect()
                if err != nil {
                        return nil, fmt.Errorf("连接浏览器 DevTools 失败: %w", err)
                }
                browser.NoDefaultDevice()
                return browser, nil
        }

        l := launcher.New()
        if HeadlessBrowser {
                l.Headless(true)
        }
        if DisableGPUBrowser {
                l.Set("disable-gpu", "true")
        }
        if DisableDevToolsBrowser {
                l.Set("disable-dev-tools", "true")
        }
        if NoSandboxBrowser {
                l.NoSandbox(true)
        }

        url, err := l.Launch()
        if err != nil {
                return nil, fmt.Errorf("启动浏览器进程失败: %w (浏览器路径: %s)", err, browserPath)
        }

        browser := rod.New().ControlURL(url)
        err = browser.Connect()
        if err != nil {
                return nil, fmt.Errorf("连接浏览器 DevTools 失败: %w", err)
        }

        return browser, nil
}

// launchBrowserDirectUserMode 以用户模式启动浏览器（使用默认用户配置文件）
// 对应 rod launcher.NewUserMode() 的行为，但绕过其内部 LookPath 调用
// 用户模式下使用固定端口 37712 和默认 Chrome 用户数据目录
func launchBrowserDirectUserMode(binPath string) (*rod.Browser, error) {
        userDir := getDefaultChromeUserDataDir()

        // ── Phase 0: 嘗試連接已運行的 Chrome 實例（常見端口） ──
        if existingURL := tryConnectExistingBrowser(); existingURL != "" {
                log.Printf("[Browser] 檢測到已運行的 Chrome 實例，直接連接: %s", existingURL)
                browser := rod.New().ControlURL(existingURL)
                if err := browser.Connect(); err == nil {
                        browser.NoDefaultDevice()
                        return browser, nil
                }
                log.Printf("[Browser] 連接已有實例失敗，將繼續嘗試其他方式")
        }

        // ── Phase 0.5: Profile 鎖定檢測 + 動態端口掃描 ──
        // 如果 Chrome profile 被鎖定（SingletonLock 等文件存在），說明另一個 Chrome
        // 實例正在使用該 profile（如 opencli 啟動的 Chrome）。此時直接啟動 Chrome
        // 會因 profile 鎖衝突而立即退出。嘗試從運行中的 Chrome 進程動態發現其
        // --remote-debugging-port 參數，繞過固定端口列表。
        if isChromeProfileLocked(userDir) {
                log.Printf("[Browser] Chrome profile 被鎖定 (%s)，嘗試動態掃描 debugging 端口...", userDir)

                debugPorts := findChromeDebugPorts()
                log.Printf("[Browser] 動態掃描到 Chrome debugging 端口: %v", debugPorts)

                for _, port := range debugPorts {
                        if url := getDevToolsURLViaHTTP(port); url != "" {
                                log.Printf("[Browser] 動態掃描發現可用 Chrome 實例 (port=%d)，連接: %s", port, url)
                                browser := rod.New().ControlURL(url)
                                if connErr := browser.Connect(); connErr == nil {
                                        browser.NoDefaultDevice()
                                        return browser, nil
                                } else {
                                        log.Printf("[Browser] 連接 port=%d 失敗: %v", port, connErr)
                                }
                        }
                }

                log.Printf("[Browser] Profile 鎖定且未找到可用 debugging 端口，"+
                        "可能原因：已有 Chrome 正在運行但未啟用 --remote-debugging-port。"+
                        "將嘗試啟動獨立 profile...")
        }

        // ── Phase 1: 啟動新的 Chrome 實例 ──
        args := []string{
                "--no-first-run",
                "--no-default-browser-check",
                "--disable-background-networking",
                "--disable-client-side-phishing-detection",
                "--disable-default-apps",
                "--disable-hang-monitor",
                "--disable-popup-blocking",
                "--disable-prompt-on-repost",
                "--disable-sync",
                "--metrics-recording-only",
                "--safebrowsing-disable-auto-update",
                "--no-startup-window",
                fmt.Sprintf("--user-data-dir=%s", userDir),
                "--remote-debugging-port=37712",
        }

        cmd := exec.Command(binPath, args...)

        // 用 pipe 捕获 stderr：提取 DevTools URL + 透传到 os.Stderr + 保存完整日誌用於診斷
        stderrPipe, err := cmd.StderrPipe()
        if err != nil {
                return nil, fmt.Errorf("创建浏览器 stderr pipe 失败: %w (浏览器路径: %s)", err, binPath)
        }
        cmd.Stdout = os.Stdout

        if err := cmd.Start(); err != nil {
                return nil, fmt.Errorf("启动浏览器进程失败: %w (浏览器路径: %s)", err, binPath)
        }

        // 异步读取 stderr：提取 DevTools URL + 透传到终端 + 捕獲完整輸出用於錯誤診斷
        var stderrBuf strings.Builder
        devToolsURLCh := make(chan string, 1)
        stderrDoneCh := make(chan struct{}, 1)
        go func() {
                defer func() { stderrDoneCh <- struct{}{} }()
                scanner := bufio.NewScanner(io.TeeReader(stderrPipe, os.Stderr))
                for scanner.Scan() {
                        line := scanner.Text()
                        stderrBuf.WriteString(line)
                        stderrBuf.WriteString("\n")
                        if matches := devToolsURLPattern.FindStringSubmatch(line); len(matches) > 1 {
                                devToolsURLCh <- matches[1]
                                return
                        }
                }
        }()

        // 等待 DevTools URL：优先从 stderr 获取，如果 stderr 关闭而未找到则回退
        var wsURL string
        var userModeErr error
        select {
        case wsURL = <-devToolsURLCh:
                // 成功从 stderr 获取
        case <-stderrDoneCh:
                // stderr 已关闭但未找到 DevTools URL。
                // 捕獲 Chrome 的完整 stderr 輸出，用於診斷真正的失敗原因。
                stderrContent := strings.TrimSpace(stderrBuf.String())

                // 判斷常見的 profile 鎖衝突特徵
                isProfileLock := strings.Contains(stderrContent, "SingletonLock") ||
                        strings.Contains(stderrContent, "SingletonCookie") ||
                        strings.Contains(stderrContent, "profile") ||
                        strings.Contains(stderrContent, "Exiting") ||
                        strings.Contains(stderrContent, "already running")

                if isProfileLock {
                        log.Printf("[Browser] Chrome 因 profile 鎖衝突而退出。stderr 摘要: %s", TruncateString(stderrContent, 300))
                } else {
                        log.Printf("[Browser] Chrome 異常退出（非 profile 鎖）。stderr 摘要: %s", TruncateString(stderrContent, 300))
                }

                // 回退 1: 讀取 DevToolsActivePort 文件
                portURL, portErr := readDevToolsActivePort(userDir)
                if portErr == nil {
                        wsURL = portURL
                } else {
                        // 回退 2: 多端口 HTTP 探測（含動態掃描到的端口）
                        httpURL := tryConnectExistingBrowser()
                        if httpURL != "" {
                                wsURL = httpURL
                        } else {
                                cmd.Process.Kill()
                                cmd.Wait()

                                // 構建詳細的錯誤信息，包含 Chrome 的真實退出原因
                                cause := "未知原因"
                                if stderrContent != "" {
                                        cause = fmt.Sprintf("Chrome 輸出: %s", TruncateString(stderrContent, 500))
                                } else {
                                        cause = "Chrome 無任何輸出即退出（可能信號被殺死）"
                                }
                                if isProfileLock {
                                        cause = "Profile 鎖衝突（另一個 Chrome 實例正在使用此 profile）。" + cause
                                }

                                userModeErr = fmt.Errorf(
                                        "用戶模式啟動失敗: %s (profile 目錄: %s, 瀏覽器: %s)",
                                        cause, userDir, binPath,
                                )
                        }
                }
        case <-time.After(30 * time.Second):
                cmd.Process.Kill()
                cmd.Wait()
                userModeErr = fmt.Errorf("等待 Chrome DevTools URL 超時 (30s) (瀏覽器: %s)", binPath)
        }

        // 如果用戶模式全部失敗，回退到非用戶模式（臨時 profile）
        if userModeErr != nil {
                log.Printf("[Browser] 用戶模式失敗，回退到臨時 profile 模式: %v", userModeErr)
                fallbackBrowser, fallbackErr := launchBrowserDirect(binPath)
                if fallbackErr != nil {
                        return nil, fmt.Errorf("用戶模式和臨時 profile 模式均失敗。\n  用戶模式: %v\n  臨時模式: %v", userModeErr, fallbackErr)
                }
                log.Printf("[Browser] 已回退到臨時 profile 模式（注意：無法使用已登錄的 cookies/sessions）")
                return fallbackBrowser, nil
        }

        // 用戶模式成功獲取到 DevTools URL，連接瀏覽器
        browser := rod.New().ControlURL(wsURL)
        err = browser.Connect()
        if err != nil {
                cmd.Process.Kill()
                cmd.Wait()
                return nil, fmt.Errorf("連接 DevTools 失敗: %w (URL: %s, 瀏覽器: %s)", err, wsURL, binPath)
        }
        browser.NoDefaultDevice()

        return browser, nil
}

// isChromeProfileLocked 檢測 Chrome 用戶數據目錄是否存在鎖文件。
// 當另一個 Chrome 實例正在使用該 profile 時，Chrome 會創建這些鎖文件。
func isChromeProfileLocked(userDir string) bool {
        if _, err := os.Stat(userDir); os.IsNotExist(err) {
                return false
        }
        lockFiles := []string{"SingletonLock", "SingletonCookie", "SingletonSocket"}
        for _, name := range lockFiles {
                if _, err := os.Stat(filepath.Join(userDir, name)); err == nil {
                        return true
                }
        }
        return false
}

// chromeDebugPortPattern 匹配 Chrome 命令行中的 --remote-debugging-port=PORT
var chromeDebugPortPattern = regexp.MustCompile(`--remote-debugging-port=(\d+)`)

// findChromeDebugPorts 動態掃描運行中的 Chrome 進程命令行，
// 查找其 --remote-debugging-port 參數以發現已開啟的 DevTools 端口。
// 這比固定端口列表更可靠，因為 opencli 等工具可能使用非標準端口。
func findChromeDebugPorts() []int {
        // 使用 pgrep 搜索包含 remote-debugging-port 的進程
        // 如果 pgrep 不可用，回退到 ps
        var output string
        out, err := exec.Command("pgrep", "-af", "remote-debugging-port").Output()
        if err == nil && len(out) > 0 {
                output = string(out)
        } else {
                // 回退：使用 ps 搜索所有 Chrome/Chromium 進程
                out, err = exec.Command("ps", "-A", "-o", "command").Output()
                if err != nil {
                        return nil
                }
                output = string(out)
        }

        seen := make(map[int]bool)
        var ports []int
        for _, line := range strings.Split(output, "\n") {
                // 只檢查包含 chrome/chromium 的行
                lower := strings.ToLower(line)
                if !strings.Contains(lower, "chrome") && !strings.Contains(lower, "chromium") {
                        continue
                }
                if matches := chromeDebugPortPattern.FindStringSubmatch(line); len(matches) > 1 {
                        if port, err := strconv.Atoi(matches[1]); err == nil && port > 0 && !seen[port] {
                                seen[port] = true
                                ports = append(ports, port)
                        }
                }
        }
        return ports
}

// tryConnectExistingBrowser 嘗試連接已運行的 Chrome 實例。
// 遍歷常見的 remote-debugging-port 列表，通過 HTTP API 探測並返回 WebSocket URL。
// 如果沒有找到任何可連接的實例，返回空字符串。
func tryConnectExistingBrowser() string {
        // 常見的 debugging port 列表：
        //  37712  — GhostClaw User Mode 預設
        //  9222   — Chrome 標準 debugging port（opencli、Puppeteer 等常用）
        //  9229   — Node.js debugging port（部分工具使用）
        //  21222  — 部分 Chromium 衍生版本的預設 port
        candidatePorts := []int{37712, 9222, 9229, 21222}

        for _, port := range candidatePorts {
                url := getDevToolsURLViaHTTP(port)
                if url != "" {
                        log.Printf("[Browser] 通過 HTTP 探測到已有 Chrome 實例 (port=%d): %s", port, url)
                        return url
                }
        }

        return ""
}

// getDevToolsURLViaHTTP 通过 Chrome DevTools HTTP API 获取浏览器 WebSocket URL
// 当 Chrome 已在运行时，可以通过 http://127.0.0.1:{port}/json/version 获取 websocketDebuggerUrl
func getDevToolsURLViaHTTP(port int) string {
        // 使用 net/http 获取 Chrome 的 version 信息
        httpClient := &http.Client{Timeout: 3 * time.Second}
        resp, err := httpClient.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
        if err != nil {
                return ""
        }
        defer resp.Body.Close()

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return ""
        }

        // JSON 格式: {"Browser":"Chrome/xxx","Protocol-Version":"1.3","User-Agent":"...","V8-Version":"...","WebKit-Version":"...","webSocketDebuggerUrl":"ws://127.0.0.1:PORT/devtools/browser/UUID"}
        // 用简单的字符串匹配提取 webSocketDebuggerUrl
        key := `"webSocketDebuggerUrl":"`
        idx := strings.Index(string(body), key)
        if idx == -1 {
                return ""
        }
        start := idx + len(key)
        rest := string(body)[start:]
        end := strings.Index(rest, `"`)
        if end == -1 {
                return ""
        }
        url := rest[:end]
        log.Printf("通过 HTTP API 获取到 DevTools URL: %s", url)
        return url
}

// getDefaultChromeUserDataDir 返回当前平台的 Chrome 默认用户数据目录
func getDefaultChromeUserDataDir() string {
        homeDir, err := os.UserHomeDir()
        if err != nil {
                homeDir = os.Getenv("HOME")
                if homeDir == "" {
                        homeDir = "/tmp"
                }
        }

        switch runtime.GOOS {
        case "windows":
                return filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "User Data")
        case "darwin":
                return filepath.Join(homeDir, "Library", "Application Support", "Google", "Chrome")
        default:
                // Linux 和 BSD (FreeBSD, GhostBSD 等)
                // FreeBSD/GhostBSD 上 Chrome 通常使用 ~/.config/google-chrome 或 ~/.config/chromium
                for _, name := range []string{"google-chrome", "chromium", "chrome"} {
                        dir := filepath.Join(homeDir, ".config", name)
                        if info, err := os.Stat(dir); err == nil && info.IsDir() {
                                return dir
                        }
                }
                // 找不到就返回 google-chrome 默认路径（Chrome 启动时会自动创建）
                return filepath.Join(homeDir, ".config", "google-chrome")
        }
}

// launchBrowserDirect 直接通过 exec.Command 启动浏览器并返回 rod 实例
// 绕过 rod launcher 的版本检测，解决 ARM64 等平台上 launcher 无法正确解析浏览器版本的问题
func launchBrowserDirect(binPath string) (*rod.Browser, error) {
        // 在临时目录创建用户数据目录
        tmpDir, err := os.MkdirTemp("", "ghostclaw-chrome-")
        if err != nil {
                return nil, fmt.Errorf("创建浏览器临时目录失败: %w", err)
        }

        // 构建 Chrome 启动参数
        args := []string{
                "--no-first-run",
                "--no-default-browser-check",
                "--disable-background-networking",
                "--disable-client-side-phishing-detection",
                "--disable-default-apps",
                "--disable-hang-monitor",
                "--disable-popup-blocking",
                "--disable-prompt-on-repost",
                "--disable-sync",
                "--metrics-recording-only",
                "--safebrowsing-disable-auto-update",
                fmt.Sprintf("--user-data-dir=%s", tmpDir),
                "--remote-debugging-port=0", // 让系统自动分配端口
        }

        if HeadlessBrowser {
                args = append(args, "--headless=new")
        }
        if DisableGPUBrowser {
                args = append(args, "--disable-gpu")
        }
        if DisableDevToolsBrowser {
                args = append(args, "--disable-dev-tools")
        }
        if NoSandboxBrowser {
                args = append(args, "--no-sandbox")
        }

        cmd := exec.Command(binPath, args...)

        // 用 pipe 捕获 stderr，从中提取 DevTools URL，同时透传到 os.Stderr
        stderrPipe, err := cmd.StderrPipe()
        if err != nil {
                os.RemoveAll(tmpDir)
                return nil, fmt.Errorf("创建浏览器 stderr pipe 失败: %w (浏览器路径: %s)", err, binPath)
        }
        // 同时将 stdout 透传到 os.Stdout（Chrome 的用户数据目录提示等）
        cmd.Stdout = os.Stdout

        if err := cmd.Start(); err != nil {
                os.RemoveAll(tmpDir)
                return nil, fmt.Errorf("启动浏览器进程失败: %w (浏览器路径: %s)", err, binPath)
        }

        // 异步读取 stderr：提取 DevTools URL + 透传到终端
        devToolsURLCh := make(chan string, 1)
        stderrDoneCh := make(chan struct{}, 1)
        go func() {
                defer func() { stderrDoneCh <- struct{}{} }()
                scanner := bufio.NewScanner(io.TeeReader(stderrPipe, os.Stderr))
                for scanner.Scan() {
                        line := scanner.Text()
                        if matches := devToolsURLPattern.FindStringSubmatch(line); len(matches) > 1 {
                                devToolsURLCh <- matches[1]
                                return
                        }
                }
        }()

        // 等待 DevTools URL：优先从 stderr 获取，如果 stderr 关闭而未找到则回退到 DevToolsActivePort 文件
        var wsURL string
        select {
        case wsURL = <-devToolsURLCh:
                // 成功从 stderr 获取
        case <-stderrDoneCh:
                // stderr 已关闭但未找到 DevTools URL，回退到读取 DevToolsActivePort 文件
                log.Printf("stderr 未包含 DevTools URL，尝试从 DevToolsActivePort 文件读取...")
                portURL, portErr := readDevToolsActivePort(tmpDir)
                if portErr != nil {
                        cmd.Process.Kill()
                        cmd.Wait()
                        os.RemoveAll(tmpDir)
                        return nil, fmt.Errorf("获取浏览器 DevTools URL 失败（stderr和文件两种方式均失败）: %v (浏览器路径: %s)", portErr, binPath)
                }
                wsURL = portURL
        case <-time.After(30 * time.Second):
                cmd.Process.Kill()
                cmd.Wait()
                os.RemoveAll(tmpDir)
                return nil, fmt.Errorf("等待浏览器 DevTools URL 超时 (30s) (浏览器路径: %s)", binPath)
        }

        // 连接 rod 到浏览器
        browser := rod.New().ControlURL(wsURL)
        err = browser.Connect()
        if err != nil {
                cmd.Process.Kill()
                cmd.Wait()
                os.RemoveAll(tmpDir)
                return nil, fmt.Errorf("连接浏览器 DevTools 失败: %w (浏览器路径: %s)", err, binPath)
        }

        // 异步监控浏览器进程：浏览器关闭后清理子进程和临时目录
        go func() {
                // 等待 Chrome 进程退出（它会在连接断开后自动退出）
                cmd.Wait()
                os.RemoveAll(tmpDir)
        }()

        return browser, nil
}

// 搜索结果结构
type SearchResult struct {
        Title string `json:"title"`
        Link  string `json:"link"`
}

// BrowserError 浏览器操作错误
type BrowserError struct {
        Op      string // 操作名称
        Err     error  // 原始错误
        Timeout int    // 超时时间（秒），仅当真正超时时设置
}

func (e *BrowserError) Error() string {
        if e.Timeout > 0 {
                return fmt.Sprintf("浏览器操作失败 [%s]: %v (超时设置: %d秒)", e.Op, e.Err, e.Timeout)
        }
        return fmt.Sprintf("浏览器操作失败 [%s]: %v", e.Op, e.Err)
}

// IsTimeout 检查是否是真正的超时错误
func (e *BrowserError) IsTimeout() bool {
        return e.Timeout > 0 && (e.Err != nil && strings.Contains(e.Err.Error(), "context deadline exceeded"))
}

// Search 使用百度搜索关键词（通过 BrowserSessionManager 复用会话）
func Search(sessionID, keyword string) (results []SearchResult, err error) {
        // 使用 recover 捕获 panic
        defer func() {
                if r := recover(); r != nil {
                        errStr := fmt.Sprintf("%v", r)
                        isTimeout := strings.Contains(errStr, "context deadline exceeded")
                        timeout := globalTimeoutConfig.Browser
                        if timeout <= 0 {
                                timeout = DefaultBrowserTimeout
                        }
                        if isTimeout {
                                err = &BrowserError{Op: "Search", Err: fmt.Errorf("%v", r), Timeout: timeout}
                        } else {
                                err = &BrowserError{Op: "Search", Err: fmt.Errorf("%v", r)}
                        }
                }
        }()

        searchURL := fmt.Sprintf("https://www.baidu.com/s?ie=UTF-8&wd=%s", keyword)

        // 通过 BrowserSessionManager 获取或创建页面（复用会话）
        page, _, err := getOrCreatePage(sessionID, "search", searchURL)
        if err != nil {
                return nil, &BrowserError{Op: "Search", Err: err}
        }

        // 等待網絡空閒 + DOM 穩定，確保百度搜索結果已渲染完成
        // 百度是重度 JS 渲染頁面，#content_left 可能 load 事件之後才出現
        waitIdleFunc := page.MustWaitRequestIdle()
        waitIdleFunc()
        if err := page.WaitDOMStable(5*time.Second, 0.1); err != nil {
                log.Printf("[Search] WaitDOMStable: %v", err)
        }

        // 等待搜索结果元素出现
        _, err = page.Element("#content_left")
        if err != nil {
                return nil, fmt.Errorf("等待搜索结果超时: %w", err)
        }

        // 提取标题和链接
        titles := page.MustEval(`() => {
                return Array.from(document.querySelectorAll('h3.t a')).map(a => a.innerText)
        }`).Str()

        links := page.MustEval(`() => {
                return Array.from(document.querySelectorAll('h3.t a')).map(a => a.href)
        }`).Str()

        // 解析 JSON 字符串为数组
        titlesJSON := strings.Trim(titles, "[]\"")
        linksJSON := strings.Trim(links, "[]\"")

        // 简单解析（假设没有特殊字符）
        titlesSlice := strings.Split(titlesJSON, "\",\"")
        linksSlice := strings.Split(linksJSON, "\",\"")

        // 清理引号
        for i := range titlesSlice {
                titlesSlice[i] = strings.Trim(titlesSlice[i], "\"")
        }
        for i := range linksSlice {
                linksSlice[i] = strings.Trim(linksSlice[i], "\"")
        }

        results = make([]SearchResult, 0, len(titlesSlice))
        for i, title := range titlesSlice {
                if i < len(linksSlice) {
                        results = append(results, SearchResult{
                                Title: title,
                                Link:  linksSlice[i],
                        })
                }
                fmt.Printf("Title: %s\nLink: %s\n\n", title, linksSlice[i])
        }
        return results, nil
}

// VisitResult 访问结果
type VisitResult struct {
        URL       string `json:"url"`
        Title     string `json:"title"`
        Text      string `json:"text"`      // 短内容直接返回，长内容为提示信息
        Length    int    `json:"length"`    // 原始内容长度
        SavedFile string `json:"savedFile"` // 如果内容过长，保存到文件，返回文件路径
}

// Visit 访问 URL 并提取页面文本内容（通过 BrowserSessionManager 复用会话）
func Visit(sessionID, url string) (result *VisitResult, err error) {
        // 使用 recover 捕获 panic
        defer func() {
                if r := recover(); r != nil {
                        errStr := fmt.Sprintf("%v", r)
                        isTimeout := strings.Contains(errStr, "context deadline exceeded")
                        timeout := globalTimeoutConfig.Browser
                        if timeout <= 0 {
                                timeout = DefaultBrowserTimeout
                        }
                        if isTimeout {
                                err = &BrowserError{Op: "Visit", Err: fmt.Errorf("%v", r), Timeout: timeout}
                        } else {
                                err = &BrowserError{Op: "Visit", Err: fmt.Errorf("%v", r)}
                        }
                }
        }()

        // 统一的安全检查
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }

        // 通过 BrowserSessionManager 获取或创建页面（复用会话）
        page, _, err := getOrCreatePage(sessionID, "visit", url)
        if err != nil {
                return nil, &BrowserError{Op: "Visit", Err: err}
        }

        // 等待页面加载完成：網絡空閒 + DOM 穩定
        // 替換 MustWaitLoad() + time.Sleep(2s)，現代 SPA 頁面在 load 事件後才開始 AJAX 加載內容
        waitIdleFunc := page.MustWaitRequestIdle()
        waitIdleFunc()
        if err := page.WaitDOMStable(3*time.Second, 0.1); err != nil {
                log.Printf("[Visit] WaitDOMStable: %v", err)
        }

        // 获取页面标题
        pageTitle := page.MustEval(`() => document.title`).Str()

        // 提取页面文本内容
        pageTextStr := page.MustEval(`() => {
                const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, null, false);
                let text = '';
                while (walker.nextNode()) {
                        const node = walker.currentNode;
                        if (!node.parentElement.matches('script, style, .confirm-dialog, noscript') &&
                                window.getComputedStyle(node.parentElement).display !== 'none' &&
                                window.getComputedStyle(node.parentElement).visibility !== 'hidden') {
                                text += node.nodeValue.trim() + ' ';
                        }
                }
                return text.trim();
        }`).Str()

        pageTextStr = strings.TrimPrefix(pageTextStr, "You need to enable JavaScript to run this app.")

        // 记录原始长度
        originalLen := len(pageTextStr)

        if len(pageTextStr) > 512 {
                fmt.Println("Page content (truncated): " + TruncateString(pageTextStr, 512))
        } else {
                fmt.Println(pageTextStr)
        }

        // 短内容直接返回，长内容保存到文件
        maxDirectLen := 16000 // 最大直接返回字符数（约 4000 tokens）
        var savedFilePath string
        var returnText string

        if len(pageTextStr) > maxDirectLen {
                // 内容过长，保存到临时文件
                downloadDir := filepath.Join(globalExecDir, "download")
                if err := os.MkdirAll(downloadDir, 0755); err != nil {
                        returnText = TruncateString(pageTextStr, maxDirectLen) + "\n... [内容已截断，原始长度: " + fmt.Sprintf("%d", originalLen) + " 字符，保存文件失败]"
                } else {
                        hash := md5.Sum([]byte(url))
                        urlHash := fmt.Sprintf("%x", hash)[:8]
                        timestamp := time.Now().Format("20060102_150405")
                        fileName := fmt.Sprintf("browser_visit_%s_%s.txt", timestamp, urlHash)
                        filePath := filepath.Join(downloadDir, fileName)

                        contentToSave := fmt.Sprintf("URL: %s\nTitle: %s\nLength: %d\nDate: %s\n\n%s",
                                url, pageTitle, originalLen, time.Now().Format("2006-01-02 15:04:05"), cleanControlChars(pageTextStr))
                        if err := os.WriteFile(filePath, []byte(contentToSave), 0644); err != nil {
                                returnText = TruncateString(pageTextStr, maxDirectLen) + "\n... [内容已截断，原始长度: " + fmt.Sprintf("%d", originalLen) + " 字符，保存文件失败]"
                        } else {
                                savedFilePath = filePath
                                returnText = fmt.Sprintf("[内容过长已保存到文件]\n原始长度: %d 字符\n文件路径: %s\n可使用 read_file 工具读取完整内容", originalLen, filePath)
                                fmt.Println("Content saved to: " + filePath)
                        }
                }
        } else {
                returnText = pageTextStr
        }

        return &VisitResult{
                URL:       url,
                Title:     pageTitle,
                Text:      returnText,
                Length:    originalLen,
                SavedFile: savedFilePath,
        }, nil
}

// Download 下载页面 HTML 并保存为文件（通过 BrowserSessionManager 复用会话）
func Download(sessionID, url string) (result string, err error) {
        // 使用 recover 捕获 panic
        defer func() {
                if r := recover(); r != nil {
                        errStr := fmt.Sprintf("%v", r)
                        isTimeout := strings.Contains(errStr, "context deadline exceeded")
                        timeout := globalTimeoutConfig.Browser
                        if timeout <= 0 {
                                timeout = DefaultBrowserTimeout
                        }
                        if isTimeout {
                                err = &BrowserError{Op: "Download", Err: fmt.Errorf("%v", r), Timeout: timeout}
                        } else {
                                err = &BrowserError{Op: "Download", Err: fmt.Errorf("%v", r)}
                        }
                }
        }()

        // 统一的安全检查
        if err := ValidateURLForFetch(url); err != nil {
                return "", err
        }

        // 通过 BrowserSessionManager 获取或创建页面（复用会话）
        page, _, err := getOrCreatePage(sessionID, "download", url)
        if err != nil {
                return "", &BrowserError{Op: "Download", Err: err}
        }

        // 等待页面加载完成
        page.MustWaitLoad()

        // 获取页面 HTML
        pageHTML, err := page.HTML()
        if err != nil {
                log.Printf("获取页面 HTML 失败: %v", err)
                return "", err
        }

        // 使用 download 目录作为保存位置
        downloadDir := filepath.Join(globalExecDir, "download")
        if err := os.MkdirAll(downloadDir, 0755); err != nil {
                log.Printf("创建下载目录失败: %v", err)
                // 降级：保存到当前目录
                fileName := "download_" + time.Now().Format("20060102150405") + ".html"
                err = os.WriteFile(fileName, []byte(pageHTML), 0644)
                if err != nil {
                        log.Printf("保存文件失败: %v", err)
                        return "", err
                }
                fmt.Printf("下载完成，保存至: %s\n", fileName)
                return fileName, nil
        }

        fileName := filepath.Join(downloadDir, "download_"+time.Now().Format("20060102150405")+".html")
        err = os.WriteFile(fileName, []byte(pageHTML), 0644)
        if err != nil {
                log.Printf("保存文件失败: %v", err)
                return "", err
        }

        fmt.Printf("下载完成，保存至: %s\n", fileName)
        return fileName, nil
}
