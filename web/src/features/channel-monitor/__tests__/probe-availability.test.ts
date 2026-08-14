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

import { canRunManualProbe, getManualProbeModels } from '../probe-availability'
import type { ChannelMonitorRow, MonitorConfig } from '../types'

function makeConfig(
  enabled: boolean,
  monitoredModels: string[]
): MonitorConfig {
  return {
    enabled,
    monitorMode: 'default',
    endpointType: 'openai',
    stream: true,
    intervalSeconds: 60,
    jitterSeconds: 0,
    monitoredModels,
    templateName: '',
    headers: [],
    bodyMode: 'default',
    bodyJson: '',
    remark: '',
    managed: false,
  }
}

function makeRow(config: MonitorConfig | null): ChannelMonitorRow {
  return {
    id: 1,
    name: 'channel-a',
    type: 'OpenAI',
    group: 'default',
    models: ['model-a'],
    priority: 0,
    config,
    lastCheckedAt: 0,
    managedStates: [],
  }
}

describe('manual probe availability', () => {
  test('allows channel models when scheduled and model monitoring are disabled', () => {
    assert.equal(canRunManualProbe(makeRow(makeConfig(false, []))), true)
  })

  test('requires a monitor config and at least one channel model', () => {
    const rowWithoutModels = makeRow(makeConfig(false, ['model-a']))
    rowWithoutModels.models = []

    assert.equal(canRunManualProbe(makeRow(null)), false)
    assert.equal(canRunManualProbe(rowWithoutModels), false)
  })

  test('returns all channel models in channel order without duplicates', () => {
    const row = makeRow(makeConfig(false, ['model-b']))
    row.models = [' model-a ', 'model-b', 'model-a', '', 'model-c']

    assert.deepEqual(getManualProbeModels(row), [
      'model-a',
      'model-b',
      'model-c',
    ])
  })
})
