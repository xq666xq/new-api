package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"golang.org/x/sync/errgroup"
)

// RegisterScheduledSystemTasks wires the periodic channel test, upstream model
// update, and async task polling (Midjourney / Suno / video) jobs into the
// system task framework so a DB lease dedups execution across multiple master
// instances and each run is recorded as one task row. Call this before
// service.StartSystemTaskRunner.
func RegisterScheduledSystemTasks() {
	service.RegisterSystemTaskHandler(channelTestHandler{})
	service.RegisterSystemTaskHandler(channelMonitorHandler{})
	service.RegisterSystemTaskHandler(channelCurfewNotifyHandler{})
	service.RegisterSystemTaskHandler(modelUpdateHandler{})
	service.RegisterSystemTaskHandler(midjourneyPollHandler{})
	service.RegisterSystemTaskHandler(asyncTaskPollHandler{})
}

type channelMonitorHandler struct{}

func (channelMonitorHandler) Type() string { return model.SystemTaskTypeChannelMonitor }

// Enabled folds the "is any channel due right now?" check into enablement so the
// scheduler creates a channel_monitor row only when there is real probing work.
// This keeps row creation at roughly the actual probe cadence instead of one row
// every tick, which matters because system_tasks has no retention cleanup.
func (channelMonitorHandler) Enabled() bool {
	if !operation_setting.IsChannelMonitorEnabled() {
		return false
	}
	// Curfew pauses all probing: skip scheduling entirely while it is active so
	// no channel_monitor row is created during the quiet window.
	if operation_setting.IsChannelMonitorCurfewActive(time.Now()) {
		return false
	}
	return model.HasDueChannelMonitorConfigs(common.GetTimestamp())
}

// Interval is the scheduler tick, i.e. the finest resolution at which per-channel
// second-level intervals + jitter can take effect. 15s matches the system task
// runner's idle poll floor; sub-15s per-channel intervals are quantized to it.
func (channelMonitorHandler) Interval() time.Duration { return 15 * time.Second }

func (channelMonitorHandler) NewPayload() any { return nil }

type channelMonitorTaskResult struct {
	Channels int `json:"channels"`
	Checks   int `json:"checks"`
	Success  int `json:"success"`
	Failed   int `json:"failed"`
}

type channelMonitorProbeJob struct {
	channel         *model.Channel
	channelLoadErr  error
	config          *model.ChannelMonitorConfig
	testUserID      int
	modelName       string
	questionID      int
	questionContent string
}

type channelMonitorProbeExecutor func(context.Context, channelMonitorProbeJob) (*model.ChannelMonitorResult, error)

// executeChannelMonitorProbeBatch runs channel/model probes with bounded
// concurrency. A queued probe receives no probe timeout until its worker starts
// executing it, so waiting for a slot cannot turn an unstarted probe into a
// timeout failure. Results keep the same order as jobs for deterministic task
// summaries and tests.
func executeChannelMonitorProbeBatch(
	ctx context.Context,
	jobs []channelMonitorProbeJob,
	concurrency int,
	execute channelMonitorProbeExecutor,
) ([]*model.ChannelMonitorResult, error) {
	results := make([]*model.ChannelMonitorResult, len(jobs))
	if len(jobs) == 0 {
		return results, nil
	}
	if concurrency <= 0 {
		concurrency = len(jobs)
	}
	if concurrency > len(jobs) {
		concurrency = len(jobs)
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)
	for index := range jobs {
		index := index
		group.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}
			result, err := execute(groupCtx, jobs[index])
			if err != nil {
				return err
			}
			results[index] = result
			return nil
		})
	}
	return results, group.Wait()
}

func selectMonitorQuestion(questions []*model.MonitorQuestion) (int, string) {
	validQuestions := make([]*model.MonitorQuestion, 0, len(questions))
	for _, question := range questions {
		if question == nil || strings.TrimSpace(question.Content) == "" {
			continue
		}
		validQuestions = append(validQuestions, question)
	}
	if len(validQuestions) == 0 {
		return 0, defaultMonitorProbeQuestion
	}
	question := validQuestions[common.GetRandomInt(len(validQuestions))]
	return question.Id, strings.TrimSpace(question.Content)
}

// executeChannelMonitorProbe is the single model-level probe path shared by
// scheduler and manual diagnostics. Both assemble and relay the same request;
// manual callers only add an in-memory trace and a non-policy trigger marker.
func executeChannelMonitorProbe(
	ctx context.Context,
	channel *model.Channel,
	channelLoadErr error,
	config *model.ChannelMonitorConfig,
	testUserID int,
	modelName string,
	questionID int,
	questionContent string,
	triggerType string,
	trace *relaycommon.MonitorProbeTrace,
) (*model.ChannelMonitorResult, error) {
	startedAt := time.Now()
	endpointType := config.EndpointType
	if endpointType == "auto" {
		endpointType = ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stream := config.Stream || config.Managed
	result := testResult{localErr: channelLoadErr}
	if channelLoadErr == nil && channel != nil {
		// Bound each probe with an independent timeout so a hung or very slow
		// upstream is cancelled and recorded as a failure instead of stalling the
		// serial sweep for minutes. This is separate from RELAY_TIMEOUT so probe
		// health checks can be tightened without shortening real forwarding. The
		// child context is derived from the caller's, so scheduler cancellation
		// (task stop) and the manual request context still apply.
		probeCtx, cancel := context.WithTimeout(ctx, operation_setting.GetChannelMonitorProbeTimeout())
		defer cancel()
		result = testChannelWithMonitorTrace(
			probeCtx,
			channel,
			testUserID,
			modelName,
			endpointType,
			stream,
			config,
			questionContent,
			trace,
		)
	} else if channelLoadErr == nil {
		result.localErr = fmt.Errorf("channel %d not found", config.ChannelId)
	}

	record := &model.ChannelMonitorResult{
		ChannelId:       config.ChannelId,
		ModelName:       modelName,
		TriggerType:     triggerType,
		QuestionId:      questionID,
		QuestionContent: questionContent,
		Success:         result.localErr == nil && result.newAPIError == nil,
		LatencyMs:       time.Since(startedAt).Milliseconds(),
		TtftMs:          result.ttftMs,
		CheckedAt:       common.GetTimestamp(),
	}
	if result.newAPIError != nil {
		record.StatusCode = result.newAPIError.StatusCode
		record.ErrorMessage = result.newAPIError.MaskSensitiveErrorWithStatusCode()
	} else if result.localErr != nil {
		record.ErrorMessage = common.MaskSensitiveInfo(result.localErr.Error())
	}
	if err := model.InsertChannelMonitorResult(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (channelMonitorHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary, err := runChannelMonitorTask(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// nextChannelMonitorCheckAt chooses between the normal interval+jitter cadence
// and the faster confirmation cadence used while a managed model is accumulating
// consecutive ban/recovery evidence.
func nextChannelMonitorCheckAt(
	config *model.ChannelMonitorConfig,
	finishedAt int64,
	setting *operation_setting.ManagedPolicySetting,
	states map[string]*model.ChannelManagedState,
) int64 {
	nextCheckAt := config.NextProbeAt(finishedAt)
	if !config.Managed || setting == nil || !setting.BanEnabled {
		return nextCheckAt
	}

	confirmInterval := int64(setting.BanConfirmIntervalSeconds)
	if confirmInterval < operation_setting.ManagedConfirmIntervalFloorSeconds {
		confirmInterval = operation_setting.ManagedConfirmIntervalFloorSeconds
	}
	confirmationNextAt := int64(0)
	for _, modelName := range config.GetMonitoredModels() {
		state := states[modelName]
		if state == nil || state.ConfirmCount == 0 {
			continue
		}
		candidate := state.LastConfirmProbeAt + confirmInterval
		if state.LastConfirmProbeAt == 0 || candidate <= finishedAt {
			candidate = finishedAt + 1
		}
		if confirmationNextAt == 0 || candidate < confirmationNextAt {
			confirmationNextAt = candidate
		}
	}
	if confirmationNextAt > 0 {
		return confirmationNextAt
	}
	return nextCheckAt
}

func runChannelMonitorTask(ctx context.Context) (channelMonitorTaskResult, error) {
	summary := channelMonitorTaskResult{}
	if !operation_setting.IsChannelMonitorEnabled() {
		return summary, nil
	}
	// Curfew pauses all probing. Checked here too (not just in Enabled) because a
	// task row may already have been leased when the quiet window began.
	if operation_setting.IsChannelMonitorCurfewActive(time.Now()) {
		return summary, nil
	}
	now := common.GetTimestamp()
	if err := model.DeleteOldChannelMonitorResults(now - 30*24*60*60); err != nil {
		return summary, err
	}
	configs, err := model.GetDueChannelMonitorConfigs(now)
	if err != nil {
		return summary, err
	}
	if len(configs) == 0 {
		return summary, nil
	}
	questions, err := model.GetAllMonitorQuestions()
	if err != nil {
		return summary, err
	}
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return summary, err
	}
	finishedAtByChannel := make(map[int]int64, len(configs))
	jobs := make([]channelMonitorProbeJob, 0)
	for _, config := range configs {
		if !operation_setting.IsChannelMonitorEnabled() {
			return summary, nil
		}
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		models := config.GetMonitoredModels()
		if len(models) == 0 {
			// 没有勾选任何模型：稍后仍会推进 next_check_at，避免该配置
			// 永远到期、每趟被反复选中。
			finishedAtByChannel[config.ChannelId] = now
			continue
		}
		channel, channelErr := model.GetChannelById(config.ChannelId, true)
		summary.Channels++
		for _, modelName := range models {
			questionId, questionContent := selectMonitorQuestion(questions)
			jobs = append(jobs, channelMonitorProbeJob{
				channel:         channel,
				channelLoadErr:  channelErr,
				config:          config,
				testUserID:      testUserID,
				modelName:       modelName,
				questionID:      questionId,
				questionContent: questionContent,
			})
		}
	}

	records, probeErr := executeChannelMonitorProbeBatch(
		ctx,
		jobs,
		operation_setting.GetChannelMonitorProbeConcurrency(),
		func(probeCtx context.Context, job channelMonitorProbeJob) (*model.ChannelMonitorResult, error) {
			return executeChannelMonitorProbe(
				probeCtx,
				job.channel,
				job.channelLoadErr,
				job.config,
				job.testUserID,
				job.modelName,
				job.questionID,
				job.questionContent,
				model.ChannelMonitorTriggerScheduled,
				nil,
			)
		},
	)
	for _, record := range records {
		if record == nil {
			continue
		}
		summary.Checks++
		if record.Success {
			summary.Success++
		} else {
			summary.Failed++
		}
		if record.CheckedAt > finishedAtByChannel[record.ChannelId] {
			finishedAtByChannel[record.ChannelId] = record.CheckedAt
		}
	}
	if probeErr != nil {
		return summary, probeErr
	}
	// Apply the channel-managed policy once the whole sweep is done: the speed
	// stage ranks channels against each other, so it needs every managed channel's
	// fresh probe data in this cycle. Best-effort inside the engine — it never
	// fails the probe sweep.
	runChannelManagedPolicy()

	// Persist schedules only after the policy has consumed this sweep. A channel
	// with a pending ban/recovery confirmation is probed again at the configured
	// confirmation interval; once the threshold is fully confirmed (or the latest
	// result agrees with the stable state), ConfirmCount returns to zero and the
	// channel resumes its normal interval+jitter schedule.
	setting := operation_setting.GetManagedPolicySetting()
	for _, config := range configs {
		finishedAt := finishedAtByChannel[config.ChannelId]
		if finishedAt == 0 {
			finishedAt = common.GetTimestamp()
		}
		var states map[string]*model.ChannelManagedState
		if config.Managed && setting.BanEnabled {
			states, err = model.GetChannelManagedStatesByChannel(config.ChannelId)
			if err != nil {
				return summary, err
			}
		}
		nextCheckAt := nextChannelMonitorCheckAt(config, finishedAt, setting, states)
		if err := model.UpdateChannelMonitorSchedule(config.ChannelId, finishedAt, nextCheckAt); err != nil {
			return summary, err
		}
	}
	return summary, nil
}

// channelTestHandler runs the scheduled "test all channels" job. Enablement and
// cadence still come from the monitor settings; only the execution path moved
// into the system task runner.
type channelTestHandler struct{}

func (channelTestHandler) Type() string { return model.SystemTaskTypeChannelTest }

func (channelTestHandler) Enabled() bool {
	return operation_setting.GetMonitorSetting().AutoTestChannelEnabled
}

func (channelTestHandler) Interval() time.Duration {
	minutes := operation_setting.GetMonitorSetting().AutoTestChannelMinutes
	if minutes <= 0 {
		minutes = 10
	}
	return time.Duration(minutes * float64(time.Minute))
}

func (channelTestHandler) NewPayload() any { return nil }

// channelTestTaskPayload controls one channel_test run. A nil/empty payload is a
// scheduled run, which uses the configured monitor ChannelTestMode and does not
// notify. A manual "test all channels" trigger sets Mode=scheduled_all and
// Notify=true to reproduce the legacy manual behavior (test every channel and
// notify root on completion).
type channelTestTaskPayload struct {
	Mode   string `json:"mode,omitempty"`
	Notify bool   `json:"notify,omitempty"`
}

func (channelTestHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := channelTestTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary, err := runChannelTestTask(ctx, payload.Mode, payload.Notify, service.NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// modelUpdateHandler runs the scheduled upstream model update detection job.
type modelUpdateHandler struct{}

func (modelUpdateHandler) Type() string { return model.SystemTaskTypeModelUpdate }

func (modelUpdateHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", true)
}

func (modelUpdateHandler) Interval() time.Duration {
	intervalMinutes := common.GetEnvOrDefault(
		"CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES",
		channelUpstreamModelUpdateTaskDefaultIntervalMinutes,
	)
	if intervalMinutes < 1 {
		intervalMinutes = channelUpstreamModelUpdateTaskDefaultIntervalMinutes
	}
	return time.Duration(intervalMinutes) * time.Minute
}

func (modelUpdateHandler) NewPayload() any { return nil }

// modelUpdateTaskPayload controls one model_update run. A scheduled run
// (Manual=false) respects the per-channel minimum check interval and may
// auto-apply detected models when a channel has auto-sync enabled. A manual
// "detect all" trigger sets Manual=true to reproduce the legacy detect-all
// semantics: force a re-check regardless of the interval and never auto-apply,
// so the admin reviews and applies changes explicitly.
type modelUpdateTaskPayload struct {
	Manual bool `json:"manual,omitempty"`
}

func (modelUpdateHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := modelUpdateTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary := runChannelUpstreamModelUpdateTaskOnce(ctx, payload.Manual, !payload.Manual, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// midjourneyPollHandler runs one Midjourney polling pass per scheduled run.
// Enabled() folds the "are there unfinished tasks?" check into enablement so the
// scheduler creates no row when the system is idle; only when at least one
// Midjourney task is in progress does a row get scheduled.
type midjourneyPollHandler struct{}

func (midjourneyPollHandler) Type() string { return model.SystemTaskTypeMidjourneyPoll }

func (midjourneyPollHandler) Enabled() bool {
	return constant.UpdateTask && model.HasUnfinishedMidjourneyTasks()
}

func (midjourneyPollHandler) Interval() time.Duration { return 15 * time.Second }

func (midjourneyPollHandler) NewPayload() any { return nil }

func (midjourneyPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := runMidjourneyTaskUpdateOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// asyncTaskPollHandler runs one async-task (Suno/video) polling pass per
// scheduled run. Like midjourneyPollHandler, Enabled() folds in the unfinished
// task existence check so an idle system schedules no rows.
type asyncTaskPollHandler struct{}

func (asyncTaskPollHandler) Type() string { return model.SystemTaskTypeAsyncTaskPoll }

func (asyncTaskPollHandler) Enabled() bool {
	return constant.UpdateTask && model.HasUnfinishedSyncTasks()
}

func (asyncTaskPollHandler) Interval() time.Duration { return 15 * time.Second }

func (asyncTaskPollHandler) NewPayload() any { return nil }

func (asyncTaskPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := service.RunTaskPollingOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

func finishSystemTaskHandler(task *model.SystemTask, runnerID string, status model.SystemTaskStatus, result any, runErr error) {
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage); err != nil {
		common.SysLog(fmt.Sprintf("system task %s failed to persist result: %v", task.TaskID, err))
	}
}
