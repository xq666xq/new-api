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
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkSamples builds speed samples in the given TTFT-mean order. clusterSpeedTiers
// requires them pre-sorted fastest-first, which mirrors how applySpeedForModel
// calls it.
func mkSamples(means ...float64) []speedSample {
	samples := make([]speedSample, 0, len(means))
	for i, m := range means {
		samples = append(samples, speedSample{
			pair:    managedModelPair{channelID: i + 1, model: "m"},
			meanMs:  m,
			hasData: true,
		})
	}
	return samples
}

// tierMeans flattens a tiering into its per-tier mean lists so assertions read
// clearly regardless of the sample struct internals.
func tierMeans(tiers [][]speedSample) [][]float64 {
	out := make([][]float64, 0, len(tiers))
	for _, tier := range tiers {
		row := make([]float64, 0, len(tier))
		for _, s := range tier {
			row = append(row, s.meanMs)
		}
		out = append(out, row)
	}
	return out
}

func TestClusterSpeedTiers(t *testing.T) {
	t.Run("all within gap collapse into one tier", func(t *testing.T) {
		// 100, 110, 125 are each within 30% of the tier anchor (100 -> threshold
		// 130), so they must share a single tier and keep weight balancing.
		tiers := clusterSpeedTiers(mkSamples(100, 110, 125), 30)
		require.Len(t, tiers, 1)
		assert.Equal(t, [][]float64{{100, 110, 125}}, tierMeans(tiers))
	})

	t.Run("clear gap splits into separate tiers", func(t *testing.T) {
		// 100 anchors tier 1 (threshold 130); 200 exceeds it -> new tier anchored
		// at 200 (threshold 260); 500 exceeds that -> third tier.
		tiers := clusterSpeedTiers(mkSamples(100, 200, 500), 30)
		require.Len(t, tiers, 3)
		assert.Equal(t, [][]float64{{100}, {200}, {500}}, tierMeans(tiers))
	})

	t.Run("anchor is the tier's fastest, not the previous sample", func(t *testing.T) {
		// With gap 30%: anchor 100 -> threshold 130. 120 joins (<=130). 140 is
		// >130 so it would start a new tier even though it is within 30% of 120.
		// This confirms the anchor stays the tier's fastest member, preventing
		// unbounded drift where each near neighbor keeps extending one tier.
		tiers := clusterSpeedTiers(mkSamples(100, 120, 140), 30)
		require.Len(t, tiers, 2)
		assert.Equal(t, [][]float64{{100, 120}, {140}}, tierMeans(tiers))
	})

	t.Run("zero gap puts every distinct-speed channel in its own tier", func(t *testing.T) {
		tiers := clusterSpeedTiers(mkSamples(100, 101, 102), 0)
		require.Len(t, tiers, 3)
	})

	t.Run("empty input yields no tiers", func(t *testing.T) {
		assert.Empty(t, clusterSpeedTiers(nil, 30))
	})

	t.Run("single sample is one tier", func(t *testing.T) {
		tiers := clusterSpeedTiers(mkSamples(42), 30)
		require.Len(t, tiers, 1)
		assert.Equal(t, [][]float64{{42}}, tierMeans(tiers))
	})
}

func TestNextChannelMonitorCheckAtUsesConfirmationCadence(t *testing.T) {
	config := &model.ChannelMonitorConfig{
		ChannelId:       7,
		Managed:         true,
		IntervalSeconds: 600,
	}
	require.NoError(t, config.SetMonitoredModels([]string{"model-a"}))
	setting := &operation_setting.ManagedPolicySetting{
		BanEnabled:                true,
		BanConfirmIntervalSeconds: 30,
	}
	finishedAt := int64(1_800_000_000)

	t.Run("first failed probe schedules a confirmation", func(t *testing.T) {
		states := map[string]*model.ChannelManagedState{
			"model-a": {
				ModelName:          "model-a",
				BanState:           model.ManagedBanStateActive,
				ConfirmCount:       1,
				LastConfirmProbeAt: finishedAt,
			},
		}
		assert.Equal(t, finishedAt+30, nextChannelMonitorCheckAt(config, finishedAt, setting, states))
	})

	t.Run("recovery confirmation uses the same cadence", func(t *testing.T) {
		states := map[string]*model.ChannelManagedState{
			"model-a": {
				ModelName:          "model-a",
				BanState:           model.ManagedBanStateBanned,
				ConfirmCount:       1,
				LastConfirmProbeAt: finishedAt,
			},
		}
		assert.Equal(t, finishedAt+30, nextChannelMonitorCheckAt(config, finishedAt, setting, states))
	})

	t.Run("fully confirmed state resumes the normal interval", func(t *testing.T) {
		states := map[string]*model.ChannelManagedState{
			"model-a": {
				ModelName:    "model-a",
				BanState:     model.ManagedBanStateBanned,
				ConfirmCount: 0,
			},
		}
		assert.Equal(t, finishedAt+600, nextChannelMonitorCheckAt(config, finishedAt, setting, states))
	})

	t.Run("only exact monitored model state changes the schedule", func(t *testing.T) {
		states := map[string]*model.ChannelManagedState{
			"model-a-*": {
				ModelName:          "model-a-*",
				ConfirmCount:       1,
				LastConfirmProbeAt: finishedAt,
			},
		}
		assert.Equal(t, finishedAt+600, nextChannelMonitorCheckAt(config, finishedAt, setting, states))
	})
}
