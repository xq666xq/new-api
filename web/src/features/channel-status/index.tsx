import { useQuery } from '@tanstack/react-query'
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
import { Loader2, RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  getChannelMonitorResults,
  getChannelStatus,
  getModelStatus,
} from './api'
import type {
  BucketState,
  ChannelHealth,
  ChannelMonitorResult,
  ChannelStatusRow,
  RecentCheck,
  StatusRange,
} from './types'

/** Range options for the time-window dropdown. */
const RANGE_OPTIONS: StatusRange[] = ['1h', '6h', '12h', '24h', '7d']

/** Sentinel select value for "all tags" (empty string is not a valid item value). */
const ALL_TAGS = '__all__'

/**
 * Debounced text filter input that stays responsive while typing and is
 * IME-safe. The visible value updates on every keystroke, but the debounced
 * value is only reported to the parent once the user pauses (~500ms).
 *
 * The debounce is a ref-based timer, not a state-based hook: a hook like
 * useDebounce fires an internal setState every interval, and that extra
 * rerender aborts an in-progress IME composition (typing pinyin, then pressing
 * space a second later would drop the Chinese text). Here composition is tracked
 * in a ref and the timer callback only calls the parent, so nothing rerenders
 * the input mid-composition. Reporting is suppressed during composition and
 * resumed on compositionend, so a half-typed pinyin string never filters.
 */
function DebouncedSearchInput({
  value,
  onDebouncedChange,
  placeholder,
  className,
}: {
  value: string
  onDebouncedChange: (value: string) => void
  placeholder: string
  className?: string
}) {
  const [draft, setDraft] = useState(value)
  const composingRef = useRef(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Hold the latest callback in a ref so the timer always calls the current one
  // without needing to reschedule when the parent re-renders.
  const onChangeRef = useRef(onDebouncedChange)
  onChangeRef.current = onDebouncedChange

  // Keep the draft in sync when the parent clears/changes the value externally
  // (e.g. the "Clear filters" button) so the input reflects the reset.
  useEffect(() => {
    setDraft(value)
  }, [value])

  // Clear any pending timer on unmount.
  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    },
    []
  )

  const scheduleReport = (next: string) => {
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => {
      timerRef.current = null
      onChangeRef.current(next)
    }, 500)
  }

  return (
    <Input
      value={draft}
      onChange={(e) => {
        const next = e.target.value
        setDraft(next)
        // While composing, only the visible draft updates; the report waits for
        // compositionend so a partial pinyin string never triggers a filter.
        if (!composingRef.current) scheduleReport(next)
      }}
      onCompositionStart={() => {
        composingRef.current = true
        if (timerRef.current) clearTimeout(timerRef.current)
      }}
      onCompositionEnd={(e) => {
        composingRef.current = false
        const next = e.currentTarget.value
        setDraft(next)
        scheduleReport(next)
      }}
      placeholder={placeholder}
      className={className}
    />
  )
}

type SelectedBucket = {
  row: ChannelStatusRow
  check: RecentCheck
}

/** Chip styling per health: translucent fill + tinted border + colored text. */
const HEALTH_CHIP_CLASS: Record<ChannelHealth, string> = {
  none: 'border-gray-300 bg-gray-100 text-gray-500 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-400',
  healthy:
    'border-[#52c41a]/30 bg-[#52c41a]/10 text-[#3f9714] dark:text-[#73d13d]',
  degraded:
    'border-[#faad14]/40 bg-[#faad14]/10 text-[#d48806] dark:text-[#ffc53d]',
  down: 'border-[#f5222d]/30 bg-[#f5222d]/10 text-[#f5222d] dark:text-[#ff7875]',
}

function healthLabel(health: ChannelHealth): string {
  switch (health) {
    case 'none':
      return 'No data'
    case 'healthy':
      return 'Normal'
    case 'degraded':
      return 'Degraded'
    case 'down':
      return 'Abnormal'
  }
}

/** Bar color per time-bucket state (AntD palette; gray = no traffic). */
const BUCKET_BAR_CLASS: Record<BucketState, string> = {
  healthy: 'bg-[#52c41a]',
  degraded: 'bg-[#faad14]',
  down: 'bg-[#f5222d]',
  none: 'bg-gray-200 dark:bg-gray-700',
}

/** Format a bucket window as "MM-DD HH:mm ~ HH:mm". */
function bucketWindow(check: RecentCheck): string {
  const start = dayjs(check.startAt * 1000)
  const end = dayjs(check.endAt * 1000)
  return `${start.format('MM-DD HH:mm')} ~ ${end.format('HH:mm')}`
}

/**
 * Three-point time axis (start · midpoint · now) derived from the actual bucket
 * timestamps, so it stays correct for every range. Sub-day windows show only the
 * time; multi-day windows (7d) add the date so the span reads unambiguously.
 */
function axisLabels(checks: RecentCheck[]): [string, string, string] {
  const first = checks.at(0)
  const last = checks.at(-1)
  if (!first || !last) return ['', '', '']
  const startAt = first.startAt
  const endAt = last.endAt
  const spanSeconds = endAt - startAt
  const format = spanSeconds > 24 * 60 * 60 ? 'MM-DD HH:mm' : 'HH:mm'
  const midAt = startAt + Math.floor(spanSeconds / 2)
  return [
    dayjs(startAt * 1000).format(format),
    dayjs(midAt * 1000).format(format),
    dayjs(endAt * 1000).format(format),
  ]
}

/**
 * Rich hover tooltip for a single time bucket: window, request count, success
 * count and success rate. Positioned above the hovered bar via inline `left`.
 */
function BucketTooltip({
  check,
  leftPct,
  t,
}: {
  check: RecentCheck
  leftPct: number
  t: (key: string) => string
}) {
  const rate =
    check.total > 0 ? ((check.success / check.total) * 100).toFixed(2) : '0.00'
  return (
    <div
      className='pointer-events-none absolute bottom-full z-20 mb-2 whitespace-nowrap'
      // Shift the tooltip proportionally to the bar's position: leftmost bars
      // left-align (translateX 0), rightmost right-align (translateX -100%),
      // middle bars center (~-50%). This keeps the tooltip within the sparkline
      // width so it never spills past the card edge and gets clipped.
      style={{ left: `${leftPct}%`, transform: `translateX(-${leftPct}%)` }}
    >
      <div className='bg-foreground text-background flex min-w-[168px] flex-col gap-1 rounded-md px-2.5 py-1.5 text-xs shadow-md'>
        <span className='mb-0.5 font-semibold opacity-90'>
          {bucketWindow(check)}
        </span>
        <div className='flex items-center justify-between gap-4'>
          <span className='opacity-70'>{t('Total Requests')}</span>
          <span className='font-semibold tabular-nums'>
            {check.health === 'none' ? '-' : formatNumber(check.total)}
          </span>
        </div>
        <div className='flex items-center justify-between gap-4'>
          <span className='opacity-70'>{t('Successful')}</span>
          <span className='font-semibold tabular-nums'>
            {check.health === 'none' ? '-' : formatNumber(check.success)}
          </span>
        </div>
        <div className='flex items-center justify-between gap-4'>
          <span className='opacity-70'>{t('Success Rate')}</span>
          <span className='font-semibold tabular-nums'>
            {check.health === 'none' ? '-' : `${rate}%`}
          </span>
        </div>
      </div>
    </div>
  )
}

/**
 * Full-width health sparkline, oldest bucket on the left. Bars are uniform,
 * full-height and thin; color alone encodes each time bucket's outcome —
 * green (healthy), amber (degraded), red (down), gray (no traffic). Hovering
 * a bar scales it up and reveals a rich tooltip.
 */
function RecentChecksBar({
  checks,
  onSelect,
}: {
  checks: RecentCheck[]
  onSelect?: (check: RecentCheck) => void
}) {
  const { t } = useTranslation()
  const [hovered, setHovered] = useState<number | null>(null)

  return (
    <div className='relative h-full'>
      <div className='flex h-full items-stretch gap-px'>
        {checks.map((check, i) => (
          <button
            key={check.startAt}
            type='button'
            aria-label={bucketWindow(check)}
            className={cn(
              'min-w-0 flex-1 origin-bottom rounded-[3px] transition-transform duration-150 focus-visible:z-10 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
              onSelect && 'cursor-pointer',
              BUCKET_BAR_CLASS[check.health],
              hovered === i && 'scale-y-110 ring-2 ring-current/25'
            )}
            onMouseEnter={() => setHovered(i)}
            onMouseLeave={() => setHovered((h) => (h === i ? null : h))}
            onClick={() => onSelect?.(check)}
          />
        ))}
      </div>
      {hovered !== null && (
        <BucketTooltip
          check={checks[hovered]}
          leftPct={((hovered + 0.5) / checks.length) * 100}
          t={t}
        />
      )}
    </div>
  )
}

/** Compact ms/s formatter for probe timings: sub-second stays in ms, one second
 *  or more switches to seconds with one decimal so a slow probe reads cleanly. */
function formatProbeMs(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.round(ms)}ms`
}

/** Speed color for a probe timing: green (fast) / amber (fair) / red (slow),
 *  matching the health palette. `fair`/`slow` are the ms thresholds above which
 *  the value turns amber then red. TTFT and total latency use different scales,
 *  so each call passes its own thresholds. */
function speedColorClass(ms: number, fair: number, slow: number): string {
  if (ms >= slow) return 'text-[#f5222d] dark:text-[#ff7875]'
  if (ms >= fair) return 'text-[#d48806] dark:text-[#ffc53d]'
  return 'text-[#3f9714] dark:text-[#73d13d]'
}

/**
 * Latest-probe speed for the card header: time-to-first-token and total latency.
 * Each number is colored by how fast it is (green/amber/red) so operators can
 * eyeball speed at a glance; TTFT and total latency use separate thresholds
 * because a healthy first-token time is far shorter than a full-response time.
 * Each metric is omitted when its value is 0 (unavailable — e.g. TTFT for a
 * non-stream probe), and the whole block renders nothing when neither is known,
 * so a channel with no recent measurable probe simply shows no speed line.
 */
function ProbeSpeed({
  ttftMs,
  latencyMs,
}: {
  ttftMs: number
  latencyMs: number
}) {
  const { t } = useTranslation()
  if (ttftMs <= 0 && latencyMs <= 0) return null
  return (
    <div className='text-muted-foreground flex shrink-0 items-baseline gap-1.5 text-xs'>
      {ttftMs > 0 && (
        <span className='inline-flex items-baseline gap-1'>
          <span>{t('First token')}</span>
          <span
            className={cn(
              'font-semibold tabular-nums',
              speedColorClass(ttftMs, 2000, 5000)
            )}
          >
            {formatProbeMs(ttftMs)}
          </span>
        </span>
      )}
      {ttftMs > 0 && latencyMs > 0 && <span>·</span>}
      {latencyMs > 0 && (
        <span className='inline-flex items-baseline gap-1'>
          <span>{t('Latency')}</span>
          <span
            className={cn(
              'font-semibold tabular-nums',
              speedColorClass(latencyMs, 5000, 10000)
            )}
          >
            {formatProbeMs(latencyMs)}
          </span>
        </span>
      )}
    </div>
  )
}

/**
 * Admin-only routing badges for a channel+model pair: whether the model is
 * currently enabled on this channel and the priority used to order it during
 * selection. These describe routing state from the abilities table, not probe
 * health, so a healthy sparkline can still show a disabled model (e.g. banned by
 * the managed policy) and vice versa. Rendered before the probe speed.
 */
function ModelRouting({
  enabled,
  priority,
}: {
  enabled: boolean
  priority: number
}) {
  const { t } = useTranslation()
  return (
    <div className='flex shrink-0 items-center gap-1.5 text-[11px] leading-none'>
      <span
        className={cn(
          'inline-flex items-center rounded-full border px-2 py-0.5 font-medium',
          enabled
            ? 'border-[#52c41a]/30 bg-[#52c41a]/10 text-[#3f9714] dark:text-[#73d13d]'
            : 'border-[#f5222d]/30 bg-[#f5222d]/10 text-[#f5222d] dark:text-[#ff7875]'
        )}
      >
        {enabled ? t('Enabled') : t('Disabled')}
      </span>
      <span className='text-muted-foreground inline-flex items-center gap-1'>
        {t('Priority')}
        <span className='text-foreground font-semibold tabular-nums'>
          {priority}
        </span>
      </span>
    </div>
  )
}

function ChannelCard({
  row,
  onSelectBucket,
  showChannelName,
}: {
  row: ChannelStatusRow
  // Optional: members get no drill-down (details need a channel id, which the
  // aggregated member view intentionally does not carry).
  onSelectBucket?: (selection: SelectedBucket) => void
  showChannelName: boolean
}) {
  const { t } = useTranslation()
  const [axisStart, axisMid] = axisLabels(row.recentChecks)
  return (
    <div
      className={cn(
        'bg-card flex cursor-pointer flex-col gap-3 rounded-3xl border p-5 transition-[border-color,box-shadow] duration-200',
        'border-foreground/[0.08] hover:border-[#0884dd] hover:shadow-[0_2px_12px_0_rgba(0,0,0,0.06)]'
      )}
    >
      {/* Header: logo + a two-row column. Row 1 pairs model + status with the
          success-rate/requests metrics; row 2 pairs the channel name with the
          latest probe's speed (TTFT + total latency), each row justified so the
          right-hand info stays flush with the card edge. */}
      <div className='flex items-start gap-2'>
        {showChannelName && (
          <span
            className='bg-muted text-muted-foreground mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-md text-[9px] font-semibold uppercase'
            aria-hidden='true'
          >
            {row.type.slice(0, 2)}
          </span>
        )}
        <div className='min-w-0 flex-1'>
          <div className='flex min-w-0 items-center justify-between gap-3'>
            <div className='flex min-w-0 items-center gap-2'>
              <span
                className='truncate text-sm font-semibold'
                title={row.model}
              >
                {row.model}
              </span>
              <span
                className={cn(
                  'inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 text-[11px] leading-none font-semibold',
                  HEALTH_CHIP_CLASS[row.health]
                )}
              >
                {t(healthLabel(row.health))}
              </span>
            </div>
            <div className='flex shrink-0 items-baseline gap-1.5 text-xs'>
              <span className='font-bold tabular-nums'>
                {row.successRate.toFixed(2)}%
              </span>
              <span className='text-muted-foreground'>{t('Success Rate')}</span>
              <span className='text-muted-foreground'>·</span>
              <span className='font-bold tabular-nums'>
                {formatNumber(row.requests)}
              </span>
              <span className='text-muted-foreground'>{t('Requests')}</span>
            </div>
          </div>
          <div className='flex min-w-0 items-center justify-between gap-3'>
            {showChannelName ? (
              <div
                className='text-muted-foreground min-w-0 truncate text-xs'
                title={row.name}
              >
                {row.name}
              </div>
            ) : (
              <span />
            )}
            <div className='flex shrink-0 items-center gap-3'>
              {showChannelName && (
                <ModelRouting
                  enabled={row.modelEnabled}
                  priority={row.modelPriority}
                />
              )}
              <ProbeSpeed
                ttftMs={row.lastTtftMs}
                latencyMs={row.lastLatencyMs}
              />
            </div>
          </div>
        </div>
      </div>

      {/* Sparkline */}
      <div className='h-8 w-full'>
        <RecentChecksBar
          checks={row.recentChecks}
          onSelect={
            onSelectBucket
              ? (check) => onSelectBucket({ row, check })
              : undefined
          }
        />
      </div>

      {/* Time axis: derived from the actual bucket window so it adapts to the
          selected range (start ~ midpoint ~ now). */}
      <div className='text-muted-foreground flex items-center justify-between text-xs'>
        <span>{axisStart}</span>
        <span>{axisMid}</span>
        <span>{t('Now')}</span>
      </div>
    </div>
  )
}

function DetectionResultBadge({ result }: { result: ChannelMonitorResult }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant={result.success ? 'outline' : 'destructive'}
      className={
        result.success
          ? 'border-success/30 bg-success/10 text-success'
          : undefined
      }
    >
      {result.success ? t('Success') : t('Failed')}
    </Badge>
  )
}

function BucketDetailsDialog({
  selection,
  onClose,
}: {
  selection: SelectedBucket | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const row = selection?.row
  const check = selection?.check
  const { data: results = [], isLoading } = useQuery({
    queryKey: [
      'channel-monitor',
      'status-details',
      row?.channelId,
      row?.model,
      check?.startAt,
      check?.endAt,
    ],
    queryFn: () => {
      if (!row || !check) return Promise.resolve([])
      return getChannelMonitorResults(
        row.channelId,
        row.model,
        check.startAt,
        check.endAt
      )
    },
    enabled: !!row && !!check,
  })

  if (!selection) return null

  // Totals/success/rate come from the bucket's merged counts (probe + real
  // forwarding traffic), matching the sparkline tooltip. The table below stays
  // probe-only, so its row count can be smaller than Total Requests when the
  // bucket also had forwarding traffic. Average latency stays probe-derived
  // because forwarding logs have no comparable millisecond latency.
  const totalRequests = check?.total ?? 0
  const successCount = check?.success ?? 0
  const successRate =
    totalRequests > 0 ? (successCount / totalRequests) * 100 : 0
  const probeResults = results.length
  // Latency is probe-only, so a bucket with only forwarding traffic has no
  // measurable latency: show "—" rather than a misleading 0 ms.
  const averageLatency =
    probeResults > 0
      ? Math.round(
          results.reduce((total, result) => total + result.latencyMs, 0) /
            probeResults
        )
      : null

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={`${t('Details')} · ${selection.row.model}`}
      description={`${selection.row.name} · ${bucketWindow(selection.check)}`}
      contentClassName='sm:max-w-4xl'
      contentHeight='min(560px, calc(100vh - 12rem))'
      showCloseButton
    >
      <div className='space-y-4'>
        <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
          <div className='bg-muted/40 rounded-lg border p-3'>
            <div className='text-muted-foreground text-xs'>
              {t('Total Requests')}
            </div>
            <div className='mt-1 text-lg font-semibold tabular-nums'>
              {formatNumber(totalRequests)}
            </div>
          </div>
          <div className='bg-muted/40 rounded-lg border p-3'>
            <div className='text-muted-foreground text-xs'>
              {t('Successful')}
            </div>
            <div className='mt-1 text-lg font-semibold tabular-nums'>
              {formatNumber(successCount)}
            </div>
          </div>
          <div className='bg-muted/40 rounded-lg border p-3'>
            <div className='text-muted-foreground text-xs'>
              {t('Success Rate')}
            </div>
            <div className='mt-1 text-lg font-semibold tabular-nums'>
              {successRate.toFixed(2)}%
            </div>
          </div>
          <div className='bg-muted/40 rounded-lg border p-3'>
            <div className='text-muted-foreground text-xs'>
              {t('Average latency (probe only)')}
            </div>
            <div className='mt-1 text-lg font-semibold tabular-nums'>
              {averageLatency === null ? '—' : `${averageLatency} ms`}
            </div>
          </div>
        </div>

        {/* The stats above merge probe + real forwarding traffic; the table
            below lists probe records only, so its row count can be lower than
            Total Requests when the bucket also had forwarding traffic. */}
        <p className='text-muted-foreground text-xs'>
          {t('Probe records only; forwarding traffic is counted in the stats above but not listed here.')}
        </p>

        <div className='rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Time')}</TableHead>
                <TableHead>{t('Probe question')}</TableHead>
                <TableHead>{t('Result')}</TableHead>
                <TableHead>{t('Latency')}</TableHead>
                <TableHead>{t('Status Code')}</TableHead>
                <TableHead>{t('Error Message')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading && (
                <TableRow>
                  <TableCell colSpan={6} className='h-24 text-center'>
                    <Loader2 className='text-muted-foreground mx-auto size-4 animate-spin' />
                  </TableCell>
                </TableRow>
              )}
              {!isLoading && results.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-muted-foreground h-24 text-center'
                  >
                    {t('No probe records in this window.')}
                  </TableCell>
                </TableRow>
              )}
              {!isLoading &&
                results.map((result) => (
                  <TableRow key={result.id}>
                    <TableCell>
                      {dayjs(result.checkedAt * 1000).format('MM-DD HH:mm:ss')}
                    </TableCell>
                    <TableCell className='max-w-md break-words whitespace-pre-wrap'>
                      {result.questionContent || '-'}
                    </TableCell>
                    <TableCell>
                      <DetectionResultBadge result={result} />
                    </TableCell>
                    <TableCell>{result.latencyMs} ms</TableCell>
                    <TableCell>
                      {result.statusCode > 0 ? result.statusCode : '-'}
                    </TableCell>
                    <TableCell className='max-w-sm whitespace-normal'>
                      {result.errorMessage || '-'}
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        </div>
      </div>
    </Dialog>
  )
}

const LEGEND_ITEMS: { label: string; className: string }[] = [
  { label: 'Healthy (95%+)', className: 'bg-[#52c41a]' },
  { label: 'Degraded', className: 'bg-[#faad14]' },
  { label: 'Abnormal', className: 'bg-[#f5222d]' },
  { label: 'No data', className: 'bg-gray-200 dark:bg-gray-700' },
]

export function ChannelStatus() {
  const { t } = useTranslation()
  const [selectedBucket, setSelectedBucket] = useState<SelectedBucket | null>(
    null
  )
  const [range, setRange] = useState<StatusRange>('1h')
  // These hold the debounced, IME-safe values reported by the search inputs
  // below; the inputs own their own instant-feedback draft state internally.
  const [channelQuery, setChannelQuery] = useState('')
  const [modelQuery, setModelQuery] = useState('')
  const [tag, setTag] = useState<string>(ALL_TAGS)
  // Admins see per-channel rows with channel identity; normal members see the
  // same data aggregated by model with channel identity stripped. isAdmin gates
  // both the data source (getChannelStatus vs getModelStatus) and every piece of
  // channel-identity UI (channel name/search, tag filter, bucket drill-down).
  const { auth } = useAuthStore()
  const isAdmin = (auth.user?.role ?? 0) >= ROLE.ADMIN
  const {
    data: rows = [],
    isLoading,
    isFetching,
    isError,
    refetch,
  } = useQuery({
    queryKey: ['channel-monitor', 'status', isAdmin ? 'channel' : 'model', range],
    queryFn: () => (isAdmin ? getChannelStatus(range) : getModelStatus(range)),
  })

  // Tag options come from the loaded rows so the dropdown only ever lists tags
  // that actually have monitored channels.
  const tagOptions = useMemo(() => {
    const tags = new Set<string>()
    for (const row of rows) {
      if (row.tag) tags.add(row.tag)
    }
    return [...tags].sort((a, b) => a.localeCompare(b))
  }, [rows])

  // Client-side filtering: channel/model are case-insensitive substring matches,
  // tag is an exact match. Filtering here (not on the server) keeps every range
  // switch a single query and makes typing feel instant.
  const filteredRows = useMemo(() => {
    const channelNeedle = channelQuery.trim().toLowerCase()
    const modelNeedle = modelQuery.trim().toLowerCase()
    return rows.filter((row) => {
      // Channel name / tag filters only apply to the admin (channel) view; the
      // member view carries no channel identity so those needles are always empty.
      if (channelNeedle && !row.name.toLowerCase().includes(channelNeedle)) {
        return false
      }
      if (modelNeedle && !row.model.toLowerCase().includes(modelNeedle)) {
        return false
      }
      if (tag !== ALL_TAGS && row.tag !== tag) {
        return false
      }
      return true
    })
  }, [rows, channelQuery, modelQuery, tag])

  const hasActiveFilters =
    channelQuery.trim() !== '' || modelQuery.trim() !== '' || tag !== ALL_TAGS

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-2'>
          <span className='truncate'>
            {isAdmin ? t('Channel Status') : t('Model Status')}
          </span>
          <Badge variant='outline' className='shrink-0'>
            {t('Preview')}
          </Badge>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={isFetching}
          onClick={() => void refetch()}
        >
          <RefreshCw
            data-icon='inline-start'
            className={cn('size-3.5', isFetching && 'animate-spin')}
          />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          {/* Filter bar: range + tag are dropdowns, channel + model are text
              inputs. Range drives the query; the rest filter client-side. */}
          <div className='flex flex-wrap items-center gap-2'>
            <Select
              value={range}
              onValueChange={(v) => v && setRange(v as StatusRange)}
            >
              <SelectTrigger className='w-[104px]' aria-label={t('Time range')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {RANGE_OPTIONS.map((option) => (
                  <SelectItem key={option} value={option}>
                    {option}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {isAdmin && (
              <DebouncedSearchInput
                value={channelQuery}
                onDebouncedChange={setChannelQuery}
                placeholder={t('Search channel name')}
                className='w-full sm:w-48'
              />
            )}
            <DebouncedSearchInput
              value={modelQuery}
              onDebouncedChange={setModelQuery}
              placeholder={t('Search model name...')}
              className='w-full sm:w-48'
            />
            {isAdmin && (
              <Select value={tag} onValueChange={(v) => v && setTag(v)}>
                <SelectTrigger className='w-[160px]' aria-label={t('Tag')}>
                  <SelectValue>
                    {tag === ALL_TAGS ? t('All Tags') : tag}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectItem value={ALL_TAGS}>{t('All Tags')}</SelectItem>
                  {tagOptions.map((option) => (
                    <SelectItem key={option} value={option}>
                      {option}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            {hasActiveFilters && (
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => {
                  setChannelQuery('')
                  setModelQuery('')
                  setTag(ALL_TAGS)
                }}
              >
                {t('Clear filters')}
              </Button>
            )}
            {/* Legend sits at the far right of the filter row. */}
            <div className='text-muted-foreground ml-auto flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs'>
              {LEGEND_ITEMS.map((item) => (
                <span
                  key={item.label}
                  className='inline-flex items-center gap-1.5'
                >
                  <span
                    className={cn('size-2.5 rounded-[3px]', item.className)}
                    aria-hidden='true'
                  />
                  {t(item.label)}
                </span>
              ))}
            </div>
          </div>

          {/* Channel health cards */}
          {isLoading && (
            <div className='text-muted-foreground flex min-h-32 items-center justify-center gap-2 text-sm'>
              <Loader2 className='size-4 animate-spin' />
              {t('Loading...')}
            </div>
          )}
          {!isLoading && (isError || rows.length === 0) && (
            <div className='text-muted-foreground flex min-h-32 items-center justify-center rounded-xl border border-dashed text-sm'>
              {t('No data')}
            </div>
          )}
          {!isLoading &&
            !isError &&
            rows.length > 0 &&
            (filteredRows.length > 0 ? (
              <div className='grid grid-cols-1 gap-4 md:grid-cols-2 2xl:grid-cols-3'>
                {filteredRows.map((row) => (
                  <ChannelCard
                    key={row.id}
                    row={row}
                    showChannelName={isAdmin}
                    onSelectBucket={isAdmin ? setSelectedBucket : undefined}
                  />
                ))}
              </div>
            ) : (
              <div className='text-muted-foreground flex min-h-32 items-center justify-center rounded-xl border border-dashed text-sm'>
                {t('No channels match the current filters.')}
              </div>
            ))}

          <BucketDetailsDialog
            selection={selectedBucket}
            onClose={() => setSelectedBucket(null)}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
