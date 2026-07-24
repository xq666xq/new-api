import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import { api } from '@/lib/api'

import type {
  ChannelHealth,
  ChannelMonitorResult,
  ChannelStatusRow,
  StatusRange,
} from './types'

type ApiResponse<T> = { success: boolean; data?: T }

type RawCheck = {
  health: ChannelHealth
  total: number
  success: number
  start_at: number
  end_at: number
}

type RawRow = {
  channel_id: number
  channel_name: string
  channel_type: number
  group: string
  tag: string
  model: string
  health: ChannelHealth
  success_rate: number
  requests: number
  avg_response_ms: number
  last_checked_at: number
  last_ttft_ms: number
  last_latency_ms: number
  recent_checks: RawCheck[]
}

type RawResult = {
  id: number
  channel_id: number
  model_name: string
  question_id: number
  question_content: string
  success: boolean
  latency_ms: number
  status_code: number
  error_message: string
  checked_at: number
}

export async function getChannelStatus(
  range: StatusRange
): Promise<ChannelStatusRow[]> {
  const response = await api.get<ApiResponse<RawRow[]>>(
    '/api/channel_monitor/status',
    { params: { range } }
  )
  return (response.data.data ?? []).map((row) => ({
    id: `${row.channel_id}:${row.model}`,
    channelId: row.channel_id,
    name: row.channel_name,
    type: getChannelTypeLabel(row.channel_type),
    group: row.group,
    tag: row.tag ?? '',
    health: row.health,
    model: row.model,
    successRate: row.success_rate,
    avgResponseMs: row.avg_response_ms,
    requests: row.requests,
    lastCheckedAt: row.last_checked_at,
    lastTtftMs: row.last_ttft_ms,
    lastLatencyMs: row.last_latency_ms,
    recentChecks: row.recent_checks.map((check) => ({
      health: check.health,
      total: check.total,
      success: check.success,
      startAt: check.start_at,
      endAt: check.end_at,
    })),
  }))
}

// getModelStatus is the member-facing fetch: the same status data aggregated by
// model with channel identity stripped server-side. Channel-only fields
// (channelId, name, type, group, tag) come back empty, so the caller must not
// surface them; the row id falls back to the model name since there is no channel.
export async function getModelStatus(
  range: StatusRange
): Promise<ChannelStatusRow[]> {
  const response = await api.get<ApiResponse<RawRow[]>>('/api/model_status/', {
    params: { range },
  })
  return (response.data.data ?? []).map((row) => ({
    id: row.model,
    channelId: 0,
    name: '',
    type: '',
    group: '',
    tag: '',
    health: row.health,
    model: row.model,
    successRate: row.success_rate,
    avgResponseMs: row.avg_response_ms,
    requests: row.requests,
    lastCheckedAt: row.last_checked_at,
    lastTtftMs: row.last_ttft_ms,
    lastLatencyMs: row.last_latency_ms,
    recentChecks: row.recent_checks.map((check) => ({
      health: check.health,
      total: check.total,
      success: check.success,
      startAt: check.start_at,
      endAt: check.end_at,
    })),
  }))
}

export async function getChannelMonitorResults(
  channelId: number,
  modelName: string,
  startAt: number,
  endAt: number
): Promise<ChannelMonitorResult[]> {
  const response = await api.get<ApiResponse<RawResult[]>>(
    '/api/channel_monitor/status/details',
    {
      params: {
        channel_id: channelId,
        model: modelName,
        start_at: startAt,
        end_at: endAt,
      },
    }
  )
  return (response.data.data ?? []).map((result) => ({
    id: result.id,
    channelId: result.channel_id,
    modelName: result.model_name,
    questionId: result.question_id ?? 0,
    questionContent: result.question_content ?? '',
    success: result.success,
    latencyMs: result.latency_ms,
    statusCode: result.status_code,
    errorMessage: result.error_message ?? '',
    checkedAt: result.checked_at,
  }))
}
