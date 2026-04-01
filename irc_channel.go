// +build irc

package main

import (
        "fmt"
        "log"
        "strings"
        "sync"
)

// IRCConfig holds IRC connection configuration.
type IRCConfig struct {
        Enabled     bool     `toon:"enabled" json:"Enabled"`
        Server      string   `toon:"server" json:"Server"`
        Port        int      `toon:"port" json:"Port"`
        Nick        string   `toon:"nick" json:"Nick"`
        Password    string   `toon:"password" json:"Password"`
        Channels    []string `toon:"channels" json:"Channels"`
        UseTLS      bool     `toon:"use_tls" json:"UseTLS"`
        GroupPolicy string   `toon:"group_policy" json:"GroupPolicy"`
}

// IRCChannel implements the Channel interface for IRC.
type IRCChannel struct {
        *BaseChannel
        config IRCConfig
        mu     sync.RWMutex
        stopCh chan struct{}
}

// NewIRCChannel creates a new IRC channel instance.
func NewIRCChannel(config *IRCConfig) (*IRCChannel, error) {
        if config == nil {
                return nil, fmt.Errorf("IRC config is nil")
        }
        return &IRCChannel{
                BaseChannel: NewBaseChannel("irc"),
                config:      *config,
                stopCh:      make(chan struct{}),
        }, nil
}

// Start starts the IRC bot connection.
func (irc *IRCChannel) Start(messageHandler func(chatID, senderID, content string, metadata map[string]interface{})) error {
        log.Printf("[IRC] Starting IRC bot: %s@%s:%d", irc.config.Nick, irc.config.Server, irc.config.Port)

        // TODO: Implement actual IRC connection using go-irc/ircevent or similar library
        // For now, log a warning that IRC is not yet fully implemented
        log.Printf("[IRC] WARNING: IRC channel is a stub implementation. Full IRC support requires adding an IRC library dependency.")

        // Monitor stop channel
        go func() {
                <-irc.stopCh
                log.Println("[IRC] Stopped.")
        }()

        return nil
}

// Stop stops the IRC bot.
func (irc *IRCChannel) Stop() {
        close(irc.stopCh)
}

// WriteChunk sends a response chunk to IRC.
func (irc *IRCChannel) WriteChunk(chunk StreamChunk) error {
        // TODO: Send message to appropriate IRC channel
        log.Printf("[IRC] WriteChunk: %s", truncateString(chunk.Content, 100))
        return nil
}

// RegisterToBus registers the IRC channel with the message bus.
func (irc *IRCChannel) RegisterToBus() {
        if globalMessageBus != nil {
                globalMessageBus.RegisterChannelSender("irc", irc)
                log.Println("[IRC] Registered to message bus")
        }
}

// SendToUser sends a message to a specific IRC user/channel (implements MessageSender).
func (irc *IRCChannel) SendToUser(userID string, message string) error {
        // TODO: Implement actual IRC message sending
        log.Printf("[IRC] SendToUser to %s: %s", userID, truncateString(message, 100))
        return nil
}

// GetChannelType returns the channel type (implements MessageSender).
func (irc *IRCChannel) GetChannelType() string {
        return "irc"
}

// SendMessage sends a message to a specific IRC channel.
func (irc *IRCChannel) SendMessage(target, message string) {
        // TODO: Implement actual IRC message sending
        log.Printf("[IRC] SendMessage to %s: %s", target, truncateString(message, 100))
}

// IsConnected returns whether the IRC connection is active.
func (irc *IRCChannel) IsConnected() bool {
        return false // stub
}

// GetNick returns the configured IRC nickname.
func (irc *IRCChannel) GetNick() string {
        return irc.config.Nick
}

// shouldRespond determines if the bot should respond to a message.
func (irc *IRCChannel) shouldRespond(content string, isDirectMention bool) bool {
        switch strings.ToLower(irc.config.GroupPolicy) {
        case "silent":
                return isDirectMention
        case "active":
                return true
        default:
                return isDirectMention
        }
}

func (ic *IRCChannel) shouldRespondInGroup(channel, message string) bool {
        // 使用全局群聊策略
        if globalGroupChatConfig != nil {
                return ShouldRespondInGroup(globalGroupChatConfig, channel, message, ic.config.Nick)
        }
        // 回退
        policy := ic.config.GroupPolicy
        if policy == "" {
                return strings.Contains(message, "@"+ic.config.Nick)
        }
        switch policy {
        case "open":
                return true
        case "mention":
                return strings.Contains(message, "@"+ic.config.Nick)
        default:
                return false
        }
}

