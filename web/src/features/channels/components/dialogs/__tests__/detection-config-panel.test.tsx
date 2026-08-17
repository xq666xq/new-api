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

import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLTextAreaElement',
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

const { act, useRef, useState } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { TooltipProvider } = await import('@/components/ui/tooltip')
const { api } = await import('@/lib/api')
const { DetectionConfigPanel } = await import('../detection-config-panel')
type DetectionConfigPanelHandle = import('../detection-config-panel').DetectionConfigPanelHandle
const { ModelMonitoringSwitch } = await import('../model-monitoring-switch')

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
  put: ApiMethod
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPut = apiClient.put
let savedPayload: Record<string, unknown> | null = null

function installApiFixtures(
  config: Record<string, unknown> | null = null
): void {
  apiClient.get = async (url) => {
    if (url === '/api/channel_monitor/config/12') {
      return { data: { success: true, data: config } }
    }
    if (url === '/api/channel_monitor/templates') {
      return {
        data: {
          success: true,
          data: [
            {
              id: 5,
              name: 'Streaming probe',
              description: 'Responses request',
              endpoint_type: 'openai-response',
              stream: true,
              headers: [{ key: 'X-Probe', value: 'template-value' }],
              body_mode: 'override',
              body_json: '{"input":"template"}',
              updated_time: 100,
            },
          ],
        },
      }
    }
    throw new Error(`Unexpected GET ${url}`)
  }
  apiClient.put = async (url, data) => {
    assert.equal(url, '/api/channel_monitor/config')
    assert.ok(data && typeof data === 'object')
    savedPayload = data as Record<string, unknown>
    return {
      data: {
        success: true,
        data: {
          ...savedPayload,
          id: 9,
          updated_time: 101,
        },
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

// Mirrors how ChannelTestDialog hosts the panel: the save button lives outside
// it and triggers the panel through its ref, so the tests drive the same path
// the dialog footer does.
function DetectionConfigHarness() {
  const [endpointType, setEndpointType] = useState('auto')
  const [stream, setStream] = useState(false)
  const [monitoringEnabled, setMonitoringEnabled] = useState(false)
  const [managed, setManaged] = useState(false)
  const [monitoredModels, setMonitoredModels] = useState<string[]>([])
  const [saveBusy, setSaveBusy] = useState(false)
  const panelRef = useRef<DetectionConfigPanelHandle>(null)
  const [queryClient] = useState(() => new QueryClient())
  return (
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <TooltipProvider>
          <DetectionConfigPanel
            ref={panelRef}
            open
            channelId={12}
            monitoringEnabled={monitoringEnabled}
            managed={managed}
            monitoredModels={monitoredModels}
            endpointType={endpointType}
            stream={stream}
            onMonitoringEnabledChange={setMonitoringEnabled}
            onManagedChange={setManaged}
            onMonitoredModelsChange={setMonitoredModels}
            onEndpointTypeChange={setEndpointType}
            onStreamChange={setStream}
            onSaveBusyChange={setSaveBusy}
          />
          <button
            type='button'
            disabled={saveBusy}
            onClick={() => panelRef.current?.save()}
          >
            Save detection config
          </button>
          <output data-testid='endpoint-type'>{endpointType}</output>
          <output data-testid='stream'>{String(stream)}</output>
        </TooltipProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

function ModelMonitoringHarness() {
  const [checked, setChecked] = useState(false)
  return (
    <I18nextProvider i18n={i18n}>
      <ModelMonitoringSwitch
        model='gpt-test'
        checked={checked}
        onCheckedChange={() => setChecked((current) => !current)}
      />
    </I18nextProvider>
  )
}

function findButton(label: string): HTMLButtonElement {
  const button = [
    ...document.querySelectorAll<HTMLButtonElement>('button'),
  ].find((candidate) => candidate.textContent?.includes(label))
  assert.ok(button)
  return button
}

describe('channel test monitoring configuration', () => {
  after(() => {
    apiClient.get = originalGet
    apiClient.put = originalPut
    domWindow.close()
  })

  test('uses a selected template without exposing its request details', async () => {
    installApiFixtures()
    savedPayload = null
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)

    await act(async () => root.render(<DetectionConfigHarness />))

    const templateTrigger = host.querySelector<HTMLButtonElement>(
      '#monitor-template-select'
    )
    assert.ok(templateTrigger)
    await act(async () => templateTrigger.click())
    await waitForCondition(
      () =>
        [
          ...document.querySelectorAll<HTMLElement>(
            '[data-slot="select-item"]'
          ),
        ].some((item) => item.textContent?.includes('Streaming probe')),
      'template option did not load'
    )

    const templateOption = [
      ...document.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
    ].find((item) => item.textContent?.includes('Streaming probe'))
    assert.ok(templateOption)
    await act(async () => templateOption.click())

    await waitForCondition(
      () =>
        host.querySelector('[data-testid="endpoint-type"]')?.textContent ===
          'openai-response' &&
        templateTrigger.textContent?.includes('Streaming probe') === true,
      'template snapshot was not applied'
    )
    assert.equal(templateTrigger.textContent?.trim(), 'Streaming probe')
    assert.equal(
      host.querySelector('[data-testid="stream"]')?.textContent,
      'true'
    )
    assert.equal(
      host.querySelector<HTMLInputElement>('input[aria-label="Header name"]'),
      null
    )
    assert.equal(
      host.querySelector<HTMLInputElement>('input[aria-label="Header value"]'),
      null
    )
    assert.equal(
      host.querySelector<HTMLTextAreaElement>('#monitor-body-json'),
      null
    )

    const monitoringSwitch = host.querySelector<HTMLButtonElement>(
      '#channel-monitoring-enabled'
    )
    const managedSwitch =
      host.querySelector<HTMLButtonElement>('#channel-managed')
    assert.ok(monitoringSwitch)
    assert.ok(managedSwitch)
    // Hosting and banned-only probing are already the defaults for a channel
    // with no saved config, so only monitoring has to be switched on here.
    await act(async () => monitoringSwitch.click())

    await act(async () => findButton('Save detection config').click())
    await waitForCondition(() => savedPayload !== null, 'config was not saved')

    assert.deepEqual(savedPayload, {
      id: 0,
      channel_id: 12,
      enabled: true,
      managed: true,
      monitor_mode: 'banned_only',
      interval_seconds: 600,
      jitter_seconds: 60,
      monitored_models: [],
      endpoint_type: 'openai-response',
      stream: true,
      template_id: 5,
      headers: [{ key: 'X-Probe', value: 'template-value' }],
      body_mode: 'override',
      body_json: '{"input":"template"}',
    })

    await act(async () => root.unmount())
    host.remove()
  })

  test('restores a saved template by name after templates load', async () => {
    installApiFixtures({
      id: 9,
      channel_id: 12,
      endpoint_type: 'openai-response',
      stream: true,
      template_id: 5,
      headers: [{ key: 'X-Probe', value: 'template-value' }],
      body_mode: 'override',
      body_json: '{"input":"template"}',
      updated_time: 101,
    })
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)

    await act(async () => root.render(<DetectionConfigHarness />))

    await waitForCondition(
      () =>
        host
          .querySelector<HTMLButtonElement>('#monitor-template-select')
          ?.textContent?.includes('Streaming probe') === true,
      'saved template name was not restored'
    )

    const templateTrigger = host.querySelector<HTMLButtonElement>(
      '#monitor-template-select'
    )
    assert.ok(templateTrigger)
    assert.equal(templateTrigger.textContent?.trim(), 'Streaming probe')
    assert.equal(
      host.querySelector<HTMLInputElement>('input[aria-label="Header name"]'),
      null
    )
    assert.equal(
      host.querySelector<HTMLTextAreaElement>('#monitor-body-json'),
      null
    )

    await act(async () => root.unmount())
    host.remove()
  })

  test('clears template request settings when switching to no template', async () => {
    installApiFixtures()
    savedPayload = null
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)

    await act(async () => root.render(<DetectionConfigHarness />))

    const templateTrigger = host.querySelector<HTMLButtonElement>(
      '#monitor-template-select'
    )
    assert.ok(templateTrigger)
    await act(async () => templateTrigger.click())
    await waitForCondition(
      () =>
        [
          ...document.querySelectorAll<HTMLElement>(
            '[data-slot="select-item"]'
          ),
        ].some((item) => item.textContent?.includes('Streaming probe')),
      'template option did not load'
    )

    const templateOption = [
      ...document.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
    ].find((item) => item.textContent?.includes('Streaming probe'))
    assert.ok(templateOption)
    await act(async () => templateOption.click())
    await waitForCondition(
      () => templateTrigger.textContent?.includes('Streaming probe') === true,
      'template was not selected'
    )

    await act(async () => templateTrigger.click())
    const noTemplateOption = [
      ...document.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
    ].find((item) => item.textContent?.trim() === 'No template')
    assert.ok(noTemplateOption)
    await act(async () => noTemplateOption.click())

    await waitForCondition(
      () =>
        templateTrigger.textContent?.trim() === 'No template' &&
        host.querySelector('[data-testid="endpoint-type"]')?.textContent ===
          'auto' &&
        host.querySelector('[data-testid="stream"]')?.textContent === 'false',
      'template request settings were not cleared'
    )
    assert.equal(
      host.querySelector<HTMLInputElement>('input[aria-label="Header name"]'),
      null
    )
    assert.equal(
      host.querySelector<HTMLTextAreaElement>('#monitor-body-json'),
      null
    )

    await act(async () => findButton('Save detection config').click())
    await waitForCondition(() => savedPayload !== null, 'config was not saved')
    assert.deepEqual(savedPayload, {
      id: 0,
      channel_id: 12,
      enabled: false,
      managed: true,
      monitor_mode: 'banned_only',
      interval_seconds: 600,
      jitter_seconds: 60,
      monitored_models: [],
      endpoint_type: 'auto',
      stream: false,
      template_id: 0,
      headers: [],
      body_mode: 'default',
      body_json: '',
    })

    await act(async () => root.unmount())
    host.remove()
  })

  test('toggles monitoring for the named model from the models table control', async () => {
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)

    await act(async () => root.render(<ModelMonitoringHarness />))

    const monitoringSwitch = host.querySelector<HTMLButtonElement>(
      '[aria-label="Enable monitoring: gpt-test"]'
    )
    assert.ok(monitoringSwitch)
    assert.equal(monitoringSwitch.getAttribute('aria-checked'), 'false')

    await act(async () => monitoringSwitch.click())

    assert.equal(monitoringSwitch.getAttribute('aria-checked'), 'true')

    await act(async () => root.unmount())
    host.remove()
  })
})
