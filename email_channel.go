package main

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// EmailChannel 实现邮件频道
type EmailChannel struct {
	*BaseChannel
	from       string
	to         string
	subject    string
	body       strings.Builder
	smtpConfig *EmailConfig
	done       bool
}

// NewEmailChannel 创建邮件频道，用于回复特定邮件
func NewEmailChannel(from, to, subject string, smtpConfig *EmailConfig) *EmailChannel {
	return &EmailChannel{
		BaseChannel: NewBaseChannel(from + ":" + subject),
		from:        from,
		to:          to,
		subject:     subject,
		smtpConfig:  smtpConfig,
	}
}

// WriteChunk 收集内容，当收到 Done 块时发送邮件（经过流式替换处理）
func (ec *EmailChannel) WriteChunk(chunk StreamChunk) error {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	// 应用流式字符串替换
	processed := ec.ProcessChunkWithReplacement(chunk)

	if processed.Error != "" {
		ec.body.WriteString(fmt.Sprintf("Error: %s\n", processed.Error))
		return nil
	}
	if processed.Content != "" {
		ec.body.WriteString(processed.Content)
	}
	if processed.ReasoningContent != "" {
		ec.body.WriteString(processed.ReasoningContent)
	}
	if processed.Done && !ec.done {
		ec.done = true
		return ec.sendEmail()
	}
	return nil
}

// sendEmail 通过 SMTP 发送回复邮件
func (ec *EmailChannel) sendEmail() error {
	// 构建邮件内容
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Re: %s\r\n\r\n%s",
		ec.smtpConfig.SMTPUser, ec.to, ec.subject, ec.body.String())

	auth := smtp.PlainAuth("", ec.smtpConfig.SMTPUser, ec.smtpConfig.SMTPPassword, ec.smtpConfig.SMTPServer)

	addr := fmt.Sprintf("%s:%d", ec.smtpConfig.SMTPServer, ec.smtpConfig.SMTPPort)
	// 如果使用 TLS
	if ec.smtpConfig.SMTPUseTLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: ec.smtpConfig.SMTPServer})
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, ec.smtpConfig.SMTPServer)
		if err != nil {
			return err
		}
		if err = client.Auth(auth); err != nil {
			return err
		}
		if err = client.Mail(ec.smtpConfig.SMTPUser); err != nil {
			return err
		}
		if err = client.Rcpt(ec.to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(msg))
		if err != nil {
			return err
		}
		err = w.Close()
		if err != nil {
			return err
		}
		return client.Quit()
	} else {
		// 普通 SMTP
		return smtp.SendMail(addr, auth, ec.smtpConfig.SMTPUser, []string{ec.to}, []byte(msg))
	}
}

// NewEmailChannelWithConfig 创建邮件频道，使用全局邮件配置，主题包含任务名
func NewEmailChannelWithConfig(jobName, to string, config *EmailConfig) *EmailChannel {
	subject := fmt.Sprintf("Cron Job Result: %s", jobName)
	return &EmailChannel{
		BaseChannel: NewBaseChannel(to),
		from:        config.SMTPUser,
		to:          to,
		subject:     subject,
		smtpConfig:  config,
	}
}

// HealthCheck 健康检查
func (ec *EmailChannel) HealthCheck() map[string]interface{} {
	return map[string]interface{}{
		"id":      ec.id,
		"status":  "operational",
		"from":    ec.from,
		"to":      ec.to,
		"message": "Email channel health check",
	}
}

// GetSessionID 实现 Channel 接口
func (ec *EmailChannel) GetSessionID() string {
	return ""
}

// GetChannelType 获取渠道类型
func (ec *EmailChannel) GetChannelType() string {
	return "email"
}

// IsConnected 检查 Email 通道状态
func (ec *EmailChannel) IsConnected() bool {
	return true
}
