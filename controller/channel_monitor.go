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
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

func GetChannelMonitorStatus(c *gin.Context) {
	rows, err := model.GetChannelStatusRows(c.DefaultQuery("range", "1h"), time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

// GetModelStatus is the member-facing channel-status view. It aggregates the
// per-channel health/speed sparklines by model, so a normal user sees how healthy
// and fast each model is without ever learning which channel serves it (channel
// name, group, tag, id, and type are dropped by the aggregation).
func GetModelStatus(c *gin.Context) {
	rows, err := model.GetModelStatusRows(c.DefaultQuery("range", "1h"), time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func GetChannelMonitorResultDetails(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Query("channel_id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "缺少有效的渠道 ID")
		return
	}
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		common.ApiErrorMsg(c, "缺少模型名称")
		return
	}
	startAt, _ := strconv.ParseInt(c.Query("start_at"), 10, 64)
	endAt, _ := strconv.ParseInt(c.Query("end_at"), 10, 64)
	if startAt <= 0 || endAt <= startAt {
		common.ApiErrorMsg(c, "检测时间区间无效")
		return
	}
	results, err := model.GetChannelMonitorResults(channelId, modelName, startAt, endAt)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, results)
}

type manualChannelMonitorProbeRequest struct {
	ChannelId int    `json:"channel_id"`
	ModelName string `json:"model_name"`
}

type manualChannelMonitorProbeResult struct {
	RecordId        int64                                 `json:"record_id"`
	ModelName       string                                `json:"model_name"`
	EndpointType    string                                `json:"endpoint_type"`
	Stream          bool                                  `json:"stream"`
	QuestionId      int                                   `json:"question_id"`
	QuestionContent string                                `json:"question_content"`
	Success         bool                                  `json:"success"`
	LatencyMs       int64                                 `json:"latency_ms"`
	TtftMs          int64                                 `json:"ttft_ms"`
	StatusCode      int                                   `json:"status_code"`
	ErrorMessage    string                                `json:"error_message"`
	CheckedAt       int64                                 `json:"checked_at"`
	Trace           relaycommon.MonitorProbeTraceSnapshot `json:"trace"`
}

// manualProbeModels resolves the exact channel model requested by the admin.
// An empty model keeps compatibility with older clients that tested all
// channel models, while the current console always requests one model. Model
// monitoring selection only controls scheduled probes and managed policy.
func manualProbeModels(requestedModel string, channelModels []string) ([]string, bool) {
	availableModels := make([]string, 0, len(channelModels))
	seen := make(map[string]bool, len(channelModels))
	for _, modelName := range channelModels {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" || seen[modelName] {
			continue
		}
		seen[modelName] = true
		availableModels = append(availableModels, modelName)
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return availableModels, true
	}
	for _, modelName := range availableModels {
		if modelName == requestedModel {
			return []string{modelName}, true
		}
	}
	return nil, false
}

// ProbeChannelMonitorNow runs the requested channel model on one
// channel through the exact scheduler relay path. It is an explicit
// administrator diagnostic, so scheduler master/channel/model switches do not
// gate it. Only the basic probe record is saved; the final upstream
// request/response trace lives in memory for this response and does not update
// NextCheckAt or invoke policy.
func ProbeChannelMonitorNow(c *gin.Context) {
	// The response may contain administrator-visible upstream credentials and
	// raw payloads. Keep every outcome out of browser/proxy caches; the frontend
	// also discards successful trace data as soon as the one-time dialog closes.
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")

	var request manualChannelMonitorProbeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.ChannelId <= 0 {
		common.ApiErrorMsg(c, "缺少有效的渠道 ID")
		return
	}

	config, err := model.GetChannelMonitorConfig(request.ChannelId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if config == nil {
		common.ApiErrorMsg(c, "该渠道尚未配置监控")
		return
	}
	channel, err := model.GetChannelById(request.ChannelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel == nil {
		common.ApiErrorMsg(c, "渠道不存在")
		return
	}
	models, modelAllowed := manualProbeModels(
		request.ModelName,
		channel.GetModels(),
	)
	if !modelAllowed {
		common.ApiErrorMsg(c, "所选模型已不属于该渠道")
		return
	}
	if len(models) == 0 {
		common.ApiErrorMsg(c, "该渠道没有可检测的模型")
		return
	}

	questions, err := model.GetAllMonitorQuestions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	results := make([]manualChannelMonitorProbeResult, 0, len(models))
	for _, modelName := range models {
		if err := c.Request.Context().Err(); err != nil {
			common.ApiError(c, err)
			return
		}
		questionID, questionContent := selectMonitorQuestion(questions)
		trace := &relaycommon.MonitorProbeTrace{}
		record, err := executeChannelMonitorProbe(
			c.Request.Context(),
			channel,
			nil,
			config,
			testUserID,
			modelName,
			questionID,
			questionContent,
			model.ChannelMonitorTriggerManual,
			trace,
		)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		snapshot := trace.Snapshot()
		statusCode := snapshot.ResponseStatusCode
		if statusCode == 0 {
			statusCode = record.StatusCode
		}
		results = append(results, manualChannelMonitorProbeResult{
			RecordId:        record.Id,
			ModelName:       record.ModelName,
			EndpointType:    config.EndpointType,
			Stream:          config.Stream || config.Managed,
			QuestionId:      record.QuestionId,
			QuestionContent: record.QuestionContent,
			Success:         record.Success,
			LatencyMs:       record.LatencyMs,
			TtftMs:          record.TtftMs,
			StatusCode:      statusCode,
			ErrorMessage:    record.ErrorMessage,
			CheckedAt:       record.CheckedAt,
			Trace:           snapshot,
		})
	}

	common.ApiSuccess(c, results)
}

// validMonitorBodyModes 限定请求体处理模式的合法取值。
var validMonitorBodyModes = map[string]bool{
	"default":  true,
	"merge":    true,
	"override": true,
}

const monitorQuestionMaxLength = 1000

// channelManagedModelState 是列表里单个模型的托管运行态投影：策略当前把它
// 判成正常/降级/封禁，以及当前生效的优先级。前端用它在渠道行下展开模型级状态。
type channelManagedModelState struct {
	Model            string `json:"model"`
	Banned           bool   `json:"banned"`
	PriorityManaged  bool   `json:"priority_managed"`
	ManagedPriority  int64  `json:"managed_priority"`
	OriginalPriority int64  `json:"original_priority"`
	ConfirmCount     int    `json:"confirm_count"`
}

// channelMonitorRow 是渠道监控列表的一行，把渠道基础信息与其监控配置合并返回。
type channelMonitorRow struct {
	Id            int                         `json:"id"`
	Type          int                         `json:"type"`
	Priority      int64                       `json:"priority"`
	LastCheckedAt int64                       `json:"last_checked_at"`
	Name          string                      `json:"name"`
	Group         string                      `json:"group"`
	Models        []string                    `json:"models"`
	Config        *model.ChannelMonitorConfig `json:"config"`
	// ManagedStates 仅在渠道开启托管时非空，按模型名给出策略的当前判定。
	ManagedStates []channelManagedModelState `json:"managed_states"`
}

// splitModels 把渠道逗号分隔的模型串拆成去空的模型名列表。
func splitModels(models string) []string {
	if models == "" {
		return []string{}
	}
	parts := strings.Split(models, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// intersectModels 返回 selected 中同时存在于 available 的模型名，保持 selected 的顺序并去重，
// 用于把用户勾选的监控模型收敛到渠道真实拥有的模型集合。
func intersectModels(selected, available []string) []string {
	if len(selected) == 0 || len(available) == 0 {
		return []string{}
	}
	valid := make(map[string]bool, len(available))
	for _, m := range available {
		valid[strings.TrimSpace(m)] = true
	}
	seen := make(map[string]bool, len(selected))
	result := make([]string, 0, len(selected))
	for _, m := range selected {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] || !valid[m] {
			continue
		}
		seen[m] = true
		result = append(result, m)
	}
	return result
}

// GetChannelMonitorList 返回与渠道列表同步的监控列表，每行携带（可能为空的）监控配置。
func GetChannelMonitorList(c *gin.Context) {
	channels, err := model.GetChannelMonitorListItems()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	configs, err := model.GetAllChannelMonitorConfigs()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	configByChannel := make(map[int]*model.ChannelMonitorConfig, len(configs))
	for _, cfg := range configs {
		configByChannel[cfg.ChannelId] = cfg
	}

	// Managed states are keyed by managedOverlayKey(channelId, model); the list
	// shows each monitored model's policy status (ban/priority) so operators can
	// see what the engine has done. Best-effort: a load error just omits states.
	managedStates, err := model.GetAllChannelManagedStates()
	if err != nil {
		common.SysError("failed to load channel managed states for monitor list: " + err.Error())
		managedStates = map[string]*model.ChannelManagedState{}
	}

	rows := make([]channelMonitorRow, 0, len(channels))
	for _, ch := range channels {
		row := channelMonitorRow{
			Id:       ch.Id,
			Name:     ch.Name,
			Type:     ch.Type,
			Group:    ch.Group,
			Models:   splitModels(ch.Models),
			Priority: ch.Priority,
		}
		if cfg, ok := configByChannel[ch.Id]; ok {
			row.Config = cfg
			row.LastCheckedAt = cfg.LastCheckedAt
		}
		// Project the managed state for each monitored model, if any, into the
		// list-facing view so the console can show per-model ban/priority status.
		states := make([]channelManagedModelState, 0)
		for _, m := range row.Models {
			st, ok := managedStates[model.ManagedOverlayKey(ch.Id, m)]
			if !ok {
				continue
			}
			states = append(states, channelManagedModelState{
				Model:            m,
				Banned:           st.BanState == model.ManagedBanStateBanned,
				PriorityManaged:  st.PriorityManaged,
				ManagedPriority:  st.ManagedPriority,
				OriginalPriority: st.OriginalPriority,
				ConfirmCount:     st.ConfirmCount,
			})
		}
		if len(states) > 0 {
			row.ManagedStates = states
		}
		rows = append(rows, row)
	}
	common.ApiSuccess(c, rows)
}

// normalizeMonitorConfig 校验并规整监控配置的字段取值。
func normalizeMonitorConfig(cfg *model.ChannelMonitorConfig) string {
	if cfg.ChannelId <= 0 {
		return "缺少渠道 ID"
	}
	if cfg.EndpointType == "" {
		cfg.EndpointType = "auto"
	}
	if !validMonitorBodyModes[cfg.BodyMode] {
		cfg.BodyMode = "default"
	}
	if cfg.IntervalSeconds < model.MonitorMinIntervalSeconds {
		cfg.IntervalSeconds = model.MonitorMinIntervalSeconds
	}
	if cfg.IntervalSeconds > model.MonitorMaxIntervalSeconds {
		cfg.IntervalSeconds = model.MonitorMaxIntervalSeconds
	}
	// 抖动必须非负，且严格小于间隔，保证 interval-jitter >= 1，下一次探测时间始终未来。
	if cfg.JitterSeconds < 0 {
		cfg.JitterSeconds = 0
	}
	if cfg.JitterSeconds > cfg.IntervalSeconds-1 {
		cfg.JitterSeconds = cfg.IntervalSeconds - 1
	}
	cfg.Remark = strings.TrimSpace(cfg.Remark)
	if len([]rune(cfg.Remark)) > 255 {
		return "备注不能超过 255 个字符"
	}
	return ""
}

// SaveChannelMonitorConfig 新增或更新单个渠道的监控配置。
func SaveChannelMonitorConfig(c *gin.Context) {
	var cfg model.ChannelMonitorConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		common.ApiError(c, err)
		return
	}
	if msg := normalizeMonitorConfig(&cfg); msg != "" {
		common.ApiErrorMsg(c, msg)
		return
	}
	// 确认渠道存在，避免为不存在的渠道写入孤立配置。
	channel, err := model.GetChannelById(cfg.ChannelId, false)
	if err != nil || channel == nil {
		common.ApiErrorMsg(c, "渠道不存在")
		return
	}
	// 只保留渠道真实拥有的模型，剔除已下线或非法的模型名，保持监控列表干净。
	if err := cfg.SetMonitoredModels(intersectModels(cfg.GetMonitoredModels(), channel.GetModels())); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpsertChannelMonitorConfig(&cfg); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &cfg)
}

func normalizeMonitorQuestion(question *model.MonitorQuestion) string {
	question.Content = strings.TrimSpace(question.Content)
	if question.Content == "" {
		return "检测问题不能为空"
	}
	if len([]rune(question.Content)) > monitorQuestionMaxLength {
		return "检测问题不能超过 1000 个字符"
	}
	return ""
}

// GetMonitorQuestions returns the global conversational probe question library.
func GetMonitorQuestions(c *gin.Context) {
	questions, err := model.GetAllMonitorQuestions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, questions)
}

// CreateMonitorQuestion adds one question to the global probe library.
func CreateMonitorQuestion(c *gin.Context) {
	var question model.MonitorQuestion
	if err := c.ShouldBindJSON(&question); err != nil {
		common.ApiError(c, err)
		return
	}
	if msg := normalizeMonitorQuestion(&question); msg != "" {
		common.ApiErrorMsg(c, msg)
		return
	}
	duplicated, err := model.IsMonitorQuestionContentDuplicated(0, question.Content)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if duplicated {
		common.ApiErrorMsg(c, "检测问题已存在")
		return
	}
	question.Id = 0
	if err := question.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &question)
}

// UpdateMonitorQuestion edits one existing probe question.
func UpdateMonitorQuestion(c *gin.Context) {
	var question model.MonitorQuestion
	if err := c.ShouldBindJSON(&question); err != nil {
		common.ApiError(c, err)
		return
	}
	if question.Id <= 0 {
		common.ApiErrorMsg(c, "缺少检测问题 ID")
		return
	}
	if msg := normalizeMonitorQuestion(&question); msg != "" {
		common.ApiErrorMsg(c, msg)
		return
	}
	existing, err := model.GetMonitorQuestion(question.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if existing == nil {
		common.ApiErrorMsg(c, "检测问题不存在")
		return
	}
	duplicated, err := model.IsMonitorQuestionContentDuplicated(question.Id, question.Content)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if duplicated {
		common.ApiErrorMsg(c, "检测问题已存在")
		return
	}
	question.CreatedTime = existing.CreatedTime
	if err := question.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &question)
}

// DeleteMonitorQuestion removes one question. Probe history remains readable
// because every result stores a content snapshot rather than relying on a join.
func DeleteMonitorQuestion(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "缺少有效的检测问题 ID")
		return
	}
	if err := model.DeleteMonitorQuestionByID(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// GetMonitorTemplates 返回全部监控模版。
func GetMonitorTemplates(c *gin.Context) {
	templates, err := model.GetAllMonitorTemplates()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, templates)
}

// CreateMonitorTemplate 创建监控模版。
func CreateMonitorTemplate(c *gin.Context) {
	var tpl model.MonitorTemplate
	if err := c.ShouldBindJSON(&tpl); err != nil {
		common.ApiError(c, err)
		return
	}
	if strings.TrimSpace(tpl.Name) == "" {
		common.ApiErrorMsg(c, "模版名称不能为空")
		return
	}
	if dup, err := model.IsMonitorTemplateNameDuplicated(0, tpl.Name); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "模版名称已存在")
		return
	}
	if !validMonitorBodyModes[tpl.BodyMode] {
		tpl.BodyMode = "merge"
	}
	tpl.Id = 0
	if err := tpl.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &tpl)
}

// UpdateMonitorTemplate 更新监控模版。
func UpdateMonitorTemplate(c *gin.Context) {
	var tpl model.MonitorTemplate
	if err := c.ShouldBindJSON(&tpl); err != nil {
		common.ApiError(c, err)
		return
	}
	if tpl.Id == 0 {
		common.ApiErrorMsg(c, "缺少模版 ID")
		return
	}
	if strings.TrimSpace(tpl.Name) == "" {
		common.ApiErrorMsg(c, "模版名称不能为空")
		return
	}
	if dup, err := model.IsMonitorTemplateNameDuplicated(tpl.Id, tpl.Name); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "模版名称已存在")
		return
	}
	if !validMonitorBodyModes[tpl.BodyMode] {
		tpl.BodyMode = "merge"
	}
	if err := tpl.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &tpl)
}

// DeleteMonitorTemplate 删除监控模版。
func DeleteMonitorTemplate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteMonitorTemplateByID(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// ApplyMonitorTemplate 把指定模版的快照重新应用到所有引用它的渠道配置。
func ApplyMonitorTemplate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tpl, err := model.GetMonitorTemplate(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if tpl == nil {
		common.ApiErrorMsg(c, "模版不存在")
		return
	}
	affected, err := model.ApplyTemplateToChannels(tpl)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"affected": affected})
}

// GetChannelMonitorSetting returns the global monitor scheduler switch. The
// setting only controls new probes and policy runs; status queries remain
// available and continue to use persisted monitoring data.
func GetChannelMonitorSetting(c *gin.Context) {
	common.ApiSuccess(c, operation_setting.GetChannelMonitorSetting())
}

type channelMonitorSettingUpdateRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// UpdateChannelMonitorSetting persists the global monitor scheduler switch and
// updates the live setting through the standard option pipeline.
func UpdateChannelMonitorSetting(c *gin.Context) {
	var req channelMonitorSettingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOptionsBulk(map[string]string{
		"channel_monitor_setting.enabled": strconv.FormatBool(*req.Enabled),
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, operation_setting.GetChannelMonitorSetting())
}

// GetManagedPolicySetting returns the current channel-managed policy, clamped to
// safe ranges, for the policy dialog.
func GetManagedPolicySetting(c *gin.Context) {
	common.ApiSuccess(c, operation_setting.GetManagedPolicySetting())
}

// managedPolicyUpdateRequest is the editable channel-managed policy payload. All
// fields are optional-by-value; the handler clamps them to safe ranges before
// persisting, mirroring GetManagedPolicySetting so hand-edited configs and API
// writes converge on the same valid state.
type managedPolicyUpdateRequest struct {
	BanEnabled                bool   `json:"ban_enabled"`
	ConfirmCount              int    `json:"confirm_count"`
	BanConfirmIntervalSeconds int    `json:"ban_confirm_interval_seconds"`
	SpeedEnabled              bool   `json:"speed_enabled"`
	SpeedWindow               int    `json:"speed_window"`
	TierDiffPercent           int    `json:"tier_diff_percent"`
	DingTalkEnabled           bool   `json:"dingtalk_enabled"`
	DingTalkWebhookURL        string `json:"dingtalk_webhook_url"`
	DingTalkSecret            string `json:"dingtalk_secret"`
}

// UpdateManagedPolicySetting validates and persists the channel-managed policy.
// It writes through the generic option store using the GlobalConfig key prefix so
// the in-memory setting and the DB stay in sync via the standard option pipeline.
func UpdateManagedPolicySetting(c *gin.Context) {
	var req managedPolicyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	// Clamp to safe ranges before persisting.
	if req.ConfirmCount < 1 {
		req.ConfirmCount = 1
	}
	if req.BanConfirmIntervalSeconds < operation_setting.ManagedConfirmIntervalFloorSeconds {
		req.BanConfirmIntervalSeconds = operation_setting.ManagedConfirmIntervalFloorSeconds
	}
	if req.SpeedWindow < 1 {
		req.SpeedWindow = 1
	}
	if req.TierDiffPercent < 0 {
		req.TierDiffPercent = 0
	}
	// DingTalk fields: trim and, when enabled, require a plausible https webhook so
	// a typo can't silently disable alerts. An empty/invalid URL with the switch on
	// is rejected; a disabled integration accepts any (including empty) values.
	req.DingTalkWebhookURL = strings.TrimSpace(req.DingTalkWebhookURL)
	req.DingTalkSecret = strings.TrimSpace(req.DingTalkSecret)
	if req.DingTalkEnabled {
		if !strings.HasPrefix(req.DingTalkWebhookURL, "https://") {
			common.ApiErrorMsg(c, "钉钉 Webhook 地址无效，需以 https:// 开头")
			return
		}
	}
	// Persist through the standard option pipeline; keys use the GlobalConfig
	// "managed_policy_setting." prefix so handleConfigUpdate routes them into the
	// registered live struct.
	values := map[string]string{
		"managed_policy_setting.ban_enabled":                  strconv.FormatBool(req.BanEnabled),
		"managed_policy_setting.confirm_count":                strconv.Itoa(req.ConfirmCount),
		"managed_policy_setting.ban_confirm_interval_seconds": strconv.Itoa(req.BanConfirmIntervalSeconds),
		"managed_policy_setting.speed_enabled":                strconv.FormatBool(req.SpeedEnabled),
		"managed_policy_setting.speed_window":                 strconv.Itoa(req.SpeedWindow),
		"managed_policy_setting.tier_diff_percent":            strconv.Itoa(req.TierDiffPercent),
		"managed_policy_setting.dingtalk_enabled":             strconv.FormatBool(req.DingTalkEnabled),
		"managed_policy_setting.dingtalk_webhook_url":         req.DingTalkWebhookURL,
		"managed_policy_setting.dingtalk_secret":              req.DingTalkSecret,
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, operation_setting.GetManagedPolicySetting())
}

// GetChannelRecommendations returns one maintenance row per channel, merging each
// channel's persisted recommendation weight/blurb (or the zero default). The list
// always covers every channel so a newly added channel appears automatically with
// weight 0, needing no synchronization.
func GetChannelRecommendations(c *gin.Context) {
	rows, err := model.GetChannelRecommendationRows()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

type channelRecommendationSaveRequest struct {
	Recommendations []model.ChannelRecommendationRow `json:"recommendations"`
}

// SaveChannelRecommendations persists the edited recommendation weights/blurbs.
// Rows reset to the default (weight 0, empty blurb) are dropped by the model layer
// so the table stays free of no-op rows.
func SaveChannelRecommendations(c *gin.Context) {
	var req channelRecommendationSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpsertChannelRecommendations(req.Recommendations); err != nil {
		common.ApiError(c, err)
		return
	}
	rows, err := model.GetChannelRecommendationRows()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}
