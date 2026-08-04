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
	"gorm.io/gorm"
)

// newRecommendationTestDB reuses the managed-policy fixture plus the
// recommendation table, so a recommendation list can be built end to end
// against real abilities, channels, monitor results, and recommendation rows.
func newRecommendationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newManagedTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelRecommendation{}))
	return db
}

// TestLatestProbeSpeedTakesMostRecentSuccess verifies the advertised speed comes
// from the most recent successful scheduled probe, not an older one, a failure, or
// an admin-triggered diagnostic, and falls back to 0/0 when nothing usable exists.
func TestLatestProbeSpeedTakesMostRecentSuccess(t *testing.T) {
	db := newRecommendationTestDB(t)
	require.NoError(t, db.Create(&[]ChannelMonitorResult{
		{ChannelId: 1, ModelName: "m", Success: true, TtftMs: 100, LatencyMs: 200, CheckedAt: 100},
		{ChannelId: 1, ModelName: "m", Success: true, TtftMs: 300, LatencyMs: 400, CheckedAt: 300},
		// Newer but failed: must be ignored.
		{ChannelId: 1, ModelName: "m", Success: false, TtftMs: 999, LatencyMs: 999, CheckedAt: 400},
		// Newer but admin-triggered diagnostic: must be ignored.
		{ChannelId: 1, ModelName: "m", Success: true, TtftMs: 888, LatencyMs: 888, CheckedAt: 500, TriggerType: ChannelMonitorTriggerManual},
	}).Error)

	ttft, latency := latestProbeSpeed(1, "m")
	assert.Equal(t, int64(300), ttft, "latest successful scheduled ttft wins")
	assert.Equal(t, int64(400), latency, "latest successful scheduled latency wins")

	ttft, latency = latestProbeSpeed(1, "no-such-model")
	assert.Equal(t, int64(0), ttft, "no probe -> 0")
	assert.Equal(t, int64(0), latency, "no probe -> 0")
}

// TestBuildRecommendationList exercises the full assembly: only positive-weight
// channels count, models dedup to their highest-weight channel (inheriting its
// blurb), and the list is weight-desc capped at recommendationMaxItems. The
// advertised speed comes from the winning channel's latest probe; the channel is
// never surfaced, only model/speed/blurb.
func TestBuildRecommendationList(t *testing.T) {
	db := newRecommendationTestDB(t)

	// Two channels serve model "shared"; channel 2 has the higher weight, so its
	// blurb must win the dedup. Channel 1 also uniquely serves "solo". Channel 3
	// has weight 0 (unmaintained) and must be excluded entirely.
	require.NoError(t, db.Create(&[]Ability{
		{Group: "g", Model: "shared", ChannelId: 1, Enabled: true},
		{Group: "g", Model: "solo", ChannelId: 1, Enabled: true},
		{Group: "g", Model: "shared", ChannelId: 2, Enabled: true},
		{Group: "g", Model: "hidden", ChannelId: 3, Enabled: true},
		// A disabled ability must never be recommended even on a weighted channel.
		{Group: "g", Model: "disabled-model", ChannelId: 2, Enabled: false},
	}).Error)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1, Name: "chan-1"},
		{Id: 2, Name: "chan-2"},
		{Id: 3, Name: "chan-3"},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelRecommendation{
		{ChannelId: 1, Weight: 5, Blurb: "blurb-1"},
		{ChannelId: 2, Weight: 10, Blurb: "blurb-2"},
		{ChannelId: 3, Weight: 0, Blurb: "should-not-appear"},
	}).Error)

	// Only monitored pairs qualify: channel 1 monitors "shared"+"solo", channel 2
	// monitors "shared". Channel 3 has no monitor config, reinforcing its exclusion.
	cfg1 := ChannelMonitorConfig{ChannelId: 1, Enabled: true}
	require.NoError(t, cfg1.SetMonitoredModels([]string{"shared", "solo"}))
	cfg2 := ChannelMonitorConfig{ChannelId: 2, Enabled: true}
	require.NoError(t, cfg2.SetMonitoredModels([]string{"shared"}))
	require.NoError(t, db.Create(&[]ChannelMonitorConfig{cfg1, cfg2}).Error)

	// Channel 2's latest probe for "shared" is the one whose speed must surface.
	require.NoError(t, db.Create(&[]ChannelMonitorResult{
		{ChannelId: 2, ModelName: "shared", Success: true, TtftMs: 100, LatencyMs: 200, CheckedAt: 100},
		{ChannelId: 2, ModelName: "shared", Success: true, TtftMs: 200, LatencyMs: 300, CheckedAt: 200},
	}).Error)

	list, err := BuildRecommendationList()
	require.NoError(t, err)

	byModel := make(map[string]RecommendedModel, len(list))
	for _, item := range list {
		byModel[item.Model] = item
	}

	// weight-0 channel excluded, disabled ability excluded.
	_, hasHidden := byModel["hidden"]
	assert.False(t, hasHidden, "weight-0 channel must not be recommended")
	_, hasDisabled := byModel["disabled-model"]
	assert.False(t, hasDisabled, "disabled ability must not be recommended")

	// "shared" dedups to channel 2 (higher weight) and inherits its blurb; the
	// advertised speed is its latest probe (checked_at 200 -> 200/300ms).
	shared, ok := byModel["shared"]
	require.True(t, ok, "shared model must be recommended")
	assert.Equal(t, "blurb-2", shared.Blurb)
	assert.Equal(t, int64(200), shared.TtftMs)
	assert.Equal(t, int64(300), shared.LatencyMs)

	// "solo" comes only from channel 1 with its own blurb; no probe -> 0/0 speed.
	solo, ok := byModel["solo"]
	require.True(t, ok, "solo model must be recommended")
	assert.Equal(t, "blurb-1", solo.Blurb)
	assert.Equal(t, int64(0), solo.TtftMs)
	assert.Equal(t, int64(0), solo.LatencyMs)

	// Ordering: higher-weight model first (shared@10 before solo@5).
	require.Len(t, list, 2)
	assert.Equal(t, "shared", list[0].Model)
	assert.Equal(t, "solo", list[1].Model)
}

// TestBuildRecommendationListEmptyWithoutWeights confirms that with no positive
// weight maintained anywhere, the list is empty (notifications get no section)
// rather than dumping every enabled model.
func TestBuildRecommendationListEmptyWithoutWeights(t *testing.T) {
	db := newRecommendationTestDB(t)
	require.NoError(t, db.Create(&Ability{Group: "g", Model: "m", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, db.Create(&Channel{Id: 1, Name: "chan-1"}).Error)

	list, err := BuildRecommendationList()
	require.NoError(t, err)
	assert.Empty(t, list)
}

// TestUpsertChannelRecommendationsDropsDefaults verifies the persistence
// contract: a row reset to the default (weight 0, empty blurb) is deleted so the
// table only ever holds maintained rows, keeping "auto-sync new channels" true
// without a sync job.
func TestUpsertChannelRecommendationsDropsDefaults(t *testing.T) {
	db := newRecommendationTestDB(t)

	require.NoError(t, UpsertChannelRecommendations([]ChannelRecommendationRow{
		{ChannelId: 1, Weight: 7, Blurb: "keep"},
		{ChannelId: 2, Weight: 0, Blurb: ""},
	}))

	var count int64
	require.NoError(t, db.Model(&ChannelRecommendation{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "only the maintained row persists")

	// Resetting the maintained row to default removes it.
	require.NoError(t, UpsertChannelRecommendations([]ChannelRecommendationRow{
		{ChannelId: 1, Weight: 0, Blurb: "  "},
	}))
	require.NoError(t, db.Model(&ChannelRecommendation{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "row reset to default is deleted")
}
