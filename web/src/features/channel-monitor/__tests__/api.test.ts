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

import { runChannelMonitorProbe } from '../api'

describe('manual channel monitor probe API', () => {
  test('posts the selected model and maps the one-time raw trace', async () => {
    const calls: Parameters<typeof api.post>[] = []
    const originalPost = api.post
    api.post = (async (...args: Parameters<typeof api.post>) => {
      calls.push(args)
      return {
        data: {
          success: true,
          data: [
            {
              record_id: 9,
              model_name: 'gpt-test',
              endpoint_type: 'openai',
              stream: true,
              question_id: 3,
              question_content: 'hello',
              success: true,
              latency_ms: 456,
              ttft_ms: 123,
              status_code: 200,
              error_message: '',
              checked_at: 1_800_000_000,
              trace: {
                request_method: 'POST',
                request_url: 'https://upstream.example/v1/responses',
                request_headers: { 'User-Agent': ['codex-tui/0.145.0'] },
                request_body: '{"input":"hello"}',
                request_body_truncated: false,
                request_write_error: '',
                response_url: 'https://upstream.example/v1/responses',
                response_status_code: 200,
                response_status: '200 OK',
                response_headers: { 'Content-Type': ['text/event-stream'] },
                response_body: 'data: {"ok":true}\n\n',
                response_body_truncated: false,
                body_limit_bytes: 1_048_576,
              },
            },
          ],
        },
      } as Awaited<ReturnType<typeof api.post>>
    }) as typeof api.post

    try {
      const results = await runChannelMonitorProbe(12, 'gpt-test')

      assert.equal(calls.length, 1)
      assert.equal(calls[0]?.[0], '/api/channel_monitor/probe')
      assert.deepEqual(calls[0]?.[1], {
        channel_id: 12,
        model_name: 'gpt-test',
      })
      assert.equal(results[0]?.modelName, 'gpt-test')
      assert.equal(results[0]?.trace.requestMethod, 'POST')
      assert.deepEqual(results[0]?.trace.requestHeaders, {
        'User-Agent': ['codex-tui/0.145.0'],
      })
      assert.equal(results[0]?.trace.responseBody, 'data: {"ok":true}\n\n')
    } finally {
      api.post = originalPost
    }
  })
})
