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
import { useQueryClient } from '@tanstack/react-query'
import {
  Braces,
  ChevronDown,
  Loader2,
  Pencil,
  Plus,
  Save,
  Trash2,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import {
  createMonitorTemplate,
  deleteMonitorTemplate,
  getChannelMonitorConfig,
  getMonitorTemplates,
  saveChannelMonitorConfig,
  updateMonitorTemplate,
} from '../../api'
import type {
  ChannelMonitorMode,
  MonitorBodyMode,
  MonitorHeader,
  MonitorTemplate,
} from '../../types'

const NO_TEMPLATE = '__none__'

/** Endpoints the probe/test request can target, in the order they are offered. */
const endpointTypeOptions: Array<{ value: string; label: string }> = [
  { value: 'auto', label: 'Auto detect (default)' },
  { value: 'openai', label: 'OpenAI (/v1/chat/completions)' },
  { value: 'openai-response', label: 'OpenAI Responses (/v1/responses)' },
  {
    value: 'openai-response-compact',
    label: 'OpenAI Response Compaction (/v1/responses/compact)',
  },
  { value: 'anthropic', label: 'Anthropic (/v1/messages)' },
  {
    value: 'gemini',
    label: 'Gemini (/v1beta/models/{model}:generateContent)',
  },
  { value: 'jina-rerank', label: 'Jina Rerank (/v1/rerank)' },
  {
    value: 'image-generation',
    label: 'Image Generation (/v1/images/generations)',
  },
  { value: 'embeddings', label: 'Embeddings (/v1/embeddings)' },
]

const endpointSelectContentClass = 'w-[460px] max-w-[calc(100vw-2rem)]'
const endpointSelectItemClass =
  'items-start py-2 [&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:whitespace-normal'

/**
 * Endpoints with no streaming counterpart. Exported because the test dialog
 * reuses it when building the manual test request.
 */
export const STREAM_INCOMPATIBLE_ENDPOINTS = new Set([
  'embeddings',
  'image-generation',
  'jina-rerank',
  'openai-response-compact',
])

const BODY_MODE_OPTIONS: Array<{
  value: MonitorBodyMode
  label: string
}> = [
  { value: 'default', label: 'Default' },
  { value: 'merge', label: 'Merge' },
  { value: 'override', label: 'Override' },
]

const MIN_INTERVAL_SECONDS = 5
const MAX_INTERVAL_SECONDS = 24 * 60 * 60
const DEFAULT_INTERVAL_SECONDS = 600
const DEFAULT_JITTER_SECONDS = 60

let headerId = 0

function newHeader(key = '', value = ''): MonitorHeader {
  headerId += 1
  return { id: `monitor-header-${headerId}`, key, value }
}

function cloneHeaders(headers: MonitorHeader[]): MonitorHeader[] {
  return headers.map((header) => newHeader(header.key, header.value))
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

function parseSeconds(value: string): number {
  const seconds = Math.floor(Number(value))
  return Number.isFinite(seconds) && seconds >= 0 ? seconds : 0
}

type DetectionConfigPanelProps = {
  open: boolean
  channelId: number
  monitoringEnabled: boolean
  managed: boolean
  monitoredModels: string[]
  endpointType: string
  stream: boolean
  onMonitoringEnabledChange: (value: boolean) => void
  onManagedChange: (value: boolean) => void
  onMonitoredModelsChange: (value: string[]) => void
  onEndpointTypeChange: (value: string) => void
  onStreamChange: (value: boolean) => void
}

type TemplateNameDialogProps = {
  open: boolean
  template: MonitorTemplate | null
  saving: boolean
  onOpenChange: (open: boolean) => void
  onSave: (name: string, description: string) => void
}

function TemplateNameDialog(props: TemplateNameDialogProps) {
  const { t } = useTranslation()
  const [name, setName] = useState(props.template?.name ?? '')
  const [description, setDescription] = useState(
    props.template?.description ?? ''
  )

  useEffect(() => {
    if (!props.open) return
    setName(props.template?.name ?? '')
    setDescription(props.template?.description ?? '')
  }, [props.open, props.template])

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        props.template
          ? t('Edit detection template')
          : t('New detection template')
      }
      contentClassName='sm:max-w-md'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => props.onSave(name.trim(), description.trim())}
            disabled={props.saving || !name.trim()}
          >
            {props.saving ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Save className='size-4' />
            )}
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='space-y-4 py-1'>
        <div className='grid gap-2'>
          <Label htmlFor='monitor-template-name'>{t('Template name')}</Label>
          <Input
            id='monitor-template-name'
            value={name}
            maxLength={64}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='monitor-template-description'>
            {t('Description')}
          </Label>
          <Textarea
            id='monitor-template-description'
            value={description}
            maxLength={255}
            onChange={(event) => setDescription(event.target.value)}
          />
        </div>
      </div>
    </Dialog>
  )
}

export function DetectionConfigPanel(props: DetectionConfigPanelProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const channelId = props.channelId
  const panelOpen = props.open
  const applyEndpointType = props.onEndpointTypeChange
  const applyStream = props.onStreamChange
  const applyMonitoringEnabled = props.onMonitoringEnabledChange
  const applyManaged = props.onManagedChange
  const applyMonitoredModels = props.onMonitoredModelsChange
  const [configId, setConfigId] = useState(0)
  const [monitorMode, setMonitorMode] = useState<ChannelMonitorMode>('default')
  const [intervalSeconds, setIntervalSeconds] = useState(
    DEFAULT_INTERVAL_SECONDS
  )
  const [jitterSeconds, setJitterSeconds] = useState(DEFAULT_JITTER_SECONDS)
  const [templateId, setTemplateId] = useState(0)
  const [templates, setTemplates] = useState<MonitorTemplate[]>([])
  const [headers, setHeaders] = useState<MonitorHeader[]>([])
  const [bodyMode, setBodyMode] = useState<MonitorBodyMode>('default')
  const [bodyJson, setBodyJson] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [templateDialogOpen, setTemplateDialogOpen] = useState(false)
  const [editingTemplate, setEditingTemplate] =
    useState<MonitorTemplate | null>(null)
  const [templateSaving, setTemplateSaving] = useState(false)
  const [deleteTemplateOpen, setDeleteTemplateOpen] = useState(false)

  const selectedTemplate = useMemo(
    () => templates.find((template) => template.id === templateId) ?? null,
    [templateId, templates]
  )

  const endpointSelectItems = useMemo(
    () =>
      endpointTypeOptions.map((option) => ({
        value: option.value,
        label: t(option.label),
      })),
    [t]
  )
  const streamDisabled = STREAM_INCOMPATIBLE_ENDPOINTS.has(props.endpointType)

  const clearTemplateSelection = useCallback(() => {
    setTemplateId(0)
    setHeaders([])
    setBodyMode('default')
    setBodyJson('')
    setAdvancedOpen(false)
    applyEndpointType('auto')
    applyStream(false)
  }, [applyEndpointType, applyStream])

  useEffect(() => {
    if (!panelOpen) return
    let active = true
    setLoading(true)
    void Promise.all([
      getChannelMonitorConfig(channelId),
      getMonitorTemplates(),
    ])
      .then(([config, loadedTemplates]) => {
        if (!active) return
        setTemplates(loadedTemplates)
        setConfigId(config?.id ?? 0)
        applyMonitoringEnabled(config?.enabled ?? false)
        applyManaged(config?.managed ?? false)
        applyMonitoredModels(config?.monitoredModels ?? [])
        setMonitorMode(config?.monitorMode ?? 'default')
        setIntervalSeconds(config?.intervalSeconds ?? DEFAULT_INTERVAL_SECONDS)
        setJitterSeconds(config?.jitterSeconds ?? DEFAULT_JITTER_SECONDS)
        setTemplateId(config?.templateId ?? 0)
        setHeaders(cloneHeaders(config?.headers ?? []))
        setBodyMode(config?.bodyMode ?? 'default')
        setBodyJson(config?.bodyJson ?? '')
        applyEndpointType(config?.endpointType ?? 'auto')
        applyStream(config?.stream ?? false)
        setAdvancedOpen(
          Boolean(
            config &&
            (config.templateId > 0 ||
              config.headers.length > 0 ||
              config.bodyMode !== 'default')
          )
        )
      })
      .finally(() => {
        if (active) setLoading(false)
      })
      .catch((error: unknown) => {
        if (active) {
          toast.error(errorMessage(error, t('Failed to load detection config')))
        }
      })
    return () => {
      active = false
    }
  }, [
    applyEndpointType,
    applyManaged,
    applyMonitoredModels,
    applyMonitoringEnabled,
    applyStream,
    channelId,
    panelOpen,
    t,
  ])

  const applyTemplate = useCallback(
    (value: string | null) => {
      if (!value || value === NO_TEMPLATE) {
        if (templateId > 0) clearTemplateSelection()
        return
      }
      const template = templates.find((item) => String(item.id) === value)
      if (!template) return
      setTemplateId(template.id)
      setHeaders(cloneHeaders(template.headers))
      setBodyMode(template.bodyMode)
      setBodyJson(template.bodyJson)
      props.onEndpointTypeChange(template.endpointType)
      props.onStreamChange(template.stream)
      setAdvancedOpen(true)
      toast.success(t('Template applied as a snapshot'))
    },
    [clearTemplateSelection, props, t, templateId, templates]
  )

  const updateHeader = useCallback(
    (id: string, patch: Partial<MonitorHeader>) => {
      setHeaders((current) =>
        current.map((header) =>
          header.id === id ? { ...header, ...patch } : header
        )
      )
    },
    []
  )

  const saveConfig = useCallback(async () => {
    if (bodyMode !== 'default') {
      try {
        const parsed: unknown = JSON.parse(bodyJson)
        if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
          throw new Error()
        }
      } catch {
        toast.error(t('Request body must be a valid JSON object'))
        return
      }
    }
    const nextIntervalSeconds = Math.min(
      MAX_INTERVAL_SECONDS,
      Math.max(MIN_INTERVAL_SECONDS, intervalSeconds)
    )
    const nextJitterSeconds = Math.max(
      0,
      Math.min(jitterSeconds, nextIntervalSeconds - 1)
    )
    setSaving(true)
    try {
      const saved = await saveChannelMonitorConfig({
        id: configId,
        channelId: props.channelId,
        enabled: props.monitoringEnabled,
        managed: props.managed,
        monitorMode,
        intervalSeconds: nextIntervalSeconds,
        jitterSeconds: nextJitterSeconds,
        monitoredModels: props.monitoredModels,
        endpointType: props.endpointType,
        stream: props.stream,
        templateId,
        headers,
        bodyMode,
        bodyJson,
        updatedTime: 0,
      })
      setConfigId(saved.id)
      setHeaders(cloneHeaders(saved.headers))
      applyMonitoringEnabled(saved.enabled)
      applyManaged(saved.managed)
      applyMonitoredModels(saved.monitoredModels)
      setMonitorMode(saved.monitorMode)
      setIntervalSeconds(saved.intervalSeconds)
      setJitterSeconds(saved.jitterSeconds)
      void queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'list'],
      })
      toast.success(t('Monitoring config saved'))
    } catch (error: unknown) {
      toast.error(errorMessage(error, t('Failed to save detection config')))
    } finally {
      setSaving(false)
    }
  }, [
    applyManaged,
    applyMonitoredModels,
    applyMonitoringEnabled,
    bodyJson,
    bodyMode,
    configId,
    headers,
    intervalSeconds,
    jitterSeconds,
    monitorMode,
    props.channelId,
    props.endpointType,
    props.managed,
    props.monitoredModels,
    props.monitoringEnabled,
    props.stream,
    queryClient,
    t,
    templateId,
  ])

  const openTemplateDialog = useCallback((template: MonitorTemplate | null) => {
    setEditingTemplate(template)
    setTemplateDialogOpen(true)
  }, [])

  const saveTemplate = useCallback(
    async (name: string, description: string) => {
      setTemplateSaving(true)
      const draft: MonitorTemplate = {
        id: editingTemplate?.id ?? 0,
        name,
        description,
        endpointType: props.endpointType,
        stream: props.stream,
        headers,
        bodyMode,
        bodyJson,
        updatedTime: 0,
      }
      try {
        const saved = editingTemplate
          ? await updateMonitorTemplate(draft)
          : await createMonitorTemplate(draft)
        setTemplates((current) => {
          const withoutSaved = current.filter((item) => item.id !== saved.id)
          return [saved, ...withoutSaved]
        })
        setTemplateId(saved.id)
        setTemplateDialogOpen(false)
        toast.success(
          editingTemplate
            ? t('Detection template updated')
            : t('Detection template created')
        )
      } catch (error: unknown) {
        toast.error(errorMessage(error, t('Failed to save detection template')))
      } finally {
        setTemplateSaving(false)
      }
    },
    [
      bodyJson,
      bodyMode,
      editingTemplate,
      headers,
      props.endpointType,
      props.stream,
      t,
    ]
  )

  const deleteTemplate = useCallback(async () => {
    if (!selectedTemplate) return
    try {
      await deleteMonitorTemplate(selectedTemplate.id)
      setTemplates((current) =>
        current.filter((item) => item.id !== selectedTemplate.id)
      )
      clearTemplateSelection()
      setDeleteTemplateOpen(false)
      toast.success(t('Detection template deleted'))
    } catch (error: unknown) {
      toast.error(errorMessage(error, t('Failed to delete detection template')))
    }
  }, [clearTemplateSelection, selectedTemplate, t])

  const formatBody = useCallback(() => {
    try {
      setBodyJson(JSON.stringify(JSON.parse(bodyJson), null, 2))
    } catch {
      toast.error(t('Invalid JSON, unable to format'))
    }
  }, [bodyJson, t])

  return (
    <>
      <section className='space-y-3 border-t pt-4'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <h3 className='text-sm font-medium'>
            {t('Detection configuration')}
          </h3>
          <Button size='sm' onClick={saveConfig} disabled={loading || saving}>
            {saving ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Save className='size-4' />
            )}
            {t('Save detection config')}
          </Button>
        </div>

        <div className='grid gap-2'>
          <Label htmlFor='monitor-template-select'>
            {t('Detection template')}
          </Label>
          <div className='flex min-w-0 items-center gap-1'>
            <Select
              value={templateId > 0 ? String(templateId) : NO_TEMPLATE}
              onValueChange={applyTemplate}
              disabled={loading}
            >
              <SelectTrigger
                id='monitor-template-select'
                className='min-w-0 flex-1'
              >
                <SelectValue placeholder={t('No template')}>
                  {selectedTemplate?.name ?? t('No template')}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value={NO_TEMPLATE}>
                    {t('No template')}
                  </SelectItem>
                  {templates.map((template) => (
                    <SelectItem key={template.id} value={String(template.id)}>
                      {template.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant='outline'
                    size='icon-sm'
                    onClick={() => openTemplateDialog(null)}
                    aria-label={t('New detection template')}
                  />
                }
              >
                <Plus className='size-4' />
              </TooltipTrigger>
              <TooltipContent>{t('New detection template')}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant='outline'
                    size='icon-sm'
                    onClick={() => openTemplateDialog(selectedTemplate)}
                    disabled={!selectedTemplate}
                    aria-label={t('Edit detection template')}
                  />
                }
              >
                <Pencil className='size-4' />
              </TooltipTrigger>
              <TooltipContent>{t('Edit detection template')}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant='outline'
                    size='icon-sm'
                    onClick={() => setDeleteTemplateOpen(true)}
                    disabled={!selectedTemplate}
                    aria-label={t('Delete detection template')}
                  />
                }
              >
                <Trash2 className='size-4' />
              </TooltipTrigger>
              <TooltipContent>{t('Delete detection template')}</TooltipContent>
            </Tooltip>
          </div>
        </div>

        <div className='grid gap-4 md:grid-cols-2'>
          <div className='grid gap-2'>
            <Label htmlFor='endpoint-type'>{t('Endpoint Type')}</Label>
            <Select
              items={endpointSelectItems}
              value={props.endpointType}
              onValueChange={(value) => {
                if (value === null) return
                applyEndpointType(value)
              }}
            >
              <SelectTrigger id='endpoint-type' className='w-full min-w-0'>
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
                  {endpointSelectItems.map((option) => (
                    <SelectItem
                      key={option.value}
                      value={option.value}
                      className={endpointSelectItemClass}
                    >
                      <span className='min-w-0 leading-snug break-words whitespace-normal'>
                        {option.label}
                      </span>
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Override the endpoint used for testing. Leave empty to auto detect.'
              )}
            </p>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='stream-toggle'>{t('Stream Mode')}</Label>
            <div className='flex items-center gap-2'>
              <Switch
                id='stream-toggle'
                checked={props.stream}
                onCheckedChange={applyStream}
                disabled={streamDisabled}
              />
              <span className='text-sm'>
                {props.stream ? t('Enabled') : t('Disabled')}
              </span>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t('Enable streaming mode for the test request.')}
            </p>
          </div>
        </div>

        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='flex min-h-14 items-center justify-between gap-3 rounded-md border px-3 py-2'>
            <Label htmlFor='channel-monitoring-enabled'>
              {t('Enable monitoring')}
            </Label>
            <Switch
              id='channel-monitoring-enabled'
              checked={props.monitoringEnabled}
              disabled={loading || saving}
              onCheckedChange={props.onMonitoringEnabledChange}
            />
          </div>
          <div className='flex min-h-14 items-center justify-between gap-3 rounded-md border px-3 py-2'>
            <Label htmlFor='channel-managed'>{t('Channel hosting')}</Label>
            <Switch
              id='channel-managed'
              checked={props.managed}
              disabled={loading || saving}
              onCheckedChange={props.onManagedChange}
            />
          </div>
        </div>

        <div className='grid gap-2'>
          <Label id='channel-monitor-mode-label'>{t('Monitoring mode')}</Label>
          <ToggleGroup
            value={[monitorMode]}
            onValueChange={(values) => {
              const value = values[0]
              if (value === 'default' || value === 'banned_only') {
                setMonitorMode(value)
              }
            }}
            variant='outline'
            className='grid w-full grid-cols-2'
            aria-labelledby='channel-monitor-mode-label'
            disabled={loading || saving}
          >
            <ToggleGroupItem value='default'>
              {t('Default probing')}
            </ToggleGroupItem>
            <ToggleGroupItem value='banned_only'>
              {t('Banned-only probing')}
            </ToggleGroupItem>
          </ToggleGroup>
          <p className='text-muted-foreground text-xs'>
            {monitorMode === 'banned_only'
              ? t(
                  'Only probes banned models and pending ban confirmations, so healthy models are left alone.'
                )
              : t('Probes every monitored model on each interval.')}
          </p>
        </div>

        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='grid gap-2'>
            <Label htmlFor='channel-monitor-interval'>
              {t('Probe interval (seconds)')}
            </Label>
            <Input
              id='channel-monitor-interval'
              type='number'
              min={MIN_INTERVAL_SECONDS}
              max={MAX_INTERVAL_SECONDS}
              step={1}
              value={intervalSeconds}
              disabled={loading || saving}
              onChange={(event) =>
                setIntervalSeconds(parseSeconds(event.target.value))
              }
              onBlur={() => {
                const interval = Math.min(
                  MAX_INTERVAL_SECONDS,
                  Math.max(MIN_INTERVAL_SECONDS, intervalSeconds)
                )
                setIntervalSeconds(interval)
                setJitterSeconds((current) => Math.min(current, interval - 1))
              }}
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='channel-monitor-jitter'>
              {t('Random jitter (seconds)')}
            </Label>
            <Input
              id='channel-monitor-jitter'
              type='number'
              min={0}
              max={Math.max(0, intervalSeconds - 1)}
              step={1}
              value={jitterSeconds}
              disabled={loading || saving}
              onChange={(event) =>
                setJitterSeconds(parseSeconds(event.target.value))
              }
              onBlur={() =>
                setJitterSeconds((current) =>
                  Math.max(0, Math.min(current, intervalSeconds - 1))
                )
              }
            />
          </div>
        </div>

        {templateId === 0 && (
          <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
            <CollapsibleTrigger className='hover:bg-muted/50 flex w-full items-center justify-between border-y px-1 py-2 text-sm font-medium'>
              <span>{t('Request customization')}</span>
              <ChevronDown
                className={`size-4 transition-transform ${advancedOpen ? 'rotate-180' : ''}`}
              />
            </CollapsibleTrigger>
            <CollapsibleContent className='space-y-5 pt-4'>
              <div className='space-y-2'>
                <div className='flex items-center justify-between gap-2'>
                  <Label>{t('Custom headers')}</Label>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      setHeaders((current) => [...current, newHeader()])
                    }
                  >
                    <Plus className='size-4' />
                    {t('Add header')}
                  </Button>
                </div>
                {headers.length === 0 ? (
                  <div className='text-muted-foreground border-y py-3 text-sm'>
                    {t('No custom headers')}
                  </div>
                ) : (
                  <div className='space-y-2'>
                    {headers.map((header) => (
                      <div
                        key={header.id}
                        className='grid grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)_2rem] gap-2'
                      >
                        <Input
                          value={header.key}
                          placeholder={t('Header name')}
                          aria-label={t('Header name')}
                          onChange={(event) =>
                            updateHeader(header.id, { key: event.target.value })
                          }
                        />
                        <Input
                          value={header.value}
                          placeholder={t('Header value')}
                          aria-label={t('Header value')}
                          onChange={(event) =>
                            updateHeader(header.id, {
                              value: event.target.value,
                            })
                          }
                        />
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          onClick={() =>
                            setHeaders((current) =>
                              current.filter((item) => item.id !== header.id)
                            )
                          }
                          aria-label={t('Delete header')}
                        >
                          <Trash2 className='size-4' />
                        </Button>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <div className='space-y-2'>
                <Label>{t('Request body mode')}</Label>
                <ToggleGroup
                  value={[bodyMode]}
                  onValueChange={(values) => {
                    const next = values.find((value) => value !== bodyMode)
                    if (next) setBodyMode(next as MonitorBodyMode)
                  }}
                  variant='outline'
                  size='sm'
                  aria-label={t('Request body mode')}
                >
                  {BODY_MODE_OPTIONS.map((option) => (
                    <ToggleGroupItem key={option.value} value={option.value}>
                      {t(option.label)}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
                {bodyMode !== 'default' && (
                  <div className='space-y-2'>
                    <div className='flex items-center justify-between gap-2'>
                      <Label htmlFor='monitor-body-json'>
                        {t('Body JSON')}
                      </Label>
                      <Button variant='outline' size='sm' onClick={formatBody}>
                        <Braces className='size-4' />
                        {t('Format')}
                      </Button>
                    </div>
                    <Textarea
                      id='monitor-body-json'
                      value={bodyJson}
                      className='min-h-36 font-mono text-xs'
                      spellCheck={false}
                      placeholder={'{\n  "max_tokens": 20\n}'}
                      onChange={(event) => setBodyJson(event.target.value)}
                    />
                  </div>
                )}
              </div>
            </CollapsibleContent>
          </Collapsible>
        )}
      </section>

      <TemplateNameDialog
        open={templateDialogOpen}
        template={editingTemplate}
        saving={templateSaving}
        onOpenChange={setTemplateDialogOpen}
        onSave={saveTemplate}
      />
      <ConfirmDialog
        open={deleteTemplateOpen}
        onOpenChange={setDeleteTemplateOpen}
        title={t('Delete detection template')}
        desc={t('Delete template {{name}}?', {
          name: selectedTemplate?.name ?? '',
        })}
        destructive
        confirmText={t('Delete')}
        handleConfirm={deleteTemplate}
      />
    </>
  )
}
