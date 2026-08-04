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
import { ArrowLeft, Code2, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
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
import { cn } from '@/lib/utils'

import {
  MONITOR_ENDPOINT_OPTIONS,
  STREAM_INCOMPATIBLE_ENDPOINTS,
  channelsUsingTemplate,
} from '../constants'
import type { BodyMode, ChannelMonitorRow, MonitorTemplate } from '../types'
import { CustomHeadersEditor } from './custom-headers-editor'

const endpointSelectContentClass = 'w-[460px] max-w-[calc(100vw-2rem)]'
const endpointSelectItemClass =
  'items-start py-2 [&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:whitespace-normal'

const BODY_MODE_OPTIONS: Array<{ value: BodyMode; label: string }> = [
  { value: 'default', label: 'Default' },
  { value: 'merge', label: 'Merge' },
  { value: 'override', label: 'Override' },
]

/** Build a blank template for the "new template" flow. */
function newTemplate(): MonitorTemplate {
  return {
    id: 0,
    name: '',
    description: '',
    endpointType: 'openai',
    stream: true,
    headers: [],
    bodyMode: 'merge',
    bodyJson: '{\n  "max_tokens": 16\n}',
    updatedAt: Math.floor(Date.now() / 1000),
  }
}

function endpointLabel(value: string): string {
  return MONITOR_ENDPOINT_OPTIONS.find((o) => o.value === value)?.label ?? value
}

/** One template row in the list view. */
function TemplateCard({
  tpl,
  rows,
  onEdit,
  onApply,
  onDelete,
}: {
  tpl: MonitorTemplate
  rows: ChannelMonitorRow[]
  onEdit: () => void
  onApply: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const users = useMemo(
    () => channelsUsingTemplate(rows, tpl.name),
    [rows, tpl.name]
  )

  return (
    <div className='rounded-xl border p-3'>
      <div className='flex items-start justify-between gap-2'>
        <div className='min-w-0'>
          <div className='truncate text-sm font-medium'>{tpl.name}</div>
          {tpl.description ? (
            <div className='text-muted-foreground mt-0.5 text-xs'>
              {tpl.description}
            </div>
          ) : null}
        </div>
        <div className='flex shrink-0 items-center gap-1'>
          <Button type='button' variant='outline' size='sm' onClick={onEdit}>
            <Pencil data-icon='inline-start' className='size-3.5' />
            {t('Edit')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='text-muted-foreground hover:text-destructive'
            onClick={onDelete}
          >
            <Trash2 className='size-4' />
          </Button>
        </div>
      </div>

      <div className='text-muted-foreground mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
        <span>{t(endpointLabel(tpl.endpointType))}</span>
        <span>·</span>
        <span>{tpl.stream ? t('Stream') : t('Non-stream')}</span>
        <span>·</span>
        <span>{t('{{count}} headers', { count: tpl.headers.length })}</span>
        <span>·</span>
        <span>
          {tpl.bodyMode === 'override' ? t('Body override') : t('Body merge')}
        </span>
      </div>

      <div className='mt-3 flex flex-wrap items-center justify-between gap-2 border-t pt-3'>
        <div className='flex min-w-0 flex-wrap items-center gap-1.5'>
          <span className='text-muted-foreground text-xs'>{t('Used by')}</span>
          {users.length === 0 ? (
            <span className='text-muted-foreground/60 text-xs'>
              {t('No channels')}
            </span>
          ) : (
            users.map((u) => (
              <span
                key={u.id}
                className='bg-muted inline-flex items-center rounded-full px-2 py-0.5 text-[11px] leading-none font-medium'
              >
                {u.name}
              </span>
            ))
          )}
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={users.length === 0}
          onClick={onApply}
        >
          <RefreshCw data-icon='inline-start' className='size-3.5' />
          {t('Apply update')}
        </Button>
      </div>
    </div>
  )
}

/** The editor sub-view for a single template. */
function TemplateEditor({
  draft,
  onChange,
  onBack,
  onSave,
}: {
  draft: MonitorTemplate
  onChange: (next: MonitorTemplate) => void
  onBack: () => void
  onSave: () => void
}) {
  const { t } = useTranslation()
  const streamDisabled = STREAM_INCOMPATIBLE_ENDPOINTS.has(draft.endpointType)
  const effectiveStream = streamDisabled ? false : draft.stream

  const patch = (next: Partial<MonitorTemplate>) =>
    onChange({ ...draft, ...next })

  const handleEndpointChange = (value: string | null) => {
    if (!value) return
    if (STREAM_INCOMPATIBLE_ENDPOINTS.has(value)) {
      patch({ endpointType: value, stream: false })
      return
    }
    patch({ endpointType: value })
  }

  const formatBody = () => {
    const raw = draft.bodyJson.trim()
    if (!raw) return
    try {
      patch({ bodyJson: JSON.stringify(JSON.parse(raw), null, 2) })
    } catch {
      toast.error(t('Invalid JSON, unable to format'))
    }
  }

  return (
    <div className='space-y-5'>
      <button
        type='button'
        className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-sm'
        onClick={onBack}
      >
        <ArrowLeft className='size-4' />
        {t('Back to templates')}
      </button>

      {/* Name + description */}
      <div className='grid gap-4 md:grid-cols-2'>
        <div className='grid gap-2'>
          <Label htmlFor='tpl-name'>{t('Template name')}</Label>
          <Input
            id='tpl-name'
            value={draft.name}
            placeholder={t('e.g. Codex CLI')}
            className='bg-background'
            onChange={(e) => patch({ name: e.target.value })}
          />
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='tpl-desc'>{t('Description')}</Label>
          <Input
            id='tpl-desc'
            value={draft.description}
            placeholder={t('Optional short description')}
            className='bg-background'
            onChange={(e) => patch({ description: e.target.value })}
          />
        </div>
      </div>

      {/* Endpoint + stream */}
      <div className='grid gap-4 md:grid-cols-2'>
        <div className='grid gap-2'>
          <Label htmlFor='tpl-endpoint'>{t('Endpoint Type')}</Label>
          <Select
            value={draft.endpointType}
            onValueChange={handleEndpointChange}
          >
            <SelectTrigger
              id='tpl-endpoint'
              className='bg-background w-full min-w-0'
            >
              <SelectValue className='min-w-0 truncate' />
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
          <Label htmlFor='tpl-stream'>{t('Stream Mode')}</Label>
          <div className='flex h-8 items-center gap-2'>
            <Switch
              id='tpl-stream'
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

      {/* Custom headers */}
      <CustomHeadersEditor
        headers={draft.headers}
        onChange={(headers) => onChange({ ...draft, headers })}
      />

      {/* Body handling */}
      <div className='space-y-2'>
        <Label>{t('Request body handling')}</Label>
        <div className='flex flex-wrap gap-2'>
          {BODY_MODE_OPTIONS.map((option) => {
            const active = draft.bodyMode === option.value
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
        {draft.bodyMode !== 'default' ? (
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
              value={draft.bodyJson}
              placeholder={'{\n  "max_tokens": 16\n}'}
              className='bg-background min-h-32 font-mono text-xs'
              spellCheck={false}
              onChange={(e) => patch({ bodyJson: e.target.value })}
            />
          </div>
        ) : null}
      </div>

      <div className='flex justify-end gap-2 border-t pt-4'>
        <Button type='button' variant='outline' onClick={onBack}>
          {t('Cancel')}
        </Button>
        <Button type='button' onClick={onSave}>
          {t('Save template')}
        </Button>
      </div>
    </div>
  )
}

export function TemplateManagerDialog({
  open,
  templates,
  rows,
  onClose,
  onSaveTemplate,
  onDeleteTemplate,
  onApplyTemplate,
}: {
  open: boolean
  templates: MonitorTemplate[]
  rows: ChannelMonitorRow[]
  onClose: () => void
  onSaveTemplate: (tpl: MonitorTemplate) => void
  onDeleteTemplate: (id: number) => void
  onApplyTemplate: (tpl: MonitorTemplate) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<MonitorTemplate | null>(null)

  const handleClose = () => {
    setDraft(null)
    onClose()
  }

  const handleSaveDraft = () => {
    if (!draft) return
    if (!draft.name.trim()) {
      toast.error(t('Template name is required'))
      return
    }
    if (draft.bodyMode !== 'default' && draft.bodyJson.trim()) {
      try {
        JSON.parse(draft.bodyJson)
      } catch {
        toast.error(t('Invalid JSON in request body'))
        return
      }
    }
    onSaveTemplate({ ...draft, updatedAt: Math.floor(Date.now() / 1000) })
    setDraft(null)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) handleClose()
      }}
      title={draft ? t('Edit template') : t('Template management')}
      description={
        draft
          ? t('Configure a reusable probe template.')
          : t(
              'Manage reusable probe templates. Applying an update re-snapshots the template onto its channels.'
            )
      }
      contentClassName={cn('sm:max-w-2xl', draft && 'bg-muted/40')}
      footer={
        draft ? undefined : (
          <Button type='button' variant='outline' onClick={handleClose}>
            {t('Close')}
          </Button>
        )
      }
    >
      {draft ? (
        <TemplateEditor
          draft={draft}
          onChange={setDraft}
          onBack={() => setDraft(null)}
          onSave={handleSaveDraft}
        />
      ) : (
        <div className='space-y-3'>
          <div className='flex justify-end'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setDraft(newTemplate())}
            >
              <Plus data-icon='inline-start' className='size-3.5' />
              {t('New template')}
            </Button>
          </div>
          {templates.length === 0 ? (
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('No templates yet. Create one to get started.')}
            </p>
          ) : (
            <div className='space-y-3'>
              {templates.map((tpl) => (
                <TemplateCard
                  key={tpl.id}
                  tpl={tpl}
                  rows={rows}
                  onEdit={() => setDraft({ ...tpl })}
                  onApply={() => onApplyTemplate(tpl)}
                  onDelete={() => onDeleteTemplate(tpl.id)}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </Dialog>
  )
}
