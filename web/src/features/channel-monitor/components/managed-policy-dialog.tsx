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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import type { ManagedPolicySetting } from '../types'

/** Scheduler tick floor: confirmation probes can happen no more often than this. */
const CONFIRM_INTERVAL_FLOOR = 15

/** Parse a numeric input to a clamped integer, treating empty/NaN as `fallback`. */
function parseInt2(value: string, fallback: number): number {
  const n = Math.floor(Number(value))
  if (!Number.isFinite(n)) return fallback
  return n
}

/**
 * The "托管策略" dialog: edits the global channel-hosting policy. Two independent
 * mechanisms, each behind its own master switch. The dialog only clamps for input
 * UX; the backend re-clamps on save, so values here converge on the same valid
 * state as a hand-edited config.
 */
export function ManagedPolicyDialog({
  open,
  setting,
  loading,
  saving,
  onClose,
  onSave,
}: {
  open: boolean
  setting: ManagedPolicySetting | undefined
  loading: boolean
  saving: boolean
  onClose: () => void
  onSave: (setting: ManagedPolicySetting) => Promise<void> | void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<ManagedPolicySetting | null>(null)

  // Reset the local draft whenever the dialog opens with fresh server data so
  // stale edits from a previous open never leak in.
  useEffect(() => {
    if (open && setting) setDraft({ ...setting })
  }, [open, setting])

  const patch = (next: Partial<ManagedPolicySetting>) =>
    setDraft((prev) => (prev ? { ...prev, ...next } : prev))

  const handleSave = async () => {
    if (!draft) return
    // When DingTalk alerts are on, the webhook must be a plausible https URL —
    // mirror the backend guard so the user sees a friendly error instead of a
    // rejected save.
    if (
      draft.dingtalkEnabled &&
      !draft.dingtalkWebhookUrl.trim().startsWith('https://')
    ) {
      toast.error(t('Enter a valid DingTalk webhook URL (must start with https://)'))
      return
    }
    await onSave({
      ...draft,
      confirmCount: Math.max(1, draft.confirmCount),
      banConfirmIntervalSeconds: Math.max(
        CONFIRM_INTERVAL_FLOOR,
        draft.banConfirmIntervalSeconds
      ),
      speedWindow: Math.max(1, draft.speedWindow),
      tierDiffPercent: Math.max(0, draft.tierDiffPercent),
      dingtalkWebhookUrl: draft.dingtalkWebhookUrl.trim(),
      dingtalkSecret: draft.dingtalkSecret.trim(),
    })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose()
      }}
      title={t('Managed policy')}
      description={t('Ban / recover and speed policy switches')}
      contentClassName='sm:max-w-2xl'
      showCloseButton
    >
      {loading || !draft ? (
        <div className='text-muted-foreground py-10 text-center text-sm'>
          {t('Loading...')}
        </div>
      ) : (
        <div className='space-y-6'>
          {/* Ban / recover circuit breaker */}
          <section className='space-y-4 rounded-xl border p-4'>
            <div className='flex items-start justify-between gap-3'>
              <div className='min-w-0'>
                <Label className='text-sm font-medium'>
                  {t('Ban and recover')}
                </Label>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'A model is banned after {{count}} consecutive failing probes (confirmed), and recovered after the same number of consecutive successes. A single opposite result resets the counter.',
                    { count: draft.confirmCount }
                  )}
                </p>
              </div>
              <Switch
                checked={draft.banEnabled}
                onCheckedChange={(v) => patch({ banEnabled: v })}
              />
            </div>
            {draft.banEnabled ? (
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='grid gap-2'>
                  <Label htmlFor='policy-confirm-count'>
                    {t('Confirmation count')}
                  </Label>
                  <Input
                    id='policy-confirm-count'
                    type='number'
                    min={1}
                    step={1}
                    value={draft.confirmCount}
                    onChange={(e) =>
                      patch({ confirmCount: parseInt2(e.target.value, 1) })
                    }
                    onBlur={() =>
                      patch({ confirmCount: Math.max(1, draft.confirmCount) })
                    }
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Consecutive confirming probes before a ban or recover.'
                    )}
                  </p>
                </div>
                <div className='grid gap-2'>
                  <Label htmlFor='policy-confirm-interval'>
                    {t('Confirmation interval (seconds)')}
                  </Label>
                  <Input
                    id='policy-confirm-interval'
                    type='number'
                    min={CONFIRM_INTERVAL_FLOOR}
                    step={1}
                    value={draft.banConfirmIntervalSeconds}
                    onChange={(e) =>
                      patch({
                        banConfirmIntervalSeconds: parseInt2(
                          e.target.value,
                          CONFIRM_INTERVAL_FLOOR
                        ),
                      })
                    }
                    onBlur={() =>
                      patch({
                        banConfirmIntervalSeconds: Math.max(
                          CONFIRM_INTERVAL_FLOOR,
                          draft.banConfirmIntervalSeconds
                        ),
                      })
                    }
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Spacing between confirmation probes. Floored at ~15s (the scheduler tick).'
                    )}
                  </p>
                </div>
              </div>
            ) : null}
          </section>

          {/* Speed-based up/downgrade */}
          <section className='space-y-4 rounded-xl border p-4'>
            <div className='flex items-start justify-between gap-3'>
              <div className='min-w-0'>
                <Label className='text-sm font-medium'>
                  {t('Speed-based up/downgrade')}
                </Label>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Ranks channels serving the same model by recent mean TTFT and assigns priority tiers. Channels within the gap share a tier.'
                  )}
                </p>
              </div>
              <Switch
                checked={draft.speedEnabled}
                onCheckedChange={(v) => patch({ speedEnabled: v })}
              />
            </div>
            {draft.speedEnabled ? (
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='grid gap-2'>
                  <Label htmlFor='policy-speed-window'>
                    {t('Speed sample window')}
                  </Label>
                  <Input
                    id='policy-speed-window'
                    type='number'
                    min={1}
                    step={1}
                    value={draft.speedWindow}
                    onChange={(e) =>
                      patch({ speedWindow: parseInt2(e.target.value, 1) })
                    }
                    onBlur={() =>
                      patch({ speedWindow: Math.max(1, draft.speedWindow) })
                    }
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'How many recent TTFT samples to average.'
                    )}
                  </p>
                </div>
                <div className='grid gap-2'>
                  <Label htmlFor='policy-tier-gap'>
                    {t('Tier gap (%)')}
                  </Label>
                  <Input
                    id='policy-tier-gap'
                    type='number'
                    min={0}
                    step={1}
                    value={draft.tierDiffPercent}
                    onChange={(e) =>
                      patch({ tierDiffPercent: parseInt2(e.target.value, 0) })
                    }
                    onBlur={() =>
                      patch({
                        tierDiffPercent: Math.max(0, draft.tierDiffPercent),
                      })
                    }
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Channels whose mean TTFT is within this percent share a tier.'
                    )}
                  </p>
                </div>
              </div>
            ) : null}
          </section>

          {/* DingTalk notification */}
          <section className='space-y-4 rounded-xl border p-4'>
            <div className='flex items-start justify-between gap-3'>
              <div className='min-w-0'>
                <Label className='text-sm font-medium'>
                  {t('DingTalk notification')}
                </Label>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Push an action card to a DingTalk custom robot on every ban or recover, in addition to the built-in notification.'
                  )}
                </p>
              </div>
              <Switch
                checked={draft.dingtalkEnabled}
                onCheckedChange={(v) => patch({ dingtalkEnabled: v })}
              />
            </div>
            {draft.dingtalkEnabled ? (
              <div className='space-y-4'>
                <div className='grid gap-2'>
                  <Label htmlFor='policy-dingtalk-url'>
                    {t('Webhook URL')}
                  </Label>
                  <Input
                    id='policy-dingtalk-url'
                    type='url'
                    placeholder='https://oapi.dingtalk.com/robot/send?access_token=...'
                    value={draft.dingtalkWebhookUrl}
                    onChange={(e) =>
                      patch({ dingtalkWebhookUrl: e.target.value })
                    }
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'The custom robot webhook, must start with https://. Required when enabled.'
                    )}
                  </p>
                </div>
                <div className='grid gap-2'>
                  <Label htmlFor='policy-dingtalk-secret'>
                    {t('Signing secret')}
                  </Label>
                  <Input
                    id='policy-dingtalk-secret'
                    type='password'
                    autoComplete='off'
                    placeholder={t('Optional, for robots with sign enabled')}
                    value={draft.dingtalkSecret}
                    onChange={(e) => patch({ dingtalkSecret: e.target.value })}
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'The secret shown when the robot uses the "signed" security setting. Leave empty for keyword or IP-based robots.'
                    )}
                  </p>
                </div>
              </div>
            ) : null}
          </section>

          <div className='flex justify-end gap-2'>
            <Button type='button' variant='outline' onClick={onClose}>
              {t('Cancel')}
            </Button>
            <Button type='button' onClick={handleSave} disabled={saving}>
              {saving ? t('Saving...') : t('Save')}
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  )
}
