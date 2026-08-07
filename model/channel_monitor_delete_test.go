/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newMonitorConfigDeleteDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&Channel{}, &Ability{}, &ChannelMonitorConfig{}, &ChannelManagedState{},
	))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		_ = sqlDB.Close()
	})
	return db
}

// TestDeleteChannelMonitorConfigRestoresPolicyDecisions pins the contract behind
// the monitor list's delete action: removing a channel from the list must also
// undo what the managed policy did to it. Without the ability restore, a
// policy-banned model would stay disabled and a downgraded one stuck at its tier
// priority, with the row gone from the console and no way left to recover them.
func TestDeleteChannelMonitorConfigRestoresPolicyDecisions(t *testing.T) {
	db := newMonitorConfigDeleteDB(t)

	channelPriority := int64(20)
	require.NoError(t, db.Create(&Channel{
		Id:       7,
		Name:     "monitored",
		Key:      "test-key",
		Status:   common.ChannelStatusEnabled,
		Models:   "gpt-banned,gpt-slow",
		Group:    "default",
		Priority: &channelPriority,
	}).Error)

	// gpt-banned was banned by policy (enabled=false); gpt-slow was speed-tiered
	// down to priority 5. Both diverge from the channel-level values.
	bannedPriority := int64(20)
	slowPriority := int64(5)
	require.NoError(t, db.Create(&[]Ability{
		{Group: "default", Model: "gpt-banned", ChannelId: 7, Enabled: false, Priority: &bannedPriority},
		{Group: "default", Model: "gpt-slow", ChannelId: 7, Enabled: true, Priority: &slowPriority},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelManagedState{
		{ChannelId: 7, ModelName: "gpt-banned", BanState: ManagedBanStateBanned},
		{ChannelId: 7, ModelName: "gpt-slow", PriorityManaged: true, ManagedPriority: 5, OriginalPriority: 20},
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorConfig{
		ChannelId:       7,
		Enabled:         true,
		Managed:         true,
		IntervalSeconds: 60,
		BodyMode:        MonitorBodyModeDefault,
	}).Error)

	require.NoError(t, DeleteChannelMonitorConfigByChannel(7))

	config, err := GetChannelMonitorConfig(7)
	require.NoError(t, err)
	assert.Nil(t, config, "the config row must be gone so the channel leaves the monitor list")

	states, err := GetChannelManagedStatesByChannel(7)
	require.NoError(t, err)
	assert.Empty(t, states, "managed state must not outlive the config that produced it")

	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ?", 7).Order("model").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	for _, ability := range abilities {
		assert.True(t, ability.Enabled, "%s must be re-enabled from the channel status", ability.Model)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, channelPriority, *ability.Priority,
			"%s must fall back to the channel priority", ability.Model)
	}
}

// TestDeleteChannelMonitorConfigKeepsDisabledChannelDisabled guards the restore
// direction: the ability rows follow the channel's own status, so removing a
// disabled channel from monitoring must not silently re-enable it.
func TestDeleteChannelMonitorConfigKeepsDisabledChannelDisabled(t *testing.T) {
	db := newMonitorConfigDeleteDB(t)

	priority := int64(0)
	require.NoError(t, db.Create(&Channel{
		Id:       9,
		Name:     "disabled",
		Key:      "test-key",
		Status:   common.ChannelStatusManuallyDisabled,
		Models:   "gpt-test",
		Group:    "default",
		Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group: "default", Model: "gpt-test", ChannelId: 9, Enabled: false, Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorConfig{
		ChannelId:       9,
		IntervalSeconds: 60,
		BodyMode:        MonitorBodyModeDefault,
	}).Error)

	require.NoError(t, DeleteChannelMonitorConfigByChannel(9))

	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", 9).First(&ability).Error)
	assert.False(t, ability.Enabled, "a manually disabled channel must stay disabled")
}

// TestDeleteChannelMonitorConfigReportsMissingConfig keeps the delete endpoint
// honest: a channel that was never configured has nothing to remove, and the
// caller must be able to tell that apart from a successful delete.
func TestDeleteChannelMonitorConfigReportsMissingConfig(t *testing.T) {
	newMonitorConfigDeleteDB(t)

	err := DeleteChannelMonitorConfigByChannel(404)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
