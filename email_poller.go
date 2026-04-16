package main

import (
        "crypto/tls"
        "fmt"
        "io"
        "log"
        "os"
        "strings"
        "time"

        "github.com/emersion/go-imap"
        "github.com/emersion/go-imap/client"
)

type EmailPoller struct {
        config *EmailConfig
        stop   chan struct{}
}

func (p *EmailPoller) Start() {
        ticker := time.NewTicker(time.Duration(p.config.PollInterval) * time.Second)
        go func() {
                for {
                        select {
                        case <-ticker.C:
                                p.poll()
                        case <-p.stop:
                                ticker.Stop()
                                return
                        }
                }
        }()
}

func (p *EmailPoller) Stop() {
        close(p.stop)
}

func (p *EmailPoller) poll() {
        log.Println("Checking for new emails...")
        var c *client.Client
        var err error
        if p.config.IMAPUseTLS {
                c, err = client.DialTLS(fmt.Sprintf("%s:%d", p.config.IMAPServer, p.config.IMAPPort), &tls.Config{ServerName: p.config.IMAPServer})
        } else {
                c, err = client.Dial(fmt.Sprintf("%s:%d", p.config.IMAPServer, p.config.IMAPPort))
        }
        if err != nil {
                log.Printf("IMAP connection error: %v", err)
                return
        }
        defer c.Logout()
        if err := c.Login(p.config.IMAPUser, p.config.IMAPPassword); err != nil {
                log.Printf("IMAP login error: %v", err)
                return
        }
        mbox, err := c.Select("INBOX", false)
        if err != nil {
                log.Printf("IMAP select error: %v", err)
                return
        }
        if mbox.Messages == 0 {
                return
        }
        criteria := imap.NewSearchCriteria()
        criteria.WithoutFlags = []string{imap.SeenFlag}
        uids, err := c.UidSearch(criteria)
        if err != nil {
                log.Printf("IMAP search error: %v", err)
                return
        }
        if len(uids) == 0 {
                return
        }
        seqset := new(imap.SeqSet)
        seqset.AddNum(uids...)
        messages := make(chan *imap.Message, 10)
        done := make(chan error, 1)
        go func() {
                done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchBody}, messages)
        }()
        for msg := range messages {
                go p.handleEmail(msg)
        }
        if err := <-done; err != nil {
                log.Printf("IMAP fetch error: %v", err)
        }
}

func (p *EmailPoller) handleEmail(msg *imap.Message) {
        var from, to, subject string
        if msg.Envelope != nil && len(msg.Envelope.From) > 0 {
                from = msg.Envelope.From[0].Address()
        }
        if msg.Envelope != nil && len(msg.Envelope.To) > 0 {
                to = msg.Envelope.To[0].Address()
        }
        if msg.Envelope != nil {
                subject = msg.Envelope.Subject
        }
        var body string
        for _, literal := range msg.Body {
                if literal != nil {
                        b, err := io.ReadAll(literal)
                        if err == nil {
                                body = string(b)
                                break
                        }
                }
        }
        if body == "" {
                log.Printf("Empty body for email from %s", from)
                return
        }
        trimmed := strings.TrimSpace(body)
        session := GetGlobalSession()
        if HandleSlashCommandWithDefaults(trimmed,
                func(resp string) {
                        ch := NewEmailChannel(from, to, subject, p.config)
                        ch.WriteChunk(StreamChunk{Content: resp, Done: true})
                },
                func() {
                        session.CancelTask()
                },
                func() {
                        // /quit: 在邮件频道中无实际连接可断开，仅记录
                        log.Println("[Email] /quit received (no connection to close)")
                },
                func() {
                        // /exit: 退出程序
                        log.Println("[Email] Received /exit, exiting program...")
                        session.autoSaveHistory()
                        if err := session.SavePendingMessages(); err != nil {
                                log.Printf("Failed to save pending messages: %v", err)
                        }
                        os.Exit(0)
                }) {
                return
        }
        ch := NewEmailChannel(from, to, subject, p.config)
        history := []Message{{Role: "user", Content: body}}
        ok, taskID := session.TryStartTask()
        if !ok {
                ch.WriteChunk(StreamChunk{Error: "已有任务在执行中，无法处理新邮件，请稍后再试。", Done: true})
                return
        }
        taskCtx := session.GetTaskCtx()
        defer session.SetTaskRunning(false, taskID)

        effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID,
            effectiveTemperature, effectiveMaxTokens, effectiveStream, effectiveThinking :=
            getEffectiveAPIConfig()

        _, err := AgentLoop(taskCtx, ch, history, effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID, effectiveTemperature, effectiveMaxTokens, effectiveStream, effectiveThinking)
        if err != nil {
                log.Printf("AgentLoop error for email from %s: %v", from, err)
        }
}

