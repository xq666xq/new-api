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
import { useCallback, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { probeChannelStream } from '../api'
import { ProbeStreamTextDecoder } from '../lib/probe-stream-text'
import type { ChannelMonitorProbeResult } from '../types'

/** One rendered line of streaming probe output. */
export type ProbeConsoleLine = {
  id: number
  kind: 'label' | 'value' | 'text' | 'error' | 'success' | 'divider'
  label?: string
  text: string
}

function formatProbeLatency(milliseconds: number): string {
  if (milliseconds >= 1000) return `${(milliseconds / 1000).toFixed(2)} s`
  return `${Math.max(0, Math.round(milliseconds))} ms`
}

/**
 * Owns one streaming probe run: the console transcript, the decoded upstream
 * text and the final trace result. Both the channel list and the channel
 * monitor probe dialogs drive their live output through this hook so the two
 * entry points behave identically.
 */
export function useStreamingProbe() {
  const { t } = useTranslation()
  const [lines, setLines] = useState<ProbeConsoleLine[]>([])
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<ChannelMonitorProbeResult | null>(null)
  const [error, setError] = useState('')
  const lineIdRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)

  const append = useCallback(
    (kind: ProbeConsoleLine['kind'], text: string, label?: string) => {
      setLines((prev) => [
        ...prev,
        { id: lineIdRef.current++, kind, text, label },
      ])
    },
    []
  )

  const reset = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
    setLines([])
    setResult(null)
    setError('')
    setRunning(false)
  }, [])

  const abort = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
  }, [])

  const run = useCallback(
    async (channelId: number, modelName: string) => {
      abortRef.current?.abort()
      const controller = new AbortController()
      abortRef.current = controller

      setRunning(true)
      setError('')
      setResult(null)
      setLines([])
      const decoder = new ProbeStreamTextDecoder()

      try {
        await probeChannelStream(
          channelId,
          modelName,
          {
            onStart: (event) => {
              append('label', t('Starting detection...'))
              if (event.channelName) {
                append('value', event.channelName, `${t('Channel')}: `)
              }
              append('value', event.modelName, `${t('Model')}: `)
              append('value', event.endpointType, `${t('Endpoint')}: `)
              append(
                'value',
                event.stream ? t('Stream') : t('Non-stream'),
                `${t('Mode')}: `
              )
              if (event.questionContent) {
                append('value', event.questionContent, `${t('Question')}: `)
              }
              append('divider', '')
            },
            onChunk: (chunk) => {
              const text = decoder.push(chunk.delta)
              if (text) append('text', text)
            },
            onResult: (finalResult) => {
              const trailing = decoder.flush()
              if (trailing) append('text', trailing)
              append('divider', '')
              if (finalResult.success) {
                append(
                  'success',
                  t('Detection succeeded in {{duration}}', {
                    duration: formatProbeLatency(finalResult.latencyMs),
                  })
                )
              } else {
                append('error', finalResult.errorMessage || t('Request failed'))
              }
              setResult(finalResult)
            },
            onError: (message) => {
              append('divider', '')
              append('error', message)
              setError(message)
            },
          },
          controller.signal
        )
      } catch (requestError: unknown) {
        if ((requestError as Error)?.name === 'AbortError') return
        const message =
          requestError instanceof Error
            ? requestError.message
            : t('Immediate detection failed')
        setError(message)
        append('divider', '')
        append('error', message)
      } finally {
        if (!controller.signal.aborted) {
          setRunning(false)
          abortRef.current = null
        }
      }
    },
    [append, t]
  )

  return { lines, running, result, error, run, reset, abort }
}
