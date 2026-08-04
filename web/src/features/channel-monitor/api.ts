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
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import { api } from '@/lib/api'

import type {
  BodyMode,
  ChannelMonitorRow,
  ChannelMonitorSetting,
  ChannelRecommendationRow,
  HeaderEntry,
  ManualMonitorProbeResult,
  ManagedModelState,
  ManagedPolicySetting,
  MonitorConfig,
  MonitorQuestion,
  MonitorTemplate,
} from './types'

type ApiResponse<T> = {
  success: boolean
  message?: string
  data?: T
}

// ---------------------------------------------------------------------------
// Wire shapes: the backend uses snake_case, a numeric channel `type`, and a
// JSON-array `headers` column. These raw shapes are transformed into the
// camelCase contract the components already consume.
// ---------------------------------------------------------------------------

type RawHeader = { key?: string; value?: string }

type RawConfig = {
  id: number
  channel_id: number
  enabled: boolean
  endpoint_type: string
  stream: boolean
  interval_seconds: number
  jitter_seconds: number
  monitored_models: string[] | null
  template_name: string
  headers: RawHeader[] | null
  body_mode: string
  body_json: string
  remark: string
  managed: boolean
  last_checked_at: number
}

type RawManagedState = {
  model: string
  banned: boolean
  priority_managed: boolean
  managed_priority: number
  original_priority: number
  confirm_count: number
}

type RawRow = {
  id: number
  name: string
  type: number
  group: string
  models: string[] | null
  priority: number
  config: RawConfig | null
  managed_states: RawManagedState[] | null
  last_checked_at: number
}

type RawTemplate = {
  id: number
  name: string
  description: string
  endpoint_type: string
  stream: boolean
  headers: RawHeader[] | null
  body_mode: string
  body_json: string
  updated_time: number
}

type RawQuestion = {
  id: number
  content: string
  updated_time: number
}

type RawProbeTrace = {
  request_method: string
  request_url: string
  request_headers: Record<string, string[]> | null
  request_body: string
  request_body_truncated: boolean
  request_write_error: string
  response_url: string
  response_status_code: number
  response_status: string
  response_headers: Record<string, string[]> | null
  response_body: string
  response_body_truncated: boolean
  body_limit_bytes: number
}

type RawManualProbeResult = {
  record_id: number
  model_name: string
  endpoint_type: string
  stream: boolean
  question_id: number
  question_content: string
  success: boolean
  latency_ms: number
  ttft_ms: number
  status_code: number
  error_message: string
  checked_at: number
  trace: RawProbeTrace
}

function asBodyMode(value: string): BodyMode {
  return value === 'merge' || value === 'override' ? value : 'default'
}

function toHeaderEntries(raw: RawHeader[] | null): HeaderEntry[] {
  if (!raw) return []
  return raw.map((h) => ({
    id: crypto.randomUUID(),
    key: h.key ?? '',
    value: h.value ?? '',
  }))
}

function toMonitorConfig(raw: RawConfig | null): MonitorConfig | null {
  if (!raw) return null
  return {
    enabled: raw.enabled,
    endpointType: raw.endpoint_type || 'auto',
    stream: raw.stream,
    intervalSeconds: raw.interval_seconds || 60,
    jitterSeconds: raw.jitter_seconds ?? 0,
    monitoredModels: raw.monitored_models ?? [],
    templateName: raw.template_name ?? '',
    headers: toHeaderEntries(raw.headers),
    bodyMode: asBodyMode(raw.body_mode),
    bodyJson: raw.body_json ?? '',
    remark: raw.remark ?? '',
    managed: raw.managed ?? false,
  }
}

function toManagedState(raw: RawManagedState): ManagedModelState {
  return {
    model: raw.model,
    banned: raw.banned,
    priorityManaged: raw.priority_managed,
    managedPriority: raw.managed_priority,
    originalPriority: raw.original_priority,
    confirmCount: raw.confirm_count,
  }
}

function toMonitorRow(raw: RawRow): ChannelMonitorRow {
  return {
    id: raw.id,
    name: raw.name,
    type: getChannelTypeLabel(raw.type),
    group: raw.group ?? '',
    models: raw.models ?? [],
    priority: raw.priority,
    config: toMonitorConfig(raw.config),
    managedStates: (raw.managed_states ?? []).map(toManagedState),
    lastCheckedAt: raw.last_checked_at ?? 0,
  }
}

function toMonitorTemplate(raw: RawTemplate): MonitorTemplate {
  return {
    id: raw.id,
    name: raw.name,
    description: raw.description ?? '',
    endpointType: raw.endpoint_type || 'openai',
    stream: raw.stream,
    headers: toHeaderEntries(raw.headers),
    bodyMode: asBodyMode(raw.body_mode),
    bodyJson: raw.body_json ?? '',
    updatedAt: raw.updated_time ?? 0,
  }
}

function toMonitorQuestion(raw: RawQuestion): MonitorQuestion {
  return {
    id: raw.id,
    content: raw.content ?? '',
    updatedAt: raw.updated_time ?? 0,
  }
}

function toManualProbeResult(
  raw: RawManualProbeResult
): ManualMonitorProbeResult {
  return {
    recordId: raw.record_id,
    modelName: raw.model_name,
    endpointType: raw.endpoint_type,
    stream: raw.stream,
    questionId: raw.question_id,
    questionContent: raw.question_content,
    success: raw.success,
    latencyMs: raw.latency_ms,
    ttftMs: raw.ttft_ms,
    statusCode: raw.status_code,
    errorMessage: raw.error_message ?? '',
    checkedAt: raw.checked_at,
    trace: {
      requestMethod: raw.trace.request_method ?? '',
      requestUrl: raw.trace.request_url ?? '',
      requestHeaders: raw.trace.request_headers ?? {},
      requestBody: raw.trace.request_body ?? '',
      requestBodyTruncated: raw.trace.request_body_truncated,
      requestWriteError: raw.trace.request_write_error ?? '',
      responseUrl: raw.trace.response_url ?? '',
      responseStatusCode: raw.trace.response_status_code ?? 0,
      responseStatus: raw.trace.response_status ?? '',
      responseHeaders: raw.trace.response_headers ?? {},
      responseBody: raw.trace.response_body ?? '',
      responseBodyTruncated: raw.trace.response_body_truncated,
      bodyLimitBytes: raw.trace.body_limit_bytes ?? 0,
    },
  }
}

function configToWire(channelId: number, config: MonitorConfig) {
  return {
    channel_id: channelId,
    enabled: config.enabled,
    endpoint_type: config.endpointType,
    stream: config.stream,
    interval_seconds: config.intervalSeconds,
    jitter_seconds: config.jitterSeconds,
    monitored_models: config.monitoredModels,
    template_name: config.templateName,
    headers: config.headers.map(({ key, value }) => ({ key, value })),
    body_mode: config.bodyMode,
    body_json: config.bodyJson,
    remark: config.remark,
    managed: config.managed,
  }
}

function templateToWire(tpl: MonitorTemplate) {
  return {
    id: tpl.id,
    name: tpl.name,
    description: tpl.description,
    endpoint_type: tpl.endpointType,
    stream: tpl.stream,
    headers: tpl.headers.map(({ key, value }) => ({ key, value })),
    body_mode: tpl.bodyMode,
    body_json: tpl.bodyJson,
  }
}

function questionToWire(question: MonitorQuestion) {
  return {
    id: question.id,
    content: question.content,
  }
}

// ---------------------------------------------------------------------------
// API calls
// ---------------------------------------------------------------------------

/** Fetch the monitor list, synced with the channel list. */
export async function getChannelMonitorList(): Promise<ChannelMonitorRow[]> {
  const res = await api.get<ApiResponse<RawRow[]>>('/api/channel_monitor/')
  return (res.data?.data ?? []).map(toMonitorRow)
}

/** Create or update a single channel's monitor config. */
export async function saveChannelMonitorConfig(
  channelId: number,
  config: MonitorConfig
): Promise<MonitorConfig | null> {
  const res = await api.post<ApiResponse<RawConfig>>(
    '/api/channel_monitor/config',
    configToWire(channelId, config)
  )
  return toMonitorConfig(res.data?.data ?? null)
}

type RawMonitorSetting = {
  enabled: boolean
  curfew_enabled: boolean
  curfew_start: string
  curfew_end: string
  probe_timeout_seconds: number
  probe_concurrency: number
}

function toMonitorSetting(
  raw: RawMonitorSetting | null
): ChannelMonitorSetting {
  return {
    enabled: raw?.enabled ?? true,
    curfewEnabled: raw?.curfew_enabled ?? false,
    curfewStart: raw?.curfew_start || '23:00',
    curfewEnd: raw?.curfew_end || '07:00',
    probeTimeoutSeconds: raw?.probe_timeout_seconds || 60,
    probeConcurrency: raw?.probe_concurrency ?? 0,
  }
}

/** Fetch the global switch that gates scheduled probes and managed policy runs. */
export async function getChannelMonitorSetting(): Promise<ChannelMonitorSetting> {
  const res = await api.get<ApiResponse<RawMonitorSetting>>(
    '/api/channel_monitor/settings'
  )
  return toMonitorSetting(res.data?.data ?? null)
}

/** Update the global monitor switch + curfew without touching channel configs. */
export async function updateChannelMonitorSetting(
  setting: ChannelMonitorSetting
): Promise<ChannelMonitorSetting> {
  const res = await api.put<ApiResponse<RawMonitorSetting>>(
    '/api/channel_monitor/settings',
    {
      enabled: setting.enabled,
      curfew_enabled: setting.curfewEnabled,
      curfew_start: setting.curfewStart,
      curfew_end: setting.curfewEnd,
      probe_timeout_seconds: setting.probeTimeoutSeconds,
      probe_concurrency: setting.probeConcurrency,
    }
  )
  return toMonitorSetting(res.data?.data ?? null)
}

/**
 * Bring one channel's next scheduled monitor probe forward so the scheduler runs
 * it on its next tick as a normal scheduled sweep (feeding managed policy). This
 * is distinct from runChannelMonitorProbe, which is a one-off in-request diagnostic.
 */
export async function triggerChannelMonitorNow(
  channelId: number
): Promise<void> {
  const res = await api.post<ApiResponse<null>>(
    '/api/channel_monitor/trigger',
    { channel_id: channelId }
  )
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'trigger monitor failed')
  }
}

/** Run one channel model with the configured probe strategy and return its trace. */
export async function runChannelMonitorProbe(
  channelId: number,
  modelName: string
): Promise<ManualMonitorProbeResult[]> {
  const res = await api.post<ApiResponse<RawManualProbeResult[]>>(
    '/api/channel_monitor/probe',
    { channel_id: channelId, model_name: modelName }
  )
  if (!res.data?.success || !res.data.data) {
    throw new Error(res.data?.message || 'manual monitor probe failed')
  }
  return res.data.data.map(toManualProbeResult)
}

/** Fetch all reusable probe templates. */
export async function getMonitorTemplates(): Promise<MonitorTemplate[]> {
  const res = await api.get<ApiResponse<RawTemplate[]>>(
    '/api/channel_monitor/templates'
  )
  return (res.data?.data ?? []).map(toMonitorTemplate)
}

/** Create a new template. */
export async function createMonitorTemplate(
  tpl: MonitorTemplate
): Promise<MonitorTemplate | null> {
  const res = await api.post<ApiResponse<RawTemplate>>(
    '/api/channel_monitor/templates',
    templateToWire(tpl)
  )
  return res.data?.data ? toMonitorTemplate(res.data.data) : null
}

/** Update an existing template. */
export async function updateMonitorTemplate(
  tpl: MonitorTemplate
): Promise<MonitorTemplate | null> {
  const res = await api.put<ApiResponse<RawTemplate>>(
    '/api/channel_monitor/templates',
    templateToWire(tpl)
  )
  return res.data?.data ? toMonitorTemplate(res.data.data) : null
}

/** Delete a template by id. */
export async function deleteMonitorTemplate(id: number): Promise<void> {
  await api.delete(`/api/channel_monitor/templates/${id}`)
}

/** Re-apply a template snapshot to every channel currently using it. */
export async function applyMonitorTemplate(
  id: number
): Promise<{ affected: number }> {
  const res = await api.post<ApiResponse<{ affected: number }>>(
    `/api/channel_monitor/templates/${id}/apply`
  )
  return res.data?.data ?? { affected: 0 }
}

/** Fetch the global conversational probe question library. */
export async function getMonitorQuestions(): Promise<MonitorQuestion[]> {
  const res = await api.get<ApiResponse<RawQuestion[]>>(
    '/api/channel_monitor/questions'
  )
  return (res.data?.data ?? []).map(toMonitorQuestion)
}

/** Create a new probe question. */
export async function createMonitorQuestion(
  question: MonitorQuestion
): Promise<MonitorQuestion | null> {
  const res = await api.post<ApiResponse<RawQuestion>>(
    '/api/channel_monitor/questions',
    questionToWire(question)
  )
  return res.data?.data ? toMonitorQuestion(res.data.data) : null
}

/** Update an existing probe question. */
export async function updateMonitorQuestion(
  question: MonitorQuestion
): Promise<MonitorQuestion | null> {
  const res = await api.put<ApiResponse<RawQuestion>>(
    '/api/channel_monitor/questions',
    questionToWire(question)
  )
  return res.data?.data ? toMonitorQuestion(res.data.data) : null
}

/** Delete a probe question by id. */
export async function deleteMonitorQuestion(id: number): Promise<void> {
  await api.delete(`/api/channel_monitor/questions/${id}`)
}

// ---------------------------------------------------------------------------
// Managed policy (channel hosting)
// ---------------------------------------------------------------------------

type RawPolicy = {
  ban_enabled: boolean
  confirm_count: number
  ban_confirm_interval_seconds: number
  speed_enabled: boolean
  speed_window: number
  tier_diff_percent: number
  dingtalk_enabled: boolean
  dingtalk_webhook_url: string
  dingtalk_secret: string
  error_trigger_probe_enabled: boolean
  error_probe_threshold: number
  error_probe_window_seconds: number
}

function toManagedPolicy(raw: RawPolicy): ManagedPolicySetting {
  return {
    banEnabled: raw.ban_enabled,
    confirmCount: raw.confirm_count,
    banConfirmIntervalSeconds: raw.ban_confirm_interval_seconds,
    speedEnabled: raw.speed_enabled,
    speedWindow: raw.speed_window,
    tierDiffPercent: raw.tier_diff_percent,
    dingtalkEnabled: raw.dingtalk_enabled ?? false,
    dingtalkWebhookUrl: raw.dingtalk_webhook_url ?? '',
    dingtalkSecret: raw.dingtalk_secret ?? '',
    errorTriggerProbeEnabled: raw.error_trigger_probe_enabled ?? false,
    errorProbeThreshold: raw.error_probe_threshold ?? 2,
    errorProbeWindowSeconds: raw.error_probe_window_seconds ?? 60,
  }
}

/** Fetch the global channel-hosting policy, clamped to safe ranges by the server. */
export async function getManagedPolicy(): Promise<ManagedPolicySetting> {
  const res = await api.get<ApiResponse<RawPolicy>>(
    '/api/channel_monitor/policy'
  )
  const raw = res.data?.data
  return raw
    ? toManagedPolicy(raw)
    : {
        banEnabled: false,
        confirmCount: 3,
        banConfirmIntervalSeconds: 15,
        speedEnabled: false,
        speedWindow: 5,
        tierDiffPercent: 30,
        dingtalkEnabled: false,
        dingtalkWebhookUrl: '',
        dingtalkSecret: '',
        errorTriggerProbeEnabled: false,
        errorProbeThreshold: 2,
        errorProbeWindowSeconds: 60,
      }
}

/** Persist the global channel-hosting policy; returns the clamped result. */
export async function updateManagedPolicy(
  policy: ManagedPolicySetting
): Promise<ManagedPolicySetting> {
  const res = await api.put<ApiResponse<RawPolicy>>(
    '/api/channel_monitor/policy',
    {
      ban_enabled: policy.banEnabled,
      confirm_count: policy.confirmCount,
      ban_confirm_interval_seconds: policy.banConfirmIntervalSeconds,
      speed_enabled: policy.speedEnabled,
      speed_window: policy.speedWindow,
      tier_diff_percent: policy.tierDiffPercent,
      dingtalk_enabled: policy.dingtalkEnabled,
      dingtalk_webhook_url: policy.dingtalkWebhookUrl,
      dingtalk_secret: policy.dingtalkSecret,
      error_trigger_probe_enabled: policy.errorTriggerProbeEnabled,
      error_probe_threshold: policy.errorProbeThreshold,
      error_probe_window_seconds: policy.errorProbeWindowSeconds,
    }
  )
  const raw = res.data?.data
  return raw ? toManagedPolicy(raw) : policy
}

// ---------------------------------------------------------------------------
// Channel recommendations
// ---------------------------------------------------------------------------

type RawRecommendationRow = {
  channel_id: number
  channel_name: string
  channel_type: number
  weight: number
  blurb: string
}

function toRecommendationRow(
  raw: RawRecommendationRow
): ChannelRecommendationRow {
  return {
    channelId: raw.channel_id,
    channelName: raw.channel_name ?? '',
    channelType: getChannelTypeLabel(raw.channel_type),
    weight: raw.weight ?? 0,
    blurb: raw.blurb ?? '',
  }
}

/** Fetch one recommendation row per channel (default weight 0 for unedited). */
export async function getChannelRecommendations(): Promise<
  ChannelRecommendationRow[]
> {
  const res = await api.get<ApiResponse<RawRecommendationRow[]>>(
    '/api/channel_monitor/recommendations'
  )
  return (res.data?.data ?? []).map(toRecommendationRow)
}

/** Persist edited recommendation weights/blurbs; returns the merged rows. */
export async function saveChannelRecommendations(
  rows: ChannelRecommendationRow[]
): Promise<ChannelRecommendationRow[]> {
  const res = await api.post<ApiResponse<RawRecommendationRow[]>>(
    '/api/channel_monitor/recommendations',
    {
      recommendations: rows.map((r) => ({
        channel_id: r.channelId,
        weight: r.weight,
        blurb: r.blurb,
      })),
    }
  )
  return (res.data?.data ?? []).map(toRecommendationRow)
}
