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
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// ChannelRecommendation persists an operator-maintained recommendation weight and
// blurb per channel. It is intentionally minimal: only channels an operator has
// actually edited get a row; every other channel is treated as the zero default
// (weight 0, empty blurb). New channels therefore need no synchronization — the
// recommendation list left-joins channels against this table, so an unedited
// channel simply carries the default and is excluded from notifications (weight 0).
//
// Star rating is deliberately NOT stored here: it is derived live from the recent
// probe speed of each (channel, model) pair when a recommendation list is built,
// so a channel that slows down loses stars without any manual maintenance.
type ChannelRecommendation struct {
	Id          int    `json:"id"`
	ChannelId   int    `json:"channel_id" gorm:"uniqueIndex:uk_channel_recommendation_channel;not null"`
	Weight      int    `json:"weight" gorm:"default:0"`
	Blurb       string `json:"blurb" gorm:"type:varchar(255)"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

// recommendationMaxItems caps how many models a recommendation list contains so a
// DingTalk card stays compact and readable.
const recommendationMaxItems = 8

// ChannelRecommendationRow is one row of the maintenance table shown in the
// console: a channel plus its (possibly default) recommendation weight and blurb.
// It always covers every channel, so operators can set a weight on any of them.
type ChannelRecommendationRow struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	ChannelType int    `json:"channel_type"`
	Weight      int    `json:"weight"`
	Blurb       string `json:"blurb"`
}

// RecommendedModel is one entry of a built recommendation list, ready for a
// notification card. It is model-facing on purpose: the channel behind it is not
// exposed, only the model name, the recommending channel's most recent probe
// speed (time-to-first-token and total latency, in ms), and the operator blurb
// inherited from that channel. TtftMs / LatencyMs are 0 when the latest probe has
// no usable value (never probed, or a failure before first token).
type RecommendedModel struct {
	Model     string `json:"model"`
	TtftMs    int64  `json:"ttft_ms"`
	LatencyMs int64  `json:"latency_ms"`
	Blurb     string `json:"blurb"`
}

// GetChannelRecommendationRows returns one row per channel, merging the persisted
// recommendation (weight/blurb) where present and the zero default otherwise. This
// is the maintenance-table source: it always lists every channel so a new channel
// appears automatically with weight 0.
func GetChannelRecommendationRows() ([]ChannelRecommendationRow, error) {
	var channels []ChannelMonitorListItem
	if err := DB.Model(&Channel{}).
		Select("id", "type", "name").
		Order("id asc").
		Find(&channels).Error; err != nil {
		return nil, err
	}
	recs, err := getAllChannelRecommendations()
	if err != nil {
		return nil, err
	}
	byChannel := make(map[int]*ChannelRecommendation, len(recs))
	for i := range recs {
		byChannel[recs[i].ChannelId] = &recs[i]
	}
	rows := make([]ChannelRecommendationRow, 0, len(channels))
	for _, ch := range channels {
		row := ChannelRecommendationRow{
			ChannelId:   ch.Id,
			ChannelName: ch.Name,
			ChannelType: ch.Type,
		}
		if rec, ok := byChannel[ch.Id]; ok {
			row.Weight = rec.Weight
			row.Blurb = rec.Blurb
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// getAllChannelRecommendations loads every persisted recommendation row.
func getAllChannelRecommendations() ([]ChannelRecommendation, error) {
	var recs []ChannelRecommendation
	if err := DB.Find(&recs).Error; err != nil {
		return nil, err
	}
	return recs, nil
}

// UpsertChannelRecommendations persists the edited weights/blurbs. A row is only
// written when it carries a non-default value (weight != 0 or a non-empty blurb);
// a row reset to the default is deleted, keeping the table free of no-op rows so
// the "default 0, auto-sync new channels" contract holds without a sync job.
func UpsertChannelRecommendations(rows []ChannelRecommendationRow) error {
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if row.ChannelId <= 0 {
				continue
			}
			blurb := strings.TrimSpace(row.Blurb)
			if len([]rune(blurb)) > 255 {
				blurb = string([]rune(blurb)[:255])
			}
			weight := row.Weight
			if weight < 0 {
				weight = 0
			}
			if weight == 0 && blurb == "" {
				// Default value: drop any existing row so unedited channels stay absent.
				if err := tx.Where("channel_id = ?", row.ChannelId).Delete(&ChannelRecommendation{}).Error; err != nil {
					return err
				}
				continue
			}
			var existing ChannelRecommendation
			err := tx.Where("channel_id = ?", row.ChannelId).First(&existing).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if err := tx.Create(&ChannelRecommendation{
						ChannelId:   row.ChannelId,
						Weight:      weight,
						Blurb:       blurb,
						UpdatedTime: now,
					}).Error; err != nil {
						return err
					}
					continue
				}
				return err
			}
			existing.Weight = weight
			existing.Blurb = blurb
			existing.UpdatedTime = now
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// modelRecommendationCandidate is the winning (highest-weight) channel for one
// model while a recommendation list is being assembled.
type modelRecommendationCandidate struct {
	model     string
	channelId int
	weight    int
	blurb     string
}

// BuildRecommendationList assembles the model recommendation list for a
// notification card. It: (1) takes every enabled (model, channel) ability pair,
// (2) keeps only channels an operator gave a positive recommendation weight AND
// pairs the channel actually monitors (so an unmonitored model, which has no
// probe speed, is never advertised), (3) deduplicates by model keeping the
// highest-weight channel, (4) sorts by weight descending and caps at
// recommendationMaxItems, then (5) attaches each model's most recent probe speed
// (first-token + latency) from that channel. The channel is never surfaced —
// only the model, its speed, and the blurb reach the caller.
func BuildRecommendationList() ([]RecommendedModel, error) {
	abilities, err := GetAllEnableAbilityWithChannels()
	if err != nil {
		return nil, err
	}
	recs, err := getAllChannelRecommendations()
	if err != nil {
		return nil, err
	}
	weightByChannel := make(map[int]*ChannelRecommendation, len(recs))
	for i := range recs {
		if recs[i].Weight > 0 {
			weightByChannel[recs[i].ChannelId] = &recs[i]
		}
	}
	if len(weightByChannel) == 0 {
		return []RecommendedModel{}, nil
	}
	monitored, err := getMonitoredChannelModelPairs()
	if err != nil {
		return nil, err
	}

	// Dedup by model: keep the recommended channel with the highest weight. Ties
	// break on the lower channel id for deterministic output. Only monitored pairs
	// qualify, so every recommended model has a real probe speed to advertise.
	best := make(map[string]*modelRecommendationCandidate)
	for _, ab := range abilities {
		rec, ok := weightByChannel[ab.ChannelId]
		if !ok {
			continue
		}
		modelName := strings.TrimSpace(ab.Model)
		if modelName == "" {
			continue
		}
		if models, ok := monitored[ab.ChannelId]; !ok {
			continue
		} else if _, ok := models[modelName]; !ok {
			continue
		}
		current, exists := best[modelName]
		if !exists || rec.Weight > current.weight ||
			(rec.Weight == current.weight && ab.ChannelId < current.channelId) {
			best[modelName] = &modelRecommendationCandidate{
				model:     modelName,
				channelId: ab.ChannelId,
				weight:    rec.Weight,
				blurb:     rec.Blurb,
			}
		}
	}
	if len(best) == 0 {
		return []RecommendedModel{}, nil
	}

	candidates := make([]*modelRecommendationCandidate, 0, len(best))
	for _, c := range best {
		candidates = append(candidates, c)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].weight != candidates[j].weight {
			return candidates[i].weight > candidates[j].weight
		}
		return candidates[i].model < candidates[j].model
	})
	if len(candidates) > recommendationMaxItems {
		candidates = candidates[:recommendationMaxItems]
	}

	list := make([]RecommendedModel, 0, len(candidates))
	for _, c := range candidates {
		ttft, latency := latestProbeSpeed(c.channelId, c.model)
		list = append(list, RecommendedModel{
			Model:     c.model,
			TtftMs:    ttft,
			LatencyMs: latency,
			Blurb:     c.blurb,
		})
	}
	return list, nil
}

// getMonitoredChannelModelPairs returns the set of (channel, model) pairs that a
// monitoring-enabled channel actually probes, keyed by channel id. Only these
// pairs are eligible for recommendation, since only they have a probe speed —
// recommending an unmonitored model would advertise a model with no measured
// speed. Unlike the status page, recommendations intentionally remain limited
// to actively probed model pairs.
func getMonitoredChannelModelPairs() (map[int]map[string]struct{}, error) {
	var configs []ChannelMonitorConfig
	if err := DB.Where("enabled = ?", true).Find(&configs).Error; err != nil {
		return nil, err
	}
	pairs := make(map[int]map[string]struct{}, len(configs))
	for i := range configs {
		models := configs[i].GetMonitoredModels()
		if len(models) == 0 {
			continue
		}
		set := make(map[string]struct{}, len(models))
		for _, m := range models {
			if m = strings.TrimSpace(m); m != "" {
				set[m] = struct{}{}
			}
		}
		if len(set) > 0 {
			pairs[configs[i].ChannelId] = set
		}
	}
	return pairs, nil
}

// latestProbeSpeed returns the time-to-first-token and total latency (ms) of the
// most recent successful scheduled probe for a (channel, model) pair. Either value
// is 0 when the latest probe has no usable measurement (never probed, non-stream
// probe with no first-token time, or a failure). Only successful scheduled probes
// count, mirroring the speed source used by the policy engine, so a failed or
// admin-triggered diagnostic never surfaces as a recommendation's advertised speed.
func latestProbeSpeed(channelId int, modelName string) (ttftMs int64, latencyMs int64) {
	var result ChannelMonitorResult
	err := DB.Select("ttft_ms", "latency_ms").
		Where("channel_id = ? AND model_name = ? AND success = ?", channelId, modelName, true).
		Where("(trigger_type = ? OR trigger_type = '' OR trigger_type IS NULL)", ChannelMonitorTriggerScheduled).
		Order("checked_at DESC").
		First(&result).Error
	if err != nil {
		return 0, 0
	}
	return result.TtftMs, result.LatencyMs
}
