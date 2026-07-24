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
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// runChannelManagedPolicy applies the channel-managed policy after a monitor
// sweep. It is best-effort: any error is logged and the sweep still succeeds,
// because a policy hiccup must not fail probing itself. It has two independent
// stages, each gated by its own master switch in the managed-policy setting:
//
//   - Ban/recover: a symmetric-confirmation circuit breaker per (channel, model).
//     A model's probe outcome must disagree with its current stable state for
//     ConfirmCount consecutive probes before the engine flips it (bans an active
//     model, or recovers a banned one). A single agreeing probe resets the count.
//
//   - Speed up/downgrade: per model, rank the managed channels by their recent
//     mean TTFT and cluster channels whose means are within a configurable
//     relative gap into the same priority tier (fastest tier keeps the highest
//     priority). Banned pairs and pairs without enough TTFT samples are excluded.
//
// Both stages mutate ability rows (enabled / priority) and persist their decision
// in ChannelManagedState so it survives ability-table rebuilds (see
// ReplayManagedAbilities). Only channels with Managed=true participate.
func runChannelManagedPolicy() {
	if !operation_setting.IsChannelMonitorEnabled() {
		return
	}
	setting := operation_setting.GetManagedPolicySetting()
	if !setting.BanEnabled && !setting.SpeedEnabled {
		return
	}

	configs, err := model.GetManagedChannelMonitorConfigs()
	if err != nil {
		common.SysError("managed policy: failed to load managed configs: " + err.Error())
		return
	}
	if len(configs) == 0 {
		return
	}

	abilitiesChanged := false

	if setting.BanEnabled {
		if applyManagedBanStage(configs, setting) {
			abilitiesChanged = true
		}
	}

	if setting.SpeedEnabled {
		if applyManagedSpeedStage(configs, setting) {
			abilitiesChanged = true
		}
	}

	// Refresh the memory-cache overlay so the new bans/priorities take effect on
	// the selection path immediately instead of waiting for the periodic sync.
	if abilitiesChanged {
		model.InitChannelCache()
	}
}

// managedModelPair identifies one monitored (channel, model) unit of work.
type managedModelPair struct {
	channelID int
	channelNm string
	model     string
}

// collectManagedPairs expands the managed configs into their monitored models,
// keeping only models the channel still actually serves (mirrors the guard the
// status page uses so a stale monitored-model entry is ignored).
func collectManagedPairs(configs []*model.ChannelMonitorConfig) []managedModelPair {
	pairs := make([]managedModelPair, 0)
	for _, config := range configs {
		channel, err := model.GetChannelById(config.ChannelId, false)
		if err != nil || channel == nil {
			continue
		}
		available := channel.GetModels()
		availableSet := make(map[string]struct{}, len(available))
		for _, m := range available {
			availableSet[m] = struct{}{}
		}
		seen := make(map[string]struct{})
		for _, modelName := range config.GetMonitoredModels() {
			if modelName == "" {
				continue
			}
			if _, ok := availableSet[modelName]; !ok {
				continue
			}
			if _, ok := seen[modelName]; ok {
				continue
			}
			seen[modelName] = struct{}{}
			pairs = append(pairs, managedModelPair{
				channelID: config.ChannelId,
				channelNm: channel.Name,
				model:     modelName,
			})
		}
	}
	return pairs
}

// applyManagedBanStage runs the circuit breaker for every managed (channel,
// model) pair and returns whether any ability enable/disable was applied.
func applyManagedBanStage(configs []*model.ChannelMonitorConfig, setting *operation_setting.ManagedPolicySetting) bool {
	pairs := collectManagedPairs(configs)
	changed := false
	for _, pair := range pairs {
		if applyBanForPair(pair, setting) {
			changed = true
		}
	}
	return changed
}

// applyBanForPair advances the confirmation counter for one pair from its most
// recent probe and flips ban state when the threshold is reached. Returns whether
// an ability status change was applied.
func applyBanForPair(pair managedModelPair, setting *operation_setting.ManagedPolicySetting) bool {
	latest, err := model.GetLatestChannelMonitorResult(pair.channelID, pair.model)
	if err != nil {
		common.SysError(fmt.Sprintf("managed policy: latest result lookup failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
		return false
	}
	if latest == nil {
		return false // no probe yet, nothing to decide on
	}

	state, err := model.GetChannelManagedState(pair.channelID, pair.model)
	if err != nil {
		common.SysError(fmt.Sprintf("managed policy: state lookup failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
		return false
	}
	if state == nil {
		state = &model.ChannelManagedState{
			ChannelId: pair.channelID,
			ModelName: pair.model,
			BanState:  model.ManagedBanStateActive,
		}
	}

	// Idempotency + confirmation spacing. The engine runs once per ~15s sweep but
	// a pair may not have a fresh probe every sweep, so guard against counting the
	// same probe twice and against counting probes closer together than the
	// configured confirmation interval. A probe only advances the counter when it
	// is newer than the last counted probe by at least the interval. Agreement
	// (see below) still resets immediately regardless of spacing, so recovery is
	// never delayed by a stale interval gate.
	interval := int64(setting.BanConfirmIntervalSeconds)
	if interval < operation_setting.ManagedConfirmIntervalFloorSeconds {
		interval = operation_setting.ManagedConfirmIntervalFloorSeconds
	}

	// Probe "agrees" with the stable state when a success matches active or a
	// failure matches banned. Agreement resets the confirmation counter; only
	// sustained disagreement drives a flip.
	stableIsActive := state.BanState != model.ManagedBanStateBanned
	agrees := latest.Success == stableIsActive
	if agrees {
		if state.ConfirmCount != 0 {
			state.ConfirmCount = 0
			state.LastConfirmProbeAt = latest.CheckedAt
			if err := model.UpsertChannelManagedState(state); err != nil {
				common.SysError(fmt.Sprintf("managed policy: reset confirmation state failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
			}
		}
		return false
	}

	// Disagreeing probe. Only count it if it is a new probe spaced at least
	// `interval` seconds after the last one we counted; otherwise wait for a
	// fresh, sufficiently-spaced probe. This also makes re-running the engine on
	// the same probe a no-op (CheckedAt unchanged).
	if state.LastConfirmProbeAt != 0 && latest.CheckedAt-state.LastConfirmProbeAt < interval {
		return false
	}

	state.ConfirmCount++
	state.LastConfirmProbeAt = latest.CheckedAt
	threshold := setting.ConfirmCount
	if threshold < 1 {
		threshold = 1
	}
	if state.ConfirmCount < threshold {
		// Not yet confirmed; persist the running count and wait for more probes.
		if err := model.UpsertChannelManagedState(state); err != nil {
			common.SysError(fmt.Sprintf("managed policy: persist confirmation state failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
		}
		return false
	}

	// Threshold reached: flip the stable state and apply it to the ability rows.
	now := common.GetTimestamp()
	changed := false
	if stableIsActive {
		// active -> banned: disable this model on this channel.
		enabled := false
		state.BanState = model.ManagedBanStateBanned
		state.LastBanAt = now
		state.ConfirmCount = 0
		if err := model.ApplyChannelManagedAbilityState(state, &enabled, nil); err != nil {
			common.SysError(fmt.Sprintf("managed policy: disable ability failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
			return false
		}
		changed = true
		notifyManagedAction(pair, "banned", "连续探测失败达到确认次数", state, latest)
	} else {
		// banned -> active: re-enable this model on this channel.
		enabled := true
		state.BanState = model.ManagedBanStateActive
		state.LastRecoverAt = now
		state.ConfirmCount = 0
		if err := model.ApplyChannelManagedAbilityState(state, &enabled, nil); err != nil {
			common.SysError(fmt.Sprintf("managed policy: enable ability failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
			return false
		}
		changed = true
		notifyManagedAction(pair, "recovered", "连续探测成功达到确认次数", state, latest)
	}
	return changed
}

// applyManagedSpeedStage ranks channels per model by recent mean TTFT and assigns
// clustered priority tiers. Returns whether any ability priority was changed.
func applyManagedSpeedStage(configs []*model.ChannelMonitorConfig, setting *operation_setting.ManagedPolicySetting) bool {
	pairs := collectManagedPairs(configs)

	// Group pairs by model so ranking is within one model across channels.
	byModel := make(map[string][]managedModelPair)
	for _, pair := range pairs {
		byModel[pair.model] = append(byModel[pair.model], pair)
	}

	changed := false
	for modelName, modelPairs := range byModel {
		if applySpeedForModel(modelName, modelPairs, setting) {
			changed = true
		}
	}
	return changed
}

// speedSample is a channel's mean-TTFT datapoint for one model.
type speedSample struct {
	pair    managedModelPair
	meanMs  float64
	hasData bool
}

// applySpeedForModel clusters the ranked channels for one model into priority
// tiers and writes the tier priority onto each ability. Channels that are banned
// or lack enough TTFT samples keep their current priority (they are not ranked).
func applySpeedForModel(modelName string, pairs []managedModelPair, setting *operation_setting.ManagedPolicySetting) bool {
	window := setting.SpeedWindow
	if window < 1 {
		window = 1
	}

	// Gather ranked candidates: enabled (not banned) pairs with enough samples.
	samples := make([]speedSample, 0, len(pairs))
	for _, pair := range pairs {
		state, err := model.GetChannelManagedState(pair.channelID, pair.model)
		if err != nil {
			common.SysError(fmt.Sprintf("managed policy: speed state lookup failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
			continue
		}
		// Banned pairs are excluded from ranking; their priority is meaningless
		// while disabled and they re-enter ranking after recovery.
		if state != nil && state.BanState == model.ManagedBanStateBanned {
			continue
		}
		mean, count, err := model.GetRecentTtftMean(pair.channelID, pair.model, window)
		if err != nil {
			common.SysError(fmt.Sprintf("managed policy: ttft mean failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
			continue
		}
		if count == 0 {
			// Not enough data yet: leave this pair's priority untouched.
			continue
		}
		samples = append(samples, speedSample{pair: pair, meanMs: mean, hasData: true})
	}

	if len(samples) < 2 {
		// Ranking needs at least two comparable channels to be meaningful.
		return false
	}

	// Sort fastest (lowest TTFT) first.
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].meanMs < samples[j].meanMs
	})

	// Cluster into tiers: start a new (lower) tier whenever the next channel's
	// mean exceeds the previous tier's reference mean by more than the relative
	// gap. Channels within the gap share a tier and keep weight-based balancing.
	gap := float64(setting.TierDiffPercent)
	if gap < 0 {
		gap = 0
	}
	tiers := clusterSpeedTiers(samples, gap)

	// Assign descending priorities to tiers. The highest tier gets the top
	// priority; each lower tier gets a strictly smaller value so the selection
	// path treats them as separate retry levels.
	changed := false
	basePriority := int64(len(tiers)) // e.g. 3 tiers -> priorities 3,2,1
	for tierIdx, tier := range tiers {
		tierPriority := basePriority - int64(tierIdx)
		for _, sample := range tier {
			if applyManagedPriority(sample.pair, tierPriority) {
				changed = true
			}
		}
	}
	return changed
}

// clusterSpeedTiers groups speed-sorted samples into tiers. A new tier begins
// when a sample's mean exceeds the current tier's anchor mean by more than
// gapPercent (relative). Samples must already be sorted fastest-first.
func clusterSpeedTiers(samples []speedSample, gapPercent float64) [][]speedSample {
	tiers := make([][]speedSample, 0)
	if len(samples) == 0 {
		return tiers
	}
	current := []speedSample{samples[0]}
	anchor := samples[0].meanMs
	for _, sample := range samples[1:] {
		threshold := anchor * (1 + gapPercent/100)
		if sample.meanMs > threshold {
			// Gap too large: close the current tier and open a new one anchored
			// at this sample.
			tiers = append(tiers, current)
			current = []speedSample{sample}
			anchor = sample.meanMs
		} else {
			current = append(current, sample)
		}
	}
	tiers = append(tiers, current)
	return tiers
}

// applyManagedPriority sets the ability priority for one (channel, model) pair to
// the tier priority, snapshotting the original priority on first management so an
// upgrade can restore it. Returns whether the ability priority actually changed.
func applyManagedPriority(pair managedModelPair, priority int64) bool {
	state, err := model.GetChannelManagedState(pair.channelID, pair.model)
	if err != nil {
		common.SysError(fmt.Sprintf("managed policy: priority state lookup failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
		return false
	}
	if state == nil {
		state = &model.ChannelManagedState{
			ChannelId: pair.channelID,
			ModelName: pair.model,
			BanState:  model.ManagedBanStateActive,
		}
	}

	// If already managed at this priority, nothing to do.
	if state.PriorityManaged && state.ManagedPriority == priority {
		return false
	}

	// Snapshot the pre-policy priority the first time we take over this pair.
	if !state.PriorityManaged {
		original, err := model.GetChannelModelAbilityPriority(pair.channelID, pair.model)
		if err != nil {
			common.SysError(fmt.Sprintf("managed policy: original priority lookup failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
			return false
		}
		state.OriginalPriority = original
	}

	state.PriorityManaged = true
	state.ManagedPriority = priority
	if err := model.ApplyChannelManagedAbilityState(state, nil, &priority); err != nil {
		common.SysError(fmt.Sprintf("managed policy: set priority failed channel=%d model=%s: %v", pair.channelID, pair.model, err))
		return false
	}
	return true
}

// notifyManagedAction alerts operators when the policy bans or recovers a model.
// It fans out to two channels, both best-effort (a failure is logged, never
// propagated, so notification never disrupts the policy sweep):
//
//   - the existing root-user notification (email/webhook/bark/gotify), for parity
//     with channel disable/enable alerts, and
//   - an optional DingTalk action card, when configured in the managed policy.
//
// `state` carries the just-flipped ban state. Because a ban only writes LastBanAt
// and a recover only writes LastRecoverAt, the opposite timestamp still holds the
// prior event's time, so "time since last recover/ban" reads correctly here.
// `latest` is the probe that triggered the flip (its ErrorMessage feeds the ban
// card). Either may be nil; the card degrades gracefully.
func notifyManagedAction(pair managedModelPair, action string, reason string, state *model.ChannelManagedState, latest *model.ChannelMonitorResult) {
	var subject, content string
	switch action {
	case "banned":
		subject = fmt.Sprintf("托管策略：渠道「%s」（#%d）模型 %s 已封禁", pair.channelNm, pair.channelID, pair.model)
		content = fmt.Sprintf("托管策略封禁了渠道「%s」（#%d）的模型 %s，原因：%s", pair.channelNm, pair.channelID, pair.model, reason)
	case "recovered":
		subject = fmt.Sprintf("托管策略：渠道「%s」（#%d）模型 %s 已恢复", pair.channelNm, pair.channelID, pair.model)
		content = fmt.Sprintf("托管策略恢复了渠道「%s」（#%d）的模型 %s，原因：%s", pair.channelNm, pair.channelID, pair.model, reason)
	default:
		return
	}
	service.NotifyRootUser(fmt.Sprintf("managed_%d_%s_%s", pair.channelID, pair.model, action), subject, content)
	notifyManagedActionDingTalk(pair, action, state, latest)
}

// notifyManagedActionDingTalk sends the ban/recover DingTalk action card when the
// integration is enabled and configured. It runs the network call in a goroutine
// so a slow DingTalk endpoint never stalls the 15s policy sweep, and swallows all
// errors into the log (best-effort, like every other notification path).
func notifyManagedActionDingTalk(pair managedModelPair, action string, state *model.ChannelManagedState, latest *model.ChannelMonitorResult) {
	setting := operation_setting.GetManagedPolicySetting()
	if !setting.DingTalkEnabled || strings.TrimSpace(setting.DingTalkWebhookURL) == "" {
		return
	}
	title, markdown := buildManagedDingTalkCard(pair, action, state, latest)
	if title == "" {
		return
	}
	webhook := setting.DingTalkWebhookURL
	secret := setting.DingTalkSecret
	go func() {
		// No jump button: the card is intentionally self-contained.
		if err := service.SendDingTalkActionCard(webhook, secret, title, markdown, ""); err != nil {
			common.SysError(fmt.Sprintf("managed policy: dingtalk notify failed channel=%d model=%s action=%s: %v", pair.channelID, pair.model, action, err))
		}
	}()
}

// buildManagedDingTalkCard composes the action card title and markdown body for a
// ban or recover event. The ban card shows time since the last recover and the
// triggering probe's error; the recover card shows time since the last ban plus
// the recent mean first-token and latency (with graceful "暂无数据" fallbacks).
// Returns ("", "") for an unknown action so the caller skips sending.
func buildManagedDingTalkCard(pair managedModelPair, action string, state *model.ChannelManagedState, latest *model.ChannelMonitorResult) (string, string) {
	now := common.GetTimestamp()
	// DingTalk markdown supports <font color>; colorize the channel and model
	// names so operators can spot the affected target at a glance.
	channelName := fmt.Sprintf("<font color=\"#1677ff\">**%s**</font>", pair.channelNm)
	channelLabel := fmt.Sprintf("渠道 %s（#%d）", channelName, pair.channelID)
	switch action {
	case "banned":
		title := fmt.Sprintf("🔴 模型封禁 · %s", pair.model)
		modelName := fmt.Sprintf("<font color=\"#f5222d\">**%s**</font>", pair.model)
		var b strings.Builder
		b.WriteString("## 🔴 托管渠道封禁\n\n")
		b.WriteString(fmt.Sprintf("%s\n\n", channelLabel))
		b.WriteString(fmt.Sprintf("- **模型**：%s\n", modelName))
		if state != nil && state.LastRecoverAt > 0 {
			b.WriteString(fmt.Sprintf("- **距上次恢复**：%s\n", humanizeManagedDuration(now-state.LastRecoverAt)))
		} else {
			b.WriteString("- **距上次恢复**：暂无记录\n")
		}
		errMsg := "无"
		if latest != nil && strings.TrimSpace(latest.ErrorMessage) != "" {
			errMsg = truncateManagedText(strings.TrimSpace(latest.ErrorMessage), 300)
		}
		b.WriteString(fmt.Sprintf("- **最后错误**：%s\n", errMsg))
		appendRecommendationSection(&b)
		return title, b.String()
	case "recovered":
		title := fmt.Sprintf("🟢 模型恢复 · %s", pair.model)
		modelName := fmt.Sprintf("<font color=\"#52c41a\">**%s**</font>", pair.model)
		var b strings.Builder
		b.WriteString("## 🟢 托管渠道恢复\n\n")
		b.WriteString(fmt.Sprintf("%s\n\n", channelLabel))
		b.WriteString(fmt.Sprintf("- **模型**：%s\n", modelName))
		if state != nil && state.LastBanAt > 0 {
			b.WriteString(fmt.Sprintf("- **距上次封禁**：%s\n", humanizeManagedDuration(now-state.LastBanAt)))
		} else {
			b.WriteString("- **距上次封禁**：暂无记录\n")
		}
		window := operation_setting.GetManagedPolicySetting().SpeedWindow
		ttftMean, ttftCount, err := model.GetRecentTtftMean(pair.channelID, pair.model, window)
		if err == nil && ttftCount > 0 {
			b.WriteString(fmt.Sprintf("- **平均首字**：%s（近 %d 次）\n", formatManagedMs(ttftMean), ttftCount))
		} else {
			b.WriteString("- **平均首字**：暂无数据\n")
		}
		latencyMean, latencyCount, err := model.GetRecentLatencyMean(pair.channelID, pair.model, window)
		if err == nil && latencyCount > 0 {
			b.WriteString(fmt.Sprintf("- **平均延迟**：%s（近 %d 次）\n", formatManagedMs(latencyMean), latencyCount))
		} else {
			b.WriteString("- **平均延迟**：暂无数据\n")
		}
		appendRecommendationSection(&b)
		return title, b.String()
	default:
		return "", ""
	}
}

// humanizeManagedDuration renders a second count as a compact zh duration, e.g.
// "3 小时 12 分钟". Non-positive input becomes "刚刚".
func humanizeManagedDuration(seconds int64) string {
	if seconds <= 0 {
		return "刚刚"
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	parts := make([]string, 0, 2)
	switch {
	case days > 0:
		parts = append(parts, fmt.Sprintf("%d 天", days))
		if hours > 0 {
			parts = append(parts, fmt.Sprintf("%d 小时", hours))
		}
	case hours > 0:
		parts = append(parts, fmt.Sprintf("%d 小时", hours))
		if minutes > 0 {
			parts = append(parts, fmt.Sprintf("%d 分钟", minutes))
		}
	case minutes > 0:
		parts = append(parts, fmt.Sprintf("%d 分钟", minutes))
		if secs > 0 {
			parts = append(parts, fmt.Sprintf("%d 秒", secs))
		}
	default:
		parts = append(parts, fmt.Sprintf("%d 秒", secs))
	}
	return strings.Join(parts, " ")
}

// formatManagedMs renders a millisecond mean like the status card: sub-second in
// ms, one second or more in seconds with one decimal.
func formatManagedMs(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	return fmt.Sprintf("%dms", int64(ms+0.5))
}

// truncateManagedText clamps text to at most `max` runes, appending an ellipsis
// when it had to cut, so a huge upstream error body never bloats the card.
func truncateManagedText(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}

// appendRecommendationSection appends the "推荐使用" model list to a ban/recover
// card. The list is model-facing only — model name, the recommending channel's
// latest probe speed (首字 time-to-first-token / 延迟 total latency), and the
// operator blurb — never the channel behind it, as requested. It is best-effort:
// a lookup error or an empty list simply omits the section so a recommendation
// hiccup never blocks the alert.
//
// Layout: each model is one line — a gradient-colored, bold model name followed by
// its speed in parentheses (首字/延迟, each colored by tier) — with an optional
// blurb quoted underneath. DingTalk markdown has no CSS gradients, so the name
// gradient is faked by coloring each rune with an interpolated color.
func appendRecommendationSection(b *strings.Builder) {
	list, err := model.BuildRecommendationList()
	if err != nil {
		common.SysError("managed policy: build recommendation list failed: " + err.Error())
		return
	}
	if len(list) == 0 {
		return
	}
	b.WriteString("\n---\n\n### 🌟 推荐使用\n\n")
	for _, item := range list {
		b.WriteString(fmt.Sprintf("%s （首字 %s · 延迟 %s）\n\n",
			gradientModelName(item.Model),
			formatRecommendationSpeed(item.TtftMs), formatRecommendationSpeed(item.LatencyMs)))
		if blurb := strings.TrimSpace(item.Blurb); blurb != "" {
			b.WriteString(fmt.Sprintf("> %s\n\n", blurb))
		}
	}
}

// gradientModelName renders a bold model name with a per-rune color gradient
// (indigo → violet → pink), faking a CSS gradient DingTalk markdown cannot do.
// Each rune is wrapped in its own <font color> so the name reads as a smooth
// sweep; a single-rune name just takes the start color.
func gradientModelName(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return ""
	}
	// Gradient endpoints: indigo -> violet -> pink.
	start := [3]int{0x4f, 0x46, 0xe5} // #4f46e5
	end := [3]int{0xec, 0x48, 0x99}   // #ec4899
	var b strings.Builder
	b.WriteString("**")
	for i, r := range runes {
		t := 0.0
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}
		red := start[0] + int(float64(end[0]-start[0])*t+0.5)
		green := start[1] + int(float64(end[1]-start[1])*t+0.5)
		blue := start[2] + int(float64(end[2]-start[2])*t+0.5)
		b.WriteString(fmt.Sprintf("<font color=\"#%02x%02x%02x\">%c</font>", red, green, blue, r))
	}
	b.WriteString("**")
	return b.String()
}

// formatRecommendationSpeed renders a probe millisecond value colored by speed
// tier (green fast, yellow slow, red very slow), falling back to a plain "--" when
// the latest probe recorded no usable value (0), so a never-probed model still
// shows a tidy placeholder.
func formatRecommendationSpeed(ms int64) string {
	if ms <= 0 {
		return "--"
	}
	return fmt.Sprintf("<font color=\"%s\">%s</font>", recommendationSpeedColor(ms), formatManagedMs(float64(ms)))
}

// recommendationSpeedColor maps a probe millisecond value to a DingTalk font
// color: green for a fast response, yellow for a slow one, red for a very slow
// one. Thresholds are shared by both 首字 and 延迟 since either being slow is worth
// flagging to the reader.
func recommendationSpeedColor(ms int64) string {
	switch {
	case ms < 2000:
		return "#52c41a" // green: fast
	case ms < 5000:
		return "#faad14" // yellow: slow
	default:
		return "#f5222d" // red: very slow
	}
}
