package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChannelMonitorSettingDefaultsEnabled(t *testing.T) {
	assert.True(t, GetChannelMonitorSetting().Enabled)
}
