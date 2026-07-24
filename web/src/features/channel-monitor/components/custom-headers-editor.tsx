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
import { Download, Plus, Trash2, Upload } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import { parseImportedHeaders } from '../header-import'
import type { HeaderEntry } from '../types'

/**
 * Serialize header rows into a plain `{ "Name": "value" }` object for export.
 * Blank-named rows are dropped so the exported JSON is directly reusable as an
 * import; later duplicates win, matching how upstream merges repeated headers.
 */
function headersToObject(headers: HeaderEntry[]): Record<string, string> {
  const result: Record<string, string> = {}
  for (const header of headers) {
    const key = header.key.trim()
    if (!key) continue
    result[key] = header.value
  }
  return result
}

/** Modal that takes a pasted JSON object and converts it into header rows. */
function ImportHeadersDialog({
  open,
  onClose,
  onImport,
}: {
  open: boolean
  onClose: () => void
  onImport: (headers: HeaderEntry[]) => void
}) {
  const { t } = useTranslation()
  const [text, setText] = useState('')

  const handleImport = () => {
    const raw = text.trim()
    if (!raw) {
      onClose()
      return
    }
    try {
      const imported = parseImportedHeaders(
        raw,
        t('Invalid JSON, unable to import headers')
      )
      onImport(
        imported.map((header) => ({
          ...header,
          id: crypto.randomUUID(),
        }))
      )
      setText('')
      onClose()
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Invalid JSON, unable to import headers')
      )
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setText('')
          onClose()
        }
      }}
      title={t('Import headers from JSON')}
      description={t(
        'Paste a JSON object of header name/value pairs. Existing headers are replaced.'
      )}
      contentClassName='border-primary/20 bg-accent shadow-2xl ring-primary/20 sm:max-w-lg'
      headerClassName='pr-10'
      bodyClassName='border-primary/15 bg-background/60 rounded-lg border p-3 shadow-sm'
      footerClassName='border-primary/15 bg-background/90'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => {
              setText('')
              onClose()
            }}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleImport}>
            {t('Import')}
          </Button>
        </>
      }
    >
      <div className='grid gap-2'>
        <Label htmlFor='import-headers-json'>{t('Headers JSON')}</Label>
        <Textarea
          id='import-headers-json'
          value={text}
          placeholder={
            '{\n  "Authorization": "Bearer sk-...",\n  "X-Trace-Id": "abc"\n}'
          }
          className='border-primary/20 bg-background min-h-40 font-mono text-xs shadow-inner'
          spellCheck={false}
          onChange={(e) => setText(e.target.value)}
        />
      </div>
    </Dialog>
  )
}

/**
 * Shared editor for a config/template's custom request headers: editable
 * name/value rows plus JSON import (paste an object, replace the rows) and
 * export (copy the current rows as a JSON object to the clipboard). Used by both
 * the per-channel monitor config dialog and the reusable template editor so the
 * two stay in lockstep.
 */
export function CustomHeadersEditor({
  headers,
  onChange,
  hint,
}: {
  headers: HeaderEntry[]
  onChange: (headers: HeaderEntry[]) => void
  /** Optional trailing help text shown below the rows. */
  hint?: string
}) {
  const { t } = useTranslation()
  const [importOpen, setImportOpen] = useState(false)

  const updateHeader = (index: number, next: Partial<HeaderEntry>) =>
    onChange(headers.map((h, i) => (i === index ? { ...h, ...next } : h)))

  const addHeader = () =>
    onChange([...headers, { id: crypto.randomUUID(), key: '', value: '' }])

  const removeHeader = (index: number) =>
    onChange(headers.filter((_, i) => i !== index))

  const exportHeaders = async () => {
    const object = headersToObject(headers)
    if (Object.keys(object).length === 0) {
      toast.error(t('No named headers to export.'))
      return
    }
    const json = JSON.stringify(object, null, 2)
    try {
      await navigator.clipboard.writeText(json)
      toast.success(t('Headers copied to clipboard as JSON'))
    } catch {
      toast.error(t('Unable to copy to clipboard'))
    }
  }

  return (
    <div className='space-y-2'>
      <div className='flex items-center justify-between gap-2'>
        <Label>{t('Custom request headers')}</Label>
        <div className='flex shrink-0 items-center gap-1'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setImportOpen(true)}
          >
            <Upload data-icon='inline-start' className='size-3.5' />
            {t('Import JSON')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => void exportHeaders()}
          >
            <Download data-icon='inline-start' className='size-3.5' />
            {t('Export JSON')}
          </Button>
          <Button type='button' variant='outline' size='sm' onClick={addHeader}>
            <Plus data-icon='inline-start' className='size-3.5' />
            {t('Add header')}
          </Button>
        </div>
      </div>
      {headers.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t('No custom headers.')}
        </p>
      ) : (
        <div className='space-y-2'>
          {headers.map((header, i) => (
            <div key={header.id} className='flex items-center gap-2'>
              <Input
                value={header.key}
                placeholder={t('Header name')}
                className='bg-background flex-1'
                onChange={(e) => updateHeader(i, { key: e.target.value })}
              />
              <Input
                value={header.value}
                placeholder={t('Value')}
                className='bg-background flex-1'
                onChange={(e) => updateHeader(i, { value: e.target.value })}
              />
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='text-muted-foreground hover:text-destructive shrink-0'
                onClick={() => removeHeader(i)}
              >
                <Trash2 className='size-4' />
              </Button>
            </div>
          ))}
        </div>
      )}
      {hint ? <p className='text-muted-foreground text-xs'>{hint}</p> : null}
      <ImportHeadersDialog
        open={importOpen}
        onClose={() => setImportOpen(false)}
        onImport={onChange}
      />
    </div>
  )
}
