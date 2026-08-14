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

	// Error-triggered probe. When enabled, a genuine upstream fault seen on a
	// managed channel configured to probe the erroring model no longer disables
	// the whole channel; it queues a one-shot probe and defers the ban/recover
	// decision to the managed policy. This is independent of the monitoring
	// switches, the global auto-disable switch, and the channel's AutoBan flag. A probe is
	// triggered only after ErrorProbeThreshold *consecutive* faults for the same
	// (channel, model) within ErrorProbeWindowSeconds; a single successful forward
	// for that pair, or the window elapsing, resets the counter. Non-managed
	// channels (and any error the probe path declines to own) keep the legacy
	// auto-disable, still gated by the global switch + AutoBan.
	ErrorTriggerProbeEnabled bool `json:"error_trigger_probe_enabled"`
	ErrorProbeThreshold      int  `json:"error_probe_threshold"`
	ErrorProbeWindowSeconds  int  `json:"error_probe_window_seconds"`
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

	// Error-triggered probe defaults: two consecutive errors within a 60s window
	// trigger one probe. The threshold floor is 1 (a single error) and the window
	// floor is 1s so a hand-edited config can never disable counting by setting a
	// non-positive value.
	managedDefaultErrorProbeThreshold = 2
	managedDefaultErrorProbeWindow    = 60
	managedErrorProbeThresholdFloor   = 1
	managedErrorProbeWindowFloor      = 1
)

// 默认配置：两个开关默认关闭，参数取安全默认值。
var managedPolicySetting = ManagedPolicySetting{
	BanEnabled:                false,
	ConfirmCount:              managedDefaultConfirmCount,
	BanConfirmIntervalSeconds: managedDefaultConfirmInterval,
	SpeedEnabled:              false,
	SpeedWindow:               managedDefaultSpeedWindow,
	TierDiffPercent:           managedDefaultTierDiffPercent,
	ErrorTriggerProbeEnabled:  false,
	ErrorProbeThreshold:       managedDefaultErrorProbeThreshold,
	ErrorProbeWindowSeconds:   managedDefaultErrorProbeWindow,
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
	if managedPolicySetting.ErrorProbeThreshold < managedErrorProbeThresholdFloor {
		managedPolicySetting.ErrorProbeThreshold = managedDefaultErrorProbeThreshold
	}
	if managedPolicySetting.ErrorProbeWindowSeconds < managedErrorProbeWindowFloor {
		managedPolicySetting.ErrorProbeWindowSeconds = managedDefaultErrorProbeWindow
	}
	return &managedPolicySetting
}
