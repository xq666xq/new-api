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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { api } from '@/lib/api'

import {
  createMonitorTemplate,
  getChannels,
  probeChannelNow,
  saveChannelMonitorConfig,
} from '../api'

describe('channel collection API', () => {
  test('uses the canonical trailing-slash route for the channel list', async () => {
    const response = {
      success: true,
      data: { items: [], total: 0, page: 1, page_size: 20 },
    }
    const calls: Parameters<typeof api.get>[] = []
    const originalGet = api.get
    api.get = (async (...args: Parameters<typeof api.get>) => {
      calls.push(args)
      return { data: response } as Awaited<ReturnType<typeof api.get>>
    }) as typeof api.get

    try {
      const result = await getChannels({ p: 1, page_size: 20 })

      assert.deepEqual(result, response)
      assert.equal(calls.length, 1)
      assert.equal(calls[0]?.[0], '/api/channel/')
      assert.deepEqual(calls[0]?.[1], {
        params: { p: 1, page_size: 20 },
      })
    } finally {
      api.get = originalGet
    }
  })
})

describe('channel monitor API wire conversion', () => {
  test('saves a detection config without leaking local header IDs', async () => {
    const calls: Parameters<typeof api.put>[] = []
    const originalPut = api.put
    api.put = (async (...args: Parameters<typeof api.put>) => {
      calls.push(args)
      return {
        data: {
          success: true,
          data: {
            ...(args[1] as object),
            id: 7,
            updated_time: 1_800_000_000,
          },
        },
      } as Awaited<ReturnType<typeof api.put>>
    }) as typeof api.put

    try {
      const saved = await saveChannelMonitorConfig({
        id: 0,
        channelId: 12,
        endpointType: 'openai-response',
        stream: true,
        templateId: 3,
        headers: [{ id: 'local-header', key: 'X-Probe', value: 'draft' }],
        bodyMode: 'merge',
        bodyJson: '{"max_output_tokens":32}',
        updatedTime: 0,
      })

      assert.equal(calls[0]?.[0], '/api/channel_monitor/config')
      assert.deepEqual(calls[0]?.[1], {
        id: 0,
        channel_id: 12,
        endpoint_type: 'openai-response',
        stream: true,
        template_id: 3,
        headers: [{ key: 'X-Probe', value: 'draft' }],
        body_mode: 'merge',
        body_json: '{"max_output_tokens":32}',
      })
      assert.equal(saved.channelId, 12)
      assert.equal(saved.headers[0]?.value, 'draft')
    } finally {
      api.put = originalPut
    }
  })

  test('creates a detection template using the monitor wire format', async () => {
    const calls: Parameters<typeof api.post>[] = []
    const originalPost = api.post
    api.post = (async (...args: Parameters<typeof api.post>) => {
      calls.push(args)
      return {
        data: {
          success: true,
          data: {
            ...(args[1] as object),
            id: 5,
            updated_time: 1_800_000_100,
          },
        },
      } as Awaited<ReturnType<typeof api.post>>
    }) as typeof api.post

    try {
      const created = await createMonitorTemplate({
        id: 0,
        name: 'Streaming probe',
        description: 'responses endpoint',
        endpointType: 'openai-response',
        stream: true,
        headers: [{ id: 'local-header', key: 'X-Probe', value: 'template' }],
        bodyMode: 'override',
        bodyJson: '{"input":"hello"}',
        updatedTime: 0,
      })

      assert.equal(calls[0]?.[0], '/api/channel_monitor/templates')
      assert.deepEqual(calls[0]?.[1], {
        id: 0,
        name: 'Streaming probe',
        description: 'responses endpoint',
        endpoint_type: 'openai-response',
        stream: true,
        headers: [{ key: 'X-Probe', value: 'template' }],
        body_mode: 'override',
        body_json: '{"input":"hello"}',
      })
      assert.equal(created.id, 5)
    } finally {
      api.post = originalPost
    }
  })

  test('maps the first result from the manual monitor probe array', async () => {
    const originalPost = api.post
    api.post = (async () => {
      return {
        data: {
          success: true,
          data: [
            {
              model_name: 'gpt-test',
              endpoint_type: 'openai',
              stream: false,
              question_id: 3,
              question_content: 'Reply with OK.',
              success: true,
              latency_ms: 456,
              ttft_ms: 0,
              status_code: 200,
              error_message: '',
              checked_at: 1_800_000_200,
              trace: {
                request_method: 'POST',
                request_url: 'https://upstream.example/v1/chat/completions',
                response_body: '{"ok":true}',
                body_limit_bytes: 262_144,
              },
            },
          ],
        },
      } as Awaited<ReturnType<typeof api.post>>
    }) as typeof api.post

    try {
      const result = await probeChannelNow(12, 'gpt-test')

      assert.equal(result.modelName, 'gpt-test')
      assert.equal(result.questionId, 3)
      assert.equal(result.questionContent, 'Reply with OK.')
      assert.equal(result.trace.requestMethod, 'POST')
      assert.equal(result.trace.responseBody, '{"ok":true}')
      assert.equal(result.trace.bodyLimitBytes, 262_144)
    } finally {
      api.post = originalPost
    }
  })
})
