package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorMasterSwitchStopsPeriodicSchedulerWithoutPendingProbe(t *testing.T) {
	setting := operation_setting.GetChannelMonitorSetting()
	originalEnabled := setting.Enabled
	originalDB := model.DB
	setting.Enabled = false
	model.DB = nil
	t.Cleanup(func() {
		setting.Enabled = originalEnabled
		model.DB = originalDB
	})

	assert.False(t, channelMonitorHandler{}.Enabled())

	summary, err := runChannelMonitorTask(context.Background())
	require.NoError(t, err)
	assert.Equal(t, channelMonitorTaskResult{}, summary)
}

func TestChannelMonitorHandlerSchedulesManagedErrorProbeWhenMonitoringDisabled(t *testing.T) {
	previousDB := model.DB
	setting := operation_setting.GetChannelMonitorSetting()
	previousEnabled := setting.Enabled
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorConfig{}))
	model.DB = db
	setting.Enabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		setting.Enabled = previousEnabled
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	config := model.ChannelMonitorConfig{ChannelId: 41, Enabled: false, Managed: true}
	require.NoError(t, db.Create(&config).Error)
	affected, err := model.AdvanceManagedErrorProbeDue(config.ChannelId)
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	assert.True(t, channelMonitorHandler{}.Enabled())
}

func TestManagedPolicyAppliesToHostedChannelWhenMonitoringDisabled(t *testing.T) {
	db := newBanStageTestDB(t)
	monitorSetting := operation_setting.GetChannelMonitorSetting()
	previousMonitorEnabled := monitorSetting.Enabled
	policySetting := operation_setting.GetManagedPolicySetting()
	previousPolicySetting := *policySetting
	monitorSetting.Enabled = false
	policySetting.BanEnabled = true
	policySetting.ConfirmCount = 1
	policySetting.SpeedEnabled = false
	t.Cleanup(func() {
		monitorSetting.Enabled = previousMonitorEnabled
		*policySetting = previousPolicySetting
	})

	const channelID = 43
	require.NoError(t, db.Create(&model.Channel{
		Id: channelID, Name: "hosted-with-monitoring-paused", Status: common.ChannelStatusEnabled, Models: "model-a",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "model-a", ChannelId: channelID, Enabled: true,
	}).Error)
	config := model.ChannelMonitorConfig{ChannelId: channelID, Enabled: false, Managed: true}
	require.NoError(t, config.SetMonitoredModels([]string{"model-a"}))
	require.NoError(t, db.Create(&config).Error)
	require.NoError(t, db.Create(&model.ChannelMonitorResult{
		ChannelId: channelID, ModelName: "model-a", Success: false, CheckedAt: common.GetTimestamp(),
	}).Error)

	runChannelManagedPolicy()

	var ability model.Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, "model-a").First(&ability).Error)
	assert.False(t, ability.Enabled)
}

func TestProbeChannelMonitorNowIgnoresSchedulerSwitches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	setting := operation_setting.GetChannelMonitorSetting()
	previousEnabled := setting.Enabled

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelMonitorConfig{}))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	setting.Enabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		setting.Enabled = previousEnabled
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "channel-a"}).Error)
	config := &model.ChannelMonitorConfig{ChannelId: 1, Enabled: false}
	require.NoError(t, config.SetMonitoredModels([]string{}))
	require.NoError(t, db.Create(config).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/channel_monitor/probe",
		strings.NewReader(`{"channel_id":1}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	assert.NotPanics(t, func() { ProbeChannelMonitorNow(ctx) })
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "该渠道没有可检测的模型")
	assert.NotContains(t, recorder.Body.String(), "监控总开关已关闭")
	assert.NotContains(t, recorder.Body.String(), "该渠道监控未开启")
}

func TestManualProbeModelsSelectsOneExactChannelModel(t *testing.T) {
	channelModels := []string{"model-a", "model-b", "model-c"}

	models, allowed := manualProbeModels(" model-c ", channelModels)

	assert.True(t, allowed)
	assert.Equal(t, []string{"model-c"}, models)
}

func TestManualProbeModelsRejectsUnavailableModel(t *testing.T) {
	models, allowed := manualProbeModels(
		"model-d",
		[]string{"model-a", "model-b", "model-c"},
	)

	assert.False(t, allowed)
	assert.Empty(t, models)
}

func TestManualProbeModelsKeepsLegacyAllModelsBehavior(t *testing.T) {
	models, allowed := manualProbeModels(
		"",
		[]string{" model-b ", "model-a", "model-b", ""},
	)

	assert.True(t, allowed)
	assert.Equal(t, []string{"model-b", "model-a"}, models)
}

func TestUpdateChannelMonitorSettingPersistsDisabledState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB, originalLogDB := model.DB, model.LOG_DB
	setting := operation_setting.GetChannelMonitorSetting()
	originalSetting := *setting
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	model.DB, model.LOG_DB = db, db
	setting.ProbeConcurrency = 7
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		*setting = originalSetting
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/channel_monitor/settings",
		strings.NewReader(`{"enabled":false}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannelMonitorSetting(ctx)

	var response struct {
		Success bool                                    `json:"success"`
		Data    operation_setting.ChannelMonitorSetting `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.False(t, response.Data.Enabled)
	assert.Equal(t, 7, response.Data.ProbeConcurrency)
	assert.False(t, operation_setting.IsChannelMonitorEnabled())

	var option model.Option
	require.NoError(t, db.First(&option, "key = ?", "channel_monitor_setting.enabled").Error)
	assert.Equal(t, "false", option.Value)
	option = model.Option{}
	require.NoError(t, db.First(&option, "key = ?", "channel_monitor_setting.probe_concurrency").Error)
	assert.Equal(t, "7", option.Value)
}
