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

import { ProbeStreamTextDecoder } from '../probe-stream-text'

describe('probe stream text decoding', () => {
  test('extracts assistant text from each supported upstream stream format', () => {
    const cases: Array<{ name: string; frames: string[]; expected: string }> = [
      {
        name: 'openai chat completions',
        frames: [
          'data: {"choices":[{"delta":{"role":"assistant"}}]}\n\n',
          'data: {"choices":[{"delta":{"content":"He"}}]}\n\n',
          'data: {"choices":[{"delta":{"content":"llo"}}]}\n\n',
          'data: [DONE]\n\n',
        ],
        expected: 'Hello',
      },
      {
        name: 'claude messages',
        frames: [
          'event: content_block_delta\n',
          'data: {"delta":{"type":"text_delta","text":"Hi"}}\n\n',
          'data: {"delta":{"type":"text_delta","text":" there"}}\n\n',
        ],
        expected: 'Hi there',
      },
      {
        name: 'gemini generateContent',
        frames: [
          'data: {"candidates":[{"content":{"parts":[{"text":"Salut"}]}}]}\n\n',
        ],
        expected: 'Salut',
      },
    ]

    for (const testCase of cases) {
      const decoder = new ProbeStreamTextDecoder()
      let text = ''
      for (const frame of testCase.frames) text += decoder.push(frame)
      text += decoder.flush()
      assert.equal(text, testCase.expected, testCase.name)
    }
  })

  test('reassembles a frame split across chunk boundaries', () => {
    const decoder = new ProbeStreamTextDecoder()
    // The transport can cut anywhere, including mid-JSON.
    let text = decoder.push('data: {"choices":[{"delta":{"con')
    text += decoder.push('tent":"split"}}]}\n\n')
    text += decoder.flush()
    assert.equal(text, 'split')
  })

  test('surfaces an error envelope instead of dropping it', () => {
    const decoder = new ProbeStreamTextDecoder()
    const text =
      decoder.push('{"error":{"message":"Invalid API key"}}') + decoder.flush()
    assert.equal(text, 'Invalid API key\n')
  })

  test('keeps a non-JSON upstream body visible', () => {
    const decoder = new ProbeStreamTextDecoder()
    const text = decoder.push('502 Bad Gateway') + decoder.flush()
    assert.equal(text, '502 Bad Gateway\n')
  })
})
