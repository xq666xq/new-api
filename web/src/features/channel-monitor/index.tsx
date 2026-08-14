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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  BookOpenText,
  LayoutTemplate,
  MoonStar,
  PlayCircle,
  Settings2,
  ShieldCheck,
  Star,
  Trash2,
  Zap,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import {
  applyMonitorTemplate,
  createMonitorQuestion,
  createMonitorTemplate,
  deleteChannelMonitorConfig,
  deleteMonitorQuestion,
  deleteMonitorTemplate,
  getChannelMonitorList,
  getChannelMonitorSetting,
  getChannelRecommendations,
  getManagedPolicy,
  getMonitorQuestions,
  getMonitorTemplates,
  saveChannelMonitorConfig,
  saveChannelRecommendations,
  triggerChannelMonitorNow,
  updateChannelMonitorSetting,
  updateManagedPolicy,
  updateMonitorQuestion,
  updateMonitorTemplate,
} from './api'
import { ChannelRecommendationDialog } from './components/channel-recommendation-dialog'
import { ManagedPolicyDialog } from './components/managed-policy-dialog'
import { ManualProbeDialog } from './components/manual-probe-dialog'
import { MonitorConfigDialog } from './components/monitor-config-dialog'
import { QuestionLibraryDialog } from './components/question-library-dialog'
import { TemplateManagerDialog } from './components/template-manager-dialog'
import {
  channelMonitorColumns,
  channelMonitorTableClassName,
  pinnedActionsCellClassName,
  pinnedActionsHeadClassName,
} from './layout'
import { canRunManualProbe, getManualProbeModels } from './probe-availability'
import type {
  ChannelMonitorRow,
  ChannelMonitorSetting,
  ChannelRecommendationRow,
  ManagedPolicySetting,
  MonitorConfig,
  MonitorQuestion,
  MonitorTemplate,
} from './types'

const MODEL_CHIP_CLASSES = [
  'border-primary/25 bg-primary/8 text-primary',
  'border-chart-1/30 bg-chart-1/10 text-chart-1',
  'border-chart-2/30 bg-chart-2/10 text-chart-2',
  'border-chart-3/30 bg-chart-3/10 text-chart-3',
  'border-chart-4/30 bg-chart-4/10 text-chart-4',
  'border-chart-5/30 bg-chart-5/10 text-chart-5',
] as const

function modelChipClass(model: string): string {
  let hash = 0
  for (let index = 0; index < model.length; index += 1) {
    hash = (hash * 31 + model.charCodeAt(index)) >>> 0
  }
  return MODEL_CHIP_CLASSES[hash % MODEL_CHIP_CLASSES.length]
}

/**
 * A model is actively monitored when the channel master switch is on and the
 * model is in the config's monitoredModels set.
 */
function isModelMonitored(row: ChannelMonitorRow, model: string): boolean {
  return (
    !!row.config?.enabled &&
    (row.config?.monitoredModels.includes(model) ?? false)
  )
}

/** All channel models as chips; monitored ones highlighted, the rest muted. */
function ModelChips({ row }: { row: ChannelMonitorRow }) {
  const { t } = useTranslation()
  if (row.models.length === 0) {
    return <span className='text-muted-foreground/60 text-xs'>—</span>
  }
  return (
    <div className='flex max-w-full flex-wrap items-center gap-1'>
      {row.models.map((model) => {
        const on = isModelMonitored(row, model)
        return (
          <span
            key={model}
            title={model}
            className={cn(
              'inline-flex max-w-[180px] items-center gap-1 truncate rounded-md border px-1.5 py-0.5 text-[11px] leading-none font-medium transition-colors',
              modelChipClass(model),
              on &&
                'ring-success/45 ring-1 ring-offset-1 ring-offset-background'
            )}
          >
            {on && (
              <span
                className='bg-success size-1.5 shrink-0 rounded-full'
                aria-hidden='true'
              />
            )}
            {model}
          </span>
        )
      })}
      <span className='sr-only'>{t('Model')}</span>
    </div>
  )
}

/** Pill describing whether a channel has a monitor config + how many models are on. */
function StatusPills({ row }: { row: ChannelMonitorRow }) {
  const { t } = useTranslation()
  if (!row.config) {
    return (
      <span className='text-muted-foreground border-foreground/15 inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] leading-none font-medium'>
        {t('Not configured')}
      </span>
    )
  }
  const monitoredCount = row.config.enabled
    ? row.models.filter((m) => row.config?.monitoredModels.includes(m)).length
    : 0
  return (
    <div className='grid w-fit grid-cols-[auto_auto] justify-items-start gap-1.5'>
      <span className='inline-flex items-center rounded-full border border-[#0884dd]/30 bg-[#0884dd]/10 px-2 py-0.5 text-[11px] leading-none font-medium text-[#0884dd]'>
        {t('Configured')}
      </span>
      {row.config.enabled ? (
        <span className='inline-flex items-center rounded-full border border-[#52c41a]/30 bg-[#52c41a]/10 px-2 py-0.5 text-[11px] leading-none font-medium text-[#3f9714] dark:text-[#73d13d]'>
          {t('{{monitored}}/{{total}} monitored', {
            monitored: monitoredCount,
            total: row.models.length,
          })}
        </span>
      ) : (
        <span className='text-muted-foreground border-foreground/15 inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] leading-none font-medium'>
          {t('Monitoring off')}
        </span>
      )}
      <ManagedStatePills row={row} />
    </div>
  )
}

/**
 * A single "hosting active" label for a managed channel. Hidden when the
 * channel is not managed. Per-model banned/priority detail lives in the
 * managed policy dialog, not this list.
 */
function ManagedStatePills({ row }: { row: ChannelMonitorRow }) {
  const { t } = useTranslation()
  if (!row.config?.managed) {
    return null
  }
  return (
    <span className='inline-flex items-center rounded-full border border-[#722ed1]/30 bg-[#722ed1]/10 px-2 py-0.5 text-[11px] leading-none font-medium text-[#722ed1] dark:text-[#b37feb]'>
      {t('Hosting active')}
    </span>
  )
}

function summarizeConfig(
  row: ChannelMonitorRow,
  t: (key: string, opts?: Record<string, unknown>) => string
): string {
  if (!row.config) return '—'
  const parts: string[] = []
  if (row.config.monitorMode === 'banned_only') {
    parts.push(t('Banned-only probing'))
  }
  if (row.config.jitterSeconds > 0) {
    parts.push(
      t('Every {{count}}s ±{{jitter}}s', {
        count: row.config.intervalSeconds,
        jitter: row.config.jitterSeconds,
      })
    )
  } else {
    parts.push(t('Every {{count}}s', { count: row.config.intervalSeconds }))
  }
  parts.push(row.config.stream ? t('Stream') : t('Non-stream'))
  if (row.config.headers.length > 0) {
    parts.push(t('{{count}} headers', { count: row.config.headers.length }))
  }
  if (row.config.bodyMode !== 'default') {
    parts.push(
      row.config.bodyMode === 'merge' ? t('Body merge') : t('Body override')
    )
  }
  return parts.join(' · ')
}

const MONITOR_LIST_KEY = ['channel-monitor', 'list']
const MONITOR_SETTINGS_KEY = ['channel-monitor', 'settings']
const MONITOR_QUESTIONS_KEY = ['channel-monitor', 'questions']
const MONITOR_TEMPLATES_KEY = ['channel-monitor', 'templates']
const MONITOR_POLICY_KEY = ['channel-monitor', 'managed-policy']
const MONITOR_RECOMMENDATIONS_KEY = ['channel-monitor', 'recommendations']

/** Basic "HH:MM" 24-hour validation used to gate the curfew save button. */
function isValidClock(value: string): boolean {
  return /^([01]\d|2[0-3]):[0-5]\d$/.test(value)
}

// Probe timeout bounds, mirroring the backend clamp (operation_setting). The UI
// validates against these so an out-of-range value is caught before the request.
const MONITOR_PROBE_TIMEOUT_MIN = 5
const MONITOR_PROBE_TIMEOUT_MAX = 600
const MONITOR_PROBE_CONCURRENCY_MIN = 0
const MONITOR_PROBE_CONCURRENCY_MAX = 128

/**
 * Curfew control: a popover holding the daily quiet-window switch and its
 * start/end time inputs. Placed right after the master switch. While curfew is
 * active no channel/model is probed at all; the window wraps past midnight when
 * start is later than end (e.g. 23:00 → 07:00). Local edits are staged and only
 * pushed on "Save" so a half-typed time never triggers a probe pause.
 */
function CurfewControl({
  setting,
  disabled,
  saving,
  onSave,
}: {
  setting: ChannelMonitorSetting
  disabled: boolean
  saving: boolean
  onSave: (next: ChannelMonitorSetting) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [enabled, setEnabled] = useState(setting.curfewEnabled)
  const [start, setStart] = useState(setting.curfewStart)
  const [end, setEnd] = useState(setting.curfewEnd)
  // Probe timeout is edited as a string so an in-progress edit (empty field) is
  // allowed; it is parsed and range-checked before the save button enables.
  const [timeout, setTimeout] = useState(String(setting.probeTimeoutSeconds))
  const [concurrency, setConcurrency] = useState(
    String(setting.probeConcurrency)
  )

  // Re-sync the staged draft with the server value whenever the popover opens or
  // the persisted setting changes, so a discarded edit does not linger.
  useEffect(() => {
    if (open) {
      setEnabled(setting.curfewEnabled)
      setStart(setting.curfewStart)
      setEnd(setting.curfewEnd)
      setTimeout(String(setting.probeTimeoutSeconds))
      setConcurrency(String(setting.probeConcurrency))
    }
  }, [
    open,
    setting.curfewEnabled,
    setting.curfewStart,
    setting.curfewEnd,
    setting.probeTimeoutSeconds,
    setting.probeConcurrency,
  ])

  const timesValid = isValidClock(start) && isValidClock(end)
  const timeoutValue = Number(timeout)
  const timeoutValid =
    Number.isInteger(timeoutValue) &&
    timeoutValue >= MONITOR_PROBE_TIMEOUT_MIN &&
    timeoutValue <= MONITOR_PROBE_TIMEOUT_MAX
  const concurrencyValue = Number(concurrency)
  const concurrencyValid =
    Number.isInteger(concurrencyValue) &&
    concurrencyValue >= MONITOR_PROBE_CONCURRENCY_MIN &&
    concurrencyValue <= MONITOR_PROBE_CONCURRENCY_MAX
  const canSave = timesValid && timeoutValid && concurrencyValid && !saving

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={disabled}
          />
        }
      >
        <MoonStar data-icon='inline-start' className='size-3.5' />
        {setting.curfewEnabled
          ? t('Curfew {{start}}–{{end}}', {
              start: setting.curfewStart,
              end: setting.curfewEnd,
            })
          : t('Curfew')}
      </PopoverTrigger>
      <PopoverContent align='start' className='w-80 space-y-4 p-4'>
        <div className='space-y-1'>
          <div className='text-sm font-medium'>{t('Monitoring curfew')}</div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'While active, no channel or model is probed. The window wraps past midnight when the start is later than the end.'
            )}
          </p>
        </div>
        <div className='flex items-center justify-between gap-3'>
          <Label htmlFor='curfew-enabled' className='text-sm'>
            {t('Enable curfew')}
          </Label>
          <Switch
            id='curfew-enabled'
            checked={enabled}
            onCheckedChange={setEnabled}
          />
        </div>
        <div className='grid grid-cols-2 gap-3'>
          <div className='space-y-1.5'>
            <Label htmlFor='curfew-start' className='text-xs'>
              {t('Start Time')}
            </Label>
            <Input
              id='curfew-start'
              type='time'
              value={start}
              disabled={!enabled}
              onChange={(e) => setStart(e.target.value)}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='curfew-end' className='text-xs'>
              {t('End Time')}
            </Label>
            <Input
              id='curfew-end'
              type='time'
              value={end}
              disabled={!enabled}
              onChange={(e) => setEnd(e.target.value)}
            />
          </div>
        </div>
        {enabled && !timesValid && (
          <p className='text-destructive text-xs'>
            {t('Enter valid times in HH:MM format.')}
          </p>
        )}
        <div className='space-y-1.5 border-t pt-4'>
          <Label htmlFor='probe-timeout' className='text-sm'>
            {t('Probe timeout (seconds)')}
          </Label>
          <Input
            id='probe-timeout'
            type='number'
            min={MONITOR_PROBE_TIMEOUT_MIN}
            max={MONITOR_PROBE_TIMEOUT_MAX}
            value={timeout}
            onChange={(e) => setTimeout(e.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'A probe exceeding this is cancelled and recorded as a failure. Independent of the relay timeout, so it never shortens real forwarding.'
            )}
          </p>
          {!timeoutValid && (
            <p className='text-destructive text-xs'>
              {t('Enter a whole number between {{min}} and {{max}}.', {
                min: MONITOR_PROBE_TIMEOUT_MIN,
                max: MONITOR_PROBE_TIMEOUT_MAX,
              })}
            </p>
          )}
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='probe-concurrency' className='text-sm'>
            {t('Concurrent probes')}
          </Label>
          <Input
            id='probe-concurrency'
            type='number'
            min={MONITOR_PROBE_CONCURRENCY_MIN}
            max={MONITOR_PROBE_CONCURRENCY_MAX}
            value={concurrency}
            onChange={(e) => setConcurrency(e.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Maximum channel/model probes running at once. A queued probe starts its own timeout only when execution begins.'
            )}{' '}
            {t('Set to 0 to start every due probe at once.')}
          </p>
          {!concurrencyValid && (
            <p className='text-destructive text-xs'>
              {t('Enter a whole number between {{min}} and {{max}}.', {
                min: MONITOR_PROBE_CONCURRENCY_MIN,
                max: MONITOR_PROBE_CONCURRENCY_MAX,
              })}
            </p>
          )}
        </div>
        <div className='flex justify-end gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setOpen(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            size='sm'
            disabled={!canSave}
            onClick={() => {
              onSave({
                ...setting,
                curfewEnabled: enabled,
                curfewStart: start,
                curfewEnd: end,
                probeTimeoutSeconds: timeoutValue,
                probeConcurrency: concurrencyValue,
              })
              setOpen(false)
            }}
          >
            {t('Save')}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export function ChannelMonitor() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<ChannelMonitorRow | null>(null)
  const [managingQuestions, setManagingQuestions] = useState(false)
  const [managingTemplates, setManagingTemplates] = useState(false)
  const [managingPolicy, setManagingPolicy] = useState(false)
  const [managingRecommendations, setManagingRecommendations] = useState(false)
  const [probing, setProbing] = useState<ChannelMonitorRow | null>(null)
  const [pendingDelete, setPendingDelete] = useState<ChannelMonitorRow | null>(
    null
  )
  const [probeModel, setProbeModel] = useState('')

  const { data: rows = [], isLoading } = useQuery({
    queryKey: MONITOR_LIST_KEY,
    queryFn: getChannelMonitorList,
  })
  const { data: monitorSetting, isLoading: monitorSettingLoading } = useQuery({
    queryKey: MONITOR_SETTINGS_KEY,
    queryFn: getChannelMonitorSetting,
  })
  const { data: templates = [] } = useQuery({
    queryKey: MONITOR_TEMPLATES_KEY,
    queryFn: getMonitorTemplates,
  })
  const { data: questions = [], isLoading: questionsLoading } = useQuery({
    queryKey: MONITOR_QUESTIONS_KEY,
    queryFn: getMonitorQuestions,
    enabled: managingQuestions,
  })
  const { data: policy, isLoading: policyLoading } = useQuery({
    queryKey: MONITOR_POLICY_KEY,
    queryFn: getManagedPolicy,
    enabled: managingPolicy,
  })
  const { data: recommendations = [], isLoading: recommendationsLoading } =
    useQuery({
      queryKey: MONITOR_RECOMMENDATIONS_KEY,
      queryFn: getChannelRecommendations,
      enabled: managingRecommendations,
    })

  const configuredCount = useMemo(
    () => rows.filter((r) => r.config).length,
    [rows]
  )
  const enabledCount = useMemo(
    () => rows.filter((r) => r.config?.enabled).length,
    [rows]
  )

  const invalidateList = () =>
    queryClient.invalidateQueries({ queryKey: MONITOR_LIST_KEY })
  const invalidateQuestions = () =>
    queryClient.invalidateQueries({ queryKey: MONITOR_QUESTIONS_KEY })
  const invalidateTemplates = () =>
    queryClient.invalidateQueries({ queryKey: MONITOR_TEMPLATES_KEY })
  const invalidatePolicy = () =>
    queryClient.invalidateQueries({ queryKey: MONITOR_POLICY_KEY })

  const saveMonitorSetting = useMutation({
    mutationFn: (next: ChannelMonitorSetting) =>
      updateChannelMonitorSetting(next),
    onSuccess: (next) => {
      queryClient.setQueryData(MONITOR_SETTINGS_KEY, next)
    },
  })

  const currentSetting: ChannelMonitorSetting = monitorSetting ?? {
    enabled: true,
    curfewEnabled: false,
    curfewStart: '23:00',
    curfewEnd: '07:00',
    probeTimeoutSeconds: 60,
    probeConcurrency: 0,
  }

  const toggleMasterSwitch = (enabled: boolean) => {
    saveMonitorSetting.mutate(
      { ...currentSetting, enabled },
      {
        onSuccess: () => {
          toast.success(
            t(
              enabled
                ? 'Monitoring master switch enabled'
                : 'Monitoring master switch disabled'
            )
          )
        },
      }
    )
  }

  const saveCurfew = (next: ChannelMonitorSetting) => {
    saveMonitorSetting.mutate(next, {
      onSuccess: () => toast.success(t('Curfew updated')),
    })
  }

  const triggerMonitor = useMutation({
    mutationFn: (channelId: number) => triggerChannelMonitorNow(channelId),
    onSuccess: () => {
      toast.success(t('Monitor probe triggered; it will run shortly.'))
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Failed to trigger probe')
      )
    },
  })

  const savePolicy = useMutation({
    mutationFn: (next: ManagedPolicySetting) => updateManagedPolicy(next),
    onSuccess: () => {
      toast.success(t('Policy saved'))
      invalidatePolicy()
    },
  })

  const saveRecommendations = useMutation({
    mutationFn: (next: ChannelRecommendationRow[]) =>
      saveChannelRecommendations(next),
    onSuccess: (saved) => {
      queryClient.setQueryData(MONITOR_RECOMMENDATIONS_KEY, saved)
      toast.success(t('Recommendations saved'))
    },
  })

  const saveConfig = useMutation({
    mutationFn: ({ id, config }: { id: number; config: MonitorConfig }) =>
      saveChannelMonitorConfig(id, config),
    onSuccess: () => {
      toast.success(t('Monitoring config saved'))
      invalidateList()
    },
  })

  const removeConfig = useMutation({
    mutationFn: (channelId: number) => deleteChannelMonitorConfig(channelId),
    onSuccess: () => {
      toast.success(t('Removed from monitoring'))
      setPendingDelete(null)
      invalidateList()
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to remove from monitoring')
      )
    },
  })

  const saveQuestion = useMutation({
    mutationFn: (question: MonitorQuestion) =>
      question.id > 0
        ? updateMonitorQuestion(question)
        : createMonitorQuestion(question),
    onSuccess: () => {
      toast.success(t('Question saved'))
      invalidateQuestions()
    },
  })

  const removeQuestion = useMutation({
    mutationFn: (id: number) => deleteMonitorQuestion(id),
    onSuccess: () => {
      toast.success(t('Question deleted'))
      invalidateQuestions()
    },
  })

  const saveTemplate = useMutation({
    mutationFn: (template: MonitorTemplate) =>
      template.id > 0
        ? updateMonitorTemplate(template)
        : createMonitorTemplate(template),
    onSuccess: () => {
      toast.success(t('Template saved'))
      invalidateTemplates()
    },
  })

  const deleteTemplate = useMutation({
    mutationFn: (id: number) => deleteMonitorTemplate(id),
    onSuccess: () => {
      toast.success(t('Template deleted'))
      invalidateTemplates()
    },
  })

  /**
   * Re-apply a template snapshot onto every channel currently using it.
   * This is the "apply update" action: templates do not auto-sync, so this
   * button is how an admin pushes edits out to the channels on demand.
   */
  const applyTemplate = useMutation({
    mutationFn: (template: MonitorTemplate) =>
      applyMonitorTemplate(template.id),
    onSuccess: (result) => {
      if (result.affected === 0) {
        toast.info(t('No channels are using this template.'))
      } else {
        toast.success(
          t('Applied to {{count}} channels', { count: result.affected })
        )
      }
      invalidateList()
    },
  })

  const openManualProbe = (row: ChannelMonitorRow) => {
    setProbeModel(getManualProbeModels(row)[0] ?? '')
    setProbing(row)
  }

  const selectManualProbeModel = (modelName: string) => {
    if (modelName === probeModel) return
    setProbeModel(modelName)
  }

  const closeManualProbe = () => {
    setProbeModel('')
    setProbing(null)
  }

  const handleSave = (id: number, config: MonitorConfig) =>
    saveConfig.mutate({ id, config })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-3'>
          <span className='truncate'>{t('Channel Monitoring')}</span>
          <Tooltip>
            <TooltipTrigger
              render={
                <span className='bg-muted/50 inline-flex shrink-0 items-center gap-2 rounded-full border px-3 py-1' />
              }
            >
              <Switch
                size='lg'
                checked={currentSetting.enabled}
                disabled={monitorSettingLoading || saveMonitorSetting.isPending}
                aria-label={t('Monitoring master switch')}
                onCheckedChange={toggleMasterSwitch}
              />
              <span className='text-sm font-medium'>
                {t('Monitoring master switch')}
              </span>
            </TooltipTrigger>
            <TooltipContent className='max-w-sm'>
              {t(
                'Turn off to pause probes and managed policy execution without hiding channel status.'
              )}
            </TooltipContent>
          </Tooltip>
          <CurfewControl
            setting={currentSetting}
            disabled={monitorSettingLoading}
            saving={saveMonitorSetting.isPending}
            onSave={saveCurfew}
          />
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
          onClick={() => setManagingRecommendations(true)}
        >
          <Star data-icon='inline-start' className='size-3.5' />
          {t('Channel recommendation')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => setManagingPolicy(true)}
        >
          <ShieldCheck data-icon='inline-start' className='size-3.5' />
          {t('Managed policy')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => setManagingQuestions(true)}
        >
          <BookOpenText data-icon='inline-start' className='size-3.5' />
          {t('Question library')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => setManagingTemplates(true)}
        >
          <LayoutTemplate data-icon='inline-start' className='size-3.5' />
          {t('Template management')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Channels with a detection config · {{configured}} configured · {{enabled}} monitoring',
              { configured: configuredCount, enabled: enabledCount }
            )}
          </p>

          <div className='rounded-xl border'>
            <Table className={channelMonitorTableClassName}>
              <colgroup>
                {channelMonitorColumns.map((column) => (
                  <col key={column.key} className={column.className} />
                ))}
              </colgroup>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Channel')}</TableHead>
                  <TableHead>{t('Models')}</TableHead>
                  <TableHead>{t('Monitoring')}</TableHead>
                  <TableHead>{t('Remark')}</TableHead>
                  <TableHead>{t('Strategy')}</TableHead>
                  <TableHead
                    className={cn('text-right', pinnedActionsHeadClassName)}
                  >
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading && (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className='text-muted-foreground py-8 text-center text-sm'
                    >
                      {t('Loading...')}
                    </TableCell>
                  </TableRow>
                )}
                {!isLoading && rows.length === 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className='text-muted-foreground py-8 text-center text-sm'
                    >
                      {t('No channels have a detection config yet')}
                    </TableCell>
                  </TableRow>
                )}
                {!isLoading &&
                  rows.length > 0 &&
                  rows.map((row) => (
                    <TableRow key={row.id}>
                      <TableCell>
                        <div className='flex min-w-0 items-center gap-2'>
                          <span
                            className='bg-muted text-muted-foreground inline-flex size-5 shrink-0 items-center justify-center rounded-md text-[9px] font-semibold uppercase'
                            aria-hidden='true'
                          >
                            {row.type.slice(0, 2)}
                          </span>
                          <div className='min-w-0'>
                            <div className='truncate text-sm font-medium'>
                              {row.name}
                            </div>
                            <div className='text-muted-foreground text-xs'>
                              {row.type} · {row.group}
                            </div>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <ModelChips row={row} />
                      </TableCell>
                      <TableCell>
                        <StatusPills row={row} />
                      </TableCell>
                      <TableCell className='max-w-[140px]'>
                        {row.config?.remark ? (
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <span className='line-clamp-2 text-xs break-words' />
                              }
                            >
                              {row.config.remark}
                            </TooltipTrigger>
                            <TooltipContent className='max-w-sm whitespace-pre-wrap'>
                              {row.config.remark}
                            </TooltipContent>
                          </Tooltip>
                        ) : (
                          <span className='text-muted-foreground/60'>—</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {row.config ? (
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <span className='text-muted-foreground line-clamp-2 text-xs break-words' />
                              }
                            >
                              {summarizeConfig(row, t)}
                            </TooltipTrigger>
                            <TooltipContent className='max-w-sm whitespace-pre-wrap'>
                              {summarizeConfig(row, t)}
                            </TooltipContent>
                          </Tooltip>
                        ) : (
                          <span className='text-muted-foreground/60 text-xs'>
                            {summarizeConfig(row, t)}
                          </span>
                        )}
                      </TableCell>
                      <TableCell
                        className={cn('text-right', pinnedActionsCellClassName)}
                      >
                        <div className='flex items-center justify-end gap-1.5'>
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <Button
                                  type='button'
                                  variant='outline'
                                  size='icon-sm'
                                  disabled={!canRunManualProbe(row)}
                                  aria-label={t('Run probe now')}
                                  onClick={() => openManualProbe(row)}
                                />
                              }
                            >
                              <Zap
                                className={cn(
                                  'size-3.5',
                                  probing?.id === row.id && 'animate-pulse'
                                )}
                              />
                            </TooltipTrigger>
                            <TooltipContent>
                              {t('Run probe now')}
                            </TooltipContent>
                          </Tooltip>
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <Button
                                  type='button'
                                  variant='outline'
                                  size='icon-sm'
                                  disabled={
                                    !row.config?.enabled ||
                                    triggerMonitor.isPending
                                  }
                                  aria-label={t('Trigger monitoring now')}
                                  onClick={() => triggerMonitor.mutate(row.id)}
                                />
                              }
                            >
                              <PlayCircle
                                className={cn(
                                  'size-3.5',
                                  triggerMonitor.isPending &&
                                    triggerMonitor.variables === row.id &&
                                    'animate-pulse'
                                )}
                              />
                            </TooltipTrigger>
                            <TooltipContent>
                              {t(
                                'Bring this channel’s next monitoring cycle forward to run now.'
                              )}
                            </TooltipContent>
                          </Tooltip>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() => setEditing(row)}
                          >
                            <Settings2
                              data-icon='inline-start'
                              className='size-3.5'
                            />
                            {row.config ? t('Edit') : t('Configure')}
                          </Button>
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <Button
                                  type='button'
                                  variant='outline'
                                  size='icon-sm'
                                  className='text-destructive hover:text-destructive'
                                  disabled={removeConfig.isPending}
                                  aria-label={t('Remove from monitoring')}
                                  onClick={() => setPendingDelete(row)}
                                />
                              }
                            >
                              <Trash2 className='size-3.5' />
                            </TooltipTrigger>
                            <TooltipContent>
                              {t('Remove from monitoring')}
                            </TooltipContent>
                          </Tooltip>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          </div>

          <MonitorConfigDialog
            row={editing}
            templates={templates}
            onClose={() => setEditing(null)}
            onSave={handleSave}
          />

          <ConfirmDialog
            open={pendingDelete !== null}
            onOpenChange={(next) => {
              if (!next) setPendingDelete(null)
            }}
            title={t('Remove from monitoring')}
            desc={t(
              'Remove channel {{name}} from monitoring? Its detection config and hosting policy state are deleted, and any model the policy banned or downgraded is restored to the channel setting. Probe history is kept.',
              { name: pendingDelete?.name ?? '' }
            )}
            confirmText={t('Delete')}
            destructive
            isLoading={removeConfig.isPending}
            handleConfirm={() => {
              if (pendingDelete) removeConfig.mutate(pendingDelete.id)
            }}
          />

          <ManualProbeDialog
            row={probing}
            selectedModel={probeModel}
            onSelectModel={selectManualProbeModel}
            onProbed={() =>
              queryClient.invalidateQueries({
                queryKey: ['channel-monitor', 'status'],
              })
            }
            onClose={closeManualProbe}
          />

          <QuestionLibraryDialog
            open={managingQuestions}
            questions={questions}
            loading={questionsLoading}
            saving={saveQuestion.isPending}
            deleting={removeQuestion.isPending}
            onClose={() => setManagingQuestions(false)}
            onSaveQuestion={async (question) => {
              await saveQuestion.mutateAsync(question)
            }}
            onDeleteQuestion={async (id) => {
              await removeQuestion.mutateAsync(id)
            }}
          />

          <TemplateManagerDialog
            open={managingTemplates}
            templates={templates}
            rows={rows}
            onClose={() => setManagingTemplates(false)}
            onSaveTemplate={(tpl) => saveTemplate.mutate(tpl)}
            onDeleteTemplate={(id) => deleteTemplate.mutate(id)}
            onApplyTemplate={(tpl) => applyTemplate.mutate(tpl)}
          />

          <ManagedPolicyDialog
            open={managingPolicy}
            setting={policy}
            loading={policyLoading}
            saving={savePolicy.isPending}
            onClose={() => setManagingPolicy(false)}
            onSave={async (next) => {
              await savePolicy.mutateAsync(next)
            }}
          />

          <ChannelRecommendationDialog
            open={managingRecommendations}
            rows={recommendations}
            loading={recommendationsLoading}
            saving={saveRecommendations.isPending}
            onClose={() => setManagingRecommendations(false)}
            onSave={async (next) => {
              await saveRecommendations.mutateAsync(next)
            }}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
