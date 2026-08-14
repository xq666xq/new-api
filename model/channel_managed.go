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
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// loadManagedAbilityOverlay builds the (channel, model) -> decision map the memory
// cache consults during selection. Only rows where the policy currently owns a
// decision are included: banned pairs, or pairs whose priority the speed engine
// manages. Returns an empty (non-nil) map when there are no managed states so the
// selection path can treat "no overlay" and "empty overlay" identically.
func loadManagedAbilityOverlay() map[string]managedOverlayEntry {
	overlay := make(map[string]managedOverlayEntry)
	var states []ChannelManagedState
	if err := DB.Find(&states).Error; err != nil {
		common.SysError("failed to load channel managed states for overlay: " + err.Error())
		return overlay
	}
	for _, state := range states {
		banned := state.BanState == ManagedBanStateBanned
		if !banned && !state.PriorityManaged {
			continue
		}
		overlay[managedOverlayKey(state.ChannelId, state.ModelName)] = managedOverlayEntry{
			Banned:          banned,
			PriorityManaged: state.PriorityManaged,
			Priority:        state.ManagedPriority,
		}
	}
	return overlay
}

// GetChannelManagedState returns the managed state for one (channel, model) pair,
// or nil when none exists yet.
func GetChannelManagedState(channelId int, modelName string) (*ChannelManagedState, error) {
	var state ChannelManagedState
	err := DB.Where("channel_id = ? AND model_name = ?", channelId, modelName).First(&state).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &state, nil
}

// GetChannelManagedStatesByChannel returns all managed states for a channel, keyed
// by model name, so callers building per-model views avoid an N+1 query.
func GetChannelManagedStatesByChannel(channelId int) (map[string]*ChannelManagedState, error) {
	var states []ChannelManagedState
	if err := DB.Where("channel_id = ?", channelId).Find(&states).Error; err != nil {
		return nil, err
	}
	result := make(map[string]*ChannelManagedState, len(states))
	for i := range states {
		result[states[i].ModelName] = &states[i]
	}
	return result, nil
}

// GetAllChannelManagedStates returns every managed state, keyed by
// managedOverlayKey(channelId, model), for bulk views (e.g. the monitor list).
func GetAllChannelManagedStates() (map[string]*ChannelManagedState, error) {
	var states []ChannelManagedState
	if err := DB.Find(&states).Error; err != nil {
		return nil, err
	}
	result := make(map[string]*ChannelManagedState, len(states))
	for i := range states {
		result[managedOverlayKey(states[i].ChannelId, states[i].ModelName)] = &states[i]
	}
	return result, nil
}

// upsertChannelManagedState writes a managed state using the supplied database
// handle. Keeping the handle injectable lets ability changes and state changes
// share one transaction when a policy decision is applied.
func upsertChannelManagedState(tx *gorm.DB, state *ChannelManagedState) error {
	if tx == nil {
		return errors.New("managed state database is nil")
	}
	state.UpdatedTime = common.GetTimestamp()
	var existing ChannelManagedState
	err := tx.Where("channel_id = ? AND model_name = ?", state.ChannelId, state.ModelName).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(state).Error
		}
		return err
	}
	state.Id = existing.Id
	return tx.Model(&ChannelManagedState{}).Where("id = ?", existing.Id).Updates(map[string]interface{}{
		"ban_state":             state.BanState,
		"confirm_count":         state.ConfirmCount,
		"last_ban_at":           state.LastBanAt,
		"last_recover_at":       state.LastRecoverAt,
		"last_confirm_probe_at": state.LastConfirmProbeAt,
		"priority_managed":      state.PriorityManaged,
		"original_priority":     state.OriginalPriority,
		"managed_priority":      state.ManagedPriority,
		"updated_time":          state.UpdatedTime,
	}).Error
}

// UpsertChannelManagedState writes a managed state, creating it on first use and
// updating all mutable fields otherwise.
func UpsertChannelManagedState(state *ChannelManagedState) error {
	return upsertChannelManagedState(DB, state)
}

// ApplyChannelManagedAbilityState atomically applies a policy decision to the
// ability rows and persists the corresponding managed state. A failed state
// write rolls back the ability update, preventing the database from exposing a
// decision that the policy state cannot replay later.
func ApplyChannelManagedAbilityState(state *ChannelManagedState, enabled *bool, priority *int64) error {
	if state == nil {
		return errors.New("managed state is nil")
	}
	updated := *state
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if enabled != nil {
			if err := tx.Model(&Ability{}).
				Where("channel_id = ? AND model = ?", updated.ChannelId, updated.ModelName).
				Select("enabled").Update("enabled", *enabled).Error; err != nil {
				return err
			}
		}
		if priority != nil {
			if err := tx.Model(&Ability{}).
				Where("channel_id = ? AND model = ?", updated.ChannelId, updated.ModelName).
				Update("priority", *priority).Error; err != nil {
				return err
			}
		}
		return upsertChannelManagedState(tx, &updated)
	}); err != nil {
		return err
	}
	*state = updated
	return nil
}

// ApplyManualChannelManagedOverrides makes an administrator's channel-level
// status/priority change immediately authoritative for every model on the
// affected channels. Existing managed decisions are reset to the same values so
// the memory-cache overlay cannot keep serving an older per-model ban or speed
// tier. Management remains enabled: a later probe sweep may ban, recover, or
// reprioritize the models again.
func ApplyManualChannelManagedOverrides(channelIds []int, enabled *bool, priority *int64) error {
	if len(channelIds) == 0 || (enabled == nil && priority == nil) {
		return nil
	}

	uniqueIds := make([]int, 0, len(channelIds))
	seen := make(map[int]struct{}, len(channelIds))
	for _, channelId := range channelIds {
		if channelId <= 0 {
			continue
		}
		if _, ok := seen[channelId]; ok {
			continue
		}
		seen[channelId] = struct{}{}
		uniqueIds = append(uniqueIds, channelId)
	}
	if len(uniqueIds) == 0 {
		return nil
	}

	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		abilityUpdates := make(map[string]interface{}, 2)
		managedUpdates := map[string]interface{}{
			"updated_time": now,
		}
		if enabled != nil {
			abilityUpdates["enabled"] = *enabled
			managedUpdates["confirm_count"] = 0
			// Treat the manual action as the new evidence baseline so a stale
			// probe from before the click cannot immediately undo it. Fresh
			// scheduled probes may take ownership again normally.
			managedUpdates["last_confirm_probe_at"] = now
			if *enabled {
				managedUpdates["ban_state"] = ManagedBanStateActive
				managedUpdates["last_recover_at"] = now
			} else {
				managedUpdates["ban_state"] = ManagedBanStateBanned
				managedUpdates["last_ban_at"] = now
			}
		}
		if priority != nil {
			abilityUpdates["priority"] = *priority
			// Release the old speed-tier decision. Until the next policy sweep,
			// selection falls back to the channel's newly selected priority.
			managedUpdates["priority_managed"] = false
			managedUpdates["original_priority"] = *priority
			managedUpdates["managed_priority"] = *priority
		}

		if err := tx.Model(&Ability{}).
			Where("channel_id IN ?", uniqueIds).
			Updates(abilityUpdates).Error; err != nil {
			return err
		}
		return tx.Model(&ChannelManagedState{}).
			Where("channel_id IN ?", uniqueIds).
			Updates(managedUpdates).Error
	})
}

// DeleteChannelManagedStatesByChannel is an explicit maintenance operation.
// Turning hosting off intentionally does not call it: the last policy decision
// remains applied until it is explicitly cleared or replaced by a later policy
// decision.
func DeleteChannelManagedStatesByChannel(channelId int) error {
	return DB.Where("channel_id = ?", channelId).Delete(&ChannelManagedState{}).Error
}

// GetManagedChannelMonitorConfigs returns configs whose channel opted into
// policy management (Managed = true). Hosting remains authoritative when
// periodic monitoring is paused: scheduled probes stop, but error-triggered
// probes and their policy decisions still apply.
func GetManagedChannelMonitorConfigs() ([]*ChannelMonitorConfig, error) {
	var configs []*ChannelMonitorConfig
	if err := DB.Where("managed = ?", true).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// IsChannelManaged reports whether the channel has opted into hosting. It is
// intentionally independent of the monitoring Enabled switch: once hosting is
// enabled, legacy channel-level automatic banning must stay out of the way even
// if an administrator temporarily pauses probes.
func IsChannelManaged(channelId int) (bool, error) {
	var count int64
	err := DB.Model(&ChannelMonitorConfig{}).
		Where("channel_id = ? AND managed = ?", channelId, true).
		Limit(1).
		Count(&count).Error
	return count > 0, err
}

// GetLatestChannelMonitorResult returns the most recent probe result for a
// (channel, model) pair, or nil when the pair has never been probed.
func GetLatestChannelMonitorResult(channelId int, modelName string) (*ChannelMonitorResult, error) {
	var result ChannelMonitorResult
	err := DB.Where("channel_id = ? AND model_name = ?", channelId, modelName).
		Where("(trigger_type = ? OR trigger_type = '' OR trigger_type IS NULL)", ChannelMonitorTriggerScheduled).
		Order("checked_at DESC").First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &result, nil
}

// GetRecentTtftMean returns the mean time-to-first-token (ms) over the most
// recent `window` successful probes that recorded a positive TtftMs, plus the
// count of samples used. Only successful probes with a real first-token time
// count: failures and non-stream probes (TtftMs = 0) are excluded so a slow or
// failed probe never distorts the speed ranking. Count is 0 when there is no
// usable sample, signalling the caller to leave the pair's priority untouched.
func GetRecentTtftMean(channelId int, modelName string, window int) (float64, int, error) {
	if window < 1 {
		window = 1
	}
	var results []ChannelMonitorResult
	err := DB.Select("ttft_ms").
		Where("channel_id = ? AND model_name = ? AND success = ? AND ttft_ms > 0", channelId, modelName, true).
		Where("(trigger_type = ? OR trigger_type = '' OR trigger_type IS NULL)", ChannelMonitorTriggerScheduled).
		Order("checked_at DESC").Limit(window).Find(&results).Error
	if err != nil {
		return 0, 0, err
	}
	if len(results) == 0 {
		return 0, 0, nil
	}
	var sum int64
	for _, r := range results {
		sum += r.TtftMs
	}
	return float64(sum) / float64(len(results)), len(results), nil
}

// GetRecentLatencyMean returns the mean total latency (ms) over the most recent
// `window` successful probes with a positive LatencyMs, plus the sample count.
// It mirrors GetRecentTtftMean but ranks on full-response latency instead of
// time-to-first-token; only successful probes count so a failure never skews the
// mean. Count is 0 when there is no usable sample, letting the caller show
// "no data" rather than a misleading 0ms.
func GetRecentLatencyMean(channelId int, modelName string, window int) (float64, int, error) {
	if window < 1 {
		window = 1
	}
	var results []ChannelMonitorResult
	err := DB.Select("latency_ms").
		Where("channel_id = ? AND model_name = ? AND success = ? AND latency_ms > 0", channelId, modelName, true).
		Where("(trigger_type = ? OR trigger_type = '' OR trigger_type IS NULL)", ChannelMonitorTriggerScheduled).
		Order("checked_at DESC").Limit(window).Find(&results).Error
	if err != nil {
		return 0, 0, err
	}
	if len(results) == 0 {
		return 0, 0, nil
	}
	var sum int64
	for _, r := range results {
		sum += r.LatencyMs
	}
	return float64(sum) / float64(len(results)), len(results), nil
}

// ReplayManagedAbilities re-applies persisted managed decisions onto a channel's
// freshly-rebuilt ability rows within the given transaction. UpdateAbilities
// deletes and recreates a channel's abilities from the channel-level Status and
// Priority single values, which would silently discard per-model bans and
// speed-tier priorities the policy engine set. Calling this at the end of the
// rebuild restores them so a channel edit never resurrects a banned model or
// resets a downgraded one. It is a no-op for channels with no managed state.
func ReplayManagedAbilities(tx *gorm.DB, channelId int) error {
	if tx == nil {
		tx = DB
	}
	var states []ChannelManagedState
	if err := tx.Where("channel_id = ?", channelId).Find(&states).Error; err != nil {
		return err
	}
	for _, state := range states {
		updates := make(map[string]interface{})
		// A banned pair must stay disabled across the rebuild.
		if state.BanState == ManagedBanStateBanned {
			updates["enabled"] = false
		}
		// A speed-managed pair keeps the tier priority the engine assigned.
		if state.PriorityManaged {
			updates["priority"] = state.ManagedPriority
		}
		if len(updates) == 0 {
			continue
		}
		if err := tx.Model(&Ability{}).
			Where("channel_id = ? AND model = ?", channelId, state.ModelName).
			Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}
