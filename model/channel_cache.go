package model

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
// channel2advancedCustomConfig caches parsed Advanced Custom (type 58) configs so
// path-aware selection avoids re-parsing JSON per request. Refreshed on full sync.
var channel2advancedCustomConfig map[int]*dto.AdvancedCustomConfig

// managedAbilityOverlay lets the channel-managed policy engine influence the
// memory-cache selection path at (channel, model) granularity. The default
// selection path only knows channel-level Status/Priority, so without this a
// per-model ban/downgrade would be invisible to it (the DB path already honors
// ability-level enabled/priority). Keyed by managedOverlayKey(channelId, model).
// It is empty for unmanaged deployments, so their selection behavior is
// unchanged. Guarded by channelSyncLock, rebuilt on every InitChannelCache.
var managedAbilityOverlay map[string]managedOverlayEntry

// managedOverlayEntry is one policy decision for a (channel, model) pair.
// Banned removes the pair from selection even though the channel is enabled;
// PriorityManaged replaces the channel-level priority with Priority for this
// model only (speed-tiering).
type managedOverlayEntry struct {
	Banned          bool
	PriorityManaged bool
	Priority        int64
}

func managedOverlayKey(channelId int, model string) string {
	return ManagedOverlayKey(channelId, model)
}

// ManagedOverlayKey is the exported form of managedOverlayKey, so callers outside
// the model package (e.g. the monitor-list controller) can index maps returned by
// GetAllChannelManagedStates by the same (channel, model) key.
func ManagedOverlayKey(channelId int, model string) string {
	return fmt.Sprintf("%d|%s", channelId, model)
}

// managedEffectivePriority returns the model-specific priority for a channel,
// consulting an explicitly passed overlay map. Used while building the cache
// (before the global overlay is published under lock).
func managedEffectivePriority(overlay map[string]managedOverlayEntry, channelId int, model string, channelPriority int64) int64 {
	if overlay != nil {
		if entry, ok := overlay[managedOverlayKey(channelId, model)]; ok && entry.PriorityManaged {
			return entry.Priority
		}
	}
	return channelPriority
}

// effectiveChannelPriorityLocked returns the priority to use for a channel when
// serving a specific model: the policy-managed priority if the speed engine owns
// this (channel, model) pair, otherwise the channel-level priority. Caller must
// hold channelSyncLock.
func effectiveChannelPriorityLocked(channelId int, model string, channelPriority int64) int64 {
	return managedEffectivePriority(managedAbilityOverlay, channelId, model, channelPriority)
}

// isManagedBannedLocked reports whether policy has banned this (channel, model)
// pair. Caller must hold channelSyncLock.
func isManagedBannedLocked(channelId int, model string) bool {
	if managedAbilityOverlay == nil {
		return false
	}
	entry, ok := managedAbilityOverlay[managedOverlayKey(channelId, model)]
	return ok && entry.Banned
}

var channelSyncLock sync.RWMutex

func InitChannelCache() {
	if !common.MemoryCacheEnabled {
		InvalidatePricingCache()
		return
	}
	newChannelId2channel := make(map[int]*Channel)
	newChannel2advancedCustomConfig := make(map[int]*dto.AdvancedCustomConfig)
	var channels []*Channel
	DB.Find(&channels)
	for _, channel := range channels {
		newChannelId2channel[channel.Id] = channel
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
				newChannel2advancedCustomConfig[channel.Id] = config
			}
		}
	}
	var abilities []*Ability
	DB.Find(&abilities)
	groups := make(map[string]bool)
	for _, ability := range abilities {
		groups[ability.Group] = true
	}

	// Load the policy overlay so per-model bans/downgrades are visible to the
	// memory-cache selection path. Built from ChannelManagedState; empty (nil map
	// entries) for unmanaged deployments, leaving selection behavior unchanged.
	newOverlay := loadManagedAbilityOverlay()

	newGroup2model2channels := make(map[string]map[string][]int)
	for group := range groups {
		newGroup2model2channels[group] = make(map[string][]int)
	}
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue // skip disabled channels
		}
		groups := strings.Split(channel.Group, ",")
		for _, group := range groups {
			models := strings.Split(channel.Models, ",")
			for _, model := range models {
				// Policy-banned (channel, model) pairs are dropped from selection
				// even though the channel itself is enabled.
				if entry, ok := newOverlay[managedOverlayKey(channel.Id, model)]; ok && entry.Banned {
					continue
				}
				if _, ok := newGroup2model2channels[group][model]; !ok {
					newGroup2model2channels[group][model] = make([]int, 0)
				}
				newGroup2model2channels[group][model] = append(newGroup2model2channels[group][model], channel.Id)
			}
		}
	}

	// Sort by effective priority for the specific model: speed-tiering may assign
	// a per-model priority that differs from the channel-level one, so the pre-sort
	// must consult the overlay rather than GetPriority() alone.
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			sort.Slice(channels, func(i, j int) bool {
				pi := managedEffectivePriority(newOverlay, channels[i], model, newChannelId2channel[channels[i]].GetPriority())
				pj := managedEffectivePriority(newOverlay, channels[j], model, newChannelId2channel[channels[j]].GetPriority())
				return pi > pj
			})
			newGroup2model2channels[group][model] = channels
		}
	}

	channelSyncLock.Lock()
	group2model2channels = newGroup2model2channels
	managedAbilityOverlay = newOverlay
	//channelsIDM = newChannelId2channel
	for i, channel := range newChannelId2channel {
		if channel.ChannelInfo.IsMultiKey {
			channel.Keys = channel.GetKeys()
			if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
				if oldChannel, ok := channelsIDM[i]; ok {
					// 存在旧的渠道，如果是多key且轮询，保留轮询索引信息
					if oldChannel.ChannelInfo.IsMultiKey && oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
						channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
					}
				}
			}
		}
	}
	channelsIDM = newChannelId2channel
	channel2advancedCustomConfig = newChannel2advancedCustomConfig
	channelSyncLock.Unlock()
	// Lock ordering: InvalidatePricingCache acquires updatePricingLock, and
	// GetPricing (holding updatePricingLock) nests channelSyncLock.RLock via
	// loadPricingAdvancedCustomConfigs. channelSyncLock MUST be released before
	// invalidating the pricing cache, otherwise the reversed order deadlocks.
	InvalidatePricingCache()
	common.SysLog("channels synced from database")
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

// GetRandomSatisfiedChannel picks a channel for (group, model), honoring priority
// tiers and weights. excludeChannel holds channels that already failed this
// request; they are dropped from the candidate pool before tiers are computed, so
// a retry never lands on an upstream that just failed and the highest remaining
// tier is always tried before descending. Returns (nil, nil) once every candidate
// is excluded, which the caller surfaces as "no available channel".
func GetRandomSatisfiedChannel(group string, model string, requestPath string, excludeChannel map[int]struct{}) (*Channel, error) {
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		return GetChannel(group, model, requestPath, excludeChannel)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	// First, try to find channels with the exact model name.
	channels := filterChannelsByRequestPathAndModel(group2model2channels[group][model], requestPath, model)

	// If no channels found, try to find channels with the normalized model name.
	if len(channels) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		channels = filterChannelsByRequestPathAndModel(group2model2channels[group][normalizedModel], requestPath, model)
	}

	if len(channels) == 0 {
		return nil, nil
	}

	// Drop pairs the policy engine has banned for this model, plus any channel that
	// already failed this request. Empty overlay (the unmanaged case) and an empty
	// exclude set leave the list untouched.
	if managedAbilityOverlay != nil || len(excludeChannel) > 0 {
		filtered := make([]int, 0, len(channels))
		for _, channelId := range channels {
			if _, excluded := excludeChannel[channelId]; excluded {
				continue
			}
			if managedAbilityOverlay != nil && isManagedBannedLocked(channelId, model) {
				continue
			}
			filtered = append(filtered, channelId)
		}
		channels = filtered
		if len(channels) == 0 {
			return nil, nil
		}
	}

	if len(channels) == 1 {
		if channel, ok := channelsIDM[channels[0]]; ok {
			return channel, nil
		}
		return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channels[0])
	}

	// The candidate pool already excludes channels that failed this request, so the
	// tier to use is simply the highest priority still represented. Indexing tiers by
	// retry count would skip untried same-tier channels: with A1,A2 at priority 20 and
	// B at 10, retry=1 jumped straight to B and A2 was never tried.
	var targetPriority int64
	for i, channelId := range channels {
		channel, ok := channelsIDM[channelId]
		if !ok {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
		priority := effectiveChannelPriorityLocked(channelId, model, channel.GetPriority())
		if i == 0 || priority > targetPriority {
			targetPriority = priority
		}
	}

	var sumWeight = 0
	var targetChannels []*Channel
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			if effectiveChannelPriorityLocked(channelId, model, channel.GetPriority()) == targetPriority {
				sumWeight += channel.GetWeight()
				targetChannels = append(targetChannels, channel)
			}
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}

	if len(targetChannels) == 0 {
		return nil, errors.New(fmt.Sprintf("no channel found, group: %s, model: %s, priority: %d", group, model, targetPriority))
	}

	// smoothing factor and adjustment
	smoothingFactor := 1
	smoothingAdjustment := 0

	if sumWeight == 0 {
		// when all channels have weight 0, set sumWeight to the number of channels and set smoothing adjustment to 100
		// each channel's effective weight = 100
		sumWeight = len(targetChannels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(targetChannels) < 10 {
		// when the average weight is less than 10, set smoothing factor to 100
		smoothingFactor = 100
	}

	// Calculate the total weight of all channels up to endIdx
	totalWeight := sumWeight * smoothingFactor

	// Generate a random value in the range [0, totalWeight)
	randomWeight := rand.Intn(totalWeight)

	// Find a channel based on its weight
	for _, channel := range targetChannels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return channel, nil
		}
	}
	// return null if no channel is not found
	return nil, errors.New("channel not found")
}

// filterChannelsByRequestPathAndModel restricts candidates by request path and
// model. Only Advanced Custom (type 58) channels are path-checked: they are kept
// only when one of their configured routes matches requestPath and model. All
// other channel types always pass. When requestPath is empty, filtering is skipped.
// Caller must hold channelSyncLock (read lock). The cached slice is never mutated.
func filterChannelsByRequestPathAndModel(channels []int, requestPath string, model string) []int {
	if requestPath == "" || len(channels) == 0 {
		return channels
	}
	filtered := make([]int, 0, len(channels))
	for _, channelId := range channels {
		channel, ok := channelsIDM[channelId]
		if !ok {
			// keep it so the downstream consistency error is raised as before
			filtered = append(filtered, channelId)
			continue
		}
		if channel.Type != constant.ChannelTypeAdvancedCustom {
			filtered = append(filtered, channelId)
			continue
		}
		if config := channel2advancedCustomConfig[channelId]; config != nil && config.SupportsPathForModel(requestPath, model) {
			filtered = append(filtered, channelId)
		}
	}
	return filtered
}

func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return c, nil
}

func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		return &channel.ChannelInfo, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return &c.ChannelInfo, nil
}

func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok {
		channel.Status = status
	}
	if status != common.ChannelStatusEnabled {
		// delete the channel from group2model2channels
		for group, model2channels := range group2model2channels {
			for model, channels := range model2channels {
				for i, channelId := range channels {
					if channelId == id {
						// remove the channel from the slice
						group2model2channels[group][model] = append(channels[:i], channels[i+1:]...)
						break
					}
				}
			}
		}
	}
}

func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	if channel == nil {
		channelSyncLock.Unlock()
		return
	}

	if channelsIDM == nil {
		channelsIDM = make(map[int]*Channel)
	}
	if oldChannel, ok := channelsIDM[channel.Id]; ok {
		logger.LogDebug(nil, "CacheUpdateChannel before: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, oldChannel.ChannelInfo.MultiKeyPollingIndex)
	}
	channelsIDM[channel.Id] = channel
	if channel2advancedCustomConfig == nil {
		channel2advancedCustomConfig = make(map[int]*dto.AdvancedCustomConfig)
	}
	delete(channel2advancedCustomConfig, channel.Id)
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
			channel2advancedCustomConfig[channel.Id] = config
		}
	}
	logger.LogDebug(nil, "CacheUpdateChannel after: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, channel.ChannelInfo.MultiKeyPollingIndex)
	// Lock ordering: do NOT hold channelSyncLock while calling
	// InvalidatePricingCache. GetPricing acquires updatePricingLock first and then
	// channelSyncLock.RLock (via loadPricingAdvancedCustomConfigs); acquiring
	// updatePricingLock while holding channelSyncLock would be an AB-BA deadlock.
	channelSyncLock.Unlock()
	InvalidatePricingCache()
}
