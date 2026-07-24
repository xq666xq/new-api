package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newChannelMonitorTestDB spins up an isolated in-memory SQLite database with the
// channel-monitor tables migrated, and restores global DB state on cleanup.
func newChannelMonitorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelMonitorConfig{}, &ChannelMonitorResult{}, &MonitorQuestion{}, &Option{}, &Log{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		_ = sqlDB.Close()
	})
	return db
}

func TestChannelStatusRowsUseEnabledMonitorResultsPerModel(t *testing.T) {
	db := newChannelMonitorTestDB(t)

	channel := Channel{Name: "monitored", Type: 1, Key: "test-key", Models: "model-a,model-b", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	config := ChannelMonitorConfig{ChannelId: channel.Id, Enabled: true, IntervalSeconds: 600}
	require.NoError(t, config.SetMonitoredModels([]string{"model-a", "model-b"}))
	require.NoError(t, db.Create(&config).Error)

	disabledChannel := Channel{Name: "disabled", Type: 1, Key: "test-key", Models: "model-c", Group: "default"}
	require.NoError(t, db.Create(&disabledChannel).Error)
	disabledConfig := ChannelMonitorConfig{ChannelId: disabledChannel.Id, Enabled: false, IntervalSeconds: 600}
	require.NoError(t, disabledConfig.SetMonitoredModels([]string{"model-c"}))
	require.NoError(t, db.Create(&disabledConfig).Error)

	now := time.Unix(1_800_000_180, 0)
	require.NoError(t, db.Create(&[]ChannelMonitorResult{
		{ChannelId: channel.Id, ModelName: "model-a", QuestionId: 7, QuestionContent: "Explain HTTP 404 briefly.", Success: true, LatencyMs: 120, CheckedAt: now.Unix() - 60},
		{ChannelId: channel.Id, ModelName: "model-a", Success: false, LatencyMs: 280, CheckedAt: now.Unix() - 30},
		{ChannelId: channel.Id, ModelName: "model-b", Success: true, LatencyMs: 90, CheckedAt: now.Unix() - 20},
		{ChannelId: disabledChannel.Id, ModelName: "model-c", Success: true, LatencyMs: 50, CheckedAt: now.Unix() - 10},
	}).Error)

	rows, err := GetChannelStatusRows("6h", now)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	assert.Equal(t, "model-a", rows[0].Model)
	assert.Equal(t, 2, rows[0].Requests)
	assert.Equal(t, 50.0, rows[0].SuccessRate)
	assert.Equal(t, 200, rows[0].AvgResponseMs)
	assert.Equal(t, "degraded", rows[0].Health)
	assert.Equal(t, "model-b", rows[1].Model)
	assert.Equal(t, 1, rows[1].Requests)
	assert.Equal(t, 100.0, rows[1].SuccessRate)
	assert.Equal(t, "healthy", rows[1].Health)

	details, err := GetChannelMonitorResults(
		channel.Id,
		"model-a",
		now.Unix()-70,
		now.Unix(),
	)
	require.NoError(t, err)
	require.Len(t, details, 2)
	assert.GreaterOrEqual(t, details[0].CheckedAt, details[1].CheckedAt)
	assert.Equal(t, "Explain HTTP 404 briefly.", details[1].QuestionContent)
}

func TestMonitorQuestionCRUDPreservesNormalizedContent(t *testing.T) {
	newChannelMonitorTestDB(t)

	question := MonitorQuestion{Content: "Explain an API health check briefly."}
	require.NoError(t, question.Insert())
	assert.Positive(t, question.Id)
	assert.Positive(t, question.CreatedTime)
	assert.Equal(t, question.CreatedTime, question.UpdatedTime)

	questions, err := GetAllMonitorQuestions()
	require.NoError(t, err)
	require.Len(t, questions, 1)
	assert.Equal(t, question.Content, questions[0].Content)

	duplicated, err := IsMonitorQuestionContentDuplicated(0, question.Content)
	require.NoError(t, err)
	assert.True(t, duplicated)

	question.Content = "Explain what HTTP 404 means."
	require.NoError(t, question.Update())
	stored, err := GetMonitorQuestion(question.Id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, question.Content, stored.Content)
	assert.Equal(t, question.CreatedTime, stored.CreatedTime)

	require.NoError(t, DeleteMonitorQuestionByID(question.Id))
	stored, err = GetMonitorQuestion(question.Id)
	require.NoError(t, err)
	assert.Nil(t, stored)
}

func TestInitializeMonitorQuestionsSeedsReferenceLibrary(t *testing.T) {
	newChannelMonitorTestDB(t)

	require.NoError(t, InitializeMonitorQuestions())
	questions, err := GetAllMonitorQuestions()
	require.NoError(t, err)
	require.Len(t, questions, 65)

	contents := make([]string, 0, len(questions))
	for _, question := range questions {
		contents = append(contents, question.Content)
	}
	assert.Contains(t, contents, "请用一句话说明 HTTP 404 通常表示什么。")
	assert.Contains(t, contents, "帮我翻译codex的指令：/permissions  choose what Codex is allowed to do。")
	assert.Contains(t, contents, "部署和发布有什么区别？简单说说。")
}

func TestInitializeMonitorQuestionsRunsOnceAndPreservesUserChanges(t *testing.T) {
	newChannelMonitorTestDB(t)

	custom := MonitorQuestion{Content: "这是管理员自定义的问题。"}
	require.NoError(t, custom.Insert())
	require.NoError(t, InitializeMonitorQuestions())

	var seeded MonitorQuestion
	require.NoError(t, DB.Where("content = ?", "请用一句话说明 HTTP 404 通常表示什么。").First(&seeded).Error)
	require.NoError(t, DeleteMonitorQuestionByID(seeded.Id))
	require.NoError(t, InitializeMonitorQuestions())

	questions, err := GetAllMonitorQuestions()
	require.NoError(t, err)
	assert.Len(t, questions, 65)
	assert.Equal(t, int64(0), countMonitorQuestionsByContent(t, seeded.Content))
	assert.Equal(t, int64(1), countMonitorQuestionsByContent(t, custom.Content))
}

func countMonitorQuestionsByContent(t *testing.T, content string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&MonitorQuestion{}).Where("content = ?", content).Count(&count).Error)
	return count
}

// TestGetDueChannelMonitorConfigsUsesNextCheckAt locks in that due selection is
// driven purely by next_check_at (0 = never scheduled = due immediately) and
// gated on the enabled flag, and that HasDue agrees with GetDue.
func TestGetDueChannelMonitorConfigsUsesNextCheckAt(t *testing.T) {
	db := newChannelMonitorTestDB(t)
	now := int64(1_800_000_000)

	// Never scheduled (next_check_at defaults to 0) -> due.
	neverScheduled := ChannelMonitorConfig{ChannelId: 1, Enabled: true}
	require.NoError(t, db.Create(&neverScheduled).Error)
	// Past due.
	pastDue := ChannelMonitorConfig{ChannelId: 2, Enabled: true, NextCheckAt: now - 1}
	require.NoError(t, db.Create(&pastDue).Error)
	// Exactly now -> due (<=).
	dueNow := ChannelMonitorConfig{ChannelId: 3, Enabled: true, NextCheckAt: now}
	require.NoError(t, db.Create(&dueNow).Error)
	// Scheduled in the future -> not due.
	future := ChannelMonitorConfig{ChannelId: 4, Enabled: true, NextCheckAt: now + 30}
	require.NoError(t, db.Create(&future).Error)
	// Disabled but past due -> not due (enabled gate).
	disabled := ChannelMonitorConfig{ChannelId: 5, Enabled: false, NextCheckAt: now - 100}
	require.NoError(t, db.Create(&disabled).Error)

	due, err := GetDueChannelMonitorConfigs(now)
	require.NoError(t, err)
	dueChannelIds := make([]int, 0, len(due))
	for _, config := range due {
		dueChannelIds = append(dueChannelIds, config.ChannelId)
	}
	assert.ElementsMatch(t, []int{1, 2, 3}, dueChannelIds)
	assert.True(t, HasDueChannelMonitorConfigs(now))

	// Push all due configs into the future; nothing should remain due.
	require.NoError(t, db.Model(&ChannelMonitorConfig{}).
		Where("enabled = ?", true).
		Update("next_check_at", now+3600).Error)
	due, err = GetDueChannelMonitorConfigs(now)
	require.NoError(t, err)
	assert.Empty(t, due)
	assert.False(t, HasDueChannelMonitorConfigs(now))
}

// TestChannelMonitorNextProbeAt verifies the scheduling contract: the next probe
// time is always strictly in the future and stays within [interval-jitter,
// interval+jitter], including when interval/jitter are out of range and must be
// normalized (interval clamped up to the minimum, jitter clamped below interval).
func TestChannelMonitorNextProbeAt(t *testing.T) {
	now := int64(1_800_000_000)

	// No jitter -> exact interval.
	noJitter := ChannelMonitorConfig{IntervalSeconds: 60, JitterSeconds: 0}
	assert.Equal(t, now+60, noJitter.NextProbeAt(now))

	// Interval below the floor is clamped up to MonitorMinIntervalSeconds.
	tooSmall := ChannelMonitorConfig{IntervalSeconds: 1, JitterSeconds: 0}
	assert.Equal(t, now+int64(MonitorMinIntervalSeconds), tooSmall.NextProbeAt(now))

	// Symmetric jitter stays within bounds and never lands in the past, sampled
	// across many draws of the underlying RNG.
	cases := []ChannelMonitorConfig{
		{IntervalSeconds: 60, JitterSeconds: 10},
		{IntervalSeconds: 60, JitterSeconds: 200}, // jitter clamped to interval-1 (59)
	}
	for _, config := range cases {
		interval := config.IntervalSeconds
		jitter := config.JitterSeconds
		if jitter > interval-1 {
			jitter = interval - 1
		}
		low := now + int64(interval-jitter)
		high := now + int64(interval+jitter)
		for i := 0; i < 200; i++ {
			next := config.NextProbeAt(now)
			assert.GreaterOrEqualf(t, next, low, "interval=%d jitter=%d", interval, jitter)
			assert.LessOrEqualf(t, next, high, "interval=%d jitter=%d", interval, jitter)
			assert.Greaterf(t, next, now, "next probe must be in the future")
		}
	}
}

// TestDropChannelMonitorIntervalMinutes verifies the legacy-column drop actually
// removes interval_minutes and is safe to run repeatedly (idempotent no-op once
// the column is gone), which is the contract migrateDB relies on across restarts.
func TestDropChannelMonitorIntervalMinutes(t *testing.T) {
	db := newChannelMonitorTestDB(t)

	// Simulate a legacy schema by adding the obsolete column back onto the
	// AutoMigrated table.
	require.NoError(t, db.Exec("ALTER TABLE channel_monitor_configs ADD COLUMN interval_minutes integer DEFAULT 10").Error)
	require.True(t, db.Migrator().HasColumn(&ChannelMonitorConfig{}, "interval_minutes"))

	require.NoError(t, dropChannelMonitorIntervalMinutes())
	assert.False(t, db.Migrator().HasColumn(&ChannelMonitorConfig{}, "interval_minutes"))

	// Running again with the column already gone must be a clean no-op.
	require.NoError(t, dropChannelMonitorIntervalMinutes())
	assert.False(t, db.Migrator().HasColumn(&ChannelMonitorConfig{}, "interval_minutes"))
}

// TestChannelStatusRangeLayout locks the range→bucket contract the frontend
// sparkline depends on: every known range's BucketSeconds*BucketCount equals its
// declared window, and an unknown/empty key falls back to "1h" rather than
// yielding zero buckets (which would render an empty sparkline).
func TestChannelStatusRangeLayout(t *testing.T) {
	for key, window := range channelStatusRanges {
		assert.Equalf(t, window.Seconds, window.BucketSeconds*int64(window.BucketCount),
			"range %q window must equal bucketSeconds*bucketCount", key)
		assert.Positivef(t, window.BucketCount, "range %q must have buckets", key)
	}

	oneHour := channelStatusRanges["1h"]
	assert.Equal(t, oneHour, resolveChannelStatusRange(""), "empty key falls back to 1h")
	assert.Equal(t, oneHour, resolveChannelStatusRange("nonsense"), "unknown key falls back to 1h")
	assert.Equal(t, channelStatusRanges["7d"], resolveChannelStatusRange("7d"), "known key resolves exactly")
}

// TestGetChannelStatusRowsBucketCountPerRange verifies the sparkline width the API
// returns matches the selected range's declared bucket count, and that an unknown
// range key falls back to the 1h layout instead of producing an empty sparkline.
func TestGetChannelStatusRowsBucketCountPerRange(t *testing.T) {
	db := newChannelMonitorTestDB(t)

	channel := Channel{Name: "c", Type: 1, Key: "test-key", Models: "model-a", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	config := ChannelMonitorConfig{ChannelId: channel.Id, Enabled: true, IntervalSeconds: 60}
	require.NoError(t, config.SetMonitoredModels([]string{"model-a"}))
	require.NoError(t, db.Create(&config).Error)

	now := time.Unix(1_800_000_000, 0)
	for rangeKey, window := range channelStatusRanges {
		rows, err := GetChannelStatusRows(rangeKey, now)
		require.NoErrorf(t, err, "range %q", rangeKey)
		require.Lenf(t, rows, 1, "range %q", rangeKey)
		assert.Lenf(t, rows[0].RecentChecks, window.BucketCount, "range %q bucket count", rangeKey)
	}

	fallback, err := GetChannelStatusRows("bogus", now)
	require.NoError(t, err)
	require.Len(t, fallback, 1)
	assert.Len(t, fallback[0].RecentChecks, channelStatusRanges["1h"].BucketCount)
}

// TestGetChannelStatusRowsMergesForwardingTraffic verifies real forwarding logs
// (LogTypeConsume = success, LogTypeError = failure) are merged into the same
// buckets as probes: totals/success/health reflect both sources, only monitored
// channel+model pairs are counted, and AvgResponseMs stays probe-only (the log's
// whole-second use_time must not pollute the millisecond probe latency).
func TestGetChannelStatusRowsMergesForwardingTraffic(t *testing.T) {
	db := newChannelMonitorTestDB(t)

	channel := Channel{Name: "c", Type: 1, Key: "test-key", Models: "model-a,model-b", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	config := ChannelMonitorConfig{ChannelId: channel.Id, Enabled: true, IntervalSeconds: 600}
	require.NoError(t, config.SetMonitoredModels([]string{"model-a"}))
	require.NoError(t, db.Create(&config).Error)

	now := time.Unix(1_800_000_180, 0)
	// One probe for model-a: success, 100ms latency.
	require.NoError(t, db.Create(&ChannelMonitorResult{
		ChannelId: channel.Id, ModelName: "model-a", Success: true, LatencyMs: 100, CheckedAt: now.Unix() - 30,
	}).Error)
	// Forwarding logs for model-a: 3 consume (success) + 1 error (failure). use_time
	// is whole seconds and must NOT affect AvgResponseMs.
	require.NoError(t, db.Create(&[]Log{
		{ChannelId: channel.Id, ModelName: "model-a", Type: LogTypeConsume, UseTime: 5, CreatedAt: now.Unix() - 40},
		{ChannelId: channel.Id, ModelName: "model-a", Type: LogTypeConsume, UseTime: 5, CreatedAt: now.Unix() - 35},
		{ChannelId: channel.Id, ModelName: "model-a", Type: LogTypeConsume, UseTime: 5, CreatedAt: now.Unix() - 25},
		{ChannelId: channel.Id, ModelName: "model-a", Type: LogTypeError, UseTime: 5, CreatedAt: now.Unix() - 20},
		// Unmonitored model on the same channel: must be ignored.
		{ChannelId: channel.Id, ModelName: "model-b", Type: LogTypeConsume, UseTime: 5, CreatedAt: now.Unix() - 20},
		// Non-consume/error type: must be ignored even for a monitored model.
		{ChannelId: channel.Id, ModelName: "model-a", Type: LogTypeManage, UseTime: 5, CreatedAt: now.Unix() - 15},
	}).Error)

	rows, err := GetChannelStatusRows("6h", now)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	row := rows[0]
	assert.Equal(t, "model-a", row.Model)
	// 1 probe + 4 forwarding (3 consume + 1 error) = 5 requests, 4 successes.
	assert.Equal(t, 5, row.Requests)
	assert.InDelta(t, 80.0, row.SuccessRate, 0.001)
	// 4/5 = 80% < 95% -> degraded.
	assert.Equal(t, "degraded", row.Health)
	// AvgResponseMs must come only from the single probe (100ms), never the logs.
	assert.Equal(t, 100, row.AvgResponseMs)
}

func TestInsertChannelMonitorResultDefaultsToScheduledAndPreservesManual(t *testing.T) {
	newChannelMonitorTestDB(t)

	scheduled := &ChannelMonitorResult{ChannelId: 1, ModelName: "model-a"}
	require.NoError(t, InsertChannelMonitorResult(scheduled))
	assert.Equal(t, ChannelMonitorTriggerScheduled, scheduled.TriggerType)

	manual := &ChannelMonitorResult{
		ChannelId:   1,
		ModelName:   "model-a",
		TriggerType: ChannelMonitorTriggerManual,
	}
	require.NoError(t, InsertChannelMonitorResult(manual))
	assert.Equal(t, ChannelMonitorTriggerManual, manual.TriggerType)
}
