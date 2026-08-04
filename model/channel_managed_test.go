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

// newManagedTestDB spins up an isolated in-memory SQLite database with the
// tables the managed-policy layer touches (channels, abilities, monitor results,
// managed states) and restores global DB state on cleanup.
func newManagedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&Channel{}, &Ability{}, &ChannelMonitorConfig{}, &ChannelMonitorResult{}, &ChannelManagedState{},
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

// TestGetRecentTtftMean verifies the speed-ranking datapoint only averages
// successful, positive-TTFT probes over the most recent window, so failures and
// non-stream probes (TtftMs = 0) never distort a channel's ranking.
func TestGetRecentTtftMean(t *testing.T) {
	db := newManagedTestDB(t)

	// The newest row is a manual diagnostic and must never enter policy samples.
	// Scheduled newest-first probes are: 300(ok), 100(ok), 0(ok/non-stream), fail, 999(ok, older).
	require.NoError(t, db.Create(&[]ChannelMonitorResult{
		{ChannelId: 1, ModelName: "m", Success: true, TtftMs: 999, CheckedAt: 100},
		{ChannelId: 1, ModelName: "m", Success: false, TtftMs: 0, CheckedAt: 200},
		{ChannelId: 1, ModelName: "m", Success: true, TtftMs: 0, CheckedAt: 300},
		{ChannelId: 1, ModelName: "m", Success: true, TtftMs: 100, CheckedAt: 400},
		{ChannelId: 1, ModelName: "m", Success: true, TtftMs: 300, CheckedAt: 500},
		{ChannelId: 1, ModelName: "m", TriggerType: ChannelMonitorTriggerManual, Success: true, TtftMs: 1, CheckedAt: 600},
	}).Error)

	// Window of 2 over the eligible (success && ttft>0) rows newest-first: 300, 100.
	mean, count, err := GetRecentTtftMean(1, "m", 2)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.InDelta(t, 200.0, mean, 0.001)

	// A pair with no usable sample returns count 0 so the caller leaves priority alone.
	mean, count, err = GetRecentTtftMean(1, "other", 5)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, 0.0, mean)
}

func TestGetLatestChannelMonitorResultIgnoresManualDiagnostics(t *testing.T) {
	db := newManagedTestDB(t)
	require.NoError(t, db.Create(&[]ChannelMonitorResult{
		{ChannelId: 1, ModelName: "m", Success: true, CheckedAt: 100},
		{ChannelId: 1, ModelName: "m", TriggerType: ChannelMonitorTriggerManual, Success: false, CheckedAt: 200},
	}).Error)

	latest, err := GetLatestChannelMonitorResult(1, "m")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.True(t, latest.Success)
	assert.Equal(t, int64(100), latest.CheckedAt)
}

// TestChannelManagedStateRoundTrip covers upsert-then-read and that a second
// upsert updates in place rather than inserting a duplicate.
func TestChannelManagedStateRoundTrip(t *testing.T) {
	db := newManagedTestDB(t)

	require.NoError(t, UpsertChannelManagedState(&ChannelManagedState{
		ChannelId: 3, ModelName: "gpt", BanState: ManagedBanStateActive, ConfirmCount: 2,
	}))
	got, err := GetChannelManagedState(3, "gpt")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 2, got.ConfirmCount)

	// Update in place.
	got.BanState = ManagedBanStateBanned
	got.ConfirmCount = 0
	require.NoError(t, UpsertChannelManagedState(got))

	var count int64
	require.NoError(t, db.Model(&ChannelManagedState{}).Where("channel_id = ? AND model_name = ?", 3, "gpt").Count(&count).Error)
	assert.Equal(t, int64(1), count, "upsert must not duplicate the row")

	reread, err := GetChannelManagedState(3, "gpt")
	require.NoError(t, err)
	assert.Equal(t, ManagedBanStateBanned, reread.BanState)
}

// TestModelLevelAbilityOps verifies per-(channel, model) enable/priority writes
// touch only that model's ability rows, leaving the channel's other models alone.
func TestModelLevelAbilityOps(t *testing.T) {
	db := newManagedTestDB(t)

	// Two abilities for one channel: model-a and model-b in the default group.
	require.NoError(t, db.Create(&[]Ability{
		{Group: "default", Model: "model-a", ChannelId: 5, Enabled: true, Priority: ptrInt64(0)},
		{Group: "default", Model: "model-b", ChannelId: 5, Enabled: true, Priority: ptrInt64(0)},
	}).Error)

	require.NoError(t, SetChannelModelAbilityEnabled(5, "model-a", false))
	require.NoError(t, SetChannelModelAbilityPriority(5, "model-a", 7))

	var a, b Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", 5, "model-a").First(&a).Error)
	require.NoError(t, db.Where("channel_id = ? AND model = ?", 5, "model-b").First(&b).Error)

	assert.False(t, a.Enabled, "model-a disabled")
	assert.Equal(t, int64(7), *a.Priority, "model-a priority updated")
	assert.True(t, b.Enabled, "model-b left enabled")
	assert.Equal(t, int64(0), *b.Priority, "model-b priority untouched")

	prio, err := GetChannelModelAbilityPriority(5, "model-a")
	require.NoError(t, err)
	assert.Equal(t, int64(7), prio)
}

func TestApplyChannelManagedAbilityStateIsAtomic(t *testing.T) {
	db := newManagedTestDB(t)
	require.NoError(t, db.Create(&[]Ability{
		{Group: "default", Model: "model-a", ChannelId: 8, Enabled: true, Priority: ptrInt64(0)},
		{Group: "default", Model: "model-b", ChannelId: 8, Enabled: true, Priority: ptrInt64(0)},
	}).Error)

	disabled := false
	state := &ChannelManagedState{
		ChannelId: 8,
		ModelName: "model-a",
		BanState:  ManagedBanStateBanned,
	}
	require.NoError(t, ApplyChannelManagedAbilityState(state, &disabled, nil))

	var modelA Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", 8, "model-a").First(&modelA).Error)
	assert.False(t, modelA.Enabled)
	stored, err := GetChannelManagedState(8, "model-a")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, ManagedBanStateBanned, stored.BanState)

	// Force the state write to fail after the ability update has begun. The
	// transaction must roll the ability change back instead of leaving a decision
	// that cannot be replayed from ChannelManagedState.
	require.NoError(t, db.Migrator().DropTable(&ChannelManagedState{}))
	failedState := &ChannelManagedState{
		ChannelId: 8,
		ModelName: "model-b",
		BanState:  ManagedBanStateBanned,
	}
	require.Error(t, ApplyChannelManagedAbilityState(failedState, &disabled, nil))

	var modelB Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", 8, "model-b").First(&modelB).Error)
	assert.True(t, modelB.Enabled, "ability update must roll back when managed-state persistence fails")
}

func TestApplyManualChannelManagedOverridesSynchronizesModels(t *testing.T) {
	db := newManagedTestDB(t)
	require.NoError(t, db.Create(&[]Ability{
		{Group: "default", Model: "model-a", ChannelId: 21, Enabled: false, Priority: ptrInt64(7)},
		{Group: "vip", Model: "model-a", ChannelId: 21, Enabled: false, Priority: ptrInt64(7)},
		{Group: "default", Model: "model-b", ChannelId: 21, Enabled: true, Priority: ptrInt64(5)},
		{Group: "default", Model: "model-c", ChannelId: 22, Enabled: false, Priority: ptrInt64(3)},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelManagedState{
		{
			ChannelId: 21, ModelName: "model-a", BanState: ManagedBanStateBanned,
			ConfirmCount: 2, LastConfirmProbeAt: 100, PriorityManaged: true,
			OriginalPriority: 1, ManagedPriority: 7,
		},
		{
			ChannelId: 21, ModelName: "model-b", BanState: ManagedBanStateActive,
			ConfirmCount: 1, LastConfirmProbeAt: 200, PriorityManaged: true,
			OriginalPriority: 2, ManagedPriority: 5,
		},
		{
			ChannelId: 22, ModelName: "model-c", BanState: ManagedBanStateBanned,
			PriorityManaged: true, OriginalPriority: 3, ManagedPriority: 3,
		},
	}).Error)

	enabled := true
	priority := int64(9)
	require.NoError(t, ApplyManualChannelManagedOverrides([]int{21}, &enabled, &priority))

	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ?", 21).Order("model, "+commonGroupCol).Find(&abilities).Error)
	require.Len(t, abilities, 3)
	for _, ability := range abilities {
		assert.True(t, ability.Enabled)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, priority, *ability.Priority)
	}

	states, err := GetChannelManagedStatesByChannel(21)
	require.NoError(t, err)
	require.Len(t, states, 2)
	for _, state := range states {
		assert.Equal(t, ManagedBanStateActive, state.BanState)
		assert.Zero(t, state.ConfirmCount)
		assert.Positive(t, state.LastConfirmProbeAt)
		assert.Positive(t, state.LastRecoverAt)
		assert.False(t, state.PriorityManaged)
		assert.Equal(t, priority, state.OriginalPriority)
		assert.Equal(t, priority, state.ManagedPriority)
	}

	disabled := false
	require.NoError(t, ApplyManualChannelManagedOverrides([]int{21}, &disabled, nil))
	require.NoError(t, db.Where("channel_id = ?", 21).Order("model, "+commonGroupCol).Find(&abilities).Error)
	for _, ability := range abilities {
		assert.False(t, ability.Enabled)
	}
	states, err = GetChannelManagedStatesByChannel(21)
	require.NoError(t, err)
	for _, state := range states {
		assert.Equal(t, ManagedBanStateBanned, state.BanState)
		assert.Positive(t, state.LastBanAt)
	}

	var untouched Ability
	require.NoError(t, db.Where("channel_id = ?", 22).First(&untouched).Error)
	assert.False(t, untouched.Enabled)
	require.NotNil(t, untouched.Priority)
	assert.Equal(t, int64(3), *untouched.Priority)
	untouchedState, err := GetChannelManagedState(22, "model-c")
	require.NoError(t, err)
	require.NotNil(t, untouchedState)
	assert.True(t, untouchedState.PriorityManaged)
}

func TestIsChannelManagedDoesNotDependOnProbeEnabled(t *testing.T) {
	db := newManagedTestDB(t)
	require.NoError(t, db.Create(&[]ChannelMonitorConfig{
		{ChannelId: 11, Enabled: false, Managed: true},
		{ChannelId: 12, Enabled: true, Managed: false},
	}).Error)

	managed, err := IsChannelManaged(11)
	require.NoError(t, err)
	assert.True(t, managed, "pausing probes must not re-enable legacy auto-ban")

	managed, err = IsChannelManaged(12)
	require.NoError(t, err)
	assert.False(t, managed)
}

func TestManagedOverlayMatchesExactModelOnly(t *testing.T) {
	overlay := map[string]managedOverlayEntry{
		managedOverlayKey(13, "model-a"): {
			Banned:          true,
			PriorityManaged: true,
			Priority:        7,
		},
	}

	assert.Equal(t, int64(7), managedEffectivePriority(overlay, 13, "model-a", 1))
	assert.Equal(t, int64(1), managedEffectivePriority(overlay, 13, "model-a-*", 1))
	assert.Equal(t, int64(1), managedEffectivePriority(overlay, 13, "model-a-alias", 1))
}

func ptrInt64(v int64) *int64 { return &v }
