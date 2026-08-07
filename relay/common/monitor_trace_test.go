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
package common

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorProbeTraceSnapshotRedactsCredentials(t *testing.T) {
	trace := &MonitorProbeTrace{}
	request, err := http.NewRequest(
		http.MethodPost,
		"https://example.com/v1beta/models/gemini?key=gemini-secret&view=full",
		strings.NewReader(`{"prompt":"hello"}`),
	)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer channel-secret")
	request.Header.Set("X-Goog-Api-Key", "google-secret")
	request.Header.Set("X-Trace-Id", "trace-123")

	request = trace.AttachRequest(request)
	_, err = io.ReadAll(request.Body)
	require.NoError(t, err)

	response := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Set-Cookie":   []string{"session=secret"},
			"X-Request-Id": []string{"request-123"},
		},
		Body:    io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Request: request,
	}
	trace.AttachResponse(response)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)

	snapshot := trace.Snapshot()
	assert.NotContains(t, snapshot.RequestURL, "gemini-secret")
	assert.Contains(t, snapshot.RequestURL, "key=%5BREDACTED%5D")
	assert.Equal(t, []string{monitorTraceRedacted}, snapshot.RequestHeaders["Authorization"])
	assert.Equal(t, []string{monitorTraceRedacted}, snapshot.RequestHeaders["X-Goog-Api-Key"])
	assert.Equal(t, []string{"trace-123"}, snapshot.RequestHeaders["X-Trace-Id"])
	assert.Equal(t, []string{monitorTraceRedacted}, snapshot.ResponseHeaders["Set-Cookie"])
	assert.Equal(t, `{"prompt":"hello"}`, snapshot.RequestBody)
	assert.Equal(t, `{"ok":true}`, snapshot.ResponseBody)
}

func TestMonitorProbeTraceBoundsCapturedBodies(t *testing.T) {
	trace := &MonitorProbeTrace{}
	request, err := http.NewRequest(
		http.MethodPost,
		"https://example.com/probe",
		strings.NewReader(strings.Repeat("x", MonitorProbeTraceBodyLimit+32)),
	)
	require.NoError(t, err)
	request = trace.AttachRequest(request)
	_, err = io.ReadAll(request.Body)
	require.NoError(t, err)

	snapshot := trace.Snapshot()
	assert.Len(t, snapshot.RequestBody, MonitorProbeTraceBodyLimit)
	assert.True(t, snapshot.RequestBodyTruncated)
	assert.Equal(t, MonitorProbeTraceBodyLimit, snapshot.BodyLimitBytes)
}
