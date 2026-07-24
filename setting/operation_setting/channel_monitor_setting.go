package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// ChannelMonitorSetting controls whether scheduled channel probes and their
// managed-policy follow-up are allowed to run. Disabling it pauses new work but
// intentionally preserves per-channel configs, probe history, and managed state.
type ChannelMonitorSetting struct {
	Enabled bool `json:"enabled"`
}

// Monitoring defaults to enabled so upgrades preserve the behavior of existing
// installations that already have monitored channels configured.
var channelMonitorSetting = ChannelMonitorSetting{
	Enabled: true,
}

func init() {
	config.GlobalConfig.Register("channel_monitor_setting", &channelMonitorSetting)
}

func GetChannelMonitorSetting() *ChannelMonitorSetting {
	return &channelMonitorSetting
}

func IsChannelMonitorEnabled() bool {
	return channelMonitorSetting.Enabled
}
