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
import { getGroups as getUserGroups } from '@/features/users/api'
import { api, getFreshAuthHeaders, type ApiRequestConfig } from '@/lib/api'

import type {
  AddChannelRequest,
  BatchDeleteParams,
  BatchSetTagParams,
  Channel,
  ChannelBalanceResponse,
  ChannelMonitorConfig,
  ChannelMonitorProbeResult,
  ChannelOpsResponse,
  ChannelTestResponse,
  CopyChannelParams,
  CopyChannelResponse,
  FetchModelsResponse,
  GetChannelResponse,
  GetChannelsParams,
  GetChannelsResponse,
  MultiKeyManageParams,
  MultiKeyStatusResponse,
  MonitorBodyMode,
  MonitorHeader,
  MonitorTemplate,
  ProbeStreamChunk,
  ProbeStreamHandlers,
  ProbeStreamStart,
  SearchChannelsParams,
  SearchChannelsResponse,
  TagOperationParams,
} from './types'

type ApiWireResponse<T> = {
  success: boolean
  message?: string
  data?: T
}

type RawMonitorHeader = { key: string; value: string }

type RawMonitorRequestSettings = {
  endpoint_type?: string
  stream?: boolean
  headers?: RawMonitorHeader[]
  body_mode?: string
  body_json?: string
}

type RawChannelMonitorConfig = RawMonitorRequestSettings & {
  id?: number
  channel_id?: number
  enabled?: boolean
  managed?: boolean
  monitor_mode?: string
  interval_seconds?: number
  jitter_seconds?: number
  monitored_models?: string[]
  template_id?: number
  updated_time?: number
}

type RawMonitorTemplate = RawMonitorRequestSettings & {
  id?: number
  name?: string
  description?: string
  updated_time?: number
}

type RawMonitorProbeTrace = {
  request_method?: string
  request_url?: string
  request_headers?: Record<string, string[]>
  request_body?: string
  request_body_truncated?: boolean
  request_write_error?: string
  response_url?: string
  response_status_code?: number
  response_status?: string
  response_headers?: Record<string, string[]>
  response_body?: string
  response_body_truncated?: boolean
  body_limit_bytes?: number
}

type RawChannelMonitorProbeResult = {
  model_name?: string
  endpoint_type?: string
  stream?: boolean
  question_id?: number
  question_content?: string
  success?: boolean
  latency_ms?: number
  ttft_ms?: number
  status_code?: number
  error_message?: string
  checked_at?: number
  trace?: RawMonitorProbeTrace
}

const channelActionConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

export type CodexUsageResponse = {
  success: boolean
  message?: string
  upstream_status?: number
  data?: Record<string, unknown>
}

export type CodexResetCreditsResponse = CodexUsageResponse

export type CodexUsageResetResponse = CodexUsageResponse

export type CodexCredentialRefreshResponse = {
  success: boolean
  message?: string
  data?: {
    expires_at?: string
    last_refresh?: string
    account_id?: string
    email?: string
    channel_id?: number
    channel_type?: number
    channel_name?: string
  }
}

// ============================================================================
// Base Channel CRUD Operations
// ============================================================================

/**
 * Get paginated list of channels
 */
export async function getChannels(
  params: GetChannelsParams = {}
): Promise<GetChannelsResponse> {
  // Keep the collection URL canonical. Gin registers the collection handler
  // at /api/channel/; relying on an implicit trailing-slash redirect breaks
  // when sibling channel-monitor routes are present in the same route tree.
  const res = await api.get('/api/channel/', { params })
  return res.data
}

/**
 * Search channels with filters
 */
export async function searchChannels(
  params: SearchChannelsParams
): Promise<SearchChannelsResponse> {
  const res = await api.get('/api/channel/search', { params })
  return res.data
}

/**
 * Get single channel by ID
 */
export async function getChannel(id: number): Promise<GetChannelResponse> {
  const res = await api.get(`/api/channel/${id}`)
  return res.data
}

/**
 * Get channel operations summary for administrators
 */
export async function getChannelOps(): Promise<ChannelOpsResponse> {
  const res = await api.get('/api/channel/ops', channelActionConfig())
  return res.data
}

/**
 * Create new channel(s)
 * Supports single, batch, and multi-key modes
 */
export async function createChannel(
  data: AddChannelRequest
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post('/api/channel/', data, channelActionConfig())
  return res.data
}

/**
 * Update existing channel
 */
export async function updateChannel(
  id: number,
  data: Partial<Channel>
): Promise<{ success: boolean; message?: string; data?: Channel }> {
  const res = await api.put(
    '/api/channel/',
    { id, ...data },
    channelActionConfig()
  )
  return res.data
}

/**
 * Update channel enabled/disabled status.
 */
export async function updateChannelStatus(
  id: number,
  status: number
): Promise<{ success: boolean; message?: string; data?: boolean }> {
  const res = await api.post(
    `/api/channel/${id}/status`,
    { status },
    channelActionConfig()
  )
  return res.data
}

/**
 * Batch update channel enabled/disabled status.
 */
export async function batchUpdateChannelStatus(
  ids: number[],
  status: number
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post(
    '/api/channel/status/batch',
    { ids, status },
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete single channel
 */
export async function deleteChannel(
  id: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(`/api/channel/${id}`, channelActionConfig())
  return res.data
}

/**
 * Batch delete channels
 */
export async function batchDeleteChannels(
  data: BatchDeleteParams
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post('/api/channel/batch', data, channelActionConfig())
  return res.data
}

/**
 * Batch set tag for channels
 */
export async function batchSetChannelTag(
  data: BatchSetTagParams
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post(
    '/api/channel/batch/tag',
    data,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Channel Operations
// ============================================================================

/**
 * Test channel connectivity
 */
export async function testChannel(
  id: number,
  params?: {
    model?: string
    endpoint_type?: string
    stream?: boolean
    use_monitor_config?: boolean
  }
): Promise<ChannelTestResponse> {
  const res = await api.get(
    `/api/channel/test/${id}`,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Update channel balance
 */
export async function updateChannelBalance(
  id: number
): Promise<ChannelBalanceResponse> {
  const res = await api.get(
    `/api/channel/update_balance/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Fetch available models from upstream provider
 */
export async function fetchUpstreamModels(
  id: number
): Promise<FetchModelsResponse> {
  const res = await api.get(
    `/api/channel/fetch_models/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Copy/clone a channel
 */
export async function copyChannel(
  id: number,
  params: CopyChannelParams = {}
): Promise<CopyChannelResponse> {
  const res = await api.post(
    `/api/channel/copy/${id}`,
    null,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Fix channel abilities
 */
export async function fixChannelAbilities(): Promise<{
  success: boolean
  message?: string
  data?: { success: number; fails: number }
}> {
  const res = await api.post(
    '/api/channel/fix',
    undefined,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete all disabled channels
 */
export async function deleteDisabledChannels(): Promise<{
  success: boolean
  message?: string
  data?: number
}> {
  const res = await api.delete('/api/channel/disabled', channelActionConfig())
  return res.data
}

/**
 * Get channel key (requires 2FA verification)
 */
export async function getChannelKey(
  id: number,
  proofToken?: string
): Promise<{ success: boolean; message?: string; data?: { key: string } }> {
  const res = await api.post(
    `/api/channel/${id}/key`,
    undefined,
    channelActionConfig({
      headers: proofToken ? { 'X-Security-Proof': proofToken } : undefined,
    })
  )
  return res.data
}

// ============================================================================
// Codex Channel Operations
// ============================================================================

export async function refreshCodexCredential(
  channelId: number
): Promise<CodexCredentialRefreshResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/refresh`,
    {},
    channelActionConfig()
  )
  return res.data
}

export async function getCodexUsage(
  channelId: number
): Promise<CodexUsageResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function getCodexResetCredits(
  channelId: number
): Promise<CodexResetCreditsResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage/reset-credits`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function resetCodexUsage(
  channelId: number
): Promise<CodexUsageResetResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/usage/reset`,
    {},
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

// ============================================================================
// Multi-Key Management
// ============================================================================

/**
 * Manage multi-key channel operations
 */
export async function manageMultiKeys(
  params: MultiKeyManageParams
): Promise<MultiKeyStatusResponse | { success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/multi_key/manage',
    params,
    channelActionConfig()
  )
  return res.data
}

/**
 * Get key status for multi-key channel
 */
export async function getMultiKeyStatus(
  channelId: number,
  page = 1,
  pageSize = 50,
  status?: number
): Promise<MultiKeyStatusResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'get_key_status',
    page,
    page_size: pageSize,
    status,
  }) as Promise<MultiKeyStatusResponse>
}

/**
 * Enable a specific key in multi-key channel
 */
export async function enableMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Disable a specific key in multi-key channel
 */
export async function disableMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Delete a specific key in multi-key channel
 */
export async function deleteMultiKey(
  channelId: number,
  keyIndex: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_key',
    key_index: keyIndex,
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Enable all keys in multi-key channel
 */
export async function enableAllMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_all_keys',
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Disable all keys in multi-key channel
 */
export async function disableAllMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_all_keys',
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Delete all disabled keys in multi-key channel
 */
export async function deleteDisabledMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string; data?: number }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_disabled_keys',
  }) as Promise<{ success: boolean; message?: string; data?: number }>
}

// ============================================================================
// Tag Operations
// ============================================================================

/**
 * Enable all channels with a specific tag
 */
export async function enableTagChannels(
  tag: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/tag/enabled',
    { tag },
    channelActionConfig()
  )
  return res.data
}

/**
 * Disable all channels with a specific tag
 */
export async function disableTagChannels(
  tag: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/tag/disabled',
    { tag },
    channelActionConfig()
  )
  return res.data
}

/**
 * Edit all channels with a specific tag
 */
export async function editTagChannels(
  params: TagOperationParams
): Promise<{ success: boolean; message?: string }> {
  const res = await api.put('/api/channel/tag', params, channelActionConfig())
  return res.data
}

/**
 * Get models for a specific tag
 */
export async function getTagModels(
  tag: string
): Promise<{ success: boolean; message?: string; data?: string }> {
  const res = await api.get('/api/channel/tag/models', { params: { tag } })
  return res.data
}

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Fetch models from the current unsaved channel form configuration.
 */
export async function fetchModels(data: {
  base_url: string
  type: number
  key?: string
  channel_id?: number
  advanced_custom?: string
  header_override?: string
  proxy?: string
}): Promise<FetchModelsResponse> {
  const res = await api.post(
    '/api/channel/fetch_models',
    data,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete an Ollama model from a channel
 */
export async function deleteOllamaModel(params: {
  channel_id: number
  model_name: string
}): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(
    '/api/channel/ollama/delete',
    channelActionConfig({ data: params })
  )
  return res.data
}

/**
 * Test all enabled channels
 */
export async function testAllChannels(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.get('/api/channel/test', channelActionConfig())
  return res.data
}

/**
 * Update balance for all enabled channels
 */
export async function updateAllChannelsBalance(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.get(
    '/api/channel/update_balance',
    channelActionConfig()
  )
  return res.data
}

/**
 * Get all available models
 */
export async function getAllModels(): Promise<{
  success: boolean
  message?: string
  data?: Array<{ id: string; [key: string]: unknown }>
}> {
  const res = await api.get('/api/channel/models')
  return res.data
}

/**
 * Get all enabled models
 */
export async function getEnabledModels(): Promise<{
  success: boolean
  message?: string
  data?: string[]
}> {
  const res = await api.get('/api/channel/models_enabled')
  return res.data
}

// ============================================================================
// Ollama Utilities
// ============================================================================

/**
 * Check Ollama version for a given channel
 */
export async function getOllamaVersion(
  channelId: number
): Promise<{ success: boolean; message?: string; data?: { version: string } }> {
  const res = await api.get(`/api/channel/ollama/version/${channelId}`)
  return res.data
}

// ============================================================================
// Group Management
// ============================================================================

/**
 * Get all available groups (re-exported from users API for convenience)
 */
export const getGroups = getUserGroups

// ============================================================================
// Prefill Groups (Model Groups)
// ============================================================================

/**
 * Get prefill groups for quick model selection
 */
export async function getPrefillGroups(
  type: 'model' | 'group' = 'model'
): Promise<{
  success: boolean
  message?: string
  data?: Array<{ id: number; name: string; items: string | string[] }>
}> {
  const res = await api.get('/api/prefill_group', { params: { type } })
  return res.data
}

function normalizeMonitorBodyMode(value?: string): MonitorBodyMode {
  if (value === 'merge' || value === 'override') return value
  return 'default'
}

function normalizeChannelMonitorMode(
  value?: string
): ChannelMonitorConfig['monitorMode'] {
  return value === 'banned_only' ? value : 'default'
}

function toMonitorHeaders(headers?: RawMonitorHeader[]): MonitorHeader[] {
  return (headers ?? []).map((header, index) => ({
    id: `${index}-${header.key}`,
    key: header.key,
    value: header.value,
  }))
}

function monitorHeadersToWire(headers: MonitorHeader[]): RawMonitorHeader[] {
  return headers.map((header) => ({ key: header.key, value: header.value }))
}

function toChannelMonitorConfig(
  raw: RawChannelMonitorConfig
): ChannelMonitorConfig {
  return {
    id: raw.id ?? 0,
    channelId: raw.channel_id ?? 0,
    enabled: raw.enabled ?? false,
    managed: raw.managed ?? false,
    monitorMode: normalizeChannelMonitorMode(raw.monitor_mode),
    intervalSeconds: raw.interval_seconds || 600,
    jitterSeconds: raw.jitter_seconds ?? 60,
    monitoredModels: raw.monitored_models ?? [],
    endpointType: raw.endpoint_type || 'auto',
    stream: raw.stream ?? false,
    templateId: raw.template_id ?? 0,
    headers: toMonitorHeaders(raw.headers),
    bodyMode: normalizeMonitorBodyMode(raw.body_mode),
    bodyJson: raw.body_json ?? '',
    updatedTime: raw.updated_time ?? 0,
  }
}

function monitorConfigToWire(
  config: ChannelMonitorConfig
): RawChannelMonitorConfig {
  return {
    id: config.id,
    channel_id: config.channelId,
    enabled: config.enabled,
    managed: config.managed,
    monitor_mode: config.monitorMode,
    interval_seconds: config.intervalSeconds,
    jitter_seconds: config.jitterSeconds,
    monitored_models: config.monitoredModels,
    endpoint_type: config.endpointType,
    stream: config.stream,
    template_id: config.templateId,
    headers: monitorHeadersToWire(config.headers),
    body_mode: config.bodyMode,
    body_json: config.bodyJson,
  }
}

function toMonitorTemplate(raw: RawMonitorTemplate): MonitorTemplate {
  return {
    id: raw.id ?? 0,
    name: raw.name ?? '',
    description: raw.description ?? '',
    endpointType: raw.endpoint_type || 'auto',
    stream: raw.stream ?? false,
    headers: toMonitorHeaders(raw.headers),
    bodyMode: normalizeMonitorBodyMode(raw.body_mode),
    bodyJson: raw.body_json ?? '',
    updatedTime: raw.updated_time ?? 0,
  }
}

function monitorTemplateToWire(template: MonitorTemplate): RawMonitorTemplate {
  return {
    id: template.id,
    name: template.name,
    description: template.description,
    endpoint_type: template.endpointType,
    stream: template.stream,
    headers: monitorHeadersToWire(template.headers),
    body_mode: template.bodyMode,
    body_json: template.bodyJson,
  }
}

export async function getChannelMonitorConfig(
  channelId: number
): Promise<ChannelMonitorConfig | null> {
  const res = await api.get<ApiWireResponse<RawChannelMonitorConfig | null>>(
    `/api/channel_monitor/config/${channelId}`,
    channelActionConfig()
  )
  if (!res.data.success) throw new Error(res.data.message || 'Request failed')
  return res.data.data ? toChannelMonitorConfig(res.data.data) : null
}

export async function saveChannelMonitorConfig(
  config: ChannelMonitorConfig
): Promise<ChannelMonitorConfig> {
  const res = await api.put<ApiWireResponse<RawChannelMonitorConfig>>(
    '/api/channel_monitor/config',
    monitorConfigToWire(config),
    channelActionConfig()
  )
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Request failed')
  }
  return toChannelMonitorConfig(res.data.data)
}

export async function getMonitorTemplates(): Promise<MonitorTemplate[]> {
  const res = await api.get<ApiWireResponse<RawMonitorTemplate[]>>(
    '/api/channel_monitor/templates',
    channelActionConfig()
  )
  if (!res.data.success) throw new Error(res.data.message || 'Request failed')
  return (res.data.data ?? []).map(toMonitorTemplate)
}

export async function createMonitorTemplate(
  template: MonitorTemplate
): Promise<MonitorTemplate> {
  const res = await api.post<ApiWireResponse<RawMonitorTemplate>>(
    '/api/channel_monitor/templates',
    monitorTemplateToWire(template),
    channelActionConfig()
  )
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Request failed')
  }
  return toMonitorTemplate(res.data.data)
}

export async function updateMonitorTemplate(
  template: MonitorTemplate
): Promise<MonitorTemplate> {
  const res = await api.put<ApiWireResponse<RawMonitorTemplate>>(
    `/api/channel_monitor/templates/${template.id}`,
    monitorTemplateToWire(template),
    channelActionConfig()
  )
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Request failed')
  }
  return toMonitorTemplate(res.data.data)
}

export async function deleteMonitorTemplate(id: number): Promise<void> {
  const res = await api.delete<ApiWireResponse<null>>(
    `/api/channel_monitor/templates/${id}`,
    channelActionConfig()
  )
  if (!res.data.success) throw new Error(res.data.message || 'Request failed')
}

function toChannelMonitorProbeResult(
  raw: RawChannelMonitorProbeResult,
  fallbackModelName: string
): ChannelMonitorProbeResult {
  const trace = raw.trace ?? {}
  return {
    modelName: raw.model_name ?? fallbackModelName,
    endpointType: raw.endpoint_type || 'auto',
    stream: raw.stream ?? false,
    questionId: raw.question_id ?? 0,
    questionContent: raw.question_content ?? '',
    success: raw.success ?? false,
    latencyMs: raw.latency_ms ?? 0,
    ttftMs: raw.ttft_ms ?? 0,
    statusCode: raw.status_code ?? 0,
    errorMessage: raw.error_message ?? '',
    checkedAt: raw.checked_at ?? 0,
    trace: {
      requestMethod: trace.request_method ?? '',
      requestUrl: trace.request_url ?? '',
      requestHeaders: trace.request_headers ?? {},
      requestBody: trace.request_body ?? '',
      requestBodyTruncated: trace.request_body_truncated ?? false,
      requestWriteError: trace.request_write_error ?? '',
      responseUrl: trace.response_url ?? '',
      responseStatusCode: trace.response_status_code ?? 0,
      responseStatus: trace.response_status ?? '',
      responseHeaders: trace.response_headers ?? {},
      responseBody: trace.response_body ?? '',
      responseBodyTruncated: trace.response_body_truncated ?? false,
      bodyLimitBytes: trace.body_limit_bytes ?? 0,
    },
  }
}

export async function probeChannelNow(
  channelId: number,
  modelName: string
): Promise<ChannelMonitorProbeResult> {
  const res = await api.post<
    ApiWireResponse<
      RawChannelMonitorProbeResult | RawChannelMonitorProbeResult[]
    >
  >(
    '/api/channel_monitor/probe',
    { channel_id: channelId, model_name: modelName },
    channelActionConfig({ headers: { 'Cache-Control': 'no-store' } })
  )
  const payload = res.data.data
  const raw = Array.isArray(payload) ? payload[0] : payload
  if (!res.data.success || !raw) {
    throw new Error(res.data.message || 'Request failed')
  }
  return toChannelMonitorProbeResult(raw, modelName)
}

/**
 * Probe a channel and stream progress as Server-Sent Events so the console can
 * print upstream output while the request is still in flight. The probe itself
 * is identical to probeChannelNow; only the delivery differs.
 */
export async function probeChannelStream(
  channelId: number,
  modelName: string,
  handlers: ProbeStreamHandlers,
  signal?: AbortSignal
): Promise<void> {
  const authHeaders = await getFreshAuthHeaders()
  const response = await fetch('/api/channel_monitor/probe_stream', {
    method: 'POST',
    headers: {
      ...authHeaders,
      // Declaring SSE is what keeps the transcript live: the /api gzip
      // middleware skips compression for text/event-stream, and without that
      // the compressor buffers every flushed event until the request ends, so
      // the whole console would land at once when the probe finished.
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
      'Cache-Control': 'no-store',
    },
    body: JSON.stringify({ channel_id: channelId, model_name: modelName }),
    signal,
  })

  if (!response.ok || !response.body) {
    // A pre-stream failure is still a normal JSON error envelope.
    let message = `HTTP ${response.status}`
    try {
      const parsed = JSON.parse(await response.text())
      if (parsed?.message) message = parsed.message
    } catch {
      // Keep the status-code message.
    }
    throw new Error(message)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let eventName = ''
  let eventData = ''

  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      // The trailing fragment may be a partial line; keep it for the next read.
      buffer = lines.pop() ?? ''

      for (const line of lines) {
        if (line.startsWith('event: ')) {
          eventName = line.slice(7).trim()
          continue
        }
        if (line.startsWith('data: ')) {
          eventData = line.slice(6)
          continue
        }
        if (line !== '' || !eventName) continue

        if (eventName === 'end') return
        try {
          const parsed = JSON.parse(eventData)
          if (eventName === 'start') {
            handlers.onStart?.({
              modelName: parsed.model_name ?? modelName,
              endpointType: parsed.endpoint_type || 'auto',
              stream: parsed.stream ?? false,
              questionId: parsed.question_id ?? 0,
              questionContent: parsed.question_content ?? '',
              channelName: parsed.channel_name ?? '',
              channelType: parsed.channel_type ?? 0,
            } satisfies ProbeStreamStart)
          } else if (eventName === 'chunk') {
            handlers.onChunk?.({
              modelName: parsed.model_name ?? modelName,
              delta: parsed.delta ?? '',
            } satisfies ProbeStreamChunk)
          } else if (eventName === 'result') {
            handlers.onResult?.(
              toChannelMonitorProbeResult(
                parsed as RawChannelMonitorProbeResult,
                modelName
              )
            )
          } else if (eventName === 'error') {
            handlers.onError?.(parsed.message || 'Request failed')
          }
        } catch {
          // A malformed frame must not tear down an in-flight probe.
        }
        eventName = ''
        eventData = ''
      }
    }
  } finally {
    reader.releaseLock()
  }
}
