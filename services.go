package main

import (
        "context"
        "crypto/md5"
        "fmt"
        "log"
        "os"
        "os/exec"
        "path/filepath"
        "runtime"
        "strings"
        "time"

        "github.com/go-rod/rod"
        "github.com/go-rod/rod/lib/launcher"
)

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
        var l *launcher.Launcher

        // 根据配置决定使用普通模式还是用户模式
        if UserModeBrowser {
                // 使用用户模式启动浏览器
                l = launcher.NewUserMode()
        } else {
                // 创建普通启动器
                l = launcher.New()
                // 设置浏览器启动选项
                l.Headless(true)
                l.NoSandbox(true)
                l.Set("disable-gpu", "true")
                l.Set("disable-dev-tools", "true")

                // 如果检测到浏览器路径，指定可执行文件
                if browserPath != "" {
                        l.Bin(browserPath)
                }
        }

        // 启动浏览器
        url, err := l.Launch()
        if err != nil {
                return nil, fmt.Errorf("启动浏览器进程失败: %w (浏览器路径: %s)", err, browserPath)
        }

        // 连接浏览器
        browser := rod.New().ControlURL(url)
        err = browser.Connect()
        if err != nil {
                return nil, fmt.Errorf("连接浏览器 DevTools 失败: %w", err)
        }

        // 用户模式下禁用默认设备
        if UserModeBrowser {
                browser.NoDefaultDevice()
        }

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

// Search 使用百度搜索关键词
func Search(keyword string) (results []SearchResult, err error) {
        // 获取超时配置
        timeout := globalTimeoutConfig.Browser
        if timeout <= 0 {
                timeout = DefaultBrowserTimeout
        }

        // 使用 recover 捕获 panic
        defer func() {
                if r := recover(); r != nil {
                        // 检查是否是 context 超时导致的 panic
                        errStr := fmt.Sprintf("%v", r)
                        isTimeout := strings.Contains(errStr, "context deadline exceeded")
                        
                        if isTimeout {
                                err = &BrowserError{Op: "Search", Err: fmt.Errorf("%v", r), Timeout: timeout}
                        } else {
                                // 非 timeout 错误，不设置 Timeout 字段
                                err = &BrowserError{Op: "Search", Err: fmt.Errorf("%v", r)}
                        }
                }
        }()

        ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
        defer cancel()

        browser, err := launchBrowserRod()
        if err != nil {
                return nil, &BrowserError{Op: "Search", Err: err}
        }
        defer browser.Close()

        // 创建一个页面
        page := browser.MustPage()
        defer page.Close()

        // 关键：让页面操作响应 context 超时
        page = page.Context(ctx)

        searchURL := fmt.Sprintf("https://www.baidu.com/s?ie=UTF-8&wd=%s", keyword)

        err = page.Navigate(searchURL)
        if err != nil {
                log.Printf("导航到搜索页面失败: %v", err)
                return nil, err
        }

        // 等待搜索结果加载 - MustWaitLoad 会响应 context 超时
        page.MustWaitLoad()

        // 检查 context 是否已超时
        if ctx.Err() != nil {
                return nil, fmt.Errorf("搜索操作超时")
        }

        // 等待搜索结果元素出现
        _, err = page.Element("#content_left")
        if err != nil {
                return nil, fmt.Errorf("等待搜索结果超时: %w", err)
        }

        // 提取标题和链接 - MustEval 会响应 context 超时
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

// Visit 访问 URL 并提取页面文本内容
func Visit(url string) (result *VisitResult, err error) {
        // 使用 recover 捕获 panic
        defer func() {
                if r := recover(); r != nil {
                        errStr := fmt.Sprintf("%v", r)
                        isTimeout := strings.Contains(errStr, "context deadline exceeded")
                        
                        if isTimeout {
                                timeout := globalTimeoutConfig.Browser
                                if timeout <= 0 {
                                        timeout = DefaultBrowserTimeout
                                }
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

        // 获取超时配置
        timeout := globalTimeoutConfig.Browser
        if timeout <= 0 {
                timeout = 30
        }
        ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
        defer cancel()

        browser, err := launchBrowserRod()
        if err != nil {
                return nil, &BrowserError{Op: "Visit", Err: err}
        }
        defer browser.Close()

        // 创建一个页面
        page := browser.MustPage()
        defer page.Close()

        // 关键：让页面操作响应 context 超时
        page = page.Context(ctx)

        err = page.Navigate(url)
        if err != nil {
                log.Printf("导航到页面失败: %v", err)
                return nil, err
        }

        // 等待页面加载完成 - MustWaitLoad 会响应 context 超时
        page.MustWaitLoad()

        // 检查 context 是否已超时
        if ctx.Err() != nil {
                return nil, fmt.Errorf("页面访问超时")
        }

        // 额外等待确保动态内容加载完成 - 使用 select 实现可取消的等待
        select {
        case <-time.After(2 * time.Second):
                // 正常等待完成
        case <-ctx.Done():
                return nil, fmt.Errorf("页面访问超时")
        }

        // 获取页面标题
        pageTitle := page.MustEval(`() => document.title`).Str()

        // 提取页面文本内容 - MustEval 会响应 context 超时
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
                fmt.Println("Page content (truncated): " + pageTextStr[:512] + "...")
        } else {
                fmt.Println(pageTextStr)
        }

        // 短内容直接返回，长内容保存到文件
        maxDirectLen := 16000 // 最大直接返回字符数（约 4000 tokens）
        var savedFilePath string
        var returnText string

        if len(pageTextStr) > maxDirectLen {
                // 内容过长，保存到临时文件
                // 使用 download 目录作为临时存储（基于程序所在目录）
                downloadDir := filepath.Join(globalExecDir, "download")
                if err := os.MkdirAll(downloadDir, 0755); err != nil {
                        // 如果创建目录失败，降级为截断
                        returnText = pageTextStr[:maxDirectLen] + "\n... [内容已截断，原始长度: " + fmt.Sprintf("%d", originalLen) + " 字符，保存文件失败]"
                } else {
                        // 生成文件名：使用时间戳和 URL 哈希
                        hash := md5.Sum([]byte(url))
                        urlHash := fmt.Sprintf("%x", hash)[:8]
                        timestamp := time.Now().Format("20060102_150405")
                        fileName := fmt.Sprintf("browser_visit_%s_%s.txt", timestamp, urlHash)
                        filePath := filepath.Join(downloadDir, fileName)

                        // 写入文件（清理非法控制字符）
                        contentToSave := fmt.Sprintf("URL: %s\nTitle: %s\nLength: %d\nDate: %s\n\n%s",
                                url, pageTitle, originalLen, time.Now().Format("2006-01-02 15:04:05"), cleanControlChars(pageTextStr))
                        if err := os.WriteFile(filePath, []byte(contentToSave), 0644); err != nil {
                                // 写入失败，降级为截断
                                returnText = pageTextStr[:maxDirectLen] + "\n... [内容已截断，原始长度: " + fmt.Sprintf("%d", originalLen) + " 字符，保存文件失败]"
                        } else {
                                savedFilePath = filePath
                                returnText = fmt.Sprintf("[内容过长已保存到文件]\n原始长度: %d 字符\n文件路径: %s\n可使用 read_file 工具读取完整内容", originalLen, filePath)
                                fmt.Println("Content saved to: " + filePath)
                        }
                }
        } else {
                // 短内容直接返回
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

// Download 下载页面 HTML 并保存为文件
func Download(url string) (result string, err error) {
        // 使用 recover 捕获 panic
        defer func() {
                if r := recover(); r != nil {
                        errStr := fmt.Sprintf("%v", r)
                        isTimeout := strings.Contains(errStr, "context deadline exceeded")
                        
                        if isTimeout {
                                timeout := globalTimeoutConfig.Browser
                                if timeout <= 0 {
                                        timeout = DefaultBrowserTimeout
                                }
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

        // 获取超时配置
        timeout := globalTimeoutConfig.Browser
        if timeout <= 0 {
                timeout = 30
        }
        ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
        defer cancel()

        browser, err := launchBrowserRod()
        if err != nil {
                return "", &BrowserError{Op: "Download", Err: err}
        }
        defer browser.Close()

        // 创建一个页面
        page := browser.MustPage()
        defer page.Close()

        // 关键：让页面操作响应 context 超时
        page = page.Context(ctx)

        err = page.Navigate(url)
        if err != nil {
                log.Printf("导航到页面失败: %v", err)
                return "", err
        }

        // 等待页面加载完成 - MustWaitLoad 会响应 context 超时
        page.MustWaitLoad()

        // 检查 context 是否已超时
        if ctx.Err() != nil {
                return "", fmt.Errorf("下载操作超时")
        }

        // 获取页面 HTML
        pageHTML, err := page.HTML()
        if err != nil {
                log.Printf("获取页面 HTML 失败: %v", err)
                return "", err
        }

        fileName := "download_" + time.Now().Format("20060102150405") + ".html"
        err = os.WriteFile(fileName, []byte(pageHTML), 0644)
        if err != nil {
                log.Printf("保存文件失败: %v", err)
                return "", err
        }

        fmt.Printf("下载完成，保存至: %s\n", fileName)
        return fileName, nil
}
