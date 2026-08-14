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

/**
 * Incrementally turns raw upstream probe bytes into readable assistant text for
 * the console view. The probe relays whatever the provider sent, so a chunk can
 * be OpenAI/Claude/Gemini SSE frames, a bare JSON object, or an error envelope.
 * Anything that is not recognised assistant text is reported as raw so the
 * console never silently swallows an upstream response.
 */
export class ProbeStreamTextDecoder {
  private pending = ''

  /**
   * Feed one raw chunk and return the readable text it produced. Incomplete
   * trailing frames are buffered until the rest of the bytes arrive.
   */
  push(chunk: string): string {
    this.pending += chunk

    // Non-stream responses arrive without SSE framing: hold everything until
    // flush() so a single JSON body is parsed once, whole.
    if (!this.pending.includes('\n')) return ''

    const frames = this.pending.split('\n')
    this.pending = frames.pop() ?? ''

    let text = ''
    for (const frame of frames) {
      text += this.readFrame(frame)
    }
    return text
  }

  /** Drain whatever is still buffered once the stream has ended. */
  flush(): string {
    if (this.pending === '') return ''
    const remaining = this.pending
    this.pending = ''
    return this.readFrame(remaining)
  }

  private readFrame(frame: string): string {
    const line = frame.trim()
    if (line === '') return ''

    // SSE control lines carry no assistant text.
    if (
      line.startsWith('event:') ||
      line.startsWith('id:') ||
      line.startsWith('retry:') ||
      line.startsWith(':')
    ) {
      return ''
    }

    const payload = line.startsWith('data:') ? line.slice(5).trim() : line
    if (payload === '' || payload === '[DONE]') return ''

    try {
      return extractProbeText(JSON.parse(payload))
    } catch {
      // Not JSON: a plain-text or HTML error body is still worth showing.
      return line.startsWith('data:') ? '' : `${line}\n`
    }
  }
}

/**
 * Pulls assistant-visible text out of one decoded upstream payload, covering the
 * OpenAI chat/completions, OpenAI Responses, Claude and Gemini shapes plus error
 * envelopes. Returns '' when the payload carries only metadata (usage, role
 * headers, ping frames).
 */
export function extractProbeText(payload: unknown): string {
  const frame = asRecord(payload)
  if (!frame) return ''

  // Error envelopes: OpenAI-style { error: {...} } and bare { code, message }.
  if (frame.error !== undefined && frame.error !== null) {
    if (typeof frame.error === 'string') return `${frame.error}\n`
    const errorObject = asRecord(frame.error)
    const message = errorObject && asString(errorObject.message)
    return `${message || JSON.stringify(frame.error)}\n`
  }
  const bareMessage = asString(frame.message)
  if (bareMessage && frame.code !== undefined) return `${bareMessage}\n`

  // OpenAI chat completions, streaming and buffered.
  const choice = asRecord(asArray(frame.choices)?.[0])
  if (choice) {
    const delta = asRecord(choice.delta) ?? asRecord(choice.message)
    return (
      asString(delta?.content) ??
      asString(delta?.reasoning_content) ??
      asString(choice.text) ??
      ''
    )
  }

  // Claude messages streaming.
  const claudeDelta = asRecord(frame.delta)
  const claudeText = asString(claudeDelta?.text)
  if (claudeText !== undefined) return claudeText
  const contentParts = asArray(frame.content)
  if (contentParts) return joinPartTexts(contentParts)

  // OpenAI Responses streaming emits the delta as a bare string.
  const responsesDelta = asString(frame.delta)
  if (responsesDelta !== undefined) return responsesDelta

  // Gemini generateContent, streaming and buffered.
  const candidateContent = asRecord(
    asRecord(asArray(frame.candidates)?.[0])?.content
  )
  const geminiParts = asArray(candidateContent?.parts)
  if (geminiParts) return joinPartTexts(geminiParts)

  // Embeddings / rerank probes have no text; the console shows the metric line.
  return ''
}

function joinPartTexts(parts: unknown[]): string {
  return parts.map((part) => asString(asRecord(part)?.text) ?? '').join('')
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return undefined
  }
  return value as Record<string, unknown>
}

function asArray(value: unknown): unknown[] | undefined {
  return Array.isArray(value) ? value : undefined
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}
