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
  formatProbeBody,
  formatProbeDuration,
  formatProbeHeaders,
} from '../probe-display'

describe('manual probe detail formatting', () => {
  test('preserves multi-value headers and sorts names for stable inspection', () => {
    const formatted = formatProbeHeaders({
      'X-Zeta': ['z'],
      Accept: ['application/json', 'text/event-stream'],
    })

    assert.equal(
      formatted,
      '{\n  "Accept": [\n    "application/json",\n    "text/event-stream"\n  ],\n  "X-Zeta": "z"\n}'
    )
  })

  test('pretty prints JSON but leaves SSE or plain text unchanged', () => {
    assert.equal(formatProbeBody('{"ok":true}'), '{\n  "ok": true\n}')
    assert.equal(
      formatProbeBody('data: {"ok":true}\n\n'),
      'data: {"ok":true}\n\n'
    )
  })

  test('formats sub-second and multi-second timings without losing units', () => {
    assert.equal(formatProbeDuration(245), '245 ms')
    assert.equal(formatProbeDuration(1250), '1.25 s')
  })
})
