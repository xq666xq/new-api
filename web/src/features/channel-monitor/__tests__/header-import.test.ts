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

import { parseImportedHeaders } from '../header-import'

describe('channel monitor header import', () => {
  test('serializes captured structured header values as JSON strings', () => {
    const headers = parseImportedHeaders(
      JSON.stringify({
        'x-codex-turn-metadata': {
          installation_id: 'installation-123',
          request_kind: 'turn',
        },
        'x-openai-internal-codex-responses-lite': true,
      }),
      'invalid'
    )

    assert.deepEqual(headers, [
      {
        key: 'x-codex-turn-metadata',
        value: '{"installation_id":"installation-123","request_kind":"turn"}',
      },
      {
        key: 'x-openai-internal-codex-responses-lite',
        value: 'true',
      },
    ])
  })

  test('rejects a non-object header collection', () => {
    assert.throws(
      () => parseImportedHeaders('["User-Agent"]', 'invalid headers'),
      /invalid headers/
    )
  })
})
