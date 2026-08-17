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
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { ProbeConsoleLine } from '../hooks/use-streaming-probe'

const BLINKING_CURSOR = (
  <span className='bg-foreground ml-0.5 inline-block h-3.5 w-2 animate-pulse align-middle' />
)

/**
 * Terminal-style transcript of a manual probe: the assembled request summary
 * followed by decoded upstream output as it streams in.
 */
export function ProbeConsole(props: {
  lines: ProbeConsoleLine[]
  running: boolean
}) {
  const { t } = useTranslation()
  const endRef = useRef<HTMLDivElement>(null)

  // Keep the newest output in view as chunks stream in.
  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'end' })
  }, [props.lines])

  // While upstream text is streaming the cursor belongs at the end of that
  // text, not on a row of its own below it.
  const lastLine = props.lines.at(-1)
  const cursorFollowsText = props.running && lastLine?.kind === 'text'

  return (
    <div
      data-slot='probe-console'
      className='bg-muted/40 max-h-80 min-h-40 overflow-auto rounded-lg border p-3 font-mono text-xs leading-6'
    >
      {props.lines.length === 0 && !props.running ? (
        <div className='text-muted-foreground'>
          {t('Start the probe to see live upstream output.')}
        </div>
      ) : null}
      {props.lines.map((line) => {
        if (line.kind === 'divider') {
          return <div key={line.id} className='my-2 border-t' />
        }
        if (line.kind === 'error') {
          return (
            <div key={line.id} className='text-destructive flex gap-2'>
              <span aria-hidden='true'>✗</span>
              <span className='min-w-0 break-all whitespace-pre-wrap'>
                {line.text}
              </span>
            </div>
          )
        }
        if (line.kind === 'success') {
          return (
            <div key={line.id} className='text-success flex gap-2'>
              <span aria-hidden='true'>✓</span>
              <span className='min-w-0 break-all whitespace-pre-wrap'>
                {line.text}
              </span>
            </div>
          )
        }
        if (line.kind === 'value') {
          return (
            <div key={line.id} className='break-all whitespace-pre-wrap'>
              <span className='text-muted-foreground'>{line.label}</span>
              <span className='text-info'>{line.text}</span>
            </div>
          )
        }
        return (
          <div
            key={line.id}
            className={cn(
              'break-words whitespace-pre-wrap',
              line.kind === 'label'
                ? 'text-muted-foreground'
                : 'text-foreground'
            )}
          >
            {line.text}
            {cursorFollowsText && line.id === lastLine?.id
              ? BLINKING_CURSOR
              : null}
          </div>
        )
      })}
      {props.running && !cursorFollowsText ? BLINKING_CURSOR : null}
      <div ref={endRef} />
    </div>
  )
}
