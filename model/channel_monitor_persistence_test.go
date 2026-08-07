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
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newChannelMonitorPersistenceDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&ChannelMonitorConfig{}, &MonitorTemplate{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		_ = sqlDB.Close()
	})
	return db
}

func TestUpsertChannelMonitorConfigPreservesIdentityAndHeaders(t *testing.T) {
	db := newChannelMonitorPersistenceDB(t)

	initial := &ChannelMonitorConfig{
		ChannelId:    42,
		EndpointType: "openai",
		Stream:       true,
		BodyMode:     MonitorBodyModeMerge,
		BodyJson:     `{"max_tokens":16}`,
	}
	require.NoError(t, initial.SetHeaders([]ChannelMonitorHeader{{Key: "X-Probe", Value: "one"}}))
	require.NoError(t, UpsertChannelMonitorConfig(initial))
	first, err := GetChannelMonitorConfig(initial.ChannelId)
	require.NoError(t, err)
	require.NotNil(t, first)

	updated := &ChannelMonitorConfig{
		Id:           999,
		ChannelId:    initial.ChannelId,
		EndpointType: "anthropic",
		BodyMode:     MonitorBodyModeOverride,
		BodyJson:     `{"max_tokens":32}`,
	}
	require.NoError(t, updated.SetHeaders([]ChannelMonitorHeader{{Key: "X-Probe", Value: "two"}}))
	require.NoError(t, UpsertChannelMonitorConfig(updated))

	loaded, err := GetChannelMonitorConfig(initial.ChannelId)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, first.Id, loaded.Id)
	assert.Equal(t, first.CreatedTime, loaded.CreatedTime)
	assert.Equal(t, "anthropic", loaded.EndpointType)
	assert.Equal(t, MonitorBodyModeOverride, loaded.BodyMode)
	loadedHeaders := loaded.GetHeaders()
	require.Len(t, loadedHeaders, 1)
	assert.Equal(t, "two", loadedHeaders[0].Value)

	var count int64
	require.NoError(t, db.Model(&ChannelMonitorConfig{}).Where("channel_id = ?", initial.ChannelId).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestMonitorTemplateCRUDPreservesCreatedTimeAndDetectsDuplicateNames(t *testing.T) {
	newChannelMonitorPersistenceDB(t)

	template := &MonitorTemplate{
		Name:         "  smoke  ",
		Description:  "initial",
		EndpointType: "openai",
		BodyMode:     MonitorBodyModeDefault,
	}
	require.NoError(t, template.SetHeaders([]ChannelMonitorHeader{{Key: "X-Test", Value: "yes"}}))
	template.Name = strings.TrimSpace(template.Name)
	require.NoError(t, InsertMonitorTemplate(template))
	createdTime := template.CreatedTime
	config := &ChannelMonitorConfig{
		ChannelId:    42,
		TemplateId:   template.Id,
		EndpointType: template.EndpointType,
		Headers:      template.Headers,
		BodyMode:     template.BodyMode,
		BodyJson:     template.BodyJson,
	}
	require.NoError(t, UpsertChannelMonitorConfig(config))

	duplicate, err := IsMonitorTemplateNameDuplicated(0, "smoke")
	require.NoError(t, err)
	assert.True(t, duplicate)
	duplicate, err = IsMonitorTemplateNameDuplicated(template.Id, "smoke")
	require.NoError(t, err)
	assert.False(t, duplicate)

	template.Description = "updated"
	template.EndpointType = "gemini"
	require.NoError(t, UpdateMonitorTemplate(template))
	loaded, err := GetMonitorTemplate(template.Id)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, createdTime, loaded.CreatedTime)
	assert.Equal(t, "updated", loaded.Description)
	assert.Equal(t, "gemini", loaded.EndpointType)
	loadedHeaders := loaded.GetHeaders()
	require.Len(t, loadedHeaders, 1)
	assert.Equal(t, "yes", loadedHeaders[0].Value)

	require.NoError(t, DeleteMonitorTemplateByID(template.Id))
	deleted, err := GetMonitorTemplate(template.Id)
	require.NoError(t, err)
	assert.Nil(t, deleted)
	loadedConfig, err := GetChannelMonitorConfig(config.ChannelId)
	require.NoError(t, err)
	require.NotNil(t, loadedConfig)
	assert.Zero(t, loadedConfig.TemplateId)
	assert.Equal(t, "openai", loadedConfig.EndpointType, "deleting a template must preserve the saved request snapshot")
}
