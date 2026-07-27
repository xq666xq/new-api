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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// channelCurfewNotifyHandler pushes a single DingTalk card at each curfew
// boundary: 晚安 when the quiet window opens and 早安 (plus the recommendation
// list) when it closes. It exists as a separate scheduled task rather than living
// inside the monitor sweep because the sweep is entirely skipped during curfew —
// so nothing inside it could ever observe the "curfew ended" edge. Keying off a
// persisted phase (see model.GetChannelCurfewPhase) lets this fire exactly once
// per boundary, survive restarts, and dedup across masters via the task lease.
type channelCurfewNotifyHandler struct{}

func (channelCurfewNotifyHandler) Type() string { return model.SystemTaskTypeCurfewNotify }

// currentCurfewPhase folds the two master switches and the clock into a single
// phase. "active" (the quiet window) requires monitoring enabled AND curfew
// enabled AND the current local time inside the window; anything else is
// "inactive". Tying the phase to both switches keeps the card text truthful in
// the normal operating mode (monitoring on, curfew on): 晚安 means probing really
// paused, 早安 means it really resumed. Toggling a switch mid-window is a rare
// admin action; the phase simply self-heals on the next observation.
func currentCurfewPhase() string {
	if operation_setting.IsChannelMonitorEnabled() && operation_setting.IsChannelMonitorCurfewActive(time.Now()) {
		return model.CurfewPhaseActive
	}
	return model.CurfewPhaseInactive
}

// Enabled folds the "is a curfew boundary pending?" check into enablement so the
// scheduler only creates a row at the two daily transitions (and once on first
// startup to seed the phase), never one row per tick. A DB read error is treated
// as "not pending" so a transient failure can't spam boundary cards.
func (channelCurfewNotifyHandler) Enabled() bool {
	persisted, err := model.GetChannelCurfewPhase()
	if err != nil {
		common.SysError("channel curfew notify: read phase failed: " + err.Error())
		return false
	}
	return persisted != currentCurfewPhase()
}

// Interval matches the monitor scheduler tick: it is the resolution at which a
// crossed boundary is noticed, which is far finer than the once-per-boundary
// cadence Enabled() actually gates row creation to.
func (channelCurfewNotifyHandler) Interval() time.Duration { return 15 * time.Second }

func (channelCurfewNotifyHandler) NewPayload() any { return nil }

func (channelCurfewNotifyHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	persisted, err := model.GetChannelCurfewPhase()
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	current := currentCurfewPhase()
	if persisted == current {
		// Another node already handled this boundary between scheduling and now;
		// nothing to do.
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, nil, nil)
		return
	}
	// Persist the new phase first so the boundary is recorded even if the card
	// send fails: notifications are best-effort here (as everywhere in this
	// codebase), and a rare missed card is far better than a double send on retry.
	if err := model.SetChannelCurfewPhase(current); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	// An empty persisted phase means this is the first observation ever (fresh
	// install / just-upgraded install): record the phase silently so we never emit
	// a boundary card for a transition whose start we never saw.
	if persisted != "" {
		notifyCurfewBoundaryDingTalk(current)
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, nil, nil)
}

// notifyCurfewBoundaryDingTalk sends the 晚安/早安 card for a just-crossed curfew
// boundary. It reuses the managed policy's DingTalk configuration (one place for
// operators to configure the robot) and is best-effort: absent config is a no-op
// and any transport error is swallowed into the log, mirroring every other
// DingTalk path. `newPhase` is the phase just entered — active = curfew started,
// inactive = curfew ended.
func notifyCurfewBoundaryDingTalk(newPhase string) {
	setting := operation_setting.GetManagedPolicySetting()
	if setting == nil || !setting.DingTalkEnabled || strings.TrimSpace(setting.DingTalkWebhookURL) == "" {
		return
	}
	title, markdown := buildCurfewDingTalkCard(newPhase)
	webhook := setting.DingTalkWebhookURL
	secret := setting.DingTalkSecret
	if err := service.SendDingTalkActionCard(webhook, secret, title, markdown, ""); err != nil {
		common.SysError(fmt.Sprintf("channel curfew notify: dingtalk send failed phase=%s: %v", newPhase, err))
	}
}

// buildCurfewDingTalkCard composes the title/markdown for a curfew boundary card.
// The curfew-start (晚安) card is a short good-night note; the curfew-end (早安)
// card additionally carries the recommendation list so operators wake up to the
// currently recommended models. The empty-list case still renders the section
// title with a friendly "暂无可用模型" fallback (handled in appendRecommendationSection).
func buildCurfewDingTalkCard(newPhase string) (string, string) {
	if newPhase == model.CurfewPhaseActive {
		title := "🌙 晚安，小灵要休息啦！"
		var b strings.Builder
		b.WriteString("## 🌙 晚安，小灵要休息啦！\n\n")
		b.WriteString("夜间宵禁模式已开启，小灵要去睡觉啦 💤\n\n")
		b.WriteString("监控已暂停，你也早点休息哦！\n")
		return title, b.String()
	}
	title := "☀️ 早安，小灵已上线！"
	var b strings.Builder
	b.WriteString("## ☀️ 早安，小灵已上线！\n\n")
	b.WriteString("早安，又是元气满满的一天 ☀️\n\n")
	b.WriteString("模型监控已开启，请尽情创作喵！🐾\n")
	appendRecommendationSection(&b)
	return title, b.String()
}
