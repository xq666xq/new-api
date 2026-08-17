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
import { after, describe, test } from 'node:test'

import type { Row } from '@tanstack/react-table'
import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'localStorage',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'MouseEvent',
  'PointerEvent',
  'KeyboardEvent',
  'FocusEvent',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { TooltipProvider } = await import('@/components/ui/tooltip')
const { api } = await import('@/lib/api')
const { channelSchema } = await import('../../types')
const { ChannelsProvider, useChannels } = await import('../channels-provider')
const { DataTableRowActions } = await import('../data-table-row-actions')
const { ChannelProbeDialog } = await import('../dialogs/channel-probe-dialog')
const { useAuthStore } = await import('@/stores/auth-store')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type ApiMethod = (
  url: string,
  data?: unknown,
  config?: unknown
) => Promise<{ data: Record<string, unknown> }>
type MockableApi = {
  get: ApiMethod
  post: ApiMethod
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPost = apiClient.post
const originalFetch = globalThis.fetch
let probePayload: Record<string, unknown> | null = null

const probeResultEvent = {
  model_name: 'gpt-backup',
  endpoint_type: 'anthropic',
  stream: true,
  question_id: 7,
  question_content: 'Reply with a short confirmation.',
  success: true,
  latency_ms: 1250,
  ttft_ms: 245,
  status_code: 200,
  error_message: '',
  checked_at: 100,
  trace: {
    request_method: 'POST',
    request_url: 'https://upstream.example/v1/messages',
    request_headers: { Accept: ['application/json'] },
    request_body: '{"model":"gpt-backup"}',
    request_body_truncated: false,
    request_write_error: '',
    response_url: 'https://upstream.example/v1/messages',
    response_status_code: 200,
    response_status: '200 OK',
    response_headers: { 'Content-Type': ['application/json'] },
    response_body: '{"ok":true}',
    response_body_truncated: false,
    body_limit_bytes: 1048576,
  },
}

// The probe dialog consumes Server-Sent Events over fetch, so the fixture has to
// be a real streaming body: each frame is delivered as its own chunk so the test
// exercises the incremental decoder rather than one buffered parse.
function installApiFixtures(): void {
  // The streaming probe attaches auth headers itself instead of going through the
  // axios instance, so the store needs a live token or it would try to refresh.
  useAuthStore.getState().auth.setBundle({
    access_token: 'probe-test-token',
    access_expires_at: Math.floor(Date.now() / 1000) + 3600,
    user: null,
    session: null,
  } as never)

  apiClient.get = async (url) => {
    assert.equal(url, '/api/channel_monitor/config/12')
    return {
      data: {
        success: true,
        data: {
          id: 9,
          channel_id: 12,
          endpoint_type: 'anthropic',
          stream: true,
          template_id: 5,
          headers: [],
          body_mode: 'default',
          body_json: '',
          updated_time: 101,
        },
      },
    }
  }

  globalThis.fetch = (async (url: string, init?: RequestInit) => {
    assert.equal(url, '/api/channel_monitor/probe_stream')
    probePayload = JSON.parse(String(init?.body))

    const frames = [
      `event: start\ndata: ${JSON.stringify({
        model_name: 'gpt-backup',
        endpoint_type: 'anthropic',
        stream: true,
        question_id: 7,
        question_content: 'Reply with a short confirmation.',
        channel_name: 'Primary OpenAI',
        channel_type: 1,
      })}\n\n`,
      // Two separate relayed frames: the console must join them into one line
      // instead of turning each transport chunk into its own row.
      `event: chunk\ndata: ${JSON.stringify({
        model_name: 'gpt-backup',
        delta: 'data: {"choices":[{"delta":{"content":"Con"}}]}\n\n',
      })}\n\n`,
      `event: chunk\ndata: ${JSON.stringify({
        model_name: 'gpt-backup',
        delta: 'data: {"choices":[{"delta":{"content":"firmed"}}]}\n\n',
      })}\n\n`,
      `event: result\ndata: ${JSON.stringify(probeResultEvent)}\n\n`,
      'event: end\ndata: {}\n\n',
    ]

    const encoder = new TextEncoder()
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const frame of frames) controller.enqueue(encoder.encode(frame))
        controller.close()
      },
    })
    return { ok: true, status: 200, body } as unknown as Response
  }) as typeof globalThis.fetch
}

async function waitForCondition(
  condition: () => boolean,
  failureMessage: string
): Promise<void> {
  if (condition()) return

  await new Promise<void>((resolve, reject) => {
    const observer = new MutationObserver(() => {
      if (!condition()) return
      clearTimeout(timeoutId)
      observer.disconnect()
      resolve()
    })
    const timeoutId = setTimeout(() => {
      observer.disconnect()
      reject(new Error(`${failureMessage}: ${document.body.textContent}`))
    }, 1500)

    observer.observe(document, {
      attributes: true,
      childList: true,
      characterData: true,
      subtree: true,
    })
  })
}

const channel = channelSchema.parse({
  id: 12,
  type: 1,
  key: 'channel-key',
  status: 1,
  name: 'Primary OpenAI',
  created_time: 1,
  test_time: 0,
  response_time: 0,
  balance_updated_time: 0,
  models: 'gpt-test,gpt-backup',
  test_model: 'gpt-test',
})
const row = { original: channel } as Row<typeof channel>

function ChannelActionsHarness() {
  const { open, setOpen } = useChannels()
  return (
    <>
      <DataTableRowActions row={row} />
      <ChannelProbeDialog
        open={open === 'probe-channel'}
        onOpenChange={(nextOpen) => !nextOpen && setOpen(null)}
      />
    </>
  )
}

describe('channel row detection action', () => {
  after(() => {
    apiClient.get = originalGet
    apiClient.post = originalPost
    globalThis.fetch = originalFetch
    domWindow.close()
  })

  test('opens develop-style detection with all channel models visible', async () => {
    installApiFixtures()
    probePayload = null
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <QueryClientProvider client={queryClient}>
            <TooltipProvider>
              <ChannelsProvider>
                <ChannelActionsHarness />
              </ChannelsProvider>
            </TooltipProvider>
          </QueryClientProvider>
        </I18nextProvider>
      )
    )

    const detectionButton = host.querySelector<HTMLButtonElement>(
      'button[aria-label="Immediate detection"]'
    )
    assert.ok(detectionButton)

    await act(async () => detectionButton.click())

    const dialog = document.querySelector<HTMLElement>(
      '[data-slot="dialog-content"]'
    )
    assert.ok(dialog)
    await waitForCondition(
      () => dialog.textContent?.includes('anthropic') === true,
      'channel detection config was not shown'
    )
    assert.match(dialog.textContent ?? '', /Manual probe details/)
    assert.match(
      dialog.textContent ?? '',
      /Primary OpenAI.*details below are returned only once/s
    )
    assert.equal(dialog.querySelector('[data-slot="select-trigger"]'), null)

    const modelRadios = [
      ...dialog.querySelectorAll<HTMLButtonElement>(
        '[data-slot="radio-group-item"]'
      ),
    ]
    assert.equal(modelRadios.length, 2)
    assert.match(dialog.textContent ?? '', /gpt-test/)
    assert.match(dialog.textContent ?? '', /gpt-backup/)
    assert.equal(modelRadios[0].getAttribute('aria-checked'), 'true')

    await act(async () => modelRadios[1].click())
    assert.equal(modelRadios[1].getAttribute('aria-checked'), 'true')

    const startButton = [
      ...dialog.querySelectorAll<HTMLButtonElement>('button'),
    ].find((button) => button.textContent?.includes('Start probe'))
    assert.ok(startButton)
    await act(async () => startButton.click())

    // The console view streams first: it must show the decoded assistant text
    // and the success summary without the user opening anything else.
    await waitForCondition(
      () =>
        probePayload !== null &&
        dialog.textContent?.includes('Confirmed') === true &&
        dialog.textContent?.includes('Detection succeeded in 1.25 s') === true,
      'streamed console output was not shown'
    )
    assert.deepEqual(probePayload, {
      channel_id: 12,
      model_name: 'gpt-backup',
    })
    assert.match(dialog.textContent ?? '', /Retry/)
    assert.match(dialog.textContent ?? '', /Reply with a short confirmation\./)
    // Raw SSE framing stays out of the console; only decoded text is printed.
    assert.doesNotMatch(dialog.textContent ?? '', /"choices"/)

    // Consecutive deltas share one console row, so a word split across chunks
    // is not rendered as two lines.
    const probeConsole = dialog.querySelector('[data-slot="probe-console"]')
    assert.ok(probeConsole)
    const streamedRows = [...probeConsole.children].filter((node) =>
      node.textContent?.includes('Confirmed')
    )
    assert.equal(streamedRows.length, 1)
    assert.equal(streamedRows[0].textContent, 'Confirmed')

    // Switching to the trace view must still expose the full request/response
    // detail that existed before streaming was added.
    const traceToggle = [
      ...dialog.querySelectorAll<HTMLButtonElement>(
        '[data-slot="toggle-group-item"]'
      ),
    ].find((button) => button.textContent?.includes('Request details'))
    assert.ok(traceToggle)
    await act(async () => traceToggle.click())

    await waitForCondition(
      () => dialog.textContent?.includes('245 ms') === true,
      'trace details were not shown after switching views'
    )
    assert.match(dialog.textContent ?? '', /245 ms/)
    assert.match(dialog.textContent ?? '', /https:\/\/upstream\.example/)
    assert.match(
      dialog.textContent ?? '',
      /Basic probe records were saved; raw request and response details were not saved\./
    )

    await act(async () => root.unmount())
    queryClient.clear()
    host.remove()
  })
})
