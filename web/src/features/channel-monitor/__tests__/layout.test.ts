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

import { channelMonitorColumns, channelMonitorTableClassName } from '../layout'

describe('channel monitor table layout', () => {
  test('keeps stable column order and a fixed scrollable table width', () => {
    assert.deepEqual(
      channelMonitorColumns.map((column) => column.key),
      [
        'channel',
        'models',
        'monitoring',
        'hosting',
        'enabled',
        'remark',
        'strategy',
        'actions',
      ]
    )
    assert.equal(
      channelMonitorColumns.reduce((total, column) => total + column.width, 0),
      1520
    )
    assert.match(channelMonitorTableClassName, /table-fixed/)
    assert.match(channelMonitorTableClassName, /min-w-\[1520px\]/)
  })

  test('gives monitoring more room than hosting and remark', () => {
    const widths = Object.fromEntries(
      channelMonitorColumns.map((column) => [column.key, column.width])
    )

    assert.deepEqual(widths, {
      channel: 240,
      models: 300,
      monitoring: 300,
      hosting: 96,
      enabled: 84,
      remark: 140,
      strategy: 220,
      actions: 140,
    })
    assert.ok(widths.monitoring > widths.hosting)
    assert.ok(widths.monitoring > widths.remark)
    assert.ok(widths.models >= widths.monitoring)
  })
})
