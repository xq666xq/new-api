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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func marshalMonitorHeaders(t *testing.T, headers []model.ChannelMonitorHeader) model.JSONValue {
	t.Helper()
	data, err := common.Marshal(headers)
	require.NoError(t, err)
	return model.JSONValue(data)
}

func newChannelMonitorControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.ChannelMonitorConfig{},
		&model.MonitorTemplate{},
	))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestNormalizeMonitorRequestSettingsCanonicalizesHeadersAndStream(t *testing.T) {
	endpointType := "embeddings"
	stream := true
	headers := marshalMonitorHeaders(t, []model.ChannelMonitorHeader{
		{Key: "x-probe-mode", Value: "first"},
		{Key: "X-Probe-Mode", Value: "last"},
	})
	bodyMode := ""
	bodyJSON := ""

	err := normalizeMonitorRequestSettings(&endpointType, &stream, &headers, &bodyMode, &bodyJSON)
	require.NoError(t, err)
	assert.False(t, stream)
	assert.Equal(t, model.MonitorBodyModeDefault, bodyMode)
	config := &model.ChannelMonitorConfig{Headers: headers}
	normalized := config.GetHeaders()
	require.Len(t, normalized, 1)
	assert.Equal(t, "X-Probe-Mode", normalized[0].Key)
	assert.Equal(t, "last", normalized[0].Value)
}

func TestNormalizeMonitorRequestSettingsRejectsProtectedHeaders(t *testing.T) {
	protectedHeaders := []string{"Authorization", "X-Api-Key", "Cookie", "Host", "Content-Length"}
	for _, headerName := range protectedHeaders {
		t.Run(headerName, func(t *testing.T) {
			endpointType := "auto"
			stream := false
			headers := marshalMonitorHeaders(t, []model.ChannelMonitorHeader{{Key: headerName, Value: "secret"}})
			bodyMode := model.MonitorBodyModeDefault
			bodyJSON := ""

			err := normalizeMonitorRequestSettings(&endpointType, &stream, &headers, &bodyMode, &bodyJSON)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "protected")
		})
	}
}

func TestNormalizeMonitorConfigUsesScheduleDefaults(t *testing.T) {
	config := &model.ChannelMonitorConfig{
		ChannelId:    1,
		EndpointType: "auto",
		BodyMode:     model.MonitorBodyModeDefault,
	}

	require.NoError(t, normalizeMonitorConfig(config))
	assert.Equal(t, model.ChannelMonitorModeDefault, config.MonitorMode)
	assert.Equal(t, model.ChannelMonitorDefaultIntervalSeconds, config.IntervalSeconds)
	assert.Equal(t, model.ChannelMonitorDefaultJitterSeconds, config.JitterSeconds)

	config.MonitorMode = "unsupported"
	require.ErrorContains(t, normalizeMonitorConfig(config), "unsupported monitor mode")
}

func TestSaveChannelDetectionConfigPreservesMonitorScheduling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newChannelMonitorControllerDB(t)

	require.NoError(t, db.Create(&model.Channel{
		Id:     12,
		Name:   "monitored",
		Key:    "test-key",
		Models: "gpt-test",
	}).Error)
	config := &model.ChannelMonitorConfig{
		ChannelId:       12,
		Enabled:         true,
		Managed:         true,
		MonitorMode:     model.ChannelMonitorModeBannedOnly,
		EndpointType:    "openai",
		Stream:          true,
		IntervalSeconds: 300,
		JitterSeconds:   20,
		BodyMode:        model.MonitorBodyModeDefault,
		LastCheckedAt:   111,
		NextCheckAt:     222,
	}
	require.NoError(t, config.SetMonitoredModels([]string{"gpt-test"}))
	require.NoError(t, config.SetHeaders([]model.ChannelMonitorHeader{{Key: "X-Old", Value: "one"}}))
	require.NoError(t, db.Create(config).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/channel_monitor/config",
		strings.NewReader(`{"channel_id":12,"endpoint_type":"anthropic","stream":false,"template_id":0,"headers":[{"key":"X-New","value":"two"}],"body_mode":"merge","body_json":"{\"max_tokens\":32}"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	SaveChannelDetectionConfig(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	loaded, err := model.GetChannelMonitorConfig(12)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, config.Id, loaded.Id)
	assert.True(t, loaded.Enabled)
	assert.True(t, loaded.Managed)
	assert.Equal(t, model.ChannelMonitorModeBannedOnly, loaded.MonitorMode)
	assert.Equal(t, 300, loaded.IntervalSeconds)
	assert.Equal(t, 20, loaded.JitterSeconds)
	assert.Equal(t, int64(111), loaded.LastCheckedAt)
	assert.Equal(t, int64(222), loaded.NextCheckAt)
	assert.Equal(t, []string{"gpt-test"}, loaded.GetMonitoredModels())
	assert.Equal(t, "anthropic", loaded.EndpointType)
	assert.False(t, loaded.Stream)
	assert.Equal(t, model.MonitorBodyModeMerge, loaded.BodyMode)
	require.Len(t, loaded.GetHeaders(), 1)
	assert.Equal(t, "X-New", loaded.GetHeaders()[0].Key)
}

func TestSaveChannelDetectionConfigUpdatesMonitoringControls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newChannelMonitorControllerDB(t)

	require.NoError(t, db.Create(&model.Channel{
		Id:     13,
		Name:   "monitor-controls",
		Key:    "test-key",
		Models: "gpt-test,claude-test",
	}).Error)
	config := &model.ChannelMonitorConfig{
		ChannelId:       13,
		MonitorMode:     model.ChannelMonitorModeDefault,
		IntervalSeconds: model.ChannelMonitorDefaultIntervalSeconds,
		JitterSeconds:   model.ChannelMonitorDefaultJitterSeconds,
		BodyMode:        model.MonitorBodyModeDefault,
	}
	require.NoError(t, config.SetMonitoredModels([]string{"gpt-test"}))
	require.NoError(t, db.Create(config).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/channel_monitor/config",
		strings.NewReader(`{"channel_id":13,"enabled":true,"managed":true,"monitor_mode":"banned_only","interval_seconds":900,"jitter_seconds":90,"monitored_models":["claude-test","unknown"],"endpoint_type":"auto","stream":false,"template_id":0,"headers":[],"body_mode":"default","body_json":""}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	SaveChannelDetectionConfig(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	loaded, err := model.GetChannelMonitorConfig(13)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.True(t, loaded.Enabled)
	assert.True(t, loaded.Managed)
	assert.Equal(t, model.ChannelMonitorModeBannedOnly, loaded.MonitorMode)
	assert.Equal(t, 900, loaded.IntervalSeconds)
	assert.Equal(t, 90, loaded.JitterSeconds)
	assert.Equal(t, []string{"claude-test"}, loaded.GetMonitoredModels())
}

func TestGetChannelMonitorConfigResolvesLegacyTemplateName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newChannelMonitorControllerDB(t)
	template := &model.MonitorTemplate{
		Name:         "saved-template",
		EndpointType: "openai",
		BodyMode:     model.MonitorBodyModeDefault,
	}
	require.NoError(t, model.InsertMonitorTemplate(template))
	require.NoError(t, db.Create(&model.ChannelMonitorConfig{
		ChannelId:       42,
		IntervalSeconds: 60,
		TemplateName:    template.Name,
		BodyMode:        model.MonitorBodyModeDefault,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "42"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/config/42", nil)

	GetChannelMonitorConfig(ctx)

	var response struct {
		Success bool                        `json:"success"`
		Data    *model.ChannelMonitorConfig `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	require.NotNil(t, response.Data)
	assert.Equal(t, template.Id, response.Data.TemplateId)
	assert.Equal(t, template.Name, response.Data.TemplateName)
}

func TestApplyMonitorRequestBodyMergePreservesDefaultsAndOverridesFields(t *testing.T) {
	maxTokens := uint(16)
	request := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
		},
		MaxTokens: &maxTokens,
	}
	config := &model.ChannelMonitorConfig{
		BodyMode: model.MonitorBodyModeMerge,
		BodyJson: `{"max_tokens":32,"temperature":0}`,
	}

	converted, err := applyMonitorRequestBody(request, config)
	require.NoError(t, err)
	typed, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, "gpt-test", typed.Model)
	require.Len(t, typed.Messages, 1)
	require.NotNil(t, typed.MaxTokens)
	assert.Equal(t, uint(32), *typed.MaxTokens)
	require.NotNil(t, typed.Temperature)
	assert.Equal(t, float64(0), *typed.Temperature)
}

func TestApplyMonitorRequestBodyOverrideReplacesDefaultFields(t *testing.T) {
	maxTokens := uint(16)
	request := &dto.GeneralOpenAIRequest{
		Model:     "gpt-test",
		Messages:  []dto.Message{{Role: "user", Content: "hi"}},
		MaxTokens: &maxTokens,
	}
	config := &model.ChannelMonitorConfig{
		BodyMode: model.MonitorBodyModeOverride,
		BodyJson: `{"model":"custom-model","messages":[{"role":"user","content":"probe"}]}`,
	}

	converted, err := applyMonitorRequestBody(request, config)
	require.NoError(t, err)
	typed, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, "custom-model", typed.Model)
	require.Len(t, typed.Messages, 1)
	assert.Nil(t, typed.MaxTokens)
}

func TestAbnormalMonitorStreamEndErrorClassifiesProbeOutcome(t *testing.T) {
	tests := []struct {
		name    string
		reason  relaycommon.StreamEndReason
		endErr  error
		wantErr bool
	}{
		{name: "nil status is normal"},
		{name: "done", reason: relaycommon.StreamEndReasonDone},
		{name: "eof", reason: relaycommon.StreamEndReasonEOF},
		{name: "handler stop", reason: relaycommon.StreamEndReasonHandlerStop},
		{name: "timeout", reason: relaycommon.StreamEndReasonTimeout, wantErr: true},
		{name: "client gone", reason: relaycommon.StreamEndReasonClientGone, endErr: context.DeadlineExceeded, wantErr: true},
		{name: "scanner error", reason: relaycommon.StreamEndReasonScannerErr, endErr: fmt.Errorf("boom"), wantErr: true},
		{name: "ping fail", reason: relaycommon.StreamEndReasonPingFail, wantErr: true},
		{name: "panic", reason: relaycommon.StreamEndReasonPanic, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var status *relaycommon.StreamStatus
			if test.name != "nil status is normal" {
				status = relaycommon.NewStreamStatus()
				status.SetEndReason(test.reason, test.endErr)
			}

			err := abnormalStreamEndError(status)

			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
