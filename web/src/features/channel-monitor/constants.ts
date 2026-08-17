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
import type { ChannelMonitorRow, MonitorConfig } from './types'

/** Endpoint options shared with the config dialog (labels are i18n keys). */
export const MONITOR_ENDPOINT_OPTIONS: Array<{ value: string; label: string }> =
  [
    { value: 'auto', label: 'Auto detect (default)' },
    { value: 'openai', label: 'OpenAI (/v1/chat/completions)' },
    { value: 'openai-response', label: 'OpenAI Responses (/v1/responses)' },
    { value: 'anthropic', label: 'Anthropic (/v1/messages)' },
    {
      value: 'gemini',
      label: 'Gemini (/v1beta/models/{model}:generateContent)',
    },
    { value: 'jina-rerank', label: 'Jina Rerank (/v1/rerank)' },
    {
      value: 'image-generation',
      label: 'Image Generation (/v1/images/generations)',
    },
    { value: 'embeddings', label: 'Embeddings (/v1/embeddings)' },
  ]

/** Endpoints where streaming makes no sense; the stream switch is disabled. */
export const STREAM_INCOMPATIBLE_ENDPOINTS = new Set([
  'embeddings',
  'image-generation',
  'jina-rerank',
])

/** Channels currently snapshotting from a template, by template name. */
export function channelsUsingTemplate(
  rows: ChannelMonitorRow[],
  templateName: string
): ChannelMonitorRow[] {
  if (!templateName) return []
  return rows.filter((row) => row.config?.templateName === templateName)
}

/**
 * A fresh default config used when configuring an unmonitored channel. Banned-only
 * probing plus hosting is the intended starting point: the policy engine watches the
 * channel and only spends probes on models it has actually banned. Keep in sync with
 * the DEFAULT_* constants in
 * features/channels/components/dialogs/detection-config-panel.tsx.
 */
export function newDefaultConfig(): MonitorConfig {
  return {
    enabled: true,
    monitorMode: 'banned_only',
    endpointType: 'auto',
    stream: true,
    intervalSeconds: 600,
    jitterSeconds: 60,
    monitoredModels: [],
    templateName: '',
    headers: [],
    bodyMode: 'default',
    bodyJson: '',
    remark: '',
    managed: true,
  }
}
