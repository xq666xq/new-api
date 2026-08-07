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

import {
  channelMonitorColumns,
  channelMonitorTableClassName,
  pinnedActionsCellClassName,
  pinnedActionsHeadClassName,
} from '../layout'

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
      1560
    )
    assert.match(channelMonitorTableClassName, /table-fixed/)
    assert.match(channelMonitorTableClassName, /min-w-\[1560px\]/)
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
      actions: 180,
    })
    assert.ok(widths.monitoring > widths.hosting)
    assert.ok(widths.monitoring > widths.remark)
    assert.ok(widths.models >= widths.monitoring)
  })

  // The actions column is frozen by hand rather than by DataTableView, so these
  // classes must keep matching getPinnedColumnClassName in
  // components/data-table/core/column-pinning.ts — otherwise the monitor table
  // silently stops freezing while the channel list still does.
  test('pins the actions column to the right edge on header and cells', () => {
    for (const className of [
      pinnedActionsHeadClassName,
      pinnedActionsCellClassName,
    ]) {
      assert.match(className, /\bsticky\b/)
      assert.match(className, /\bright-0\b/)
      assert.match(className, /shadow-\[-8px_0_10px_-10px_hsl\(var\(--foreground\)\)\]/)
    }
    // The header must stack above pinned cells so a scrolled row cannot cover it.
    assert.match(pinnedActionsHeadClassName, /\bz-30\b/)
    assert.match(pinnedActionsCellClassName, /\bz-10\b/)
    // An opaque cell background is what actually hides the columns scrolling under it.
    assert.match(pinnedActionsCellClassName, /\bbg-background\b/)
  })
})
