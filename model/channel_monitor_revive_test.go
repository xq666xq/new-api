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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyChannelIdUniqueIndex mirrors what old deployments still carry: channel_id
// used to be declared as an unnamed uniqueIndex, so GORM generated this name with
// no `WHERE deleted_at IS NULL` predicate. The column now declares
// uk_channel_monitor_channel with the predicate, but AutoMigrate only adds indexes
// — it never drops the old one. MySQL reaches the same state by a different route,
// since it has no partial indexes and drops the predicate entirely.
const legacyChannelIdUniqueIndex = `CREATE UNIQUE INDEX idx_channel_monitor_configs_channel_id
	ON channel_monitor_configs(channel_id)`

// Removing a channel from monitoring and then configuring it again is an ordinary
// admin round trip, and under the legacy index it used to fail with a duplicate
// key error: the soft-deleted row stayed hidden from the scoped lookup while still
// owning channel_id, so the upsert inserted instead of reusing it. The revived row
// must also read as freshly created rather than carrying the removed config's
// probe timestamp.
func TestUpsertChannelMonitorConfigRevivesRemovedConfigUnderLegacyUniqueIndex(t *testing.T) {
	db := newChannelMonitorPersistenceDB(t)
	require.NoError(t, db.Exec(legacyChannelIdUniqueIndex).Error)

	original := &ChannelMonitorConfig{
		ChannelId:    2,
		EndpointType: "openai",
		BodyMode:     MonitorBodyModeDefault,
		Remark:       "before removal",
	}
	require.NoError(t, UpsertChannelMonitorConfig(original))
	require.NoError(t, db.Model(&ChannelMonitorConfig{}).
		Where("id = ?", original.Id).
		Update("last_checked_at", 1234).Error)

	// Soft-delete directly: this is the tombstone older builds left behind, and the
	// row every existing deployment may still be carrying.
	require.NoError(t, db.Where("channel_id = ?", 2).Delete(&ChannelMonitorConfig{}).Error)
	hidden, err := GetChannelMonitorConfig(2)
	require.NoError(t, err)
	require.Nil(t, hidden, "the scoped lookup must not see the tombstone")

	revived := &ChannelMonitorConfig{
		ChannelId:    2,
		EndpointType: "anthropic",
		BodyMode:     MonitorBodyModeMerge,
		BodyJson:     `{"max_tokens":16}`,
		Remark:       "after re-adding",
	}
	require.NoError(t, UpsertChannelMonitorConfig(revived),
		"re-configuring a removed channel must not violate the channel_id unique index")

	loaded, err := GetChannelMonitorConfig(2)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, original.Id, loaded.Id, "the tombstoned row must be reused, not duplicated")
	assert.Equal(t, "anthropic", loaded.EndpointType)
	assert.Equal(t, "after re-adding", loaded.Remark)
	assert.Zero(t, loaded.LastCheckedAt,
		"a revived config must not inherit the removed config's probe timestamp")

	var total int64
	require.NoError(t, db.Unscoped().Model(&ChannelMonitorConfig{}).
		Where("channel_id = ?", 2).Count(&total).Error)
	assert.Equal(t, int64(1), total, "reviving must not leave a second row behind")
}

// A monitor config that is removed must leave no tombstone: channel_id is unique
// regardless of deleted_at on older schemas, so a surviving soft-deleted row is
// what blocked re-configuring the channel in the first place.
func TestDeleteChannelMonitorConfigLeavesNoSoftDeletedRow(t *testing.T) {
	db := newMonitorConfigDeleteDB(t)

	require.NoError(t, db.Create(&Channel{
		Id:     11,
		Name:   "removable",
		Key:    "test-key",
		Models: "gpt-test",
		Group:  "default",
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorConfig{
		ChannelId: 11,
		Enabled:   true,
		BodyMode:  MonitorBodyModeDefault,
	}).Error)

	require.NoError(t, DeleteChannelMonitorConfigByChannel(11))

	var remaining int64
	require.NoError(t, db.Unscoped().Model(&ChannelMonitorConfig{}).
		Where("channel_id = ?", 11).Count(&remaining).Error)
	assert.Zero(t, remaining, "removal must hard-delete so channel_id is released")
}
