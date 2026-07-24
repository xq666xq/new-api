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

import dayjs from 'dayjs'
import {
  Activity,
  AlertCircle,
  CheckCircle2,
  Loader2,
  Play,
  RotateCcw,
  Server,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import { getManualProbeModels } from '../probe-availability'
import {
  formatProbeBody,
  formatProbeBytes,
  formatProbeDuration,
  formatProbeHeaders,
} from '../probe-display'
import type { ChannelMonitorRow, ManualMonitorProbeResult } from '../types'

type ManualProbeDialogProps = {
  row: ChannelMonitorRow | null
  selectedModel: string
  loading: boolean
  failed: boolean
  errorMessage: string
  result: ManualMonitorProbeResult | null
  onSelectModel: (modelName: string) => void
  onRun: () => void
  onClose: () => void
}

function MetricCard(props: { label: string; value: string; tone?: string }) {
  return (
    <div className='bg-muted/35 rounded-xl border p-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className={cn('mt-1 text-base font-semibold', props.tone)}>
        {props.value}
      </div>
    </div>
  )
}

function TraceBlock(props: {
  title: string
  content: string
  emptyText: string
  truncated?: boolean
  limitBytes?: number
}) {
  const { t } = useTranslation()
  return (
    <section className='space-y-2'>
      <div className='flex items-center justify-between gap-3'>
        <h4 className='text-sm font-medium'>{props.title}</h4>
        {props.truncated ? (
          <Badge variant='outline' className='text-warning border-warning/30'>
            {t('Truncated at {{size}}', {
              size: formatProbeBytes(props.limitBytes ?? 0),
            })}
          </Badge>
        ) : null}
      </div>
      <pre className='bg-muted/45 max-h-72 min-h-16 overflow-auto rounded-xl border p-3 font-mono text-xs leading-5 break-all whitespace-pre-wrap'>
        {props.content || props.emptyText}
      </pre>
    </section>
  )
}

function ProbeResultDetails(props: { result: ManualMonitorProbeResult }) {
  const { t } = useTranslation()
  const result = props.result
  const trace = result.trace
  const statusCode = trace.responseStatusCode || result.statusCode

  return (
    <div className='space-y-4'>
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <MetricCard
          label={t('Result')}
          value={result.success ? t('Success') : t('Failed')}
          tone={result.success ? 'text-success' : 'text-destructive'}
        />
        <MetricCard
          label={t('Upstream status')}
          value={statusCode > 0 ? String(statusCode) : '—'}
        />
        <MetricCard
          label={t('First token')}
          value={result.ttftMs > 0 ? formatProbeDuration(result.ttftMs) : '—'}
        />
        <MetricCard
          label={t('Total latency')}
          value={formatProbeDuration(result.latencyMs)}
        />
      </div>

      <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 rounded-xl border px-3 py-2 text-xs'>
        <span>
          {t('Endpoint')}: {result.endpointType || 'auto'}
        </span>
        <span>{result.stream ? t('Stream') : t('Non-stream')}</span>
        <span>
          {t('Checked at')}:{' '}
          {dayjs(result.checkedAt * 1000).format('YYYY-MM-DD HH:mm:ss')}
        </span>
      </div>

      <div className='rounded-xl border px-3 py-2'>
        <div className='text-muted-foreground text-xs'>
          {t('Probe question')}
        </div>
        <div className='mt-1 text-sm whitespace-pre-wrap'>
          {result.questionContent || '—'}
        </div>
      </div>

      {result.errorMessage ? (
        <div className='border-destructive/25 bg-destructive/5 text-destructive flex items-start gap-2 rounded-xl border px-3 py-2 text-sm'>
          <AlertCircle className='mt-0.5 size-4 shrink-0' aria-hidden='true' />
          <span className='break-all'>{result.errorMessage}</span>
        </div>
      ) : null}

      <Tabs defaultValue='request'>
        <TabsList>
          <TabsTrigger value='request'>{t('Request')}</TabsTrigger>
          <TabsTrigger value='response'>{t('Response')}</TabsTrigger>
        </TabsList>
        <TabsContent value='request' className='space-y-4 pt-2'>
          <section className='space-y-2'>
            <h4 className='text-sm font-medium'>{t('Request URL')}</h4>
            <div className='bg-muted/45 flex flex-wrap items-center gap-2 rounded-xl border p-3 font-mono text-xs break-all'>
              <Badge variant='outline'>{trace.requestMethod || 'POST'}</Badge>
              <span>{trace.requestUrl || '—'}</span>
            </div>
          </section>
          {trace.requestWriteError ? (
            <div className='border-destructive/25 bg-destructive/5 text-destructive rounded-xl border px-3 py-2 text-sm break-all'>
              {t('Request write error')}: {trace.requestWriteError}
            </div>
          ) : null}
          <TraceBlock
            title={t('Request headers')}
            content={formatProbeHeaders(trace.requestHeaders)}
            emptyText='{}'
          />
          <TraceBlock
            title={t('Request body')}
            content={formatProbeBody(trace.requestBody)}
            emptyText={t('No body captured.')}
            truncated={trace.requestBodyTruncated}
            limitBytes={trace.bodyLimitBytes}
          />
        </TabsContent>
        <TabsContent value='response' className='space-y-4 pt-2'>
          <section className='space-y-2'>
            <h4 className='text-sm font-medium'>{t('Response URL')}</h4>
            <div className='bg-muted/45 rounded-xl border p-3 font-mono text-xs break-all'>
              {trace.responseUrl || t('No response captured.')}
            </div>
          </section>
          <TraceBlock
            title={t('Response headers')}
            content={formatProbeHeaders(trace.responseHeaders)}
            emptyText='{}'
          />
          <TraceBlock
            title={t('Response body')}
            content={formatProbeBody(trace.responseBody)}
            emptyText={t('No body captured.')}
            truncated={trace.responseBodyTruncated}
            limitBytes={trace.bodyLimitBytes}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}

export function ManualProbeDialog(props: ManualProbeDialogProps) {
  const { t } = useTranslation()

  if (!props.row) return null

  const models = getManualProbeModels(props.row)
  const modelItems = models.map((modelName) => ({
    label: modelName,
    value: modelName,
  }))
  const result =
    props.result?.modelName === props.selectedModel ? props.result : null
  const canRun = props.selectedModel !== '' && !props.loading
  const hasOutcome = result !== null || props.failed

  let statusIcon = (
    <Activity className='text-primary size-5' aria-hidden='true' />
  )
  let statusTitle = t('Ready')
  let statusDescription = t(
    'Using the same request assembly and relay path as scheduled monitoring.'
  )
  let statusClassName = 'border-primary/20 bg-primary/5'
  if (props.loading) {
    statusIcon = (
      <Loader2
        className='text-primary size-5 animate-spin'
        aria-hidden='true'
      />
    )
    statusTitle = t('Running real probe...')
  } else if (props.failed) {
    statusIcon = (
      <AlertCircle className='text-destructive size-5' aria-hidden='true' />
    )
    statusTitle = t('Manual probe failed')
    statusDescription =
      props.errorMessage ||
      t(
        'Using the same request assembly and relay path as scheduled monitoring.'
      )
    statusClassName = 'border-destructive/25 bg-destructive/5'
  } else if (result) {
    statusIcon = result.success ? (
      <CheckCircle2 className='text-success size-5' aria-hidden='true' />
    ) : (
      <AlertCircle className='text-destructive size-5' aria-hidden='true' />
    )
    statusTitle = result.success ? t('Success') : t('Failed')
    statusDescription = `${result.modelName} · ${formatProbeDuration(result.latencyMs)}`
    statusClassName = result.success
      ? 'border-success/25 bg-success/5'
      : 'border-destructive/25 bg-destructive/5'
  }

  let actionIcon = <Play data-icon='inline-start' aria-hidden='true' />
  let actionLabel = t('Start probe')
  if (props.loading) {
    actionIcon = (
      <Loader2
        data-icon='inline-start'
        className='animate-spin'
        aria-hidden='true'
      />
    )
    actionLabel = t('Running real probe...')
  } else if (hasOutcome) {
    actionIcon = <RotateCcw data-icon='inline-start' aria-hidden='true' />
    actionLabel = t('Retry')
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Manual probe details')}
      description={`${props.row.name} · ${t('The request and response details below are returned only once and are not stored.')}`}
      contentClassName={cn(
        'transition-[max-width]',
        result ? 'sm:max-w-6xl' : 'sm:max-w-2xl'
      )}
      contentHeight={result ? 'min(720px, calc(100vh - 12rem))' : 'auto'}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={props.loading}
            onClick={props.onClose}
          >
            {t('Close')}
          </Button>
          <Button type='button' disabled={!canRun} onClick={props.onRun}>
            {actionIcon}
            {actionLabel}
          </Button>
        </>
      }
      showCloseButton
    >
      <div className='space-y-4'>
        <section className='bg-muted/20 rounded-lg border p-4'>
          <div className='flex min-w-0 items-center gap-3'>
            <div className='border-primary/15 bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-lg border'>
              <Server className='size-5' aria-hidden='true' />
            </div>
            <div className='min-w-0 flex-1'>
              <div className='truncate font-semibold'>{props.row.name}</div>
              <div className='text-muted-foreground truncate text-xs'>
                {props.row.type} · {props.row.group || '—'}
              </div>
            </div>
            <Badge
              variant='outline'
              className={cn(
                'shrink-0',
                props.row.config?.enabled
                  ? 'border-success/30 bg-success/10 text-success'
                  : 'text-muted-foreground'
              )}
            >
              {props.row.config?.enabled ? t('Enabled') : t('Disabled')}
            </Badge>
          </div>

          <div className='bg-background/70 mt-4 grid overflow-hidden rounded-lg border max-sm:divide-y sm:grid-cols-3 sm:divide-x'>
            <div className='px-3 py-2.5'>
              <div className='text-muted-foreground text-[11px] uppercase'>
                {t('Channel ID')}
              </div>
              <div className='mt-0.5 text-sm font-medium'>{props.row.id}</div>
            </div>
            <div className='px-3 py-2.5'>
              <div className='text-muted-foreground text-[11px] uppercase'>
                {t('Provider')}
              </div>
              <div className='mt-0.5 truncate text-sm font-medium'>
                {props.row.type}
              </div>
            </div>
            <div className='px-3 py-2.5'>
              <div className='text-muted-foreground text-[11px] uppercase'>
                {t('Endpoint')}
              </div>
              <div className='mt-0.5 truncate text-sm font-medium'>
                {props.row.config?.endpointType || 'auto'}
              </div>
            </div>
          </div>
        </section>

        <section className='space-y-2'>
          <Label htmlFor='manual-probe-model'>{t('Test model')}</Label>
          <Select
            items={modelItems}
            value={props.selectedModel}
            disabled={props.loading}
            onValueChange={(value) => {
              if (typeof value === 'string') props.onSelectModel(value)
            }}
          >
            <SelectTrigger
              id='manual-probe-model'
              className='h-10 w-full min-w-0 rounded-lg'
            >
              <SelectValue className='min-w-0 truncate font-mono' />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {models.map((modelName) => (
                  <SelectItem key={modelName} value={modelName}>
                    <span className='font-mono'>{modelName}</span>
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </section>

        <section
          className={cn(
            'flex items-start gap-3 rounded-lg border p-4',
            statusClassName
          )}
          aria-live='polite'
        >
          <div className='bg-background/80 flex size-9 shrink-0 items-center justify-center rounded-lg border'>
            {statusIcon}
          </div>
          <div className='min-w-0'>
            <div className='font-medium'>{statusTitle}</div>
            <div className='text-muted-foreground mt-0.5 text-sm break-all'>
              {statusDescription}
            </div>
          </div>
        </section>

        {result ? (
          <div className='space-y-4 border-t pt-4'>
            <div className='border-success/20 bg-success/5 text-success flex items-center gap-2 rounded-xl border px-3 py-2 text-xs'>
              <CheckCircle2 className='size-4 shrink-0' aria-hidden='true' />
              {t(
                'Basic probe records were saved; raw request and response details were not saved.'
              )}
            </div>
            <ProbeResultDetails result={result} />
          </div>
        ) : null}
      </div>
    </Dialog>
  )
}
