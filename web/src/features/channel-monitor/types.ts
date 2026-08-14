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

/** How the request body is combined with the endpoint's default probe body. */
export type BodyMode = 'default' | 'merge' | 'override'

/** Which monitored models receive scheduled probes. */
export type MonitorMode = 'default' | 'banned_only'

/** A single custom request header entry in the config form. */
export interface HeaderEntry {
  /** Client-side identity used to keep editable rows stable. */
  id: string
  /** Header name, e.g. "X-Trace-Id". Hop-by-hop names are ignored upstream. */
  key: string
  /** Header value. */
  value: string
}

/**
 * The monitoring strategy attached to one channel. `null` on a row means the
 * channel has no monitoring configured yet.
 */
export interface MonitorConfig {
  /** Master switch: whether monitoring actually runs for this channel. */
  enabled: boolean
  /** Default probes all selected models; banned-only probes recovery candidates. */
  monitorMode: MonitorMode
  /** Endpoint used to probe, e.g. "auto", "openai", "anthropic". */
  endpointType: string
  /** Whether the probe request is streamed. Defaults ON for new configs. */
  stream: boolean
  /** Seconds between probes (before jitter). Effective resolution is ~15s. */
  intervalSeconds: number
  /** Symmetric random jitter in seconds; each interval gets +/- this offset. */
  jitterSeconds: number
  /** Channel model names that have monitoring turned on for this channel. */
  monitoredModels: string[]
  /** Snapshot source template name, or "" when no template was used. */
  templateName: string
  /** Custom headers merged over the default probe headers (user wins). */
  headers: HeaderEntry[]
  /** How `bodyJson` combines with the default probe body. */
  bodyMode: BodyMode
  /** Raw JSON string for the merge/override body. */
  bodyJson: string
  /** Optional admin note shown in the monitor list. */
  remark: string
  /**
   * Channel hosting ("托管"): when on, the managed-policy engine autonomously
   * bans/recovers and up/downgrades this channel's monitored models based on
   * probe outcomes. The channel then bypasses the legacy real-traffic auto-ban.
   */
  managed: boolean
}

/**
 * Per-model runtime state the managed-policy engine has decided for one channel.
 * Present only for monitored models on a managed channel that the engine has
 * already acted on.
 */
export interface ManagedModelState {
  model: string
  /** True when the ban circuit-breaker has disabled this model on the channel. */
  banned: boolean
  /** True when the speed engine currently owns this model's priority. */
  priorityManaged: boolean
  /** The priority the speed engine assigned (valid when priorityManaged). */
  managedPriority: number
  /** The pre-policy priority snapshot, restored on upgrade. */
  originalPriority: number
  /** Consecutive disagreeing probes accumulated toward the next ban/recover flip. */
  confirmCount: number
}

export interface ChannelMonitorRow {
  id: number
  name: string
  /** Provider display name, e.g. "OpenAI", "Anthropic". */
  type: string
  group: string
  /** All models configured on the channel. */
  models: string[]
  priority: number
  /** Monitoring strategy, or null when nothing is configured yet. */
  config: MonitorConfig | null
  /** Unix seconds of the last probe, or 0 when never probed. */
  lastCheckedAt: number
  /** Per-model policy state, keyed nowhere — indexed by `model`. Empty unless managed. */
  managedStates: ManagedModelState[]
}

/** Global switch for scheduled probes and managed-policy execution. */
export interface ChannelMonitorSetting {
  enabled: boolean
  /** Daily quiet window master switch: while active, no channel is probed. */
  curfewEnabled: boolean
  /** Curfew start, local "HH:MM". A start later than the end wraps past midnight. */
  curfewStart: string
  /** Curfew end, local "HH:MM". */
  curfewEnd: string
  /**
   * Per-probe timeout in seconds. A probe exceeding it is cancelled and recorded
   * as a failure. Independent of the relay timeout, so it never shortens real
   * forwarding. Clamped server-side to a safe range.
   */
  probeTimeoutSeconds: number
  /** Maximum number of channel/model probes one scheduled task runs at once. */
  probeConcurrency: number
}

/** Final upstream HTTP exchange captured only for one manual probe response. */
export interface MonitorProbeTrace {
  requestMethod: string
  requestUrl: string
  requestHeaders: Record<string, string[]>
  requestBody: string
  requestBodyTruncated: boolean
  requestWriteError: string
  responseUrl: string
  responseStatusCode: number
  responseStatus: string
  responseHeaders: Record<string, string[]>
  responseBody: string
  responseBodyTruncated: boolean
  bodyLimitBytes: number
}

/** One model result returned by the administrator-triggered immediate probe. */
export interface ManualMonitorProbeResult {
  recordId: number
  modelName: string
  endpointType: string
  stream: boolean
  questionId: number
  questionContent: string
  success: boolean
  latencyMs: number
  ttftMs: number
  statusCode: number
  errorMessage: string
  checkedAt: number
  trace: MonitorProbeTrace
}

/** Global channel-hosting policy shown/edited in the managed-policy dialog. */
export interface ManagedPolicySetting {
  /** Master switch for the ban/recover circuit breaker. */
  banEnabled: boolean
  /** Consecutive confirming probes required before a ban/recover flip. */
  confirmCount: number
  /** Spacing between confirmation probes in seconds; floored at ~15s. */
  banConfirmIntervalSeconds: number
  /** Master switch for speed-based up/downgrade. */
  speedEnabled: boolean
  /** How many recent TTFT samples the speed mean is averaged over. */
  speedWindow: number
  /** Relative % gap below which channels share a speed tier. */
  tierDiffPercent: number
  /** Master switch for DingTalk ban/recover push notifications. */
  dingtalkEnabled: boolean
  /** DingTalk custom-robot webhook URL. */
  dingtalkWebhookUrl: string
  /** Optional DingTalk signing secret ("加签"); blank when unused. */
  dingtalkSecret: string
  /** Master switch: defer a managed channel's auto-disable to a monitor probe. */
  errorTriggerProbeEnabled: boolean
  /** Consecutive forwarding errors (per channel+model) that trigger a probe. */
  errorProbeThreshold: number
  /** Window in seconds within which the error streak must accumulate. */
  errorProbeWindowSeconds: number
}

/**
 * One row of the channel-recommendation maintenance table. Every channel is
 * listed; `weight` 0 with an empty `blurb` is the default (unmaintained) state.
 * The star rating is not maintained here — it is derived live from recent probe
 * speed when a recommendation list is built for a notification.
 */
export interface ChannelRecommendationRow {
  channelId: number
  channelName: string
  /** Provider display name, e.g. "OpenAI". */
  channelType: string
  /** Recommendation weight; 0 means the channel is not recommended. */
  weight: number
  /** Operator blurb, e.g. "廉价渠道，超低倍率！". */
  blurb: string
}

/** Request templates an admin can snapshot into a config's headers/body. */
export interface MonitorTemplate {
  /** Server id; 0 (or absent) for a not-yet-persisted draft. */
  id: number
  name: string
  description: string
  endpointType: string
  stream: boolean
  headers: HeaderEntry[]
  bodyMode: BodyMode
  bodyJson: string
  /** Unix seconds of the last template edit. */
  updatedAt: number
}

/** A persisted question used as the user message for conversational probes. */
export interface MonitorQuestion {
  /** Server id; 0 (or absent) for a not-yet-persisted draft. */
  id: number
  content: string
  /** Unix seconds of the last question edit. */
  updatedAt: number
}
