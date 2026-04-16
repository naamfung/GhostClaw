package main

import (
        "context"
        "crypto/md5"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "strings"
        "time"

        "github.com/go-rod/rod"
        "github.com/go-rod/rod/lib/input"
        "github.com/go-rod/rod/lib/proto"
        "github.com/toon-format/toon-go"
)

// ============================================================
// 浏览器工具增强模块
// 提供更强大的浏览器自动化能力，支持会话管理
// ============================================================

// getBrowserTimeout 获取浏览器超时时间（秒）
func getBrowserTimeout(timeoutSec int) int {
        if timeoutSec > 0 {
                return timeoutSec
        }
        timeout := globalTimeoutConfig.Browser
        if timeout <= 0 {
                timeout = 30
        }
        return timeout
}

// browserSafeOp 执行浏览器操作，捕获 panic 并转换为 error
func browserSafeOp(op string, fn func() error) (err error) {
        defer func() {
                if r := recover(); r != nil {
                        errStr := fmt.Sprintf("%v", r)
                        isTimeout := strings.Contains(errStr, "context deadline exceeded")
                        if isTimeout {
                                timeout := globalTimeoutConfig.Browser
                                if timeout <= 0 {
                                        timeout = DefaultBrowserTimeout
                                }
                                err = &BrowserError{Op: op, Err: fmt.Errorf("%v", r), Timeout: timeout}
                        } else {
                                err = &BrowserError{Op: op, Err: fmt.Errorf("%v", r)}
                        }
                }
        }()
        return fn()
}

// ============================================================
// 浏览器会话管理辅助函数
// ============================================================

// getOrCreatePage 获取或创建浏览器页面（使用会话管理器）
func getOrCreatePage(sessionID, pageID, url string) (*rod.Page, *BrowserSession, error) {
        mgr := GetBrowserSessionManager()
        sess, ok := mgr.GetSession(sessionID)
        if !ok {
                var err error
                sess, err = mgr.CreateSession(sessionID)
                if err != nil {
                        return nil, nil, err
                }
        }

        page, ok := sess.GetPage(pageID)
        if !ok || page == nil {
                var err error
                page, err = sess.CreatePage(pageID, url)
                if err != nil {
                        return nil, nil, err
                }
        } else if url != "" {
                if err := page.Navigate(url); err != nil {
                        return nil, nil, err
                }
        }

        // 设置超时
        timeout := globalTimeoutConfig.Browser
        if timeout <= 0 {
                timeout = DefaultBrowserTimeout
        }
        ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
        defer cancel()
        page = page.Context(ctx)

        return page, sess, nil
}

// ============================================================
// 基础交互操作类工具
// ============================================================

type BrowserClickResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
        URL     string `json:"url,omitempty"`
}

func BrowserClick(sessionID, url, selector string, timeoutSec int) (result *BrowserClickResult, err error) {
        err = browserSafeOp("BrowserClick", func() error {
                result, err = browserClickImpl(sessionID, url, selector, timeoutSec)
                return err
        })
        return
}

func browserClickImpl(sessionID, url, selector string, timeoutSec int) (*BrowserClickResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(selector)
        if err != nil {
                return nil, fmt.Errorf("未找到元素 '%s': %w", selector, err)
        }
        if err := element.ScrollIntoView(); err != nil {
                log.Printf("滚动到元素失败: %v", err)
        }
        element.MustClick()
        time.Sleep(500 * time.Millisecond)
        info, _ := page.Info()
        return &BrowserClickResult{
                Success: true,
                Message: fmt.Sprintf("成功点击元素: %s", selector),
                URL:     info.URL,
        }, nil
}

type BrowserTypeResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
        Value   string `json:"value,omitempty"`
}

func BrowserType(sessionID, url, selector, text string, submit bool, timeoutSec int) (result *BrowserTypeResult, err error) {
        err = browserSafeOp("BrowserType", func() error {
                result, err = browserTypeImpl(sessionID, url, selector, text, submit, timeoutSec)
                return err
        })
        return
}

func browserTypeImpl(sessionID, url, selector, text string, submit bool, timeoutSec int) (*BrowserTypeResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(selector)
        if err != nil {
                return nil, fmt.Errorf("未找到输入框 '%s': %w", selector, err)
        }
        element.MustClick()
        element.SelectAllText()
        element.Input(text)
        if submit {
                page.Keyboard.Press(input.Enter)
                time.Sleep(500 * time.Millisecond)
        }
        return &BrowserTypeResult{
                Success: true,
                Message: fmt.Sprintf("成功输入文本到: %s", selector),
                Value:   text,
        }, nil
}

type BrowserScrollResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
        Height  int    `json:"height,omitempty"`
}

func BrowserScroll(sessionID, url, direction string, amount int, timeoutSec int) (result *BrowserScrollResult, err error) {
        err = browserSafeOp("BrowserScroll", func() error {
                result, err = browserScrollImpl(sessionID, url, direction, amount, timeoutSec)
                return err
        })
        return
}

func browserScrollImpl(sessionID, url, direction string, amount int, timeoutSec int) (*BrowserScrollResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        scrollJS := ""
        if direction == "up" {
                scrollJS = fmt.Sprintf("window.scrollBy(0, -%d)", amount)
        } else {
                scrollJS = fmt.Sprintf("window.scrollBy(0, %d)", amount)
        }
        page.MustEval(scrollJS)
        height := page.MustEval("() => document.body.scrollHeight").Int()
        return &BrowserScrollResult{
                Success: true,
                Message: fmt.Sprintf("成功向%s滚动 %d 像素", direction, amount),
                Height:  height,
        }, nil
}

// ============================================================
// 等待操作类工具
// ============================================================

type BrowserWaitResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
}

func BrowserWaitElement(sessionID, url, selector string, waitTimeout int) (result *BrowserWaitResult, err error) {
        err = browserSafeOp("BrowserWaitElement", func() error {
                result, err = browserWaitElementImpl(sessionID, url, selector, waitTimeout)
                return err
        })
        return
}

func browserWaitElementImpl(sessionID, url, selector string, waitTimeout int) (*BrowserWaitResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        if waitTimeout <= 0 {
                waitTimeout = 10
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Duration(waitTimeout)*time.Second)
        defer waitCancel()
        waitPage := page.Context(waitCtx)
        _, err = waitPage.Element(selector)
        if err != nil {
                return nil, fmt.Errorf("等待元素 '%s' 超时: %w", selector, err)
        }
        return &BrowserWaitResult{
                Success: true,
                Message: fmt.Sprintf("元素 '%s' 已出现", selector),
        }, nil
}

func BrowserWaitIdle(sessionID, url string, waitTimeout int) (result *BrowserWaitResult, err error) {
        err = browserSafeOp("BrowserWaitIdle", func() error {
                result, err = browserWaitIdleImpl(sessionID, url, waitTimeout)
                return err
        })
        return
}

func browserWaitIdleImpl(sessionID, url string, waitTimeout int) (*BrowserWaitResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        if waitTimeout <= 0 {
                waitTimeout = 10
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        waitFunc := page.MustWaitRequestIdle()
        waitFunc()
        if err := page.WaitDOMStable(time.Duration(waitTimeout)*time.Second, 0.1); err != nil {
                log.Printf("DOM 稳定等待: %v", err)
        }
        return &BrowserWaitResult{
                Success: true,
                Message: "页面已加载完成且网络空闲",
        }, nil
}

// ============================================================
// 内容提取类工具
// ============================================================

type LinkInfo struct {
        Text string `json:"text"`
        Href string `json:"href"`
}

type BrowserExtractLinksResult struct {
        URL   string     `json:"url"`
        Count int        `json:"count"`
        Links []LinkInfo `json:"links"`
}

func BrowserExtractLinks(sessionID, url string, timeoutSec int) (result *BrowserExtractLinksResult, err error) {
        err = browserSafeOp("BrowserExtractLinks", func() error {
                result, err = browserExtractLinksImpl(sessionID, url, timeoutSec)
                return err
        })
        return
}

func browserExtractLinksImpl(sessionID, url string, timeoutSec int) (*BrowserExtractLinksResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        linksJSON := page.MustEval(`() => {
                return JSON.stringify(Array.from(document.querySelectorAll('a')).map(a => ({
                        text: a.innerText.trim(),
                        href: a.href
                })).filter(l => l.href && l.href.startsWith('http')));
        }`).Str()
        var links []LinkInfo
        if err := json.Unmarshal([]byte(linksJSON), &links); err != nil {
                links = parseSimpleLinks(linksJSON)
        }
        return &BrowserExtractLinksResult{
                URL:   url,
                Count: len(links),
                Links: links,
        }, nil
}

func parseSimpleLinks(jsonStr string) []LinkInfo {
        var links []LinkInfo
        jsonStr = strings.Trim(jsonStr, "[]")
        if jsonStr == "" {
                return links
        }
        parts := strings.Split(jsonStr, "},{")
        for _, part := range parts {
                part = strings.Trim(part, "{}")
                link := LinkInfo{}
                if strings.Contains(part, `"text"`) {
                        textStart := strings.Index(part, `"text"`)
                        if textStart != -1 {
                                rest := part[textStart+7:]
                                if strings.Contains(rest, `:"`) {
                                        valStart := strings.Index(rest, `:"`)
                                        if valStart != -1 {
                                                val := rest[valStart+2:]
                                                if end := strings.Index(val, `"`); end != -1 {
                                                        link.Text = val[:end]
                                                }
                                        }
                                }
                        }
                }
                if strings.Contains(part, `"href"`) {
                        hrefStart := strings.Index(part, `"href"`)
                        if hrefStart != -1 {
                                rest := part[hrefStart+7:]
                                if strings.Contains(rest, `:"`) {
                                        valStart := strings.Index(rest, `:"`)
                                        if valStart != -1 {
                                                val := rest[valStart+2:]
                                                if end := strings.Index(val, `"`); end != -1 {
                                                        link.Href = val[:end]
                                                }
                                        }
                                }
                        }
                }
                if link.Href != "" {
                        links = append(links, link)
                }
        }
        return links
}

type ImageInfo struct {
        Src string `json:"src"`
        Alt string `json:"alt"`
}

type BrowserExtractImagesResult struct {
        URL    string      `json:"url"`
        Count  int         `json:"count"`
        Images []ImageInfo `json:"images"`
}

func BrowserExtractImages(sessionID, url string, timeoutSec int) (result *BrowserExtractImagesResult, err error) {
        err = browserSafeOp("BrowserExtractImages", func() error {
                result, err = browserExtractImagesImpl(sessionID, url, timeoutSec)
                return err
        })
        return
}

func browserExtractImagesImpl(sessionID, url string, timeoutSec int) (*BrowserExtractImagesResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        imagesJSON := page.MustEval(`() => {
                return JSON.stringify(Array.from(document.querySelectorAll('img')).map(img => ({
                        src: img.src,
                        alt: img.alt || ''
                })).filter(i => i.src && i.src.startsWith('http')));
        }`).Str()
        var images []ImageInfo
        if err := json.Unmarshal([]byte(imagesJSON), &images); err != nil {
                log.Printf("解析图片列表失败: %v", err)
                images = []ImageInfo{}
        }
        return &BrowserExtractImagesResult{
                URL:    url,
                Count:  len(images),
                Images: images,
        }, nil
}

type ElementInfo struct {
        Tag     string            `json:"tag"`
        Text    string            `json:"text"`
        HTML    string            `json:"html,omitempty"`
        Attribs map[string]string `json:"attribs,omitempty"`
}

type BrowserExtractElementsResult struct {
        URL      string        `json:"url"`
        Selector string        `json:"selector"`
        Count    int           `json:"count"`
        Elements []ElementInfo `json:"elements"`
}

func BrowserExtractElements(sessionID, url, selector string, includeHTML bool, timeoutSec int) (result *BrowserExtractElementsResult, err error) {
        err = browserSafeOp("BrowserExtractElements", func() error {
                result, err = browserExtractElementsImpl(sessionID, url, selector, includeHTML, timeoutSec)
                return err
        })
        return
}

func browserExtractElementsImpl(sessionID, url, selector string, includeHTML bool, timeoutSec int) (*BrowserExtractElementsResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        includeHTMLStr := "false"
        if includeHTML {
                includeHTMLStr = "true"
        }
        escapedSelector := strings.ReplaceAll(selector, `\`, `\\`)
        escapedSelector = strings.ReplaceAll(escapedSelector, `"`, `\"`)
        script := `() => {
                const selector = "` + escapedSelector + `";
                const includeHTML = ` + includeHTMLStr + `;
                return JSON.stringify(Array.from(document.querySelectorAll(selector)).map(el => ({
                        tag: el.tagName.toLowerCase(),
                        text: el.innerText.trim(),
                        html: includeHTML ? el.innerHTML : '',
                        attribs: Array.from(el.attributes).reduce((acc, attr) => {
                                acc[attr.name] = attr.value;
                                return acc;
                        }, {})
                })));
        }`
        elementsJSON := page.MustEval(script).Str()
        var elements []ElementInfo
        if err := json.Unmarshal([]byte(elementsJSON), &elements); err != nil {
                log.Printf("解析元素列表失败: %v", err)
                elements = []ElementInfo{}
        }
        return &BrowserExtractElementsResult{
                URL:      url,
                Selector: selector,
                Count:    len(elements),
                Elements: elements,
        }, nil
}

// ============================================================
// 高级功能类工具（截图、JS执行、表单填充、PDF等）
// ============================================================

type BrowserScreenshotResult struct {
        URL       string `json:"url"`
        Success   bool   `json:"success"`
        SavedFile string `json:"savedFile,omitempty"`
        FullPage  bool   `json:"fullPage"`
        Width     int    `json:"width"`
        Height    int    `json:"height"`
        Size      int64  `json:"size"`
}

func BrowserScreenshot(sessionID, url string, fullPage bool, timeoutSec int) (result *BrowserScreenshotResult, err error) {
        err = browserSafeOp("BrowserScreenshot", func() error {
                result, err = browserScreenshotImpl(sessionID, url, fullPage, timeoutSec)
                return err
        })
        return
}

func browserScreenshotImpl(sessionID, url string, fullPage bool, timeoutSec int) (*BrowserScreenshotResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        time.Sleep(1 * time.Second)
        width := page.MustEval("() => window.innerWidth").Int()
        height := page.MustEval("() => document.body.scrollHeight").Int()
        var screenshot []byte
        if fullPage {
                screenshot = page.MustScreenshotFullPage()
        } else {
                screenshot = page.MustScreenshot()
        }
        downloadDir := filepath.Join(globalExecDir, "download")
        if err := os.MkdirAll(downloadDir, 0755); err != nil {
                return nil, fmt.Errorf("创建下载目录失败: %w", err)
        }
        timestamp := time.Now().Format("20060102_150405")
        hash := md5.Sum([]byte(url))
        urlHash := fmt.Sprintf("%x", hash)[:8]
        fileName := fmt.Sprintf("screenshot_%s_%s.png", timestamp, urlHash)
        filePath := filepath.Join(downloadDir, fileName)
        if err := os.WriteFile(filePath, screenshot, 0644); err != nil {
                return nil, fmt.Errorf("保存截图失败: %w", err)
        }
        fmt.Println("Screenshot saved to: " + filePath)
        return &BrowserScreenshotResult{
                URL:       url,
                Success:   true,
                SavedFile: filePath,
                FullPage:  fullPage,
                Width:     width,
                Height:    height,
                Size:      int64(len(screenshot)),
        }, nil
}

type BrowserExecuteJSResult struct {
        URL     string      `json:"url"`
        Success bool        `json:"success"`
        Result  interface{} `json:"result"`
}

func BrowserExecuteJS(sessionID, url, script string, timeoutSec int) (result *BrowserExecuteJSResult, err error) {
        err = browserSafeOp("BrowserExecuteJS", func() error {
                result, err = browserExecuteJSImpl(sessionID, url, script, timeoutSec)
                return err
        })
        return
}

func browserExecuteJSImpl(sessionID, url, script string, timeoutSec int) (*BrowserExecuteJSResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        result := page.MustEval(script).Str()
        return &BrowserExecuteJSResult{
                URL:     url,
                Success: true,
                Result:  result,
        }, nil
}

type FormData map[string]string

type BrowserFillFormResult struct {
        URL      string `json:"url"`
        Success  bool   `json:"success"`
        Message  string `json:"message"`
        FinalURL string `json:"finalUrl,omitempty"`
}

func BrowserFillForm(sessionID, url string, formData FormData, submitSelector string, timeoutSec int) (result *BrowserFillFormResult, err error) {
        err = browserSafeOp("BrowserFillForm", func() error {
                result, err = browserFillFormImpl(sessionID, url, formData, submitSelector, timeoutSec)
                return err
        })
        return
}

func browserFillFormImpl(sessionID, url string, formData FormData, submitSelector string, timeoutSec int) (*BrowserFillFormResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        for name, value := range formData {
                selector := fmt.Sprintf("[name='%s']", name)
                element, err := page.Element(selector)
                if err != nil {
                        selector = fmt.Sprintf("#%s", name)
                        element, err = page.Element(selector)
                        if err != nil {
                                log.Printf("未找到字段 '%s': %v", name, err)
                                continue
                        }
                }
                element.MustClick()
                element.SelectAllText()
                element.Input(value)
        }
        if submitSelector != "" {
                btn, err := page.Element(submitSelector)
                if err != nil {
                        return nil, fmt.Errorf("未找到提交按钮: %w", err)
                }
                btn.MustClick()
        } else {
                page.Keyboard.Press(input.Enter)
        }
        time.Sleep(1 * time.Second)
        page.MustWaitLoad()
        info, _ := page.Info()
        return &BrowserFillFormResult{
                URL:      url,
                Success:  true,
                Message:  "表单填写并提交成功",
                FinalURL: info.URL,
        }, nil
}

type BrowserPDFResult struct {
        URL     string `json:"url"`
        Success bool   `json:"success"`
        Base64  string `json:"base64,omitempty"`
}

func BrowserPDF(sessionID, url string, timeoutSec int) (result *BrowserPDFResult, err error) {
        err = browserSafeOp("BrowserPDF", func() error {
                result, err = browserPDFImpl(sessionID, url, timeoutSec)
                return err
        })
        return
}

func browserPDFImpl(sessionID, url string, timeoutSec int) (*BrowserPDFResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        time.Sleep(1 * time.Second)
        pdf := page.MustPDF()
        return &BrowserPDFResult{
                URL:     url,
                Success: true,
                Base64:  base64.StdEncoding.EncodeToString(pdf),
        }, nil
}

// ========== 辅助类型和函数 ==========

type CookieData struct {
        Name     string  `json:"name"`
        Value    string  `json:"value"`
        Domain   string  `json:"domain"`
        Path     string  `json:"path"`
        Expires  float64 `json:"expires"`
        HTTPOnly bool    `json:"httpOnly"`
        Secure   bool    `json:"secure"`
        SameSite string  `json:"sameSite"`
}

func extractDomain(urlStr string) string {
        urlStr = strings.TrimPrefix(urlStr, "https://")
        urlStr = strings.TrimPrefix(urlStr, "http://")
        urlStr = strings.TrimPrefix(urlStr, "www.")
        if idx := strings.Index(urlStr, "/"); idx > 0 {
                urlStr = urlStr[:idx]
        }
        urlStr = strings.ReplaceAll(urlStr, ".", "_")
        urlStr = strings.ReplaceAll(urlStr, ":", "_")
        return urlStr
}

var DevicePresets = map[string]struct {
        Width       int
        Height      int
        UserAgent   string
        DeviceScale float64
        IsMobile    bool
        HasTouch    bool
}{
        "iphone": {
                Width:       375,
                Height:      812,
                UserAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
                DeviceScale: 3,
                IsMobile:    true,
                HasTouch:    true,
        },
        "iphone_landscape": {
                Width:       812,
                Height:      375,
                UserAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
                DeviceScale: 3,
                IsMobile:    true,
                HasTouch:    true,
        },
        "ipad": {
                Width:       768,
                Height:      1024,
                UserAgent:   "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
                DeviceScale: 2,
                IsMobile:    true,
                HasTouch:    true,
        },
        "android_phone": {
                Width:       360,
                Height:      800,
                UserAgent:   "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
                DeviceScale: 3,
                IsMobile:    true,
                HasTouch:    true,
        },
        "android_tablet": {
                Width:       1024,
                Height:      768,
                UserAgent:   "Mozilla/5.0 (Linux; Android 14; Pixel Tablet) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
                DeviceScale: 2,
                IsMobile:    true,
                HasTouch:    true,
        },
        "desktop": {
                Width:       1920,
                Height:      1080,
                UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
                DeviceScale: 1,
                IsMobile:    false,
                HasTouch:    false,
        },
        "desktop_mac": {
                Width:       1920,
                Height:      1080,
                UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
                DeviceScale: 1,
                IsMobile:    false,
                HasTouch:    false,
        },
}

// ========== 高级交互工具 ==========

type BrowserHoverResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
}

func BrowserHover(sessionID, url, selector string) (result *BrowserHoverResult, err error) {
        err = browserSafeOp("BrowserHover", func() error {
                result, err = browserHoverImpl(sessionID, url, selector)
                return err
        })
        return
}

func browserHoverImpl(sessionID, url, selector string) (*BrowserHoverResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(selector)
        if err != nil {
                return nil, fmt.Errorf("未找到元素 '%s': %w", selector, err)
        }
        if err := element.Hover(); err != nil {
                return nil, fmt.Errorf("悬停失败: %w", err)
        }
        return &BrowserHoverResult{
                Success: true,
                Message: fmt.Sprintf("成功悬停在元素: %s", selector),
        }, nil
}

type BrowserDoubleClickResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
        URL     string `json:"url,omitempty"`
}

func BrowserDoubleClick(sessionID, url, selector string) (result *BrowserDoubleClickResult, err error) {
        err = browserSafeOp("BrowserDoubleClick", func() error {
                result, err = browserDoubleClickImpl(sessionID, url, selector)
                return err
        })
        return
}

func browserDoubleClickImpl(sessionID, url, selector string) (*BrowserDoubleClickResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(selector)
        if err != nil {
                return nil, fmt.Errorf("未找到元素 '%s': %w", selector, err)
        }
        if err := element.Click(proto.InputMouseButtonLeft, 2); err != nil {
                return nil, fmt.Errorf("双击失败: %w", err)
        }
        time.Sleep(500 * time.Millisecond)
        info, _ := page.Info()
        return &BrowserDoubleClickResult{
                Success: true,
                Message: fmt.Sprintf("成功双击元素: %s", selector),
                URL:     info.URL,
        }, nil
}

type BrowserRightClickResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
}

func BrowserRightClick(sessionID, url, selector string) (result *BrowserRightClickResult, err error) {
        err = browserSafeOp("BrowserRightClick", func() error {
                result, err = browserRightClickImpl(sessionID, url, selector)
                return err
        })
        return
}

func browserRightClickImpl(sessionID, url, selector string) (*BrowserRightClickResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(selector)
        if err != nil {
                return nil, fmt.Errorf("未找到元素 '%s': %w", selector, err)
        }
        if err := element.Click(proto.InputMouseButtonRight, 1); err != nil {
                return nil, fmt.Errorf("右键点击失败: %w", err)
        }
        return &BrowserRightClickResult{
                Success: true,
                Message: fmt.Sprintf("成功右键点击元素: %s", selector),
        }, nil
}

type BrowserDragResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
}

func BrowserDrag(sessionID, url, sourceSelector, targetSelector string) (result *BrowserDragResult, err error) {
        err = browserSafeOp("BrowserDrag", func() error {
                result, err = browserDragImpl(sessionID, url, sourceSelector, targetSelector)
                return err
        })
        return
}

func browserDragImpl(sessionID, url, sourceSelector, targetSelector string) (*BrowserDragResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        sourceElement, err := page.Element(sourceSelector)
        if err != nil {
                return nil, fmt.Errorf("未找到源元素 '%s': %w", sourceSelector, err)
        }
        targetElement, err := page.Element(targetSelector)
        if err != nil {
                return nil, fmt.Errorf("未找到目标元素 '%s': %w", targetSelector, err)
        }
        sourceShape, err := sourceElement.Shape()
        if err != nil {
                return nil, err
        }
        sourceBox := sourceShape.Box()
        targetShape, err := targetElement.Shape()
        if err != nil {
                return nil, err
        }
        targetBox := targetShape.Box()
        sourceX := sourceBox.X + sourceBox.Width/2
        sourceY := sourceBox.Y + sourceBox.Height/2
        if err := page.Mouse.MoveTo(proto.Point{X: sourceX, Y: sourceY}); err != nil {
                return nil, err
        }
        if err := page.Mouse.Down(proto.InputMouseButtonLeft, 1); err != nil {
                return nil, err
        }
        targetX := targetBox.X + targetBox.Width/2
        targetY := targetBox.Y + targetBox.Height/2
        if err := page.Mouse.MoveLinear(proto.Point{X: targetX, Y: targetY}, 10); err != nil {
                return nil, err
        }
        if err := page.Mouse.Up(proto.InputMouseButtonLeft, 1); err != nil {
                return nil, err
        }
        return &BrowserDragResult{
                Success: true,
                Message: fmt.Sprintf("成功将元素 '%s' 拖拽到 '%s'", sourceSelector, targetSelector),
        }, nil
}

type BrowserWaitForOptions struct {
        Visible      bool
        Interactable bool
        Stable       bool
        Timeout      int
}

func BrowserWaitForSmart(sessionID, url, selector string, opts BrowserWaitForOptions) (result *BrowserWaitResult, err error) {
        err = browserSafeOp("BrowserWaitForSmart", func() error {
                result, err = browserWaitForSmartImpl(sessionID, url, selector, opts)
                return err
        })
        return
}

func browserWaitForSmartImpl(sessionID, url, selector string, opts BrowserWaitForOptions) (*BrowserWaitResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        if opts.Timeout <= 0 {
                opts.Timeout = 10
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(selector)
        if err != nil {
                return &BrowserWaitResult{
                        Success: false,
                        Message: fmt.Sprintf("未找到元素 '%s'", selector),
                }, nil
        }
        if opts.Visible {
                if err := element.WaitVisible(); err != nil {
                        return &BrowserWaitResult{
                                Success: false,
                                Message: fmt.Sprintf("元素 '%s' 等待可见超时", selector),
                        }, nil
                }
        }
        if opts.Interactable {
                if _, err := element.WaitInteractable(); err != nil {
                        return &BrowserWaitResult{
                                Success: false,
                                Message: fmt.Sprintf("元素 '%s' 等待可交互超时", selector),
                        }, nil
                }
        }
        if opts.Stable {
                if err := element.WaitStable(time.Duration(opts.Timeout) * time.Second); err != nil {
                        return &BrowserWaitResult{
                                Success: false,
                                Message: fmt.Sprintf("元素 '%s' 等待稳定超时", selector),
                        }, nil
                }
        }
        return &BrowserWaitResult{
                Success: true,
                Message: fmt.Sprintf("元素 '%s' 已准备好", selector),
        }, nil
}

type BrowserNavigateResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
        URL     string `json:"url"`
        Title   string `json:"title"`
}

func BrowserNavigateBack(sessionID, url string) (result *BrowserNavigateResult, err error) {
        err = browserSafeOp("BrowserNavigateBack", func() error {
                result, err = browserNavigateBackImpl(sessionID, url)
                return err
        })
        return
}

func browserNavigateBackImpl(sessionID, url string) (*BrowserNavigateResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        if err := page.NavigateBack(); err != nil {
                return nil, fmt.Errorf("后退失败: %w", err)
        }
        page.MustWaitLoad()
        info, _ := page.Info()
        return &BrowserNavigateResult{
                Success: true,
                Message: "成功后退",
                URL:     info.URL,
                Title:   info.Title,
        }, nil
}

func BrowserNavigateForward(sessionID, url string) (result *BrowserNavigateResult, err error) {
        err = browserSafeOp("BrowserNavigateForward", func() error {
                result, err = browserNavigateForwardImpl(sessionID, url)
                return err
        })
        return
}

func browserNavigateForwardImpl(sessionID, url string) (*BrowserNavigateResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        if err := page.NavigateForward(); err != nil {
                return nil, fmt.Errorf("前进失败: %w", err)
        }
        page.MustWaitLoad()
        info, _ := page.Info()
        return &BrowserNavigateResult{
                Success: true,
                Message: "成功前进",
                URL:     info.URL,
                Title:   info.Title,
        }, nil
}

func BrowserRefresh(sessionID, url string) (result *BrowserNavigateResult, err error) {
        err = browserSafeOp("BrowserRefresh", func() error {
                result, err = browserRefreshImpl(sessionID, url)
                return err
        })
        return
}

func browserRefreshImpl(sessionID, url string) (*BrowserNavigateResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        if err := page.Reload(); err != nil {
                return nil, fmt.Errorf("刷新失败: %w", err)
        }
        info, _ := page.Info()
        return &BrowserNavigateResult{
                Success: true,
                Message: "成功刷新页面",
                URL:     info.URL,
                Title:   info.Title,
        }, nil
}

type BrowserGetCookiesResult struct {
        URL     string       `json:"url"`
        Count   int          `json:"count"`
        Cookies []CookieInfo `json:"cookies"`
}

type CookieInfo struct {
        Name   string `json:"name"`
        Value  string `json:"value"`
        Domain string `json:"domain"`
        Path   string `json:"path"`
}

func BrowserGetCookies(sessionID, url string) (result *BrowserGetCookiesResult, err error) {
        err = browserSafeOp("BrowserGetCookies", func() error {
                result, err = browserGetCookiesImpl(sessionID, url)
                return err
        })
        return
}

func browserGetCookiesImpl(sessionID, url string) (*BrowserGetCookiesResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        cookies, err := page.Cookies([]string{url})
        if err != nil {
                return nil, fmt.Errorf("获取 Cookies 失败: %w", err)
        }
        var cookieInfos []CookieInfo
        for _, c := range cookies {
                cookieInfos = append(cookieInfos, CookieInfo{
                        Name:   c.Name,
                        Value:  c.Value,
                        Domain: c.Domain,
                        Path:   c.Path,
                })
        }
        return &BrowserGetCookiesResult{
                URL:     url,
                Count:   len(cookieInfos),
                Cookies: cookieInfos,
        }, nil
}

type BrowserCookieSaveResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
        Count   int    `json:"count"`
        File    string `json:"file"`
}

func BrowserCookieSave(sessionID, url, filePath string) (result *BrowserCookieSaveResult, err error) {
        err = browserSafeOp("BrowserCookieSave", func() error {
                result, err = browserCookieSaveImpl(sessionID, url, filePath)
                return err
        })
        return
}

func browserCookieSaveImpl(sessionID, url, filePath string) (*BrowserCookieSaveResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        if filePath == "" {
                domain := extractDomain(url)
                filePath = fmt.Sprintf("cookies_%s.toon", domain)
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        cookies, err := page.Cookies([]string{})
        if err != nil {
                return nil, fmt.Errorf("获取 Cookies 失败: %w", err)
        }
        var cookieData []CookieData
        for _, c := range cookies {
                cookieData = append(cookieData, CookieData{
                        Name:     c.Name,
                        Value:    c.Value,
                        Domain:   c.Domain,
                        Path:     c.Path,
                        Expires:  float64(c.Expires),
                        HTTPOnly: c.HTTPOnly,
                        Secure:   c.Secure,
                        SameSite: string(c.SameSite),
                })
        }
        toonData, err := toon.Marshal(cookieData)
        if err != nil {
                return nil, fmt.Errorf("序列化 Cookies 失败: %w", err)
        }
        if err := os.WriteFile(filePath, toonData, 0644); err != nil {
                return nil, fmt.Errorf("写入文件失败: %w", err)
        }
        return &BrowserCookieSaveResult{
                Success: true,
                Message: fmt.Sprintf("成功保存 %d 个 Cookies", len(cookieData)),
                Count:   len(cookieData),
                File:    filePath,
        }, nil
}

type BrowserCookieLoadResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
        Count   int    `json:"count"`
        File    string `json:"file"`
        URL     string `json:"url"`
}

func BrowserCookieLoad(sessionID, url, filePath string) (result *BrowserCookieLoadResult, err error) {
        err = browserSafeOp("BrowserCookieLoad", func() error {
                result, err = browserCookieLoadImpl(sessionID, url, filePath)
                return err
        })
        return
}

func browserCookieLoadImpl(sessionID, url, filePath string) (*BrowserCookieLoadResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        if _, err := os.Stat(filePath); os.IsNotExist(err) {
                return nil, fmt.Errorf("Cookie 文件不存在: %s", filePath)
        }
        toonData, err := os.ReadFile(filePath)
        if err != nil {
                return nil, fmt.Errorf("读取文件失败: %w", err)
        }
        var cookieData []CookieData
        if err := toon.Unmarshal(toonData, &cookieData); err != nil {
                return nil, fmt.Errorf("解析 Cookie 文件失败: %w", err)
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        var protoCookies []*proto.NetworkCookieParam
        for _, c := range cookieData {
                protoCookies = append(protoCookies, &proto.NetworkCookieParam{
                        Name:     c.Name,
                        Value:    c.Value,
                        Domain:   c.Domain,
                        Path:     c.Path,
                        Expires:  proto.TimeSinceEpoch(c.Expires),
                        HTTPOnly: c.HTTPOnly,
                        Secure:   c.Secure,
                        SameSite: proto.NetworkCookieSameSite(c.SameSite),
                })
        }
        if len(protoCookies) > 0 {
                if err := page.SetCookies(protoCookies); err != nil {
                        log.Printf("设置 Cookies 警告: %v", err)
                }
        }
        if err := page.Navigate(url); err != nil {
                return nil, fmt.Errorf("导航失败: %w", err)
        }
        page.MustWaitLoad()
        info, _ := page.Info()
        return &BrowserCookieLoadResult{
                Success: true,
                Message: fmt.Sprintf("成功加载 %d 个 Cookies 并应用到页面", len(cookieData)),
                Count:   len(cookieData),
                File:    filePath,
                URL:     info.URL,
        }, nil
}

type BrowserSnapshotResult struct {
        URL       string               `json:"url"`
        Title     string               `json:"title"`
        Snapshot  *PageSnapshotElement `json:"snapshot,omitempty"`
        SavedFile string               `json:"savedFile,omitempty"`
        Length    int                  `json:"length"`
}

type PageSnapshotElement struct {
        Tag      string                `json:"tag"`
        Text     string                `json:"text,omitempty"`
        ID       string                `json:"id,omitempty"`
        Class    string                `json:"class,omitempty"`
        Href     string                `json:"href,omitempty"`
        Src      string                `json:"src,omitempty"`
        Children []PageSnapshotElement `json:"children,omitempty"`
        Attrs    map[string]string     `json:"attrs,omitempty"`
        Rect     *ElementRect          `json:"rect,omitempty"`
}

type ElementRect struct {
        X      float64 `json:"x"`
        Y      float64 `json:"y"`
        Width  float64 `json:"width"`
        Height float64 `json:"height"`
}

func BrowserSnapshot(sessionID, url string, maxDepth int) (result *BrowserSnapshotResult, err error) {
        err = browserSafeOp("BrowserSnapshot", func() error {
                result, err = browserSnapshotImpl(sessionID, url, maxDepth)
                return err
        })
        return
}

func browserSnapshotImpl(sessionID, url string, maxDepth int) (*BrowserSnapshotResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        if maxDepth <= 0 {
                maxDepth = 5
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        info, _ := page.Info()
        snapshotJSON := page.MustEval(`() => {
                function getSnapshot(el, depth, maxDepth) {
                        if (depth > maxDepth) return null;
                        const result = { tag: el.tagName ? el.tagName.toLowerCase() : '', text: '', attrs: {} };
                        if (el.attributes) {
                                for (let attr of el.attributes) result.attrs[attr.name] = attr.value;
                        }
                        result.id = el.id || '';
                        result.class = el.className || '';
                        result.href = el.href || '';
                        result.src = el.src || '';
                        const rect = el.getBoundingClientRect();
                        if (rect) result.rect = { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
                        let textContent = '';
                        const children = [];
                        for (let child of el.childNodes) {
                                if (child.nodeType === Node.TEXT_NODE) {
                                        textContent += child.textContent.trim() + ' ';
                                } else if (child.nodeType === Node.ELEMENT_NODE) {
                                        const childSnapshot = getSnapshot(child, depth + 1, maxDepth);
                                        if (childSnapshot) children.push(childSnapshot);
                                }
                        }
                        result.text = textContent.trim().substring(0, 200);
                        if (children.length > 0) result.children = children;
                        return result;
                }
                return JSON.stringify(getSnapshot(document.body, 0, ` + fmt.Sprintf("%d", maxDepth) + `));
        }`).Str()
        var snapshot PageSnapshotElement
        if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
                log.Printf("解析快照失败: %v", err)
        }
        maxDirectLen := 16000
        snapshotLen := len(snapshotJSON)
        if snapshotLen > maxDirectLen {
                downloadDir := filepath.Join(globalExecDir, "download")
                os.MkdirAll(downloadDir, 0755)
                timestamp := time.Now().Format("20060102_150405")
                hash := md5.Sum([]byte(url))
                urlHash := fmt.Sprintf("%x", hash)[:8]
                fileName := fmt.Sprintf("snapshot_%s_%s.toon", timestamp, urlHash)
                filePath := filepath.Join(downloadDir, fileName)
                snapshotTOON, _ := toon.Marshal(&snapshot)
                contentToSave := fmt.Sprintf("URL: %s\nTitle: %s\nMaxDepth: %d\nDate: %s\n\n%s",
                        url, info.Title, maxDepth, time.Now().Format("2006-01-02 15:04:05"), string(snapshotTOON))
                os.WriteFile(filePath, []byte(contentToSave), 0644)
                return &BrowserSnapshotResult{
                        URL:       url,
                        Title:     info.Title,
                        SavedFile: filePath,
                        Length:    snapshotLen,
                }, nil
        }
        return &BrowserSnapshotResult{
                URL:      url,
                Title:    info.Title,
                Snapshot: &snapshot,
                Length:   snapshotLen,
        }, nil
}

type BrowserUploadFileResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
}

func BrowserUploadFile(sessionID, url, fileInputSelector string, filePaths []string) (result *BrowserUploadFileResult, err error) {
        err = browserSafeOp("BrowserUploadFile", func() error {
                result, err = browserUploadFileImpl(sessionID, url, fileInputSelector, filePaths)
                return err
        })
        return
}

func browserUploadFileImpl(sessionID, url, fileInputSelector string, filePaths []string) (*BrowserUploadFileResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(fileInputSelector)
        if err != nil {
                return nil, fmt.Errorf("未找到文件输入框 '%s': %w", fileInputSelector, err)
        }
        if err := element.SetFiles(filePaths); err != nil {
                return nil, fmt.Errorf("上传文件失败: %w", err)
        }
        return &BrowserUploadFileResult{
                Success: true,
                Message: fmt.Sprintf("成功上传 %d 个文件", len(filePaths)),
        }, nil
}

type BrowserSelectOptionResult struct {
        Success bool     `json:"success"`
        Message string   `json:"message"`
        Values  []string `json:"values"`
}

func BrowserSelectOption(sessionID, url, selector string, values []string) (result *BrowserSelectOptionResult, err error) {
        err = browserSafeOp("BrowserSelectOption", func() error {
                result, err = browserSelectOptionImpl(sessionID, url, selector, values)
                return err
        })
        return
}

func browserSelectOptionImpl(sessionID, url, selector string, values []string) (*BrowserSelectOptionResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(selector)
        if err != nil {
                return nil, fmt.Errorf("未找到下拉框 '%s': %w", selector, err)
        }
        if err := element.Select(values, true, rod.SelectorTypeText); err != nil {
                return nil, fmt.Errorf("选择选项失败: %w", err)
        }
        return &BrowserSelectOptionResult{
                Success: true,
                Message: fmt.Sprintf("成功选择选项: %v", values),
                Values:  values,
        }, nil
}

type BrowserKeyPressResult struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
}

func BrowserKeyPress(sessionID, url string, keys []string) (result *BrowserKeyPressResult, err error) {
        err = browserSafeOp("BrowserKeyPress", func() error {
                result, err = browserKeyPressImpl(sessionID, url, keys)
                return err
        })
        return
}

func browserKeyPressImpl(sessionID, url string, keys []string) (*BrowserKeyPressResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        actions := page.KeyActions()
        for _, key := range keys {
                var k input.Key
                switch strings.ToLower(key) {
                case "enter": k = input.Enter
                case "tab": k = input.Tab
                case "escape", "esc": k = input.Escape
                case "backspace": k = input.Backspace
                case "delete": k = input.Delete
                case "arrowup", "up": k = input.ArrowUp
                case "arrowdown", "down": k = input.ArrowDown
                case "arrowleft", "left": k = input.ArrowLeft
                case "arrowright", "right": k = input.ArrowRight
                case "control", "ctrl": k = input.ControlLeft
                case "alt": k = input.AltLeft
                case "shift": k = input.ShiftLeft
                case "meta", "cmd", "command": k = input.MetaLeft
                default:
                        if len(key) == 1 {
                                k = input.Key(key[0])
                        } else {
                                continue
                        }
                }
                actions = actions.Press(k)
        }
        if err := actions.Do(); err != nil {
                return nil, fmt.Errorf("按键失败: %w", err)
        }
        return &BrowserKeyPressResult{
                Success: true,
                Message: fmt.Sprintf("成功按下键: %s", strings.Join(keys, "+")),
        }, nil
}

type BrowserElementScreenshotResult struct {
        Success bool   `json:"success"`
        Base64  string `json:"base64"`
}

func BrowserElementScreenshot(sessionID, url, selector string) (result *BrowserElementScreenshotResult, err error) {
        err = browserSafeOp("BrowserElementScreenshot", func() error {
                result, err = browserElementScreenshotImpl(sessionID, url, selector)
                return err
        })
        return
}

func browserElementScreenshotImpl(sessionID, url, selector string) (*BrowserElementScreenshotResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        element, err := page.Element(selector)
        if err != nil {
                return nil, fmt.Errorf("未找到元素 '%s': %w", selector, err)
        }
        screenshot, err := element.Screenshot(proto.PageCaptureScreenshotFormatPng, 100)
        if err != nil {
                return nil, fmt.Errorf("截图失败: %w", err)
        }
        return &BrowserElementScreenshotResult{
                Success: true,
                Base64:  base64.StdEncoding.EncodeToString(screenshot),
        }, nil
}

type BrowserEmulateDeviceResult struct {
        Success   bool   `json:"success"`
        Message   string `json:"message"`
        Device    string `json:"device"`
        Width     int    `json:"width"`
        Height    int    `json:"height"`
        UserAgent string `json:"userAgent"`
        URL       string `json:"url"`
}

func BrowserEmulateDevice(sessionID, url, device string) (result *BrowserEmulateDeviceResult, err error) {
        err = browserSafeOp("BrowserEmulateDevice", func() error {
                result, err = browserEmulateDeviceImpl(sessionID, url, device)
                return err
        })
        return
}

func browserEmulateDeviceImpl(sessionID, url, device string) (*BrowserEmulateDeviceResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        preset, ok := DevicePresets[device]
        if !ok {
                return nil, fmt.Errorf("未知的设备预设: %s", device)
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
                Width:             preset.Width,
                Height:            preset.Height,
                DeviceScaleFactor: preset.DeviceScale,
                Mobile:            preset.IsMobile,
        })
        page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: preset.UserAgent})
        if err := page.Navigate(url); err != nil {
                return nil, fmt.Errorf("导航失败: %w", err)
        }
        page.MustWaitLoad()
        info, _ := page.Info()
        return &BrowserEmulateDeviceResult{
                Success:   true,
                Message:   fmt.Sprintf("成功模拟 %s 设备访问页面", device),
                Device:    device,
                Width:     preset.Width,
                Height:    preset.Height,
                UserAgent: preset.UserAgent,
                URL:       info.URL,
        }, nil
}

type BrowserSetHeadersResult struct {
        Success bool     `json:"success"`
        Message string   `json:"message"`
        Headers []string `json:"headers"`
}

func BrowserSetHeaders(sessionID, url string, headers []string) (result *BrowserSetHeadersResult, err error) {
        err = browserSafeOp("BrowserSetHeaders", func() error {
                result, err = browserSetHeadersImpl(sessionID, url, headers)
                return err
        })
        return
}

func browserSetHeadersImpl(sessionID, url string, headers []string) (*BrowserSetHeadersResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        var headerPairs []string
        for _, h := range headers {
                parts := strings.SplitN(h, ":", 2)
                if len(parts) == 2 {
                        headerPairs = append(headerPairs, strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
                }
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        if len(headerPairs) > 0 {
                page.SetExtraHeaders(headerPairs)
        }
        if err := page.Navigate(url); err != nil {
                return nil, fmt.Errorf("导航失败: %w", err)
        }
        page.MustWaitLoad()
        return &BrowserSetHeadersResult{
                Success: true,
                Message: fmt.Sprintf("成功设置 %d 个请求头并访问页面", len(headerPairs)/2),
                Headers: headers,
        }, nil
}

type BrowserSetUserAgentResult struct {
        Success   bool   `json:"success"`
        Message   string `json:"message"`
        UserAgent string `json:"userAgent"`
        URL       string `json:"url"`
}

func BrowserSetUserAgent(sessionID, url, userAgent string) (result *BrowserSetUserAgentResult, err error) {
        err = browserSafeOp("BrowserSetUserAgent", func() error {
                result, err = browserSetUserAgentImpl(sessionID, url, userAgent)
                return err
        })
        return
}

func browserSetUserAgentImpl(sessionID, url, userAgent string) (*BrowserSetUserAgentResult, error) {
        if err := ValidateURLForFetch(url); err != nil {
                return nil, err
        }
        if userAgent == "" {
                return nil, fmt.Errorf("User-Agent 不能为空")
        }
        page, _, err := getOrCreatePage(sessionID, "default", url)
        if err != nil {
                return nil, err
        }
        page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: userAgent})
        if err := page.Navigate(url); err != nil {
                return nil, fmt.Errorf("导航失败: %w", err)
        }
        page.MustWaitLoad()
        info, _ := page.Info()
        return &BrowserSetUserAgentResult{
                Success:   true,
                Message:   "成功设置 User-Agent 并访问页面",
                UserAgent: userAgent,
                URL:       info.URL,
        }, nil
}

type BrowserPDFFromFileResult struct {
        FilePath string `json:"filePath"`
        Success  bool   `json:"success"`
        Base64   string `json:"base64,omitempty"`
        Message  string `json:"message"`
}

func BrowserPDFFromFile(sessionID, filePath string) (result *BrowserPDFFromFileResult, err error) {
        err = browserSafeOp("BrowserPDFFromFile", func() error {
                result, err = browserPDFFromFileImpl(sessionID, filePath)
                return err
        })
        return
}

func browserPDFFromFileImpl(sessionID, filePath string) (*BrowserPDFFromFileResult, error) {
        if _, err := os.Stat(filePath); os.IsNotExist(err) {
                return nil, fmt.Errorf("文件不存在: %s", filePath)
        }
        absPath, err := filepath.Abs(filePath)
        if err != nil {
                return nil, fmt.Errorf("获取绝对路径失败: %w", err)
        }
        fileURL := "file://" + absPath
        page, _, err := getOrCreatePage(sessionID, "default", fileURL)
        if err != nil {
                return nil, err
        }
        time.Sleep(500 * time.Millisecond)
        pdf := page.MustPDF()
        return &BrowserPDFFromFileResult{
                FilePath: filePath,
                Success:  true,
                Base64:   base64.StdEncoding.EncodeToString(pdf),
                Message:  "成功将 HTML 文件导出为 PDF",
        }, nil
}

