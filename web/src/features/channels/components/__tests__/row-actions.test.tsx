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
let probePayload: Record<string, unknown> | null = null

function installApiFixtures(): void {
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
  apiClient.post = async (url, data) => {
    assert.equal(url, '/api/channel_monitor/probe')
    assert.ok(data && typeof data === 'object')
    probePayload = data as Record<string, unknown>
    return {
      data: {
        success: true,
        data: [
          {
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
          },
        ],
      },
    }
  }
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

    await waitForCondition(
      () =>
        probePayload !== null &&
        dialog.textContent?.includes(
          'Basic probe records were saved; raw request and response details were not saved.'
        ) === true &&
        dialog.textContent?.includes('245 ms') === true,
      'probe result details were not shown'
    )
    assert.deepEqual(probePayload, {
      channel_id: 12,
      model_name: 'gpt-backup',
    })
    assert.match(dialog.textContent ?? '', /Retry/)
    assert.match(dialog.textContent ?? '', /245 ms/)
    assert.match(dialog.textContent ?? '', /1\.25 s/)
    assert.match(dialog.textContent ?? '', /Reply with a short confirmation\./)
    assert.match(dialog.textContent ?? '', /https:\/\/upstream\.example/)

    await act(async () => root.unmount())
    queryClient.clear()
    host.remove()
  })
})
