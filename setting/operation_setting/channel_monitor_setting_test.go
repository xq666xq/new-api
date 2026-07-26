package operation_setting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChannelMonitorSettingDefaultsEnabled(t *testing.T) {
	assert.True(t, GetChannelMonitorSetting().Enabled)
}

// clockAt builds a local time at the given hour:minute; the date is irrelevant
// because the curfew check only reads hour/minute.
func clockAt(hour, minute int) time.Time {
	return time.Date(2026, 7, 26, hour, minute, 0, 0, time.Local)
}

func TestIsChannelMonitorCurfewActive(t *testing.T) {
	// Preserve and restore the package-level setting so this test does not leak
	// curfew state into other tests sharing the binary.
	original := channelMonitorSetting
	t.Cleanup(func() { channelMonitorSetting = original })

	cases := []struct {
		name    string
		enabled bool
		start   string
		end     string
		now     time.Time
		want    bool
	}{
		{"disabled ignores window", false, "23:00", "07:00", clockAt(2, 0), false},
		{"same-day inside", true, "09:00", "17:00", clockAt(12, 0), true},
		{"same-day before start", true, "09:00", "17:00", clockAt(8, 59), false},
		{"same-day at start is inside", true, "09:00", "17:00", clockAt(9, 0), true},
		{"same-day at end is outside", true, "09:00", "17:00", clockAt(17, 0), false},
		{"wrap past midnight late night", true, "23:00", "07:00", clockAt(23, 30), true},
		{"wrap past midnight early morning", true, "23:00", "07:00", clockAt(6, 59), true},
		{"wrap past midnight daytime gap", true, "23:00", "07:00", clockAt(12, 0), false},
		{"wrap at end boundary is outside", true, "23:00", "07:00", clockAt(7, 0), false},
		{"equal bounds disable window", true, "08:00", "08:00", clockAt(8, 0), false},
		{"malformed start disables", true, "bad", "07:00", clockAt(2, 0), false},
		{"malformed end disables", true, "23:00", "nope", clockAt(2, 0), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channelMonitorSetting.CurfewEnabled = tc.enabled
			channelMonitorSetting.CurfewStart = tc.start
			channelMonitorSetting.CurfewEnd = tc.end
			assert.Equal(t, tc.want, IsChannelMonitorCurfewActive(tc.now))
		})
	}
}
