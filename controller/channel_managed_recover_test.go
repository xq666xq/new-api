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

package controller

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func ptrInt64(v int64) *int64 { return &v }

// newBanStageTestDB provisions an isolated in-memory SQLite database with every
// table applyBanForPair touches (channels, abilities, monitor results, managed
// states) plus a root user so the ban/recover notification path does not panic.
func newBanStageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	// The ban/recover flip notifies the root user; force the in-memory notify
	// limiter so a shared RedisEnabled flag from another test cannot send us into
	// a nil Redis client.
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.Ability{}, &model.ChannelMonitorConfig{},
		&model.ChannelMonitorResult{}, &model.ChannelManagedState{}, &model.User{},
	))
	// A root user with no email so NotifyRootUser resolves and safely no-ops.
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
	}).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled = previousRedisEnabled
		_ = sqlDB.Close()
	})
	return db
}

// TestApplyBanForPairRecoversManuallyDisabledManagedChannel proves the fix for
// the channel-level recovery gap: when a managed channel is disabled at the
// channel level (manual status 2), sustained successful probes for one of its
// models must drive the confirmation state machine to a recovery that flips the
// channel status back to enabled — and the recovery must not resurrect another
// model on the same channel that the policy still bans (UpdateChannelStatus
// re-enables every ability, so ReplayManagedAbilities must re-apply the ban).
func TestApplyBanForPairRecoversManuallyDisabledManagedChannel(t *testing.T) {
	db := newBanStageTestDB(t)

	const channelID = 42
	// Managed, but administratively disabled at the channel level. Every ability
	// row is disabled to mirror what UpdateChannelStatus did on manual disable.
	require.NoError(t, db.Create(&model.Channel{
		Id:     channelID,
		Name:   "hosted-a",
		Status: common.ChannelStatusManuallyDisabled,
		Models: "model-a,model-b",
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "model-a", ChannelId: channelID, Enabled: false, Priority: ptrInt64(0)},
		{Group: "default", Model: "model-b", ChannelId: channelID, Enabled: false, Priority: ptrInt64(0)},
	}).Error)

	// model-a is policy-active (it should recover), model-b is policy-banned (it
	// must stay disabled through the channel recovery).
	require.NoError(t, model.UpsertChannelManagedState(&model.ChannelManagedState{
		ChannelId: channelID, ModelName: "model-a", BanState: model.ManagedBanStateActive,
	}))
	require.NoError(t, model.UpsertChannelManagedState(&model.ChannelManagedState{
		ChannelId: channelID, ModelName: "model-b", BanState: model.ManagedBanStateBanned,
	}))

	// A fresh successful scheduled probe for model-a.
	require.NoError(t, db.Create(&model.ChannelMonitorResult{
		ChannelId: channelID, ModelName: "model-a", Success: true, CheckedAt: 1000,
	}).Error)

	setting := &operation_setting.ManagedPolicySetting{
		BanEnabled:                true,
		ConfirmCount:              1, // single confirmation flips immediately
		BanConfirmIntervalSeconds: 0,
	}
	pair := managedModelPair{channelID: channelID, channelNm: "hosted-a", model: "model-a"}

	changed := applyBanForPair(pair, setting)
	require.True(t, changed, "successful probe on a disabled managed channel must trigger recovery")

	// Channel is re-enabled: the selection path routes on channel status.
	var channel model.Channel
	require.NoError(t, db.First(&channel, channelID).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)

	// model-a's managed state recovered to active.
	stateA, err := model.GetChannelManagedState(channelID, "model-a")
	require.NoError(t, err)
	require.NotNil(t, stateA)
	assert.Equal(t, model.ManagedBanStateActive, stateA.BanState)

	// model-a ability is enabled, model-b ability stays disabled: channel-level
	// re-enable must not override a per-model policy ban.
	var abilityA, abilityB model.Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-a").First(&abilityA).Error)
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-b").First(&abilityB).Error)
	assert.True(t, abilityA.Enabled, "recovered model must be enabled")
	assert.False(t, abilityB.Enabled, "still-banned model must stay disabled after channel recovery")
}

// TestApplyBanForPairKeepsEnabledChannelPerModelSemantics guards the no-op case:
// for a channel that is already enabled, a successful probe on an active model
// agrees with the stable state and must not change anything (channel status
// stays enabled, confirmation count stays zero).
func TestApplyBanForPairKeepsEnabledChannelPerModelSemantics(t *testing.T) {
	db := newBanStageTestDB(t)

	const channelID = 43
	require.NoError(t, db.Create(&model.Channel{
		Id:     channelID,
		Name:   "hosted-b",
		Status: common.ChannelStatusEnabled,
		Models: "model-a",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "model-a", ChannelId: channelID, Enabled: true, Priority: ptrInt64(0),
	}).Error)
	require.NoError(t, model.UpsertChannelManagedState(&model.ChannelManagedState{
		ChannelId: channelID, ModelName: "model-a", BanState: model.ManagedBanStateActive,
	}))
	require.NoError(t, db.Create(&model.ChannelMonitorResult{
		ChannelId: channelID, ModelName: "model-a", Success: true, CheckedAt: 1000,
	}).Error)

	setting := &operation_setting.ManagedPolicySetting{BanEnabled: true, ConfirmCount: 1}
	pair := managedModelPair{channelID: channelID, channelNm: "hosted-b", model: "model-a"}

	assert.False(t, applyBanForPair(pair, setting), "agreeing probe on an enabled channel is a no-op")

	var channel model.Channel
	require.NoError(t, db.First(&channel, channelID).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
}
