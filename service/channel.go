package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelError.ChannelName, channelError.ChannelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason)
		NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
		notifyChannelStatusDingTalk(channelError.ChannelId, channelError.ChannelName, true, reason)
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
		notifyChannelStatusDingTalk(channelId, channelName, false, "")
	}
}

// notifyChannelStatusDingTalk pushes a DingTalk action card when a channel is
// auto-disabled by normal forwarding (or re-enabled), mirroring the managed
// policy's DingTalk alerts. It intentionally reuses the managed policy's DingTalk
// configuration (webhook/secret/enable switch) so operators configure one place;
// the generic NotifyUser path has no DingTalk channel, which is why this is sent
// separately here. Best-effort: gated by the enable switch + a configured
// webhook, run in a goroutine so a slow endpoint never stalls the disable path,
// and all errors are swallowed into the log.
func notifyChannelStatusDingTalk(channelId int, channelName string, disabled bool, reason string) {
	setting := operation_setting.GetManagedPolicySetting()
	if setting == nil || !setting.DingTalkEnabled || strings.TrimSpace(setting.DingTalkWebhookURL) == "" {
		return
	}
	title, markdown := buildChannelStatusDingTalkCard(channelId, channelName, disabled, reason)
	webhook := setting.DingTalkWebhookURL
	secret := setting.DingTalkSecret
	go func() {
		if err := SendDingTalkActionCard(webhook, secret, title, markdown, ""); err != nil {
			common.SysError(fmt.Sprintf("channel status: dingtalk notify failed channel=%d disabled=%v: %v", channelId, disabled, err))
		}
	}()
}

// buildChannelStatusDingTalkCard composes the title/markdown for a channel
// auto-disable or re-enable event. The disable card carries the triggering
// reason (truncated so a huge upstream error never bloats the card); the enable
// card is a short confirmation.
func buildChannelStatusDingTalkCard(channelId int, channelName string, disabled bool, reason string) (string, string) {
	channelLabel := fmt.Sprintf("渠道 <font color=\"#1677ff\">**%s**</font>（#%d）", channelName, channelId)
	var b strings.Builder
	if disabled {
		title := fmt.Sprintf("🔴 渠道禁用 · %s", channelName)
		b.WriteString("## 🔴 渠道自动禁用\n\n")
		b.WriteString(fmt.Sprintf("%s\n\n", channelLabel))
		reasonText := strings.TrimSpace(reason)
		if reasonText == "" {
			reasonText = "无"
		} else {
			runes := []rune(reasonText)
			if len(runes) > 300 {
				reasonText = string(runes[:300]) + "…"
			}
		}
		b.WriteString(fmt.Sprintf("- **原因**：%s\n", reasonText))
		b.WriteString("- **触发**：正常转发错误达到自动禁用条件\n")
		return title, b.String()
	}
	title := fmt.Sprintf("🟢 渠道启用 · %s", channelName)
	b.WriteString("## 🟢 渠道恢复启用\n\n")
	b.WriteString(fmt.Sprintf("%s\n\n", channelLabel))
	b.WriteString("- **状态**：已重新启用\n")
	return title, b.String()
}

func ShouldDisableChannel(err *types.NewAPIError) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	return isChannelErrorDisableWorthy(err)
}

// isChannelErrorDisableWorthy reports whether an error looks like a genuine
// upstream/channel fault worth acting on (a channel-tagged error, a configured
// auto-disable status code, or an auto-disable keyword match), independent of the
// global auto-disable switch. skip-retry errors and nil are never worthy. It is
// shared by ShouldDisableChannel (which additionally requires the global switch)
// and by the error-triggered probe path, so both classify "is this a real fault?"
// identically while gating on their own switch.
func isChannelErrorDisableWorthy(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywords, true)
	return search
}

func ShouldEnableChannel(newAPIError *types.NewAPIError, status int) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	return true
}
