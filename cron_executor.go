package main

import (
	"fmt"
)

// createChannelFromConf 根据配置创建 Channel，jobName 用于邮件主题
func createChannelFromConf(jobName string, conf *ChannelConf) (Channel, error) {
	if conf == nil {
		conf = &ChannelConf{Type: "log"}
	}

	switch conf.Type {
	case "log":
		return NewLogChannel(), nil

	case "email":
		if len(conf.Recipients) == 0 {
			return nil, fmt.Errorf("email channel requires at least one recipient")
		}
		if globalEmailConfig == nil {
			return nil, fmt.Errorf("email config not set")
		}
		channels := make([]Channel, 0, len(conf.Recipients))
		for _, to := range conf.Recipients {
			// 创建邮件频道，主题包含任务名称
			ch := NewEmailChannelWithConfig(jobName, to, globalEmailConfig)
			channels = append(channels, ch)
		}
		if len(channels) == 1 {
			return channels[0], nil
		}
		return NewCompositeChannel(channels...), nil

	case "composite":
		if len(conf.SubChannels) == 0 {
			return nil, fmt.Errorf("composite channel requires at least one sub-channel")
		}
		subs := make([]Channel, 0, len(conf.SubChannels))
		for _, sub := range conf.SubChannels {
			ch, err := createChannelFromConf(jobName, &sub)
			if err != nil {
				return nil, err
			}
			subs = append(subs, ch)
		}
		return NewCompositeChannel(subs...), nil

	default:
		return nil, fmt.Errorf("unknown channel type: %s", conf.Type)
	}
}

