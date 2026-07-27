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
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildCurfewDingTalkCardStart verifies the curfew-start (晚安) card carries
// the good-night title and body and, deliberately, no recommendation section —
// the list is only useful when monitoring resumes.
func TestBuildCurfewDingTalkCardStart(t *testing.T) {
	title, markdown := buildCurfewDingTalkCard(model.CurfewPhaseActive)
	assert.Contains(t, title, "晚安")
	assert.Contains(t, markdown, "夜间宵禁模式已开启")
	assert.Contains(t, markdown, "监控已暂停")
	assert.NotContains(t, markdown, "推荐使用模型", "start card must not carry the recommendation list")
}

// TestBuildCurfewDingTalkCardEndEmptyRecommendation verifies the curfew-end (早安)
// card renders the good-morning body plus the recommendation title, and — with no
// positive-weight recommendation configured — shows the friendly empty-state
// placeholder instead of dropping the section entirely.
func TestBuildCurfewDingTalkCardEndEmptyRecommendation(t *testing.T) {
	// buildRecommendationList reads channel abilities/recommendations; an isolated
	// DB with the recommendation table present but no positive-weight rows yields an
	// empty list, exercising the placeholder branch (a DB error would instead omit
	// the section, so the table must exist for this to test the intended path).
	db := newBanStageTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelRecommendation{}))

	title, markdown := buildCurfewDingTalkCard(model.CurfewPhaseInactive)
	assert.Contains(t, title, "早安")
	assert.Contains(t, markdown, "模型监控已开启")
	assert.Contains(t, markdown, "推荐使用模型", "end card must always show the recommendation title")
	assert.Contains(t, markdown, "暂无可用模型，请等待恢复喵！", "empty list must show the placeholder")
}

// TestChannelCurfewPhasePersistenceRoundTrip verifies the phase read/write helpers:
// an unset phase reads as empty (never-seeded), and a written phase reads back
// exactly, which is the contract the boundary notifier relies on to fire once.
func TestChannelCurfewPhasePersistenceRoundTrip(t *testing.T) {
	db := newBanStageTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))

	phase, err := model.GetChannelCurfewPhase()
	require.NoError(t, err)
	assert.Equal(t, "", phase, "unset phase reads as empty")

	require.NoError(t, model.SetChannelCurfewPhase(model.CurfewPhaseActive))
	phase, err = model.GetChannelCurfewPhase()
	require.NoError(t, err)
	assert.Equal(t, model.CurfewPhaseActive, phase)

	require.NoError(t, model.SetChannelCurfewPhase(model.CurfewPhaseInactive))
	phase, err = model.GetChannelCurfewPhase()
	require.NoError(t, err)
	assert.Equal(t, model.CurfewPhaseInactive, phase)

	// The stored value must be a plain string, not routed through the config
	// pipeline; a direct row lookup confirms it landed in the option table.
	var opt model.Option
	require.NoError(t, db.Where(&model.Option{Key: "channel_monitor_curfew_phase"}).First(&opt).Error)
	assert.Equal(t, model.CurfewPhaseInactive, opt.Value)
	assert.False(t, strings.Contains(opt.Key, "."), "internal phase key must not use the config-prefix form")
}
