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

// TestApplyBanForPairDoesNotRecoverMultiModelChannel verifies that one model's
// managed health never changes a multi-model channel's status.
func TestApplyBanForPairDoesNotRecoverMultiModelChannel(t *testing.T) {
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

	// Both models are policy-banned. A successful probe recovers model-a only and
	// must not recover the whole channel.
	require.NoError(t, model.UpsertChannelManagedState(&model.ChannelManagedState{
		ChannelId: channelID, ModelName: "model-a", BanState: model.ManagedBanStateBanned,
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

	event := applyBanForPair(pair, setting)
	require.NotNil(t, event)
	assert.Equal(t, "recovered", event.action)

	// The multi-model channel remains manually disabled.
	var channel model.Channel
	require.NoError(t, db.First(&channel, channelID).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)

	// model-a recovers, while model-b remains banned.
	stateA, err := model.GetChannelManagedState(channelID, "model-a")
	require.NoError(t, err)
	require.NotNil(t, stateA)
	assert.Equal(t, model.ManagedBanStateActive, stateA.BanState)

	// model-a ability is enabled, model-b ability stays disabled: channel-level
	// re-enable must not override a per-model policy ban.
	var abilityA, abilityB model.Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-a").First(&abilityA).Error)
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-b").First(&abilityB).Error)
	assert.True(t, abilityA.Enabled)
	assert.False(t, abilityB.Enabled)
}

func TestApplyBanForPairSyncsSingleModelChannelStatus(t *testing.T) {
	db := newBanStageTestDB(t)

	const channelID = 44
	require.NoError(t, db.Create(&model.Channel{
		Id: channelID, Name: "hosted-single", Status: common.ChannelStatusEnabled, Models: "model-a",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "model-a", ChannelId: channelID, Enabled: true, Priority: ptrInt64(0),
	}).Error)
	require.NoError(t, db.Create(&model.ChannelMonitorResult{
		ChannelId: channelID, ModelName: "model-a", Success: false, CheckedAt: 1000,
	}).Error)

	setting := &operation_setting.ManagedPolicySetting{BanEnabled: true, ConfirmCount: 1}
	pair := managedModelPair{channelID: channelID, channelNm: "hosted-single", model: "model-a"}

	banEvent := applyBanForPair(pair, setting)
	require.NotNil(t, banEvent)
	assert.Equal(t, "banned", banEvent.action)

	var channel model.Channel
	require.NoError(t, db.First(&channel, channelID).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	var ability model.Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-a").First(&ability).Error)
	assert.False(t, ability.Enabled)

	require.NoError(t, db.Create(&model.ChannelMonitorResult{
		ChannelId: channelID, ModelName: "model-a", Success: true, CheckedAt: 1020,
	}).Error)
	recoverEvent := applyBanForPair(pair, setting)
	require.NotNil(t, recoverEvent)
	assert.Equal(t, "recovered", recoverEvent.action)

	channel = model.Channel{}
	require.NoError(t, db.First(&channel, channelID).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	ability = model.Ability{}
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-a").First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestApplyBanForPairKeepsMultiModelChannelEnabled(t *testing.T) {
	db := newBanStageTestDB(t)

	const channelID = 45
	require.NoError(t, db.Create(&model.Channel{
		Id: channelID, Name: "hosted-multi", Status: common.ChannelStatusEnabled, Models: "model-a,model-b",
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "model-a", ChannelId: channelID, Enabled: true, Priority: ptrInt64(0)},
		{Group: "default", Model: "model-b", ChannelId: channelID, Enabled: true, Priority: ptrInt64(0)},
	}).Error)
	require.NoError(t, db.Create(&model.ChannelMonitorResult{
		ChannelId: channelID, ModelName: "model-a", Success: false, CheckedAt: 1000,
	}).Error)

	setting := &operation_setting.ManagedPolicySetting{BanEnabled: true, ConfirmCount: 1}
	pair := managedModelPair{channelID: channelID, channelNm: "hosted-multi", model: "model-a"}
	event := applyBanForPair(pair, setting)
	require.NotNil(t, event)
	assert.Equal(t, "banned", event.action)

	var channel model.Channel
	require.NoError(t, db.First(&channel, channelID).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	var abilityA, abilityB model.Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-a").First(&abilityA).Error)
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-b").First(&abilityB).Error)
	assert.False(t, abilityA.Enabled)
	assert.True(t, abilityB.Enabled)
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

	assert.Nil(t, applyBanForPair(pair, setting), "agreeing probe on an enabled channel is a no-op")

	var channel model.Channel
	require.NoError(t, db.First(&channel, channelID).Error)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
}

func TestGroupManagedFlipsByAction(t *testing.T) {
	events := []managedFlipEvent{
		{pair: managedModelPair{channelID: 7, model: "model-a"}, action: "recovered"},
		{pair: managedModelPair{channelID: 8, model: "model-b"}, action: "banned"},
		{pair: managedModelPair{channelID: 9, model: "model-c"}, action: "recovered"},
		{pair: managedModelPair{channelID: 8, model: "model-d"}, action: "banned"},
	}

	groups := groupManagedFlipsByAction(events)
	require.Len(t, groups, 2, "one sweep must create at most one group per action")
	assert.Equal(t, []string{"model-a", "model-c"}, []string{groups[0][0].pair.model, groups[0][1].pair.model})
	assert.Equal(t, []string{"model-b", "model-d"}, []string{groups[1][0].pair.model, groups[1][1].pair.model})
	assert.Equal(t, "recovered", groups[0][0].action, "action groups retain first-seen order")
	assert.Equal(t, "banned", groups[1][0].action)
}

func TestBuildManagedRootNotificationAggregatesChannelsAndModels(t *testing.T) {
	batch := []managedFlipEvent{
		{pair: managedModelPair{channelID: 7, channelNm: "hosted-a", model: "model-a"}, action: "banned", reason: "probe failed"},
		{pair: managedModelPair{channelID: 8, channelNm: "hosted-b", model: "model-b"}, action: "banned", reason: "probe failed"},
		{pair: managedModelPair{channelID: 7, channelNm: "hosted-a", model: "model-c"}, action: "banned", reason: "probe failed"},
	}

	notifyType, subject, content := buildManagedRootNotification(batch)
	assert.Equal(t, "managed_banned", notifyType)
	assert.Contains(t, subject, "2 个渠道的 3 个模型")
	assert.Contains(t, content, "渠道「hosted-a」（#7）：model-a、model-c")
	assert.Contains(t, content, "渠道「hosted-b」（#8）：model-b")
	assert.Less(t, strings.Index(content, "hosted-a"), strings.Index(content, "hosted-b"), "channel order must be stable")
}

// TestBuildManagedDingTalkCardAggregatesChannelsAndModels proves the user-facing
// aggregation contract: one action card contains every affected channel and
// model, with the recommendation section appended only once.
func TestBuildManagedDingTalkCardAggregatesChannelsAndModels(t *testing.T) {
	// The card appends the recommendation section, which reads channel abilities;
	// provision an isolated DB with the tables it touches so the lookup returns an
	// empty list instead of hitting a nil DB.
	db := newBanStageTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelRecommendation{}))

	group := []managedFlipEvent{
		{
			pair:   managedModelPair{channelID: 7, channelNm: "hosted-a", model: "model-a"},
			action: "banned",
			state:  &model.ChannelManagedState{ChannelId: 7, ModelName: "model-a"},
			latest: &model.ChannelMonitorResult{ErrorMessage: "429 rate limited"},
		},
		{
			pair:   managedModelPair{channelID: 7, channelNm: "hosted-a", model: "model-b"},
			action: "banned",
			state:  &model.ChannelManagedState{ChannelId: 7, ModelName: "model-b"},
			latest: &model.ChannelMonitorResult{ErrorMessage: "500 upstream"},
		},
		{
			pair:   managedModelPair{channelID: 8, channelNm: "hosted-b", model: "model-c"},
			action: "banned",
			state:  &model.ChannelManagedState{ChannelId: 8, ModelName: "model-c"},
			latest: &model.ChannelMonitorResult{ErrorMessage: "524 timeout"},
		},
	}

	title, markdown := buildManagedDingTalkCard(group)
	assert.Contains(t, title, "2个渠道 / 3个模型")
	assert.Contains(t, markdown, "托管渠道批量封禁")
	assert.Contains(t, markdown, "model-a")
	assert.Contains(t, markdown, "model-b")
	assert.Contains(t, markdown, "model-c")
	assert.Contains(t, markdown, "429 rate limited")
	assert.Contains(t, markdown, "500 upstream")
	assert.Contains(t, markdown, "524 timeout")
	assert.Equal(t, 1, strings.Count(markdown, "hosted-a"), "channel is named once for all of its models")
	assert.Equal(t, 1, strings.Count(markdown, "hosted-b"))
	assert.Equal(t, 1, strings.Count(markdown, "推荐使用模型"), "recommendations are appended once per action batch")
	assert.Less(t, strings.Index(markdown, "hosted-a"), strings.Index(markdown, "hosted-b"), "channel order must be stable")

	recoveryGroup := []managedFlipEvent{
		{
			pair:   managedModelPair{channelID: 7, channelNm: "hosted-a", model: "model-a"},
			action: "recovered",
			state:  &model.ChannelManagedState{ChannelId: 7, ModelName: "model-a"},
		},
		{
			pair:   managedModelPair{channelID: 8, channelNm: "hosted-b", model: "model-c"},
			action: "recovered",
			state:  &model.ChannelManagedState{ChannelId: 8, ModelName: "model-c"},
		},
	}

	recoveryTitle, recoveryMarkdown := buildManagedDingTalkCard(recoveryGroup)
	assert.Contains(t, recoveryTitle, "2个渠道 / 2个模型")
	assert.Contains(t, recoveryMarkdown, "托管渠道批量恢复")
	assert.Contains(t, recoveryMarkdown, "hosted-a")
	assert.Contains(t, recoveryMarkdown, "hosted-b")
	assert.Contains(t, recoveryMarkdown, "model-a")
	assert.Contains(t, recoveryMarkdown, "model-c")
	assert.Equal(t, 1, strings.Count(recoveryMarkdown, "推荐使用模型"))
}
