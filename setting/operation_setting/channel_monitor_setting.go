package operation_setting

import (
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

// ChannelMonitorSetting controls whether scheduled channel probes and their
// managed-policy follow-up are allowed to run. Disabling it pauses new work but
// intentionally preserves per-channel configs, probe history, and managed state.
//
// Curfew is an optional daily quiet window: while active, no channel/model is
// probed at all (see IsChannelMonitorCurfewActive). It only gates probing, not
// status queries or persisted data. CurfewStart/CurfewEnd are local-time
// "HH:MM" strings; a window whose start is later than its end wraps past
// midnight (e.g. 23:00 → 07:00).
//
// ProbeTimeoutSeconds bounds how long a single model probe may run. It is
// independent of the relay RELAY_TIMEOUT so tightening probe health checks never
// shortens real forwarding: a probe that exceeds it is cancelled and recorded as
// a failure, instead of hanging for minutes on a slow upstream and stalling the
// whole serial sweep. See GetChannelMonitorProbeTimeout for the clamped value.
type ChannelMonitorSetting struct {
	Enabled             bool   `json:"enabled"`
	CurfewEnabled       bool   `json:"curfew_enabled"`
	CurfewStart         string `json:"curfew_start"`
	CurfewEnd           string `json:"curfew_end"`
	ProbeTimeoutSeconds int    `json:"probe_timeout_seconds"`
}

// Probe timeout bounds. The default keeps a probe from hanging indefinitely while
// still allowing a slow-but-alive upstream to answer; the floor prevents a
// too-aggressive value from failing healthy channels, and the ceiling caps how
// long one probe can stall the serial sweep.
const (
	MonitorProbeTimeoutDefaultSeconds = 60
	MonitorProbeTimeoutMinSeconds     = 5
	MonitorProbeTimeoutMaxSeconds     = 600
)

// Monitoring defaults to enabled so upgrades preserve the behavior of existing
// installations that already have monitored channels configured. Curfew is off
// by default so nothing is silently paused on upgrade.
var channelMonitorSetting = ChannelMonitorSetting{
	Enabled:             true,
	CurfewEnabled:       false,
	CurfewStart:         "23:00",
	CurfewEnd:           "07:00",
	ProbeTimeoutSeconds: MonitorProbeTimeoutDefaultSeconds,
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

// GetChannelMonitorProbeTimeout returns the per-probe timeout, clamped to a safe
// range. A zero/unset value (e.g. an install upgraded before this field existed)
// falls back to the default rather than "no timeout", so an old config still gets
// bounded probes; out-of-range values are clamped to the floor/ceiling.
func GetChannelMonitorProbeTimeout() time.Duration {
	seconds := channelMonitorSetting.ProbeTimeoutSeconds
	if seconds <= 0 {
		seconds = MonitorProbeTimeoutDefaultSeconds
	}
	if seconds < MonitorProbeTimeoutMinSeconds {
		seconds = MonitorProbeTimeoutMinSeconds
	}
	if seconds > MonitorProbeTimeoutMaxSeconds {
		seconds = MonitorProbeTimeoutMaxSeconds
	}
	return time.Duration(seconds) * time.Second
}

// parseCurfewMinutes parses an "HH:MM" 24-hour string into minutes-of-day
// [0,1440). It returns ok=false for any malformed input so a bad setting simply
// disables the curfew rather than pausing probes indefinitely.
func parseCurfewMinutes(value string) (int, bool) {
	t, err := time.Parse("15:04", value)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// IsChannelMonitorCurfewActive reports whether the given local time falls inside
// the configured daily curfew window. It returns false when curfew is disabled
// or misconfigured (unparseable bounds, or equal start/end which would describe
// an empty or full-day window). A window whose start is after its end wraps
// across midnight.
func IsChannelMonitorCurfewActive(now time.Time) bool {
	if !channelMonitorSetting.CurfewEnabled {
		return false
	}
	start, okStart := parseCurfewMinutes(channelMonitorSetting.CurfewStart)
	end, okEnd := parseCurfewMinutes(channelMonitorSetting.CurfewEnd)
	if !okStart || !okEnd || start == end {
		return false
	}
	current := now.Hour()*60 + now.Minute()
	if start < end {
		return current >= start && current < end
	}
	// Window wraps past midnight, e.g. 23:00 → 07:00.
	return current >= start || current < end
}
