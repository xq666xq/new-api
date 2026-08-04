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

import { getChannels } from '../api'

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
