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
import { Check, ChevronDown, Code2, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { cn } from '@/lib/utils'

import {
  MONITOR_ENDPOINT_OPTIONS,
  STREAM_INCOMPATIBLE_ENDPOINTS,
  newDefaultConfig,
} from '../constants'
import type {
  BodyMode,
  ChannelMonitorRow,
  MonitorConfig,
  MonitorTemplate,
} from '../types'
import { CustomHeadersEditor } from './custom-headers-editor'

const endpointSelectContentClass = 'w-[460px] max-w-[calc(100vw-2rem)]'
const endpointSelectItemClass =
  'items-start py-2 [&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:whitespace-normal'

const NO_TEMPLATE = '__none__'

const BODY_MODE_OPTIONS: Array<{ value: BodyMode; label: string }> = [
  { value: 'default', label: 'Default' },
  { value: 'merge', label: 'Merge' },
  { value: 'override', label: 'Override' },
]

/**
 * Probe interval bounds in seconds, mirroring the backend
 * (model.MonitorMin/MaxIntervalSeconds). The backend re-clamps on save, so these
 * only drive input UX. Effective resolution is ~15s (the scheduler tick).
 */
const MIN_INTERVAL_SECONDS = 5
const MAX_INTERVAL_SECONDS = 24 * 60 * 60

/** Parse a number input to a non-negative integer, treating empty/NaN as 0. */
function parseSeconds(value: string): number {
  const n = Math.floor(Number(value))
  if (!Number.isFinite(n) || n < 0) return 0
  return n
}

function MonitorConfigDialogInner({
  row,
  templates,
  onClose,
  onSave,
}: {
  row: ChannelMonitorRow
  templates: MonitorTemplate[]
  onClose: () => void
  onSave: (config: MonitorConfig) => void
}) {
  const { t } = useTranslation()
  const [config, setConfig] = useState<MonitorConfig>(
    () => row.config ?? newDefaultConfig()
  )
  // Advanced holds the rarely-touched fields (template, headers, body, remark),
  // and starts open so they are reachable without an extra click.
  const [advancedOpen, setAdvancedOpen] = useState(true)

  const streamDisabled = STREAM_INCOMPATIBLE_ENDPOINTS.has(config.endpointType)
  const effectiveStream = streamDisabled ? false : config.stream
  const monitorModeDescription =
    config.monitorMode === 'banned_only'
      ? t(
          'Only probes banned models and pending ban confirmations, so healthy models are left alone.'
        )
      : t('Probes every monitored model on each interval.')
  let bodyModeDescription = t('Use the endpoint default probe body as-is.')
  if (config.bodyMode === 'merge') {
    bodyModeDescription = t(
      'Shallow-merge your fields over the default body; same-level keys with the same name are overwritten. Use Override when you need to replace the entire body.'
    )
  } else if (config.bodyMode === 'override') {
    bodyModeDescription = t(
      'Replace the default body entirely with the JSON below, including model/messages/contents.'
    )
  }

  const patch = (next: Partial<MonitorConfig>) =>
    setConfig((prev) => ({ ...prev, ...next }))

  const handleEndpointChange = (value: string | null) => {
    if (!value) return
    if (STREAM_INCOMPATIBLE_ENDPOINTS.has(value)) {
      patch({ endpointType: value, stream: false })
      return
    }
    patch({ endpointType: value })
  }

  /** Copy a template's headers/body as a one-time snapshot (no live sync). */
  const handleTemplateChange = (value: string | null) => {
    if (!value || value === NO_TEMPLATE) {
      patch({ templateName: '' })
      return
    }
    const tpl = templates.find((item) => String(item.id) === value)
    if (!tpl) return
    patch({
      templateName: tpl.name,
      endpointType: tpl.endpointType,
      stream: tpl.stream,
      headers: tpl.headers.map((h) => ({ ...h })),
      bodyMode: tpl.bodyMode,
      bodyJson: tpl.bodyJson,
    })
    setAdvancedOpen(true)
    toast.success(t('Template applied as a snapshot'))
  }

  const formatBody = () => {
    const raw = config.bodyJson.trim()
    if (!raw) return
    try {
      patch({ bodyJson: JSON.stringify(JSON.parse(raw), null, 2) })
    } catch {
      toast.error(t('Invalid JSON, unable to format'))
    }
  }

  const handleSave = () => {
    if (config.bodyMode !== 'default' && config.bodyJson.trim()) {
      try {
        JSON.parse(config.bodyJson)
      } catch {
        toast.error(t('Invalid JSON in request body'))
        return
      }
    }
    onSave({ ...config, stream: effectiveStream })
    onClose()
  }

  const matchedTemplate = templates.find(
    (tpl) => tpl.name === config.templateName
  )
  const templateSelectValue = matchedTemplate
    ? String(matchedTemplate.id)
    : NO_TEMPLATE

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={
        row.config
          ? t('Edit monitoring for {{name}}', { name: row.name })
          : t('Add monitoring for {{name}}', { name: row.name })
      }
      description={t('Configure how this channel is probed.')}
      contentClassName='sm:max-w-2xl'
      footer={
        <>
          <Button type='button' variant='outline' onClick={onClose}>
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleSave}>
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='space-y-5'>
        {/* Endpoint + stream */}
        <div className='grid gap-4 md:grid-cols-2'>
          <div className='grid gap-2'>
            <Label htmlFor='monitor-endpoint'>{t('Endpoint Type')}</Label>
            <Select
              value={config.endpointType}
              onValueChange={handleEndpointChange}
            >
              <SelectTrigger id='monitor-endpoint' className='w-full min-w-0'>
                <SelectValue
                  className='min-w-0 truncate'
                  placeholder={t('Auto detect (default)')}
                />
              </SelectTrigger>
              <SelectContent
                alignItemWithTrigger={false}
                className={endpointSelectContentClass}
              >
                <SelectGroup>
                  {MONITOR_ENDPOINT_OPTIONS.map((option) => (
                    <SelectItem
                      key={option.value}
                      value={option.value}
                      className={endpointSelectItemClass}
                    >
                      <span className='min-w-0 leading-snug break-words whitespace-normal'>
                        {t(option.label)}
                      </span>
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='monitor-stream'>{t('Stream Mode')}</Label>
            <div className='flex h-8 items-center gap-2'>
              <Switch
                id='monitor-stream'
                checked={effectiveStream}
                onCheckedChange={(v) => patch({ stream: v })}
                disabled={streamDisabled}
              />
              <span className='text-sm'>
                {effectiveStream ? t('Enabled') : t('Disabled')}
              </span>
            </div>
            {streamDisabled ? (
              <p className='text-muted-foreground text-xs'>
                {t('This endpoint does not support streaming.')}
              </p>
            ) : null}
          </div>
        </div>

        <div className='grid gap-2'>
          <Label id='monitor-mode-label'>{t('Monitoring mode')}</Label>
          <ToggleGroup
            value={[config.monitorMode]}
            onValueChange={(values) => {
              const value = values[0]
              if (value === 'default' || value === 'banned_only') {
                patch({ monitorMode: value })
              }
            }}
            variant='outline'
            className='grid w-full grid-cols-2'
            aria-labelledby='monitor-mode-label'
          >
            <ToggleGroupItem value='default'>
              {t('Default probing')}
            </ToggleGroupItem>
            <ToggleGroupItem value='banned_only'>
              {t('Banned-only probing')}
            </ToggleGroupItem>
          </ToggleGroup>
          <p className='text-muted-foreground text-xs'>
            {monitorModeDescription}
          </p>
        </div>

        {/* Interval + jitter (seconds) */}
        <div className='grid gap-4 md:grid-cols-2'>
          <div className='grid gap-2'>
            <Label htmlFor='monitor-interval'>
              {t('Probe interval (seconds)')}
            </Label>
            <Input
              id='monitor-interval'
              type='number'
              min={MIN_INTERVAL_SECONDS}
              max={MAX_INTERVAL_SECONDS}
              step={1}
              value={config.intervalSeconds}
              onChange={(e) =>
                patch({ intervalSeconds: parseSeconds(e.target.value) })
              }
              onBlur={() => {
                const interval = Math.min(
                  MAX_INTERVAL_SECONDS,
                  Math.max(MIN_INTERVAL_SECONDS, config.intervalSeconds)
                )
                patch({
                  intervalSeconds: interval,
                  jitterSeconds: Math.min(config.jitterSeconds, interval - 1),
                })
              }}
            />
            <p className='text-muted-foreground text-xs'>
              {t('Effective resolution is about 15s (the scheduler tick).')}
            </p>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='monitor-jitter'>
              {t('Random jitter (seconds)')}
            </Label>
            <Input
              id='monitor-jitter'
              type='number'
              min={0}
              max={Math.max(0, config.intervalSeconds - 1)}
              step={1}
              value={config.jitterSeconds}
              onChange={(e) =>
                patch({ jitterSeconds: parseSeconds(e.target.value) })
              }
              onBlur={() =>
                patch({
                  jitterSeconds: Math.max(
                    0,
                    Math.min(config.jitterSeconds, config.intervalSeconds - 1)
                  ),
                })
              }
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Adds a random offset in [-jitter, +jitter] to each interval to spread probes.'
              )}
            </p>
          </div>
        </div>

        {/* Channel-level switches, side by side: "enable" gates the scheduler for
            this channel at all, "hosting" opts it into the managed policy engine. */}
        <div className='grid gap-4 md:grid-cols-2'>
          <div className='rounded-lg border p-3'>
            <div className='flex items-center justify-between gap-3'>
              <div className='min-w-0'>
                <Label
                  htmlFor='monitor-enabled'
                  className='text-sm font-medium'
                >
                  {t('Enable monitoring')}
                </Label>
                <p className='text-muted-foreground mt-0.5 text-xs'>
                  {t('When off, this channel is skipped by scheduled probes.')}
                </p>
              </div>
              <Switch
                id='monitor-enabled'
                checked={config.enabled}
                onCheckedChange={(v) => patch({ enabled: v })}
              />
            </div>
          </div>

          {/* Behavior is configured globally in the "托管策略" dialog; this switch
              only opts the channel in. */}
          <div className='rounded-lg border border-[#0884dd]/25 bg-[#0884dd]/5 p-3'>
            <div className='flex items-center justify-between gap-3'>
              <div className='min-w-0'>
                <Label
                  htmlFor='monitor-managed'
                  className='text-sm font-medium'
                >
                  {t('Channel hosting')}
                </Label>
                <p className='text-muted-foreground mt-0.5 text-xs'>
                  {t(
                    'When on, this channel is governed by the hosting policy: probes drive automatic ban/recover and speed-based up/downgrade per model.'
                  )}
                </p>
              </div>
              <Switch
                id='monitor-managed'
                checked={config.managed}
                onCheckedChange={(v) => patch({ managed: v })}
              />
            </div>
          </div>
        </div>

        {/* Monitored models: click a name to include or exclude it. Chips stay
            compact so a channel with many models still fits without scrolling. */}
        <div className='space-y-2'>
          <div className='flex items-center justify-between gap-3'>
            <Label>
              {t('Monitored models')}
              {row.models.length > 0 ? (
                <span className='text-muted-foreground ml-2 text-xs font-normal tabular-nums'>
                  {config.monitoredModels.length}/{row.models.length}
                </span>
              ) : null}
            </Label>
            {row.models.length > 0 ? (
              <div className='flex items-center gap-2'>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  className='h-7 px-2 text-xs'
                  onClick={() => patch({ monitoredModels: [...row.models] })}
                >
                  {t('Select all')}
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  className='h-7 px-2 text-xs'
                  onClick={() => patch({ monitoredModels: [] })}
                >
                  {t('Select none')}
                </Button>
              </div>
            ) : null}
          </div>
          {row.models.length === 0 ? (
            <p className='text-muted-foreground text-xs'>
              {t('This channel has no models configured.')}
            </p>
          ) : (
            <div className='max-h-40 overflow-y-auto rounded-lg border p-2'>
              <div className='flex flex-wrap gap-1.5'>
                {row.models.map((model) => {
                  const on = config.monitoredModels.includes(model)
                  return (
                    <button
                      key={model}
                      type='button'
                      aria-pressed={on}
                      title={model}
                      className={cn(
                        'focus-visible:ring-ring inline-flex max-w-full items-center gap-1.5 rounded-full border px-2.5 py-1 font-mono text-xs transition-colors focus-visible:ring-2 focus-visible:outline-none',
                        on
                          ? 'border-primary bg-primary/10 text-foreground'
                          : 'text-muted-foreground hover:border-primary/40 hover:text-foreground'
                      )}
                      onClick={() =>
                        setConfig((prev) => ({
                          ...prev,
                          monitoredModels: on
                            ? prev.monitoredModels.filter((m) => m !== model)
                            : [...prev.monitoredModels, model],
                        }))
                      }
                    >
                      {on ? (
                        <Check className='size-3 shrink-0' aria-hidden='true' />
                      ) : (
                        <Plus className='size-3 shrink-0' aria-hidden='true' />
                      )}
                      <span className='truncate'>{model}</span>
                    </button>
                  )
                })}
              </div>
            </div>
          )}
          <p className='text-muted-foreground text-xs'>
            {t(
              'Each enabled model is probed on its own; the channel switch above must also be on.'
            )}
          </p>
        </div>

        {/* Advanced (optional) */}
        <div className='rounded-lg border'>
          <button
            type='button'
            className='flex w-full items-center justify-between gap-2 px-3 py-2.5 text-sm font-medium'
            onClick={() => setAdvancedOpen((o) => !o)}
          >
            <span>{t('Advanced (optional)')}</span>
            <ChevronDown
              className={cn(
                'size-4 transition-transform',
                advancedOpen && 'rotate-180'
              )}
            />
          </button>
          {advancedOpen ? (
            <div className='space-y-5 border-t px-3 py-4'>
              {/* Template */}
              <div className='grid gap-2'>
                <Label htmlFor='monitor-template'>
                  {t('Request template')}
                </Label>
                <Select
                  value={templateSelectValue}
                  onValueChange={handleTemplateChange}
                >
                  <SelectTrigger
                    id='monitor-template'
                    className='w-full min-w-0'
                  >
                    <SelectValue placeholder={t('No template')} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value={NO_TEMPLATE}>
                        {t('No template')}
                      </SelectItem>
                      {templates.map((tpl) => (
                        <SelectItem key={tpl.id} value={String(tpl.id)}>
                          {tpl.name}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Selecting a template copies its headers and body as a snapshot. Later template changes are not synced.'
                  )}
                </p>
              </div>

              {/* Custom headers */}
              <CustomHeadersEditor
                headers={config.headers}
                onChange={(headers) => patch({ headers })}
                hint={t(
                  'Merged over the default headers; your values win. Hop-by-hop headers like Host and Content-Length are ignored.'
                )}
              />

              {/* Body handling */}
              <div className='space-y-2'>
                <Label>{t('Request body handling')}</Label>
                <div className='flex flex-wrap gap-2'>
                  {BODY_MODE_OPTIONS.map((option) => {
                    const active = config.bodyMode === option.value
                    return (
                      <button
                        key={option.value}
                        type='button'
                        onClick={() => patch({ bodyMode: option.value })}
                        className={cn(
                          'rounded-md border px-3 py-1.5 text-sm font-medium transition-colors',
                          active
                            ? 'border-[#52c41a]/40 bg-[#52c41a]/10 text-[#3f9714] dark:text-[#73d13d]'
                            : 'text-muted-foreground hover:bg-muted'
                        )}
                      >
                        {t(option.label)}
                      </button>
                    )
                  })}
                </div>
                <p className='text-muted-foreground text-xs'>
                  {bodyModeDescription}
                </p>
                {config.bodyMode !== 'default' ? (
                  <div className='space-y-2'>
                    <div className='flex items-center justify-between'>
                      <span className='text-muted-foreground inline-flex items-center gap-1.5 text-xs'>
                        <Code2 className='size-3.5' />
                        {t('Body JSON')}
                      </span>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={formatBody}
                      >
                        {t('Format')}
                      </Button>
                    </div>
                    <Textarea
                      value={config.bodyJson}
                      placeholder={'{\n  "max_tokens": 20\n}'}
                      className='min-h-32 font-mono text-xs'
                      spellCheck={false}
                      onChange={(e) => patch({ bodyJson: e.target.value })}
                    />
                  </div>
                ) : null}
              </div>

              <div className='grid gap-2'>
                <Label htmlFor='monitor-remark'>{t('Remark')}</Label>
                <Textarea
                  id='monitor-remark'
                  value={config.remark}
                  maxLength={255}
                  className='min-h-20 resize-none'
                  onChange={(event) => patch({ remark: event.target.value })}
                />
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </Dialog>
  )
}

export function MonitorConfigDialog({
  row,
  templates,
  onClose,
  onSave,
}: {
  row: ChannelMonitorRow | null
  templates: MonitorTemplate[]
  onClose: () => void
  onSave: (id: number, config: MonitorConfig) => void
}) {
  if (!row) return null
  // `key` re-mounts the inner form per row so its state resets cleanly when
  // switching channels.
  return (
    <MonitorConfigDialogInner
      key={row.id}
      row={row}
      templates={templates}
      onClose={onClose}
      onSave={(config) => onSave(row.id, config)}
    />
  )
}
