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
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	MonitorBodyModeDefault  = "default"
	MonitorBodyModeMerge    = "merge"
	MonitorBodyModeOverride = "override"

	ChannelMonitorModeDefault    = "default"
	ChannelMonitorModeBannedOnly = "banned_only"

	ChannelMonitorDefaultIntervalSeconds = 600
	ChannelMonitorDefaultJitterSeconds   = 60
)

// channelStatusRange describes one selectable time window on the channel status
// page: how far back it spans and how the span is sliced into sparkline buckets.
// BucketSeconds * BucketCount == the total window; bucket counts are kept in the
// 60-84 range so every window renders a similar-width sparkline.
type channelStatusRange struct {
	Seconds       int64
	BucketSeconds int64
	BucketCount   int
}

// channelStatusRanges maps the range keys the frontend sends to their bucket
// layout. Unknown keys fall back to "1h" (see resolveChannelStatusRange).
var channelStatusRanges = map[string]channelStatusRange{
	"1h":  {Seconds: 60 * 60, BucketSeconds: 60, BucketCount: 60},
	"6h":  {Seconds: 6 * 60 * 60, BucketSeconds: 5 * 60, BucketCount: 72},
	"12h": {Seconds: 12 * 60 * 60, BucketSeconds: 10 * 60, BucketCount: 72},
	"24h": {Seconds: 24 * 60 * 60, BucketSeconds: 20 * 60, BucketCount: 72},
	"7d":  {Seconds: 7 * 24 * 60 * 60, BucketSeconds: 2 * 60 * 60, BucketCount: 84},
}

// resolveChannelStatusRange returns the bucket layout for a range key, defaulting
// to "1h" for empty or unknown keys so a bad query never produces zero buckets.
func resolveChannelStatusRange(rangeKey string) channelStatusRange {
	if r, ok := channelStatusRanges[rangeKey]; ok {
		return r
	}
	return channelStatusRanges["1h"]
}

// logBucketIndexExpr returns a SQL expression that maps a log's created_at to a
// zero-based bucket index within [startAt, endAt): floor((created_at-start)/step).
// Integer division differs per dialect: MySQL/ClickHouse `/` is floating, so they
// need DIV/intDiv; SQLite and PostgreSQL do integer division for integer operands.
// The result is used both in SELECT and GROUP BY, so it must be a plain expression
// (no alias) that every dialect accepts in GROUP BY.
func logBucketIndexExpr(startAt, bucketSeconds int64) string {
	diff := fmt.Sprintf("(logs.created_at - %d)", startAt)
	switch {
	case common.UsingLogDatabase(common.DatabaseTypeMySQL):
		return fmt.Sprintf("(%s DIV %d)", diff, bucketSeconds)
	case common.UsingLogDatabase(common.DatabaseTypeClickHouse):
		return fmt.Sprintf("intDiv(%s, %d)", diff, bucketSeconds)
	default: // sqlite, postgres: integer / integer truncates toward zero
		return fmt.Sprintf("(%s / %d)", diff, bucketSeconds)
	}
}

// channelForwardBucketStat is one aggregated forwarding-log bucket for a
// channel+model pair: how many real requests fell in the bucket and how many
// succeeded (Consume = success, Error = failure).
type channelForwardBucketStat struct {
	ChannelId   int    `gorm:"column:channel_id"`
	ModelName   string `gorm:"column:model_name"`
	BucketIndex int    `gorm:"column:bucket_index"`
	Total       int    `gorm:"column:total"`
	Success     int    `gorm:"column:success"`
}

// aggregateChannelForwardStats groups real forwarding logs (LogTypeConsume /
// LogTypeError) by channel_id + model_name + time bucket over [startAt, endAt).
// Aggregation runs in the log database so only one small row per non-empty bucket
// crosses the wire, which matters because the log table can be huge and may live
// in a separate database (LOG_SQL_DSN), possibly ClickHouse. Only the given
// channel/model pairs are counted, matching the monitored set shown on the page.
func aggregateChannelForwardStats(channelIds []int, modelNames []string, startAt, endAt, bucketSeconds int64) ([]channelForwardBucketStat, error) {
	if len(channelIds) == 0 || len(modelNames) == 0 {
		return nil, nil
	}
	bucketExpr := logBucketIndexExpr(startAt, bucketSeconds)
	// COUNT(*) is total; SUM(CASE type=consume) is success. CASE/COUNT/SUM are
	// portable across sqlite/mysql/postgres/clickhouse.
	selectExpr := fmt.Sprintf(
		"logs.channel_id AS channel_id, logs.model_name AS model_name, %s AS bucket_index, "+
			"COUNT(*) AS total, "+
			"SUM(CASE WHEN logs.type = %d THEN 1 ELSE 0 END) AS success",
		bucketExpr, LogTypeConsume,
	)
	var stats []channelForwardBucketStat
	err := LOG_DB.Table("logs").
		Select(selectExpr).
		Where("logs.type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("logs.channel_id IN ?", channelIds).
		Where("logs.model_name IN ?", modelNames).
		Where("logs.created_at >= ? AND logs.created_at < ?", startAt, endAt).
		Group("logs.channel_id, logs.model_name, " + bucketExpr).
		Scan(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}

type ChannelStatusCheck struct {
	Health  string `json:"health"`
	Total   int    `json:"total"`
	Success int    `json:"success"`
	StartAt int64  `json:"start_at"`
	EndAt   int64  `json:"end_at"`
}

type ChannelStatusRow struct {
	ChannelId     int     `json:"channel_id"`
	ChannelName   string  `json:"channel_name"`
	ChannelType   int     `json:"channel_type"`
	Group         string  `json:"group"`
	Tag           string  `json:"tag"`
	Model         string  `json:"model"`
	Health        string  `json:"health"`
	SuccessRate   float64 `json:"success_rate"`
	Requests      int     `json:"requests"`
	AvgResponseMs int     `json:"avg_response_ms"`
	LastCheckedAt int64   `json:"last_checked_at"`
	// LastTtftMs / LastLatencyMs are the time-to-first-token and total latency of
	// the most recent probe (by CheckedAt) for this channel+model pair, both in
	// milliseconds and 0 when unavailable (no probe, or a non-stream probe for
	// TTFT). The card surfaces them so operators can eyeball the latest speed.
	LastTtftMs    int64 `json:"last_ttft_ms"`
	LastLatencyMs int64 `json:"last_latency_ms"`
	// ModelEnabled / ModelPriority are this channel+model pair's current routing
	// state, read from the abilities table: whether the model is enabled on this
	// channel (a disabled channel or a policy ban leaves it false) and the priority
	// used to order it during selection. They describe routing, not probe health,
	// so a healthy sparkline can still show a disabled model (e.g. banned by policy)
	// and vice versa. Only populated in the admin channel view; the aggregated
	// member view leaves them zero-valued since it hides channel identity.
	ModelEnabled  bool                 `json:"model_enabled"`
	ModelPriority int64                `json:"model_priority"`
	RecentChecks  []ChannelStatusCheck `json:"recent_checks"`
}

// channelModelAbilityStatus is one (channel, model) pair's routing state derived
// from the abilities table.
type channelModelAbilityStatus struct {
	Enabled  bool
	Priority int64
}

// getChannelModelAbilityStatuses batch-loads the routing state for the given
// channel+model pairs from the abilities table, keyed "channelId\x00model". A
// pair spans one ability row per group; they share a priority and normally share
// an enabled flag, so the pair is reported enabled when any row is enabled and
// takes the highest priority seen. Pairs with no ability row are simply absent
// from the map (caller treats them as disabled with zero priority).
func getChannelModelAbilityStatuses(channelIds []int, modelNames []string) (map[string]channelModelAbilityStatus, error) {
	statuses := make(map[string]channelModelAbilityStatus)
	if len(channelIds) == 0 || len(modelNames) == 0 {
		return statuses, nil
	}
	var abilities []Ability
	if err := DB.Model(&Ability{}).
		Select("channel_id", "model", "enabled", "priority").
		Where("channel_id IN ? AND model IN ?", channelIds, modelNames).
		Find(&abilities).Error; err != nil {
		return nil, err
	}
	for _, ability := range abilities {
		key := fmt.Sprintf("%d\x00%s", ability.ChannelId, ability.Model)
		priority := int64(0)
		if ability.Priority != nil {
			priority = *ability.Priority
		}
		existing, ok := statuses[key]
		if !ok {
			statuses[key] = channelModelAbilityStatus{Enabled: ability.Enabled, Priority: priority}
			continue
		}
		existing.Enabled = existing.Enabled || ability.Enabled
		if priority > existing.Priority {
			existing.Priority = priority
		}
		statuses[key] = existing
	}
	return statuses, nil
}

const (
	ChannelMonitorTriggerScheduled = "scheduled"
	ChannelMonitorTriggerManual    = "manual"
)

type ChannelMonitorResult struct {
	Id        int64  `json:"id" gorm:"primaryKey"`
	ChannelId int    `json:"channel_id" gorm:"index:idx_channel_monitor_result_lookup,priority:1;not null"`
	ModelName string `json:"model_name" gorm:"type:varchar(191);index:idx_channel_monitor_result_lookup,priority:2;not null"`
	// TriggerType distinguishes scheduler evidence from administrator-triggered
	// diagnostics. Both appear in status history, but only scheduled results may
	// drive managed ban/recovery and speed policy decisions.
	TriggerType     string `json:"trigger_type" gorm:"type:varchar(16)"`
	QuestionId      int    `json:"question_id"`
	QuestionContent string `json:"question_content" gorm:"type:text"`
	Success         bool   `json:"success"`
	LatencyMs       int64  `json:"latency_ms" gorm:"bigint;default:0"`
	// TtftMs is the time-to-first-token in milliseconds for a streamed probe, or 0
	// when unavailable (non-stream probe, failure before first token). The policy
	// engine's speed-based up/downgrade ranks channels by their recent TtftMs mean.
	TtftMs       int64  `json:"ttft_ms" gorm:"bigint;default:0"`
	StatusCode   int    `json:"status_code" gorm:"default:0"`
	ErrorMessage string `json:"error_message" gorm:"type:text"`
	CheckedAt    int64  `json:"checked_at" gorm:"bigint;index:idx_channel_monitor_result_lookup,priority:3;index"`
}

// Managed-state constants. BanState is the circuit-breaker's stable direction for
// one channel+model pair; the engine only acts after ConfirmCount consecutive
// probes disagree with the current stable direction (see channel_managed policy).
const (
	// ManagedBanStateActive means the model is (or should be) enabled/serving.
	ManagedBanStateActive = "active"
	// ManagedBanStateBanned means the model is (or should be) disabled by policy.
	ManagedBanStateBanned = "banned"
)

// ChannelManagedState persists the policy engine's per-(channel, model) decision
// state so it survives restarts and, crucially, survives ability-table rebuilds:
// editing a channel wipes and recreates its abilities from the channel-level
// Status/Priority single values, so the managed enabled/priority must be replayed
// from here afterwards (see ReplayManagedAbilities). One row per channel+model.
//
// BanState + ConfirmCount implement the symmetric-confirmation circuit breaker:
// ConfirmCount counts consecutive probe results that disagree with BanState; a
// single agreeing probe resets it to 0. When it reaches the configured threshold
// the engine flips BanState and applies the ability enable/disable.
//
// OriginalPriority snapshots the channel/ability priority the first time the model
// is downgraded, so upgrading can restore the pre-policy ordering. ManagedPriority
// is the priority the speed-tiering currently assigns; the replay writes it onto
// the ability rows. PriorityManaged marks whether the speed engine currently owns
// this pair's priority (false = leave the channel's own priority untouched).
type ChannelManagedState struct {
	Id        int    `json:"id"`
	ChannelId int    `json:"channel_id" gorm:"uniqueIndex:uk_channel_managed_state,priority:1;not null"`
	ModelName string `json:"model_name" gorm:"type:varchar(191);uniqueIndex:uk_channel_managed_state,priority:2;not null"`

	// Ban circuit-breaker.
	BanState      string `json:"ban_state" gorm:"type:varchar(16);default:'active'"`
	ConfirmCount  int    `json:"confirm_count" gorm:"default:0"`
	LastBanAt     int64  `json:"last_ban_at" gorm:"bigint;default:0"`
	LastRecoverAt int64  `json:"last_recover_at" gorm:"bigint;default:0"`
	// LastConfirmProbeAt is the CheckedAt of the probe that last advanced (or reset)
	// the confirmation counter. It makes counting idempotent — the engine runs every
	// scheduler tick but only reacts to a probe it has not seen — and enforces the
	// configured confirmation interval: a new probe is only counted when it is at
	// least BanConfirmIntervalSeconds newer than the last counted one.
	LastConfirmProbeAt int64 `json:"last_confirm_probe_at" gorm:"bigint;default:0"`

	// Speed tiering.
	PriorityManaged  bool  `json:"priority_managed"`
	OriginalPriority int64 `json:"original_priority" gorm:"bigint;default:0"`
	ManagedPriority  int64 `json:"managed_priority" gorm:"bigint;default:0"`

	UpdatedTime int64 `json:"updated_time" gorm:"bigint"`
}

func channelStatusHealth(total, success int) string {
	if total == 0 {
		return "none"
	}
	if success == 0 {
		return "down"
	}
	if float64(success)/float64(total) >= 0.95 {
		return "healthy"
	}
	return "degraded"
}

// GetChannelStatusRows builds the sparkline for every monitored model on
// channels that have a monitor config. The channel monitoring switch controls
// probe execution, not status-page visibility; the per-model monitoring
// selection still determines which model cards are shown. Over the given time
// range (see channelStatusRanges for valid keys; unknown keys fall back to
// "1h"), each bucket merges two sources:
// active probe results (ChannelMonitorResult) and real forwarding traffic
// (LogTypeConsume = success, LogTypeError = failure) aggregated in the log DB.
// Both feed totals/success/health; only probe results feed AvgResponseMs, since
// forwarding use_time is second-granular while probe latency is milliseconds.
// Empty buckets remain explicit no-data points so the sparkline keeps a fixed
// width.
func GetChannelStatusRows(rangeKey string, now time.Time) ([]ChannelStatusRow, error) {
	window := resolveChannelStatusRange(rangeKey)
	bucketSeconds := window.BucketSeconds
	bucketCount := window.BucketCount
	var configs []ChannelMonitorConfig
	if err := DB.Find(&configs).Error; err != nil {
		return nil, err
	}
	if len(configs) == 0 {
		return []ChannelStatusRow{}, nil
	}

	channelIds := make([]int, 0, len(configs))
	for _, config := range configs {
		channelIds = append(channelIds, config.ChannelId)
	}
	var channels []ChannelMonitorListItem
	if err := DB.Model(&Channel{}).
		Select("id", "type", "name", commonGroupCol, "tag", "models", "priority").
		Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelById := make(map[int]ChannelMonitorListItem, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}

	type pairKey struct {
		channelId int
		model     string
	}
	rows := make([]ChannelStatusRow, 0)
	rowByPair := make(map[pairKey]int)
	modelSet := make(map[string]struct{})
	endAt := (now.Unix()/bucketSeconds + 1) * bucketSeconds
	startAt := endAt - int64(bucketCount)*bucketSeconds
	for _, config := range configs {
		channel, ok := channelById[config.ChannelId]
		if !ok {
			continue
		}
		available := make(map[string]struct{})
		for _, modelName := range strings.Split(channel.Models, ",") {
			modelName = strings.TrimSpace(modelName)
			if modelName != "" {
				available[modelName] = struct{}{}
			}
		}
		seen := make(map[string]struct{})
		for _, modelName := range config.GetMonitoredModels() {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				continue
			}
			if _, ok := available[modelName]; !ok {
				continue
			}
			if _, ok := seen[modelName]; ok {
				continue
			}
			seen[modelName] = struct{}{}
			checks := make([]ChannelStatusCheck, bucketCount)
			for i := range checks {
				checks[i].Health = "none"
				checks[i].StartAt = startAt + int64(i)*bucketSeconds
				checks[i].EndAt = checks[i].StartAt + bucketSeconds
			}
			rowByPair[pairKey{config.ChannelId, modelName}] = len(rows)
			modelSet[modelName] = struct{}{}
			rows = append(rows, ChannelStatusRow{ChannelId: channel.Id, ChannelName: channel.Name, ChannelType: channel.Type, Group: channel.Group, Tag: channel.Tag, Model: modelName, Health: "none", RecentChecks: checks})
		}
	}
	if len(rows) == 0 {
		return rows, nil
	}
	modelNames := make([]string, 0, len(modelSet))
	for modelName := range modelSet {
		modelNames = append(modelNames, modelName)
	}
	var results []ChannelMonitorResult
	if err := DB.Model(&ChannelMonitorResult{}).
		Where("channel_id IN ? AND model_name IN ?", channelIds, modelNames).
		Where("checked_at >= ? AND checked_at < ?", startAt, endAt).
		Find(&results).Error; err != nil {
		return nil, err
	}
	// totalLatency/probeCount track probe results only, since AvgResponseMs is
	// computed from probe latency alone (forwarding logs have second-granularity
	// use_time, not comparable millisecond latency).
	totalLatency := make([]int64, len(rows))
	probeCount := make([]int, len(rows))
	for _, result := range results {
		rowIndex, ok := rowByPair[pairKey{result.ChannelId, result.ModelName}]
		if !ok {
			continue
		}
		bucketIndex := int((result.CheckedAt - startAt) / bucketSeconds)
		if bucketIndex < 0 || bucketIndex >= bucketCount {
			continue
		}
		row := &rows[rowIndex]
		check := &row.RecentChecks[bucketIndex]
		check.Total++
		row.Requests++
		totalLatency[rowIndex] += result.LatencyMs
		probeCount[rowIndex]++
		if result.Success {
			check.Success++
		}
		// Capture the latest probe's TTFT/latency alongside LastCheckedAt so the
		// card can show the most recent measured speed for this pair.
		if result.CheckedAt > row.LastCheckedAt {
			row.LastCheckedAt = result.CheckedAt
			row.LastTtftMs = result.TtftMs
			row.LastLatencyMs = result.LatencyMs
		}
	}
	// Merge real forwarding traffic into the same buckets. This is best-effort:
	// the log DB may be a separate database (possibly ClickHouse) and a failure
	// here must not blank the whole page, so on error we log and fall back to
	// probe-only data. Forwarding requests count toward totals/success/health but
	// NOT toward AvgResponseMs: the log's use_time is whole seconds while probe
	// latency is milliseconds, so mixing them would corrupt the latency metric.
	forwardStats, forwardErr := aggregateChannelForwardStats(channelIds, modelNames, startAt, endAt, bucketSeconds)
	if forwardErr != nil {
		common.SysError("channel status: failed to aggregate forwarding logs, showing probe data only: " + forwardErr.Error())
	} else {
		for _, stat := range forwardStats {
			rowIndex, ok := rowByPair[pairKey{stat.ChannelId, stat.ModelName}]
			if !ok {
				continue
			}
			if stat.BucketIndex < 0 || stat.BucketIndex >= bucketCount {
				continue
			}
			row := &rows[rowIndex]
			check := &row.RecentChecks[stat.BucketIndex]
			check.Total += stat.Total
			check.Success += stat.Success
			row.Requests += stat.Total
		}
	}
	// Per-model routing state (enabled + priority) from the abilities table, so the
	// admin card can show each model's current status/priority next to its health.
	// Best-effort: a lookup error just leaves the fields zero-valued rather than
	// blanking the whole page.
	abilityStatuses, abilityErr := getChannelModelAbilityStatuses(channelIds, modelNames)
	if abilityErr != nil {
		common.SysError("channel status: failed to load ability routing state: " + abilityErr.Error())
		abilityStatuses = map[string]channelModelAbilityStatus{}
	}
	for i := range rows {
		success := 0
		for j := range rows[i].RecentChecks {
			check := &rows[i].RecentChecks[j]
			check.Health = channelStatusHealth(check.Total, check.Success)
			success += check.Success
		}
		rows[i].Health = channelStatusHealth(rows[i].Requests, success)
		if rows[i].Requests > 0 {
			rows[i].SuccessRate = float64(success) * 100 / float64(rows[i].Requests)
		}
		if probeCount[i] > 0 {
			rows[i].AvgResponseMs = int(totalLatency[i] / int64(probeCount[i]))
		}
		if status, ok := abilityStatuses[fmt.Sprintf("%d\x00%s", rows[i].ChannelId, rows[i].Model)]; ok {
			rows[i].ModelEnabled = status.Enabled
			rows[i].ModelPriority = status.Priority
		}
	}
	// Order by operator recommendation weight (descending) so the channels an
	// operator promoted surface first. Weight is per channel and defaults to 0 for
	// unedited channels, so they naturally fall to the bottom. Ties break on the
	// lower channel id, then model name, keeping a stable, human-friendly order.
	recs, err := getAllChannelRecommendations()
	if err != nil {
		return nil, err
	}
	weightByChannel := make(map[int]int, len(recs))
	for _, rec := range recs {
		weightByChannel[rec.ChannelId] = rec.Weight
	}
	sort.SliceStable(rows, func(i, j int) bool {
		wi := weightByChannel[rows[i].ChannelId]
		wj := weightByChannel[rows[j].ChannelId]
		if wi != wj {
			return wi > wj
		}
		if rows[i].ChannelId != rows[j].ChannelId {
			return rows[i].ChannelId < rows[j].ChannelId
		}
		return rows[i].Model < rows[j].Model
	})
	return rows, nil
}

// GetModelStatusRows aggregates GetChannelStatusRows by model, collapsing every
// channel that serves a model into a single row so channel identity (name, group,
// tag, id, type) is never exposed. It is the member-facing view: normal users see
// only how healthy and fast each model is, not which channel is behind it.
//
// Per model it merges each bucket's Total/Success across channels, sums Requests,
// recomputes SuccessRate/Health from the merged totals, averages AvgResponseMs
// over the channels that recorded probe latency, and takes the most recent probe's
// speed (by LastCheckedAt) as the advertised last first-token/latency. Rows are
// ordered by model name for a stable, identity-free listing.
func GetModelStatusRows(rangeKey string, now time.Time) ([]ChannelStatusRow, error) {
	rows, err := GetChannelStatusRows(rangeKey, now)
	if err != nil {
		return nil, err
	}
	byModel := make(map[string]*ChannelStatusRow)
	order := make([]string, 0)
	respSum := make(map[string]int64)
	respCount := make(map[string]int)
	for i := range rows {
		src := &rows[i]
		agg, ok := byModel[src.Model]
		if !ok {
			checks := make([]ChannelStatusCheck, len(src.RecentChecks))
			copy(checks, src.RecentChecks)
			agg = &ChannelStatusRow{
				Model:         src.Model,
				Requests:      src.Requests,
				LastCheckedAt: src.LastCheckedAt,
				LastTtftMs:    src.LastTtftMs,
				LastLatencyMs: src.LastLatencyMs,
				RecentChecks:  checks,
			}
			byModel[src.Model] = agg
			order = append(order, src.Model)
		} else {
			agg.Requests += src.Requests
			// Merge buckets position-wise; equal ranges guarantee equal length.
			for j := range agg.RecentChecks {
				if j >= len(src.RecentChecks) {
					break
				}
				agg.RecentChecks[j].Total += src.RecentChecks[j].Total
				agg.RecentChecks[j].Success += src.RecentChecks[j].Success
			}
			// The most recent probe across channels wins the advertised speed.
			if src.LastCheckedAt > agg.LastCheckedAt {
				agg.LastCheckedAt = src.LastCheckedAt
				agg.LastTtftMs = src.LastTtftMs
				agg.LastLatencyMs = src.LastLatencyMs
			}
		}
		if src.AvgResponseMs > 0 {
			respSum[src.Model] += int64(src.AvgResponseMs)
			respCount[src.Model]++
		}
	}
	result := make([]ChannelStatusRow, 0, len(order))
	for _, m := range order {
		agg := byModel[m]
		success := 0
		for j := range agg.RecentChecks {
			check := &agg.RecentChecks[j]
			check.Health = channelStatusHealth(check.Total, check.Success)
			success += check.Success
		}
		agg.Health = channelStatusHealth(agg.Requests, success)
		if agg.Requests > 0 {
			agg.SuccessRate = float64(success) * 100 / float64(agg.Requests)
		}
		if respCount[m] > 0 {
			agg.AvgResponseMs = int(respSum[m] / int64(respCount[m]))
		}
		result = append(result, *agg)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Model < result[j].Model
	})
	return result, nil
}

// HasDueChannelMonitorConfigs 判断当前是否存在到期待探测的渠道配置。调度器用它
// 折入 channelMonitorHandler.Enabled()，只有真正有活儿时才创建任务行，避免 15s
// 节拍无条件建行导致 system_tasks 膨胀（该表无保留清理）。
func HasDueChannelMonitorConfigs(now int64) bool {
	var count int64
	err := DB.Model(&ChannelMonitorConfig{}).
		Where("enabled = ? AND next_check_at <= ?", true, now).
		Limit(1).Count(&count).Error
	return err == nil && count > 0
}

// GetDueChannelMonitorConfigs 返回到期的启用配置：next_check_at <= now
// （新配置 next_check_at 为 0，天然立即到期）。到期判断完全依据持久化的
// NextCheckAt，避免每趟现算随机阈值导致到期状态抖动。
func GetDueChannelMonitorConfigs(now int64) ([]*ChannelMonitorConfig, error) {
	var configs []*ChannelMonitorConfig
	if err := DB.Where("enabled = ? AND next_check_at <= ?", true, now).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

const managedErrorProbePendingAt int64 = -1

// HasPendingManagedErrorProbeConfigs reports whether an error-triggered probe
// is waiting. These one-shot probes are driven by hosting rather than periodic
// monitoring, so neither the global nor per-channel monitoring switch gates
// them.
func HasPendingManagedErrorProbeConfigs() bool {
	if DB == nil {
		return false
	}
	var count int64
	err := DB.Model(&ChannelMonitorConfig{}).
		Where("managed = ? AND next_check_at = ?", true, managedErrorProbePendingAt).
		Limit(1).Count(&count).Error
	return err == nil && count > 0
}

// GetPendingManagedErrorProbeConfigs returns one-shot probes requested by the
// managed error policy. A completed sweep replaces the sentinel with the next
// regular check time; disabled configs then remain idle until another error
// streak requests a probe.
func GetPendingManagedErrorProbeConfigs() ([]*ChannelMonitorConfig, error) {
	var configs []*ChannelMonitorConfig
	if err := DB.Where("managed = ? AND next_check_at = ?", true, managedErrorProbePendingAt).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// HasPendingManagedErrorProbe reports whether this config was explicitly
// queued by the managed error policy. The one-shot probe remains a full policy
// check even when the periodic mode only probes banned models.
func (c *ChannelMonitorConfig) HasPendingManagedErrorProbe() bool {
	return c != nil && c.NextCheckAt == managedErrorProbePendingAt
}

func InsertChannelMonitorResult(result *ChannelMonitorResult) error {
	if result.CheckedAt == 0 {
		result.CheckedAt = common.GetTimestamp()
	}
	if result.TriggerType == "" {
		result.TriggerType = ChannelMonitorTriggerScheduled
	}
	return DB.Create(result).Error
}

func GetChannelMonitorResults(channelId int, modelName string, startAt int64, endAt int64) ([]ChannelMonitorResult, error) {
	var results []ChannelMonitorResult
	query := DB.Where("channel_id = ? AND model_name = ?", channelId, modelName)
	if startAt > 0 {
		query = query.Where("checked_at >= ?", startAt)
	}
	if endAt > 0 {
		query = query.Where("checked_at < ?", endAt)
	}
	err := query.Order("checked_at DESC").Limit(200).Find(&results).Error
	return results, err
}

func DeleteOldChannelMonitorResults(before int64) error {
	return DB.Where("checked_at < ?", before).Delete(&ChannelMonitorResult{}).Error
}

// UpdateChannelMonitorSchedule 在一轮探测结束后同时写回 last_checked_at 与
// next_check_at（下一次到期时间已叠加抖动）。写 next_check_at 保证该渠道在下个
// 周期到达前不会被 GetDueChannelMonitorConfigs 反复选中。
func UpdateChannelMonitorSchedule(channelId int, checkedAt int64, nextCheckAt int64) error {
	return DB.Model(&ChannelMonitorConfig{}).
		Where("channel_id = ?", channelId).
		Updates(map[string]interface{}{
			"last_checked_at": checkedAt,
			"next_check_at":   nextCheckAt,
			"updated_time":    checkedAt,
		}).Error
}

// AdvanceChannelMonitorConfigDue brings one enabled channel's next probe forward
// so the scheduler picks it up on its next tick and runs a full scheduled sweep
// (including managed-policy follow-up) for it. Setting next_check_at to 0 makes
// the config immediately due (GetDueChannelMonitorConfigs matches next_check_at
// <= now). It only affects an enabled config; a disabled or missing config
// reports rows affected 0 so the caller can surface an accurate message.
func AdvanceChannelMonitorConfigDue(channelId int) (int64, error) {
	result := DB.Model(&ChannelMonitorConfig{}).
		Where("channel_id = ? AND enabled = ?", channelId, true).
		Updates(map[string]interface{}{
			"next_check_at": 0,
			"updated_time":  common.GetTimestamp(),
		})
	return result.RowsAffected, result.Error
}

// AdvanceManagedErrorProbeDue queues one managed-policy probe independently of
// periodic monitoring. The negative sentinel distinguishes this one-shot work
// from a normal due time, so a disabled config does not start probing on a
// cadence after the requested sweep completes.
func AdvanceManagedErrorProbeDue(channelId int) (int64, error) {
	result := DB.Model(&ChannelMonitorConfig{}).
		Where("channel_id = ? AND managed = ?", channelId, true).
		Updates(map[string]interface{}{
			"next_check_at": managedErrorProbePendingAt,
			"updated_time":  common.GetTimestamp(),
		})
	return result.RowsAffected, result.Error
}

const (
	// MonitorMinIntervalSeconds 是探测间隔下限。因为系统任务调度器的最小节拍是
	// 15s（见 controller/system_task_handlers.go 的 channelMonitorHandler.Interval），
	// 实际生效精度约为 15s，取更小的值也会被量化到节拍边界，这里仍允许到 5s。
	MonitorMinIntervalSeconds = 5
	// MonitorMaxIntervalSeconds 是探测间隔上限（24 小时）。
	MonitorMaxIntervalSeconds = 24 * 60 * 60
)

// ChannelMonitorConfig 保存单个渠道的监控探测策略。
// 每个渠道最多一条配置（ChannelId 唯一）。调度器按 IntervalSeconds（叠加
// ±JitterSeconds 的随机抖动）对每个 MonitoredModels 项独立探测。Headers 使用
// JSON 数组保存 {key,value} 列表。Enabled 是渠道级总开关；MonitoredModels 记录
// 被勾选监控的模型名列表，某个模型“正在监控”当且仅当 Enabled 为真且该模型在此
// 列表中。NextCheckAt 是下一次到期的绝对时间戳（秒），每次周期探测完由
// NextProbeAt 重新计算并持久化；为 0 表示“从未调度、立即到期”，为 -1 表示托管
// 策略排队的一次性错误探测。
type ChannelMonitorConfig struct {
	Id        int  `json:"id"`
	ChannelId int  `json:"channel_id" gorm:"uniqueIndex:uk_channel_monitor_channel,where:deleted_at IS NULL;not null"`
	Enabled   bool `json:"enabled"`
	// Managed turns on autonomous channel management for this channel: the policy
	// engine bans/recovers and up/downgrades its monitored models based purely on
	// probe results (see ChannelManagedModelState and channel_managed_policy).
	Managed         bool      `json:"managed"`
	MonitorMode     string    `json:"monitor_mode" gorm:"type:varchar(16);default:'default'"`
	EndpointType    string    `json:"endpoint_type" gorm:"type:varchar(64);default:'auto'"`
	Stream          bool      `json:"stream"`
	IntervalSeconds int       `json:"interval_seconds" gorm:"default:600"`
	JitterSeconds   int       `json:"jitter_seconds" gorm:"default:0"`
	MonitoredModels JSONValue `json:"monitored_models" gorm:"type:json"`
	// TemplateId is the stable reference used by the channel dialog. TemplateName
	// remains for compatibility with the channel-monitor console and old rows.
	TemplateId    int       `json:"template_id" gorm:"index"`
	TemplateName  string    `json:"template_name" gorm:"type:varchar(64)"`
	Headers       JSONValue `json:"headers" gorm:"type:json"`
	BodyMode      string    `json:"body_mode" gorm:"type:varchar(16);default:'default'"`
	BodyJson      string    `json:"body_json" gorm:"type:text"`
	Remark        string    `json:"remark" gorm:"type:varchar(255)"`
	LastCheckedAt int64     `json:"last_checked_at" gorm:"bigint;default:0"`
	NextCheckAt   int64     `json:"next_check_at" gorm:"bigint;default:0;index"`
	CreatedTime   int64     `json:"created_time" gorm:"bigint"`
	UpdatedTime   int64     `json:"updated_time" gorm:"bigint"`

	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

// NextProbeAt 根据 IntervalSeconds 与 JitterSeconds 计算下一次探测的绝对时间戳。
// 抖动是对称的：在 [-JitterSeconds, +JitterSeconds] 内取一个随机整数加到间隔上。
// 归一化保证 interval 落在 [MonitorMinIntervalSeconds, MonitorMaxIntervalSeconds]、
// jitter 落在 [0, interval-1]，因此 interval+offset >= 1，结果始终严格晚于 now。
func (c *ChannelMonitorConfig) NextProbeAt(now int64) int64 {
	interval := c.IntervalSeconds
	if interval < MonitorMinIntervalSeconds {
		interval = MonitorMinIntervalSeconds
	}
	if interval > MonitorMaxIntervalSeconds {
		interval = MonitorMaxIntervalSeconds
	}
	jitter := c.JitterSeconds
	if jitter < 0 {
		jitter = 0
	}
	if jitter > interval-1 {
		jitter = interval - 1
	}
	offset := 0
	if jitter > 0 {
		offset = common.GetRandomInt(2*jitter+1) - jitter
	}
	next := now + int64(interval+offset)
	if next <= now {
		next = now + 1
	}
	return next
}

type ChannelMonitorHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (c *ChannelMonitorConfig) GetHeaders() []ChannelMonitorHeader {
	if c == nil {
		return []ChannelMonitorHeader{}
	}
	if len(c.Headers) == 0 {
		return []ChannelMonitorHeader{}
	}
	var headers []ChannelMonitorHeader
	if err := common.Unmarshal(c.Headers, &headers); err != nil {
		return []ChannelMonitorHeader{}
	}
	return headers
}

func (c *ChannelMonitorConfig) SetHeaders(headers []ChannelMonitorHeader) error {
	if headers == nil {
		headers = []ChannelMonitorHeader{}
	}
	data, err := common.Marshal(headers)
	if err != nil {
		return err
	}
	c.Headers = JSONValue(data)
	return nil
}

// MonitorTemplate 可复用的探测请求模版。选择模版会把它的 headers/body 快照
// 复制到渠道配置里，之后模版变化不会自动同步，需要显式“应用更新”。
type MonitorTemplate struct {
	Id           int       `json:"id"`
	Name         string    `json:"name" gorm:"size:64;not null;uniqueIndex:uk_monitor_template_name,where:deleted_at IS NULL"`
	Description  string    `json:"description" gorm:"type:varchar(255)"`
	EndpointType string    `json:"endpoint_type" gorm:"type:varchar(64);default:'openai'"`
	Stream       bool      `json:"stream"`
	Headers      JSONValue `json:"headers" gorm:"type:json"`
	BodyMode     string    `json:"body_mode" gorm:"type:varchar(16);default:'merge'"`
	BodyJson     string    `json:"body_json" gorm:"type:text"`
	CreatedTime  int64     `json:"created_time" gorm:"bigint"`
	UpdatedTime  int64     `json:"updated_time" gorm:"bigint"`

	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (t *MonitorTemplate) GetHeaders() []ChannelMonitorHeader {
	if t == nil || len(t.Headers) == 0 {
		return []ChannelMonitorHeader{}
	}
	var headers []ChannelMonitorHeader
	if err := common.Unmarshal(t.Headers, &headers); err != nil {
		return []ChannelMonitorHeader{}
	}
	return headers
}

func (t *MonitorTemplate) SetHeaders(headers []ChannelMonitorHeader) error {
	if headers == nil {
		headers = []ChannelMonitorHeader{}
	}
	data, err := common.Marshal(headers)
	if err != nil {
		return err
	}
	t.Headers = JSONValue(data)
	return nil
}

// MonitorQuestion is one reusable conversational prompt for scheduled channel
// probes. Questions are global: every channel/model probe selects independently
// from the same persisted library.
type MonitorQuestion struct {
	Id          int    `json:"id"`
	Content     string `json:"content" gorm:"type:text;not null"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

// GetMonitoredModels 解析 MonitoredModels JSON 数组，返回被勾选监控的模型名列表。
// 空或非法 JSON 时返回空切片。
func (c *ChannelMonitorConfig) GetMonitoredModels() []string {
	if len(c.MonitoredModels) == 0 {
		return []string{}
	}
	var models []string
	if err := common.Unmarshal(c.MonitoredModels, &models); err != nil {
		return []string{}
	}
	return models
}

// SetMonitoredModels 把模型名列表写回 MonitoredModels JSON 数组。
func (c *ChannelMonitorConfig) SetMonitoredModels(models []string) error {
	if models == nil {
		models = []string{}
	}
	data, err := common.Marshal(models)
	if err != nil {
		return err
	}
	c.MonitoredModels = JSONValue(data)
	return nil
}

// ChannelMonitorListItem 是渠道监控列表的精简投影，只取列表展示需要的字段，
// 避免加载 Key 等敏感/大字段。
type ChannelMonitorListItem struct {
	Id       int    `json:"id"`
	Type     int    `json:"type"`
	Name     string `json:"name"`
	Group    string `json:"group"`
	Tag      string `json:"tag"`
	Models   string `json:"models"`
	Priority int64  `json:"priority"`
}

// GetChannelMonitorListItems 返回全部渠道的精简信息。调用方按监控配置过滤，
// 只展示已配置检测端点/模版的渠道。
func GetChannelMonitorListItems() ([]*ChannelMonitorListItem, error) {
	var items []*ChannelMonitorListItem
	err := DB.Model(&Channel{}).
		Select("id", "type", "name", commonGroupCol, "models", "priority").
		Order("priority desc").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

// ---- ChannelMonitorConfig access ----

// GetAllChannelMonitorConfigs 返回全部渠道监控配置，按 channel_id 建索引方便前端合并。
func GetAllChannelMonitorConfigs() ([]*ChannelMonitorConfig, error) {
	var configs []*ChannelMonitorConfig
	if err := DB.Model(&ChannelMonitorConfig{}).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// GetChannelMonitorConfig 返回指定渠道的监控配置，不存在时返回 (nil, nil)。
func GetChannelMonitorConfig(channelId int) (*ChannelMonitorConfig, error) {
	var config ChannelMonitorConfig
	err := DB.Where("channel_id = ?", channelId).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

// UpsertChannelMonitorConfig 按 channel_id 新增或更新监控配置。
// NextCheckAt 不由前端传入（DTO 中恒为 0），因此新增与编辑都会让 next_check_at
// 归零、在下个调度节拍立即到期并按新的 interval/jitter 重新排期——这正是“保存即
// 生效”的语义：改了间隔或抖动后马上按新策略走，无需等上一周期耗尽。
func UpsertChannelMonitorConfig(config *ChannelMonitorConfig) error {
	now := common.GetTimestamp()
	existing, err := GetChannelMonitorConfig(config.ChannelId)
	if err != nil {
		return err
	}
	if existing == nil {
		config.Id = 0
		config.CreatedTime = now
		config.UpdatedTime = now
		return DB.Create(config).Error
	}
	config.Id = existing.Id
	config.CreatedTime = existing.CreatedTime
	config.LastCheckedAt = existing.LastCheckedAt
	config.UpdatedTime = now
	return DB.Save(config).Error
}

// DeleteChannelMonitorConfigByChannel removes a channel from the monitor list and
// undoes what the managed policy did to it. The list only shows channels that own
// a config row, so deleting that row is what "remove from monitoring" means.
// Managed state is cleared and the ability rows are restored to the channel-level
// enabled/priority in the same transaction: leaving them behind would keep a
// policy-banned model disabled with no console entry point left to recover it.
// Probe history is deliberately preserved — the model-status page reads it and
// DeleteOldChannelMonitorResults already prunes it on retention.
func DeleteChannelMonitorConfigByChannel(channelId int) error {
	if channelId <= 0 {
		return errors.New("invalid channel ID")
	}
	// An orphan config whose channel is already gone must still be deletable, so a
	// missing channel only skips the ability restore (there are no rows to restore).
	channel, err := GetChannelById(channelId, false)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("channel_id = ?", channelId).Delete(&ChannelMonitorConfig{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if channel != nil {
			if err := tx.Model(&Ability{}).
				Where("channel_id = ?", channelId).
				Updates(map[string]interface{}{
					"enabled":  channel.Status == common.ChannelStatusEnabled,
					"priority": channel.GetPriority(),
				}).Error; err != nil {
				return err
			}
		}
		return tx.Where("channel_id = ?", channelId).Delete(&ChannelManagedState{}).Error
	})
}

// ApplyTemplateToChannels 把模版快照重新写入所有引用该模版的渠道配置，
// 返回受影响的渠道数量。
func ApplyTemplateToChannels(tpl *MonitorTemplate) (int64, error) {
	now := common.GetTimestamp()
	result := DB.Model(&ChannelMonitorConfig{}).
		Where("template_id = ?", tpl.Id).
		Or("template_name = ?", tpl.Name).
		Updates(map[string]interface{}{
			"template_id":   tpl.Id,
			"template_name": tpl.Name,
			"endpoint_type": tpl.EndpointType,
			"stream":        tpl.Stream,
			"headers":       tpl.Headers,
			"body_mode":     tpl.BodyMode,
			"body_json":     tpl.BodyJson,
			"updated_time":  now,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// ---- MonitorQuestion access ----

// GetAllMonitorQuestions returns the complete question library, newest edits
// first for the management dialog. The scheduler may reuse the same result as
// an in-memory random-selection pool for one task run.
func GetAllMonitorQuestions() ([]*MonitorQuestion, error) {
	var questions []*MonitorQuestion
	if err := DB.Model(&MonitorQuestion{}).Order("updated_time DESC, id DESC").Find(&questions).Error; err != nil {
		return nil, err
	}
	return questions, nil
}

// GetMonitorQuestion returns one question by ID, or (nil, nil) when absent.
func GetMonitorQuestion(id int) (*MonitorQuestion, error) {
	var question MonitorQuestion
	err := DB.First(&question, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &question, nil
}

// IsMonitorQuestionContentDuplicated checks for an exact duplicate while
// excluding the row currently being edited.
func IsMonitorQuestionContentDuplicated(id int, content string) (bool, error) {
	if content == "" {
		return false, nil
	}
	var count int64
	err := DB.Model(&MonitorQuestion{}).Where("content = ? AND id <> ?", content, id).Count(&count).Error
	return count > 0, err
}

func (q *MonitorQuestion) Insert() error {
	now := common.GetTimestamp()
	q.CreatedTime = now
	q.UpdatedTime = now
	return DB.Create(q).Error
}

func (q *MonitorQuestion) Update() error {
	q.UpdatedTime = common.GetTimestamp()
	return DB.Save(q).Error
}

func DeleteMonitorQuestionByID(id int) error {
	return DB.Delete(&MonitorQuestion{}, id).Error
}

// ---- MonitorTemplate access ----

// GetAllMonitorTemplates 返回全部模版，按更新时间倒序。
func GetAllMonitorTemplates() ([]*MonitorTemplate, error) {
	var templates []*MonitorTemplate
	if err := DB.Model(&MonitorTemplate{}).Order("updated_time DESC").Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

// GetMonitorTemplate 按 ID 返回模版，不存在时返回 (nil, nil)。
func GetMonitorTemplate(id int) (*MonitorTemplate, error) {
	var tpl MonitorTemplate
	err := DB.First(&tpl, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &tpl, nil
}

func GetMonitorTemplateByName(name string) (*MonitorTemplate, error) {
	var template MonitorTemplate
	err := DB.Where("name = ?", strings.TrimSpace(name)).First(&template).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &template, nil
}

// IsMonitorTemplateNameDuplicated 检查模版名称是否重复（排除自身 ID）。
func IsMonitorTemplateNameDuplicated(id int, name string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&MonitorTemplate{}).Where("name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

// Insert 新建模版。
func (t *MonitorTemplate) Insert() error {
	now := common.GetTimestamp()
	t.CreatedTime = now
	t.UpdatedTime = now
	return DB.Create(t).Error
}

// Update 更新模版。
func (t *MonitorTemplate) Update() error {
	t.UpdatedTime = common.GetTimestamp()
	return DB.Save(t).Error
}

// DeleteMonitorTemplateByID 按 ID 删除模版。
func DeleteMonitorTemplateByID(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var template MonitorTemplate
		if err := tx.First(&template, id).Error; err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		configs := tx.Model(&ChannelMonitorConfig{}).Where("template_id = ?", id)
		if template.Name != "" {
			configs = configs.Or("template_name = ?", template.Name)
		}
		if err := configs.
			Updates(map[string]interface{}{"template_id": 0, "template_name": ""}).Error; err != nil {
			return err
		}
		return tx.Delete(&MonitorTemplate{}, id).Error
	})
}

// InsertMonitorTemplate and UpdateMonitorTemplate keep the persistence API
// explicit for controllers and tests while the methods above remain convenient
// for callers that already own a template value.
func InsertMonitorTemplate(template *MonitorTemplate) error {
	if template == nil {
		return fmt.Errorf("monitor template is nil")
	}
	template.Id = 0
	return template.Insert()
}

func UpdateMonitorTemplate(template *MonitorTemplate) error {
	if template == nil {
		return fmt.Errorf("monitor template is nil")
	}
	existing, err := GetMonitorTemplate(template.Id)
	if err != nil {
		return err
	}
	if existing == nil {
		return gorm.ErrRecordNotFound
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		template.CreatedTime = existing.CreatedTime
		template.UpdatedTime = common.GetTimestamp()
		if err := tx.Model(&ChannelMonitorConfig{}).
			Where("template_id = ?", template.Id).
			Or("template_name = ?", existing.Name).
			Updates(map[string]interface{}{
				"template_id":   template.Id,
				"template_name": template.Name,
			}).Error; err != nil {
			return err
		}
		return tx.Save(template).Error
	})
}
