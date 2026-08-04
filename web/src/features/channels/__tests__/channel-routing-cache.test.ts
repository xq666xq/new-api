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

import { QueryClient } from '@tanstack/react-query'

import { channelStatusQueryKeys } from '@/features/channel-status/query-keys'

import {
  channelsQueryKeys,
  invalidateChannelRoutingQueries,
} from '../lib/channel-actions'

describe('channel routing cache invalidation', () => {
  test('manual channel routing changes invalidate channel and model status views', () => {
    const queryClient = new QueryClient()
    const channelListKey = channelsQueryKeys.list({ p: 1 })
    const channelStatusKey = channelStatusQueryKeys.list('channel', '1h')
    const modelStatusKey = channelStatusQueryKeys.list('model', '24h')
    queryClient.setQueryData(channelListKey, [])
    queryClient.setQueryData(channelStatusKey, [])
    queryClient.setQueryData(modelStatusKey, [])

    invalidateChannelRoutingQueries(queryClient)

    assert.equal(queryClient.getQueryState(channelListKey)?.isInvalidated, true)
    assert.equal(
      queryClient.getQueryState(channelStatusKey)?.isInvalidated,
      true
    )
    assert.equal(queryClient.getQueryState(modelStatusKey)?.isInvalidated, true)
  })
})
