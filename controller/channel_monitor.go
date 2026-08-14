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
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	monitorBodyJSONMaxBytes = 256 << 10
	monitorHeaderMaxCount   = 32
)

var supportedMonitorEndpointTypes = map[string]bool{
	"auto":                              true,
	string(constant.EndpointTypeOpenAI): true,
	string(constant.EndpointTypeOpenAIResponse):        true,
	string(constant.EndpointTypeOpenAIResponseCompact): true,
	string(constant.EndpointTypeAnthropic):             true,
	string(constant.EndpointTypeGemini):                true,
	string(constant.EndpointTypeJinaRerank):            true,
	string(constant.EndpointTypeImageGeneration):       true,
	string(constant.EndpointTypeEmbeddings):            true,
}

var monitorStreamIncompatibleEndpoints = map[string]bool{
	string(constant.EndpointTypeOpenAIResponseCompact): true,
	string(constant.EndpointTypeJinaRerank):            true,
	string(constant.EndpointTypeImageGeneration):       true,
	string(constant.EndpointTypeEmbeddings):            true,
}

func normalizeMonitorRequestSettings(endpointType *string, stream *bool, headers *model.JSONValue, bodyMode *string, bodyJSON *string) error {
	*endpointType = strings.TrimSpace(*endpointType)
	if *endpointType == "" {
		*endpointType = "auto"
	}
	if !supportedMonitorEndpointTypes[*endpointType] {
		return fmt.Errorf("unsupported endpoint type: %s", *endpointType)
	}
	if monitorStreamIncompatibleEndpoints[*endpointType] {
		*stream = false
	}

	*bodyMode = strings.TrimSpace(*bodyMode)
	if *bodyMode == "" {
		*bodyMode = model.MonitorBodyModeDefault
	}
	if *bodyMode != model.MonitorBodyModeDefault &&
		*bodyMode != model.MonitorBodyModeMerge &&
		*bodyMode != model.MonitorBodyModeOverride {
		return fmt.Errorf("unsupported request body mode: %s", *bodyMode)
	}
	if len(*bodyJSON) > monitorBodyJSONMaxBytes {
		return fmt.Errorf("request body cannot exceed %d bytes", monitorBodyJSONMaxBytes)
	}
	if *bodyMode != model.MonitorBodyModeDefault {
		if strings.TrimSpace(*bodyJSON) == "" {
			return errors.New("request body JSON is required for merge or override mode")
		}
		var object map[string]any
		if err := common.UnmarshalJsonStr(*bodyJSON, &object); err != nil {
			return fmt.Errorf("request body must be a valid JSON object: %w", err)
		}
		if object == nil {
			return errors.New("request body must be a JSON object")
		}
	}

	parsedHeaders := make([]model.ChannelMonitorHeader, 0)
	if len(*headers) > 0 {
		if err := common.Unmarshal(*headers, &parsedHeaders); err != nil {
			return errors.New("custom headers must be an array")
		}
	}
	if len(parsedHeaders) > monitorHeaderMaxCount {
		return fmt.Errorf("custom headers cannot exceed %d entries", monitorHeaderMaxCount)
	}
	normalizedHeaders := make([]model.ChannelMonitorHeader, 0, len(parsedHeaders))
	indexes := make(map[string]int, len(parsedHeaders))
	for _, header := range parsedHeaders {
		key := strings.TrimSpace(header.Key)
		if !validMonitorHeaderName(key) {
			return fmt.Errorf("invalid custom header name: %s", key)
		}
		if relaychannel.IsMonitorHeaderProtected(key) {
			return fmt.Errorf("custom header is protected and cannot be overridden: %s", key)
		}
		if strings.ContainsAny(header.Value, "\r\n") {
			return fmt.Errorf("custom header value contains a line break: %s", key)
		}
		if len(header.Value) > 8192 {
			return fmt.Errorf("custom header value is too long: %s", key)
		}
		canonicalKey := http.CanonicalHeaderKey(key)
		lookupKey := strings.ToLower(canonicalKey)
		entry := model.ChannelMonitorHeader{Key: canonicalKey, Value: header.Value}
		if index, ok := indexes[lookupKey]; ok {
			normalizedHeaders[index] = entry
			continue
		}
		indexes[lookupKey] = len(normalizedHeaders)
		normalizedHeaders = append(normalizedHeaders, entry)
	}
	data, err := common.Marshal(normalizedHeaders)
	if err != nil {
		return err
	}
	*headers = model.JSONValue(data)
	return nil
}

func validMonitorHeaderName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, char := range []byte(name) {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(char)) {
			continue
		}
		return false
	}
	return true
}

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

// manualProbePlan holds everything a manual probe needs after validation. Both
// the buffered (ProbeChannelMonitorNow) and streaming
// (ProbeChannelMonitorStream) endpoints resolve it the same way so the two
// entry points can never drift on which channel/model/question is probed.
type manualProbePlan struct {
	channel    *model.Channel
	config     *model.ChannelMonitorConfig
	models     []string
	questions  []*model.MonitorQuestion
	testUserID int
}

// resolveManualProbePlan validates the request and loads the channel, monitor
// config, question pool and test user. It writes the API error itself and
// returns nil when the probe must not run, so callers can simply return.
func resolveManualProbePlan(c *gin.Context, request manualChannelMonitorProbeRequest) *manualProbePlan {
	if request.ChannelId <= 0 {
		common.ApiErrorMsg(c, "缺少有效的渠道 ID")
		return nil
	}

	config, err := model.GetChannelMonitorConfig(request.ChannelId)
	if err != nil {
		common.ApiError(c, err)
		return nil
	}
	if config == nil {
		common.ApiErrorMsg(c, "该渠道尚未配置监控")
		return nil
	}
	channel, err := model.GetChannelById(request.ChannelId, true)
	if err != nil {
		common.ApiError(c, err)
		return nil
	}
	if channel == nil {
		common.ApiErrorMsg(c, "渠道不存在")
		return nil
	}
	models, modelAllowed := manualProbeModels(request.ModelName, channel.GetModels())
	if !modelAllowed {
		common.ApiErrorMsg(c, "所选模型已不属于该渠道")
		return nil
	}
	if len(models) == 0 {
		common.ApiErrorMsg(c, "该渠道没有可检测的模型")
		return nil
	}
	questions, err := model.GetAllMonitorQuestions()
	if err != nil {
		common.ApiError(c, err)
		return nil
	}
	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return nil
	}

	return &manualProbePlan{
		channel:    channel,
		config:     config,
		models:     models,
		questions:  questions,
		testUserID: testUserID,
	}
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

// buildManualProbeResult assembles the administrator-facing probe payload from a
// saved record plus the in-memory trace snapshot.
func buildManualProbeResult(
	record *model.ChannelMonitorResult,
	config *model.ChannelMonitorConfig,
	snapshot relaycommon.MonitorProbeTraceSnapshot,
) manualChannelMonitorProbeResult {
	statusCode := snapshot.ResponseStatusCode
	if statusCode == 0 {
		statusCode = record.StatusCode
	}
	return manualChannelMonitorProbeResult{
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
	}
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

	plan := resolveManualProbePlan(c, request)
	if plan == nil {
		return
	}

	results := make([]manualChannelMonitorProbeResult, 0, len(plan.models))
	for _, modelName := range plan.models {
		if err := c.Request.Context().Err(); err != nil {
			common.ApiError(c, err)
			return
		}
		questionID, questionContent := selectMonitorQuestion(plan.questions)
		trace := &relaycommon.MonitorProbeTrace{}
		record, err := executeChannelMonitorProbe(
			c.Request.Context(),
			plan.channel,
			nil,
			plan.config,
			plan.testUserID,
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
		results = append(results, buildManualProbeResult(record, plan.config, trace.Snapshot()))
	}

	common.ApiSuccess(c, results)
}

// probeStreamEvent is one server-sent event emitted while a manual probe runs.
type probeStreamEvent struct {
	name string
	data any
}

// ProbeChannelMonitorStream runs the same manual probe as ProbeChannelMonitorNow
// but streams progress to the administrator as Server-Sent Events so the console
// can print upstream output while the request is still in flight. The probe
// itself, the saved record and the trace snapshot are identical; only the
// delivery differs. Event names: start, chunk, result, error, end.
func ProbeChannelMonitorStream(c *gin.Context) {
	var request manualChannelMonitorProbeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}

	// Validate before switching to SSE so failures are still ordinary JSON errors.
	plan := resolveManualProbePlan(c, request)
	if plan == nil {
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Connection", "keep-alive")
	// Reverse proxies buffer by default, which would defeat the whole point.
	c.Header("X-Accel-Buffering", "no")

	ctx := c.Request.Context()
	// The relay stream handler may read the upstream body on its own goroutine,
	// so trace chunks are funnelled through a channel and only ever written to
	// the gin ResponseWriter by this handler's goroutine.
	events := make(chan probeStreamEvent, 512)

	go func() {
		defer close(events)
		emit := func(name string, data any) bool {
			select {
			case events <- probeStreamEvent{name: name, data: data}:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for _, modelName := range plan.models {
			if ctx.Err() != nil {
				return
			}
			questionID, questionContent := selectMonitorQuestion(plan.questions)
			stream := plan.config.Stream || plan.config.Managed
			if !emit("start", map[string]any{
				"model_name":       modelName,
				"endpoint_type":    plan.config.EndpointType,
				"stream":           stream,
				"question_id":      questionID,
				"question_content": questionContent,
				"channel_name":     plan.channel.Name,
				"channel_type":     plan.channel.Type,
			}) {
				return
			}

			trace := &relaycommon.MonitorProbeTrace{}
			trace.SetOnResponseWrite(func(chunk []byte) {
				emit("chunk", map[string]any{
					"model_name": modelName,
					"delta":      string(chunk),
				})
			})

			record, err := executeChannelMonitorProbe(
				ctx,
				plan.channel,
				nil,
				plan.config,
				plan.testUserID,
				modelName,
				questionID,
				questionContent,
				model.ChannelMonitorTriggerManual,
				trace,
			)
			// Stop feeding the channel before the snapshot so no late chunk can
			// arrive after this model's result event.
			trace.SetOnResponseWrite(nil)
			if err != nil {
				emit("error", map[string]any{
					"model_name": modelName,
					"message":    common.MaskSensitiveInfo(err.Error()),
				})
				return
			}
			if !emit("result", buildManualProbeResult(record, plan.config, trace.Snapshot())) {
				return
			}
		}
		emit("end", map[string]any{})
	}()

	for event := range events {
		payload, err := common.Marshal(event.data)
		if err != nil {
			common.SysError("channel monitor probe stream marshal failed: " + err.Error())
			continue
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.name, payload); err != nil {
			return
		}
		c.Writer.Flush()
	}
}

type triggerChannelMonitorRequest struct {
	ChannelId int `json:"channel_id"`
}

// TriggerChannelMonitorNow brings a channel's next scheduled probe forward so the
// monitor scheduler runs it on its next tick. Unlike ProbeChannelMonitorNow (an
// out-of-band diagnostic that never touches scheduling or policy), this simply
// makes the config immediately due, so the resulting sweep is a normal scheduled
// run: results are tagged scheduled and drive managed ban/recover and speed
// policy exactly as the regular cadence would. It requires monitoring to be
// enabled, not in curfew, and the channel's monitor config to be enabled.
func TriggerChannelMonitorNow(c *gin.Context) {
	var request triggerChannelMonitorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.ChannelId <= 0 {
		common.ApiErrorMsg(c, "缺少有效的渠道 ID")
		return
	}
	if !operation_setting.IsChannelMonitorEnabled() {
		common.ApiErrorMsg(c, "监控总开关已关闭，无法触发探测")
		return
	}
	if operation_setting.IsChannelMonitorCurfewActive(time.Now()) {
		common.ApiErrorMsg(c, "当前处于宵禁时间，暂停探测")
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
	if !config.Enabled {
		common.ApiErrorMsg(c, "该渠道监控未开启")
		return
	}
	affected, err := model.AdvanceChannelMonitorConfigDue(request.ChannelId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if affected == 0 {
		common.ApiErrorMsg(c, "该渠道监控未开启")
		return
	}
	common.ApiSuccess(c, nil)
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

// GetChannelMonitorList 返回监控列表。只有在渠道列表里保存过检测配置（测试端点/
// 模版）的渠道才会出现——列表是监控配置表的投影，不再自动同步全部渠道，因此删除
// 一条监控配置就等于把该渠道移出列表。
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

	rows := make([]channelMonitorRow, 0, len(configs))
	for _, ch := range channels {
		cfg, configured := configByChannel[ch.Id]
		if !configured {
			continue
		}
		row := channelMonitorRow{
			Id:            ch.Id,
			Name:          ch.Name,
			Type:          ch.Type,
			Group:         ch.Group,
			Models:        splitModels(ch.Models),
			Priority:      ch.Priority,
			Config:        cfg,
			LastCheckedAt: cfg.LastCheckedAt,
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

func resolveMonitorConfigTemplate(cfg *model.ChannelMonitorConfig) error {
	cfg.TemplateName = strings.TrimSpace(cfg.TemplateName)
	if cfg.TemplateId <= 0 && cfg.TemplateName == "" {
		cfg.TemplateId = 0
		return nil
	}

	var template *model.MonitorTemplate
	var err error
	if cfg.TemplateId > 0 {
		template, err = model.GetMonitorTemplate(cfg.TemplateId)
	} else {
		template, err = model.GetMonitorTemplateByName(cfg.TemplateName)
	}
	if err != nil {
		return err
	}
	if template == nil {
		return errors.New("monitor template does not exist")
	}
	cfg.TemplateId = template.Id
	cfg.TemplateName = template.Name
	return nil
}

// normalizeMonitorConfig validates request settings and normalizes scheduler fields.
func normalizeMonitorConfig(cfg *model.ChannelMonitorConfig) error {
	if cfg.ChannelId <= 0 {
		return errors.New("缺少渠道 ID")
	}
	if err := normalizeMonitorRequestSettings(
		&cfg.EndpointType,
		&cfg.Stream,
		&cfg.Headers,
		&cfg.BodyMode,
		&cfg.BodyJson,
	); err != nil {
		return err
	}
	if err := resolveMonitorConfigTemplate(cfg); err != nil {
		return err
	}
	if err := normalizeMonitorScheduleSettings(cfg); err != nil {
		return err
	}
	cfg.Remark = strings.TrimSpace(cfg.Remark)
	if len([]rune(cfg.Remark)) > 255 {
		return errors.New("备注不能超过 255 个字符")
	}
	return nil
}

func normalizeMonitorScheduleSettings(cfg *model.ChannelMonitorConfig) error {
	cfg.MonitorMode = strings.TrimSpace(cfg.MonitorMode)
	if cfg.MonitorMode == "" {
		cfg.MonitorMode = model.ChannelMonitorModeDefault
	}
	if cfg.MonitorMode != model.ChannelMonitorModeDefault &&
		cfg.MonitorMode != model.ChannelMonitorModeBannedOnly {
		return fmt.Errorf("unsupported monitor mode: %s", cfg.MonitorMode)
	}
	if cfg.IntervalSeconds == 0 {
		cfg.IntervalSeconds = model.ChannelMonitorDefaultIntervalSeconds
		if cfg.JitterSeconds == 0 {
			cfg.JitterSeconds = model.ChannelMonitorDefaultJitterSeconds
		}
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
	return nil
}

func GetChannelMonitorConfig(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "invalid channel ID")
		return
	}
	config, err := model.GetChannelMonitorConfig(channelID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if config != nil && config.TemplateId == 0 && strings.TrimSpace(config.TemplateName) != "" {
		template, err := model.GetMonitorTemplateByName(config.TemplateName)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if template != nil {
			config.TemplateId = template.Id
		}
	}
	common.ApiSuccess(c, config)
}

// SaveChannelMonitorConfig 新增或更新单个渠道的监控配置。
func SaveChannelMonitorConfig(c *gin.Context) {
	var cfg model.ChannelMonitorConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := normalizeMonitorConfig(&cfg); err != nil {
		common.ApiErrorMsg(c, err.Error())
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

type channelDetectionConfigRequest struct {
	Id              int             `json:"id"`
	ChannelId       int             `json:"channel_id"`
	Enabled         *bool           `json:"enabled"`
	Managed         *bool           `json:"managed"`
	MonitorMode     *string         `json:"monitor_mode"`
	IntervalSeconds *int            `json:"interval_seconds"`
	JitterSeconds   *int            `json:"jitter_seconds"`
	MonitoredModels *[]string       `json:"monitored_models"`
	EndpointType    string          `json:"endpoint_type"`
	Stream          bool            `json:"stream"`
	TemplateId      int             `json:"template_id"`
	Headers         model.JSONValue `json:"headers"`
	BodyMode        string          `json:"body_mode"`
	BodyJson        string          `json:"body_json"`
}

// DeleteChannelMonitorConfig removes one channel from the monitor list. Because
// the list is a projection of the monitor config table, dropping the config row is
// the removal. The model layer also clears managed state and restores the ability
// rows, so the channel cache must be rebuilt for the overlay to forget any ban or
// speed-tier priority it was still serving.
func DeleteChannelMonitorConfig(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "invalid channel ID")
		return
	}
	if err := model.DeleteChannelMonitorConfigByChannel(channelID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "monitor config does not exist")
			return
		}
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	common.ApiSuccess(c, nil)
}

// SaveChannelDetectionConfig updates the request snapshot and any monitoring
// controls explicitly sent by the channel dialog. Older clients omit those
// optional controls, preserving existing scheduler and managed-policy fields.
func SaveChannelDetectionConfig(c *gin.Context) {
	var request channelDetectionConfigRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.ChannelId <= 0 {
		common.ApiErrorMsg(c, "invalid channel ID")
		return
	}
	channel, err := model.GetChannelById(request.ChannelId, false)
	if err != nil || channel == nil {
		common.ApiErrorMsg(c, "channel does not exist")
		return
	}

	config, err := model.GetChannelMonitorConfig(request.ChannelId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if config == nil {
		config = &model.ChannelMonitorConfig{
			ChannelId:       request.ChannelId,
			MonitorMode:     model.ChannelMonitorModeDefault,
			IntervalSeconds: model.ChannelMonitorDefaultIntervalSeconds,
			JitterSeconds:   model.ChannelMonitorDefaultJitterSeconds,
		}
	}
	if request.Enabled != nil {
		config.Enabled = *request.Enabled
	}
	if request.Managed != nil {
		config.Managed = *request.Managed
	}
	if request.MonitorMode != nil {
		config.MonitorMode = *request.MonitorMode
	}
	if request.IntervalSeconds != nil {
		config.IntervalSeconds = *request.IntervalSeconds
	}
	if request.JitterSeconds != nil {
		config.JitterSeconds = *request.JitterSeconds
	}
	if request.MonitoredModels != nil {
		monitoredModels := intersectModels(*request.MonitoredModels, channel.GetModels())
		if err := config.SetMonitoredModels(monitoredModels); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	config.EndpointType = request.EndpointType
	config.Stream = request.Stream
	config.TemplateId = request.TemplateId
	config.TemplateName = ""
	config.Headers = request.Headers
	config.BodyMode = request.BodyMode
	config.BodyJson = request.BodyJson

	if err := normalizeMonitorRequestSettings(
		&config.EndpointType,
		&config.Stream,
		&config.Headers,
		&config.BodyMode,
		&config.BodyJson,
	); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := resolveMonitorConfigTemplate(config); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := normalizeMonitorScheduleSettings(config); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := model.UpsertChannelMonitorConfig(config); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, config)
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

func normalizeMonitorTemplate(template *model.MonitorTemplate) error {
	template.Name = strings.TrimSpace(template.Name)
	if template.Name == "" {
		return errors.New("template name is required")
	}
	if utf8.RuneCountInString(template.Name) > 64 {
		return errors.New("template name cannot exceed 64 characters")
	}
	template.Description = strings.TrimSpace(template.Description)
	if utf8.RuneCountInString(template.Description) > 255 {
		return errors.New("template description cannot exceed 255 characters")
	}
	return normalizeMonitorRequestSettings(
		&template.EndpointType,
		&template.Stream,
		&template.Headers,
		&template.BodyMode,
		&template.BodyJson,
	)
}

// CreateMonitorTemplate 创建监控模版。
func CreateMonitorTemplate(c *gin.Context) {
	var tpl model.MonitorTemplate
	if err := c.ShouldBindJSON(&tpl); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := normalizeMonitorTemplate(&tpl); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if dup, err := model.IsMonitorTemplateNameDuplicated(0, tpl.Name); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "模版名称已存在")
		return
	}
	if err := model.InsertMonitorTemplate(&tpl); err != nil {
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
	if pathID := strings.TrimSpace(c.Param("id")); pathID != "" {
		id, err := strconv.Atoi(pathID)
		if err != nil || id <= 0 {
			common.ApiErrorMsg(c, "invalid template ID")
			return
		}
		tpl.Id = id
	}
	if tpl.Id <= 0 {
		common.ApiErrorMsg(c, "缺少模版 ID")
		return
	}
	if err := normalizeMonitorTemplate(&tpl); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if dup, err := model.IsMonitorTemplateNameDuplicated(tpl.Id, tpl.Name); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "模版名称已存在")
		return
	}
	if err := model.UpdateMonitorTemplate(&tpl); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "模版不存在")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &tpl)
}

// DeleteMonitorTemplate 删除监控模版。
func DeleteMonitorTemplate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid template ID")
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
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid template ID")
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
	// Curfew is an optional daily quiet window; all three fields are optional and
	// only applied when present, so an old client sending just `enabled` keeps the
	// existing curfew config untouched.
	CurfewEnabled *bool   `json:"curfew_enabled"`
	CurfewStart   *string `json:"curfew_start"`
	CurfewEnd     *string `json:"curfew_end"`
	// ProbeTimeoutSeconds bounds a single probe; optional so an old client omitting
	// it keeps the stored value. Clamped to the setting's safe range before saving.
	ProbeTimeoutSeconds *int `json:"probe_timeout_seconds"`
	// ProbeConcurrency bounds the number of channel/model probes in flight for one
	// scheduled task. Optional so older clients preserve the stored value.
	ProbeConcurrency *int `json:"probe_concurrency"`
}

// validCurfewTime reports whether a string is a well-formed 24-hour "HH:MM" time,
// the format the scheduler parses to decide the quiet window.
func validCurfewTime(value string) bool {
	_, err := time.Parse("15:04", value)
	return err == nil
}

// UpdateChannelMonitorSetting persists the global monitor scheduler switch and
// optional curfew window, updating the live setting through the standard option
// pipeline.
func UpdateChannelMonitorSetting(c *gin.Context) {
	var req channelMonitorSettingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	values := map[string]string{
		"channel_monitor_setting.enabled": strconv.FormatBool(*req.Enabled),
	}
	// Seed the curfew fields from the live setting so a partial update never wipes
	// the other curfew fields.
	current := operation_setting.GetChannelMonitorSetting()
	curfewEnabled := current.CurfewEnabled
	curfewStart := current.CurfewStart
	curfewEnd := current.CurfewEnd
	if req.CurfewEnabled != nil {
		curfewEnabled = *req.CurfewEnabled
	}
	if req.CurfewStart != nil {
		curfewStart = strings.TrimSpace(*req.CurfewStart)
	}
	if req.CurfewEnd != nil {
		curfewEnd = strings.TrimSpace(*req.CurfewEnd)
	}
	// When curfew is on, both bounds must be valid HH:MM and describe a non-empty
	// window; an equal start/end has no meaningful duration and is rejected so the
	// switch can't silently do nothing.
	if curfewEnabled {
		if !validCurfewTime(curfewStart) || !validCurfewTime(curfewEnd) {
			common.ApiErrorMsg(c, "宵禁时间格式无效，需为 HH:MM")
			return
		}
		if curfewStart == curfewEnd {
			common.ApiErrorMsg(c, "宵禁开始与结束时间不能相同")
			return
		}
	}
	values["channel_monitor_setting.curfew_enabled"] = strconv.FormatBool(curfewEnabled)
	values["channel_monitor_setting.curfew_start"] = curfewStart
	values["channel_monitor_setting.curfew_end"] = curfewEnd
	// Seed the probe timeout from the live value, then clamp any provided override
	// to the setting's safe range so a hand-edited or out-of-range value can't
	// disable the timeout or set an absurd one.
	probeTimeout := current.ProbeTimeoutSeconds
	if req.ProbeTimeoutSeconds != nil {
		probeTimeout = *req.ProbeTimeoutSeconds
	}
	if probeTimeout < operation_setting.MonitorProbeTimeoutMinSeconds {
		probeTimeout = operation_setting.MonitorProbeTimeoutMinSeconds
	}
	if probeTimeout > operation_setting.MonitorProbeTimeoutMaxSeconds {
		probeTimeout = operation_setting.MonitorProbeTimeoutMaxSeconds
	}
	values["channel_monitor_setting.probe_timeout_seconds"] = strconv.Itoa(probeTimeout)
	probeConcurrency := operation_setting.GetChannelMonitorProbeConcurrency()
	if req.ProbeConcurrency != nil {
		probeConcurrency = *req.ProbeConcurrency
	}
	if probeConcurrency < 0 {
		probeConcurrency = operation_setting.MonitorProbeConcurrencyDefault
	} else if probeConcurrency > 0 && probeConcurrency < operation_setting.MonitorProbeConcurrencyMin {
		probeConcurrency = operation_setting.MonitorProbeConcurrencyMin
	}
	if probeConcurrency > operation_setting.MonitorProbeConcurrencyMax {
		probeConcurrency = operation_setting.MonitorProbeConcurrencyMax
	}
	values["channel_monitor_setting.probe_concurrency"] = strconv.Itoa(probeConcurrency)
	if err := model.UpdateOptionsBulk(values); err != nil {
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
	ErrorTriggerProbeEnabled  bool   `json:"error_trigger_probe_enabled"`
	ErrorProbeThreshold       int    `json:"error_probe_threshold"`
	ErrorProbeWindowSeconds   int    `json:"error_probe_window_seconds"`
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
	if req.ErrorProbeThreshold < 1 {
		req.ErrorProbeThreshold = 1
	}
	if req.ErrorProbeWindowSeconds < 1 {
		req.ErrorProbeWindowSeconds = 1
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
		"managed_policy_setting.error_trigger_probe_enabled":  strconv.FormatBool(req.ErrorTriggerProbeEnabled),
		"managed_policy_setting.error_probe_threshold":        strconv.Itoa(req.ErrorProbeThreshold),
		"managed_policy_setting.error_probe_window_seconds":   strconv.Itoa(req.ErrorProbeWindowSeconds),
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
