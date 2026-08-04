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
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { ChannelRecommendationRow } from '../types'

/** Parse a weight input to a non-negative integer, treating empty/NaN as 0. */
function parseWeight(value: string): number {
  const n = Math.floor(Number(value))
  if (!Number.isFinite(n) || n < 0) return 0
  return n
}

/**
 * The "渠道推荐" dialog: maintains a per-channel recommendation weight and blurb.
 * Every channel is listed (default weight 0); only channels with a positive weight
 * appear in ban/recover notifications. The star rating is derived live from probe
 * speed on the backend, so it is not edited here — only the weight (推荐系数, the
 * ordering) and the blurb (推荐话术) are operator-maintained.
 */
export function ChannelRecommendationDialog({
  open,
  rows,
  loading,
  saving,
  onClose,
  onSave,
}: {
  open: boolean
  rows: ChannelRecommendationRow[]
  loading: boolean
  saving: boolean
  onClose: () => void
  onSave: (rows: ChannelRecommendationRow[]) => Promise<void> | void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<ChannelRecommendationRow[]>([])
  const [filter, setFilter] = useState('')

  // Reset the local draft whenever the dialog opens with fresh server data so
  // stale edits from a previous open never leak in.
  useEffect(() => {
    if (open) {
      setDraft(rows.map((r) => ({ ...r })))
      setFilter('')
    }
  }, [open, rows])

  const patch = (channelId: number, next: Partial<ChannelRecommendationRow>) =>
    setDraft((prev) =>
      prev.map((r) => (r.channelId === channelId ? { ...r, ...next } : r))
    )

  const visible = useMemo(() => {
    const keyword = filter.trim().toLowerCase()
    if (!keyword) return draft
    return draft.filter(
      (r) =>
        r.channelName.toLowerCase().includes(keyword) ||
        r.channelType.toLowerCase().includes(keyword)
    )
  }, [draft, filter])

  const handleSave = async () => {
    await onSave(
      draft.map((r) => ({
        ...r,
        weight: Math.max(0, Math.floor(r.weight)),
        blurb: r.blurb.trim(),
      }))
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose()
      }}
      title={t('Channel recommendation')}
      description={t(
        'Maintain a recommendation weight and blurb per channel. Only channels with a weight above 0 appear in notifications, ordered by weight. Star rating is derived from recent probe speed automatically.'
      )}
      contentClassName='sm:max-w-3xl'
      contentHeight='min(600px, calc(100vh - 12rem))'
      showCloseButton
      footer={
        <div className='flex justify-end gap-2'>
          <Button type='button' variant='outline' onClick={onClose}>
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={() => void handleSave()}
            disabled={saving || loading}
          >
            {saving ? t('Saving...') : t('Save')}
          </Button>
        </div>
      }
    >
      {loading ? (
        <div className='text-muted-foreground py-10 text-center text-sm'>
          {t('Loading...')}
        </div>
      ) : (
        <div className='flex flex-col gap-3'>
          <Input
            value={filter}
            placeholder={t('Search channels')}
            onChange={(e) => setFilter(e.target.value)}
          />
          <div className='rounded-xl border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Channel')}</TableHead>
                  <TableHead className='w-28'>
                    {t('Recommendation weight')}
                  </TableHead>
                  <TableHead>{t('Recommendation blurb')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.length === 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={3}
                      className='text-muted-foreground py-8 text-center text-sm'
                    >
                      {t('No channels')}
                    </TableCell>
                  </TableRow>
                )}
                {visible.map((row) => (
                  <TableRow key={row.channelId}>
                    <TableCell>
                      <div className='min-w-0'>
                        <div className='truncate text-sm font-medium'>
                          {row.channelName}
                        </div>
                        <div className='text-muted-foreground text-xs'>
                          {row.channelType} · #{row.channelId}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        value={row.weight}
                        onChange={(e) =>
                          patch(row.channelId, {
                            weight: parseWeight(e.target.value),
                          })
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.blurb}
                        maxLength={255}
                        placeholder={t('e.g. Cheap channel, ultra-low rate!')}
                        onChange={(e) =>
                          patch(row.channelId, { blurb: e.target.value })
                        }
                      />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      )}
    </Dialog>
  )
}
