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

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSelectionChannels installs a channel set into both the memory cache and the
// abilities table so a single case can assert identical behavior on the
// memory-cache and DB selection paths.
func seedSelectionChannels(t *testing.T, modelName string, channels []*Channel) {
	t.Helper()

	previousGroups, previousIDM, previousOverlay := group2model2channels, channelsIDM, managedAbilityOverlay
	previousConfigs := channel2advancedCustomConfig
	t.Cleanup(func() {
		group2model2channels, channelsIDM, managedAbilityOverlay = previousGroups, previousIDM, previousOverlay
		channel2advancedCustomConfig = previousConfigs
	})

	ids := make([]int, 0, len(channels))
	idm := make(map[int]*Channel, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.Id)
		idm[channel.Id] = channel
		// The DB path resolves the chosen ability back to its channel row.
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    uint(channel.GetWeight()),
		}).Error)
	}

	group2model2channels = map[string]map[string][]int{"default": {modelName: ids}}
	channelsIDM = idm
	managedAbilityOverlay = nil
	channel2advancedCustomConfig = nil
}

func selectionChannel(id int, priority int64) *Channel {
	weight := uint(0)
	return &Channel{Id: id, Priority: &priority, Weight: &weight, Status: common.ChannelStatusEnabled}
}

// TestChannelSelectionSkipsExcludedChannels pins the retry contract: a channel that
// already failed the request is never selected again, the highest priority tier
// among the *remaining* channels wins (so a same-tier sibling is tried before
// descending), and exhausting every candidate reports "no channel" instead of
// falling back to a failed one. Both selection paths must agree.
func TestChannelSelectionSkipsExcludedChannels(t *testing.T) {
	const modelName = "gpt-test"

	// A1/A2 share the top tier; B sits below it. Before the fix, retry indexed the
	// tier list, so retry=1 jumped straight to B and A2 was never attempted.
	newChannels := func() []*Channel {
		return []*Channel{
			selectionChannel(1, 20),
			selectionChannel(2, 20),
			selectionChannel(3, 10),
		}
	}

	cases := []struct {
		name        string
		memoryCache bool
	}{
		{name: "memory cache", memoryCache: true},
		{name: "database", memoryCache: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newManagedTestDB(t)
			previousMemoryCache := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = tc.memoryCache
			t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })
			seedSelectionChannels(t, modelName, newChannels())

			// No exclusions: must land in the top tier.
			channel, err := GetRandomSatisfiedChannel("default", modelName, "", nil)
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Contains(t, []int{1, 2}, channel.Id, "first attempt must use the top priority tier")

			// Channel 1 failed: the top tier still has channel 2, so it must be next.
			channel, err = GetRandomSatisfiedChannel("default", modelName, "", map[int]struct{}{1: {}})
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, 2, channel.Id, "same-tier sibling must be tried before descending a tier")

			// Top tier exhausted: descend to the lower tier rather than reusing a failure.
			channel, err = GetRandomSatisfiedChannel("default", modelName, "", map[int]struct{}{1: {}, 2: {}})
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, 3, channel.Id, "must descend to the lower tier once the top tier is exhausted")

			// Everything failed: report no channel instead of retrying a failed one.
			channel, err = GetRandomSatisfiedChannel("default", modelName, "", map[int]struct{}{1: {}, 2: {}, 3: {}})
			require.NoError(t, err)
			assert.Nil(t, channel, "all candidates excluded must yield no channel, not a failed one")
		})
	}
}

// TestChannelSelectionSingleChannelExcluded covers the single-channel deployment,
// which previously re-selected the same failing channel on every retry because the
// one-candidate short circuit returned before any exclusion check.
func TestChannelSelectionSingleChannelExcluded(t *testing.T) {
	const modelName = "gpt-solo"

	cases := []struct {
		name        string
		memoryCache bool
	}{
		{name: "memory cache", memoryCache: true},
		{name: "database", memoryCache: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newManagedTestDB(t)
			previousMemoryCache := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = tc.memoryCache
			t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })
			seedSelectionChannels(t, modelName, []*Channel{selectionChannel(7, 0)})

			channel, err := GetRandomSatisfiedChannel("default", modelName, "", map[int]struct{}{7: {}})
			require.NoError(t, err)
			assert.Nil(t, channel, "the only channel already failed; it must not be selected again")
		})
	}
}
