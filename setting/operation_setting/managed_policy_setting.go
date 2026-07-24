package operation_setting

import (
	"github.com/QuantumNous/new-api/setting/config"
)

// ManagedPolicySetting holds the global channel-hosting ("托管") policy. It is
// applied only to channels whose monitor config has Managed=true. Two independent
// mechanisms live here, each with its own master switch:
//
//   - Ban/recover: a symmetric-confirmation circuit breaker driven by probe
//     success/failure. The engine only acts after ConfirmCount consecutive probes
//     disagree with the current stable state; a single agreeing probe resets the
//     counter. BanConfirmIntervalSeconds is the spacing between confirmation
//     probes and is quantized to the ~15s scheduler tick, so it is floored there.
//   - Speed up/downgrade: ranks channels serving the same model by their recent
//     TtftMs mean and assigns priority tiers. Channels whose mean TTFT differs by
//     less than TierDiffPercent (relative to the tier's fastest) share a tier and
//     keep weight-based load balancing within it.
type ManagedPolicySetting struct {
	// Ban/recover circuit breaker.
	BanEnabled                bool `json:"ban_enabled"`
	ConfirmCount              int  `json:"confirm_count"`
	BanConfirmIntervalSeconds int  `json:"ban_confirm_interval_seconds"`

	// Speed-based up/downgrade.
	SpeedEnabled    bool `json:"speed_enabled"`
	SpeedWindow     int  `json:"speed_window"`
	TierDiffPercent int  `json:"tier_diff_percent"`

	// DingTalk notification. When enabled and a webhook URL is set, the engine
	// pushes an actionCard to the DingTalk custom robot on every ban/recover
	// flip, in addition to the existing root-user notification. Secret is the
	// optional signing secret ("加签"); when non-empty the request is signed with
	// HMAC-SHA256 as DingTalk requires.
	DingTalkEnabled    bool   `json:"dingtalk_enabled"`
	DingTalkWebhookURL string `json:"dingtalk_webhook_url"`
	DingTalkSecret     string `json:"dingtalk_secret"`
}

const (
	// ManagedConfirmIntervalFloorSeconds mirrors the 15s channel-monitor scheduler
	// tick: confirmation probes cannot happen more often than the scheduler runs,
	// so a smaller configured interval is quantized up to this floor.
	ManagedConfirmIntervalFloorSeconds = 15

	managedDefaultConfirmCount    = 3
	managedDefaultConfirmInterval = 15
	managedDefaultSpeedWindow     = 5
	managedDefaultTierDiffPercent = 30
)

// 默认配置：两个开关默认关闭，参数取安全默认值。
var managedPolicySetting = ManagedPolicySetting{
	BanEnabled:                false,
	ConfirmCount:              managedDefaultConfirmCount,
	BanConfirmIntervalSeconds: managedDefaultConfirmInterval,
	SpeedEnabled:              false,
	SpeedWindow:               managedDefaultSpeedWindow,
	TierDiffPercent:           managedDefaultTierDiffPercent,
}

func init() {
	config.GlobalConfig.Register("managed_policy_setting", &managedPolicySetting)
}

// GetManagedPolicySetting returns the live policy with all fields clamped to safe
// ranges, so callers never see a zero/negative window or a sub-tick interval even
// if the persisted config was hand-edited.
func GetManagedPolicySetting() *ManagedPolicySetting {
	if managedPolicySetting.ConfirmCount < 1 {
		managedPolicySetting.ConfirmCount = managedDefaultConfirmCount
	}
	if managedPolicySetting.BanConfirmIntervalSeconds < ManagedConfirmIntervalFloorSeconds {
		managedPolicySetting.BanConfirmIntervalSeconds = ManagedConfirmIntervalFloorSeconds
	}
	if managedPolicySetting.SpeedWindow < 1 {
		managedPolicySetting.SpeedWindow = managedDefaultSpeedWindow
	}
	if managedPolicySetting.TierDiffPercent < 0 {
		managedPolicySetting.TierDiffPercent = managedDefaultTierDiffPercent
	}
	return &managedPolicySetting
}
