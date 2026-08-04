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
import { ArrowLeft, BookOpenText, Pencil, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'

import type { MonitorQuestion } from '../types'

const QUESTION_MAX_LENGTH = 1000

function newQuestion(): MonitorQuestion {
  return {
    id: 0,
    content: '',
    updatedAt: Math.floor(Date.now() / 1000),
  }
}

function QuestionCard({
  question,
  onEdit,
  onDelete,
}: {
  question: MonitorQuestion
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-3 rounded-xl border p-3'>
      <p className='text-sm leading-relaxed break-words whitespace-pre-wrap'>
        {question.content}
      </p>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <span className='text-muted-foreground text-xs'>
          {t('Last updated: {{time}}', {
            time: dayjs(question.updatedAt * 1000).format('YYYY-MM-DD HH:mm'),
          })}
        </span>
        <div className='flex items-center gap-1'>
          <Button type='button' variant='outline' size='sm' onClick={onEdit}>
            <Pencil data-icon='inline-start' className='size-3.5' />
            {t('Edit')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='text-muted-foreground hover:text-destructive'
            aria-label={t('Delete question')}
            onClick={onDelete}
          >
            <Trash2 className='size-4' />
          </Button>
        </div>
      </div>
    </div>
  )
}

function QuestionEditor({
  draft,
  saving,
  onChange,
  onBack,
  onSave,
}: {
  draft: MonitorQuestion
  saving: boolean
  onChange: (next: MonitorQuestion) => void
  onBack: () => void
  onSave: () => void
}) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-5'>
      <button
        type='button'
        className='text-muted-foreground hover:text-foreground inline-flex w-fit items-center gap-1 text-sm'
        onClick={onBack}
      >
        <ArrowLeft className='size-4' />
        {t('Back to questions')}
      </button>

      <FieldGroup>
        <Field>
          <FieldLabel htmlFor='monitor-question'>{t('Question')}</FieldLabel>
          <Textarea
            id='monitor-question'
            value={draft.content}
            maxLength={QUESTION_MAX_LENGTH}
            rows={8}
            placeholder={t('Enter a short health-check question.')}
            onChange={(event) =>
              onChange({ ...draft, content: event.target.value })
            }
          />
          <div className='flex items-start justify-between gap-3'>
            <FieldDescription>
              {t(
                'This text is sent as the user message for conversational probes.'
              )}
            </FieldDescription>
            <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
              {draft.content.length}/{QUESTION_MAX_LENGTH}
            </span>
          </div>
        </Field>
      </FieldGroup>

      <div className='flex justify-end'>
        <Button type='button' disabled={saving} onClick={onSave}>
          {t('Save question')}
        </Button>
      </div>
    </div>
  )
}

function QuestionList({
  questions,
  loading,
  onNew,
  onEdit,
  onDelete,
}: {
  questions: MonitorQuestion[]
  loading: boolean
  onNew: () => void
  onEdit: (question: MonitorQuestion) => void
  onDelete: (question: MonitorQuestion) => void
}) {
  const { t } = useTranslation()

  let content: React.ReactNode
  if (loading) {
    content = (
      <div className='flex flex-col gap-3'>
        <Skeleton className='h-24 rounded-xl' />
        <Skeleton className='h-24 rounded-xl' />
        <Skeleton className='h-24 rounded-xl' />
      </div>
    )
  } else if (questions.length === 0) {
    content = (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <BookOpenText />
          </EmptyMedia>
          <EmptyTitle>{t('No questions yet')}</EmptyTitle>
          <EmptyDescription>
            {t(
              'Create a question to replace the default "hi" conversational probe.'
            )}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button type='button' onClick={onNew}>
            <Plus data-icon='inline-start' className='size-4' />
            {t('New question')}
          </Button>
        </EmptyContent>
      </Empty>
    )
  } else {
    content = (
      <div className='flex flex-col gap-3'>
        {questions.map((question) => (
          <QuestionCard
            key={question.id}
            question={question}
            onEdit={() => onEdit(question)}
            onDelete={() => onDelete(question)}
          />
        ))}
      </div>
    )
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex justify-end'>
        <Button type='button' variant='outline' size='sm' onClick={onNew}>
          <Plus data-icon='inline-start' className='size-3.5' />
          {t('New question')}
        </Button>
      </div>
      {content}
    </div>
  )
}

export function QuestionLibraryDialog({
  open,
  questions,
  loading,
  saving,
  deleting,
  onClose,
  onSaveQuestion,
  onDeleteQuestion,
}: {
  open: boolean
  questions: MonitorQuestion[]
  loading: boolean
  saving: boolean
  deleting: boolean
  onClose: () => void
  onSaveQuestion: (question: MonitorQuestion) => Promise<void>
  onDeleteQuestion: (id: number) => Promise<void>
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<MonitorQuestion | null>(null)
  const [pendingDelete, setPendingDelete] = useState<MonitorQuestion | null>(
    null
  )

  const handleClose = () => {
    setDraft(null)
    setPendingDelete(null)
    onClose()
  }

  const handleSave = async () => {
    if (!draft) return
    const content = draft.content.trim()
    if (!content) {
      toast.error(t('Question is required'))
      return
    }
    if (content.length > QUESTION_MAX_LENGTH) {
      toast.error(
        t('Question cannot exceed {{count}} characters', {
          count: QUESTION_MAX_LENGTH,
        })
      )
      return
    }
    try {
      await onSaveQuestion({ ...draft, content })
      setDraft(null)
    } catch {
      // Keep the editor open so the user can correct or retry after a server error.
    }
  }

  const handleDelete = async () => {
    if (!pendingDelete) return
    try {
      await onDeleteQuestion(pendingDelete.id)
      setPendingDelete(null)
    } catch {
      // Keep the confirmation open so the user can retry after a server error.
    }
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next) handleClose()
        }}
        title={draft ? t('Edit question') : t('Question library')}
        description={
          draft
            ? t('Edit the question sent by conversational probes.')
            : t(
                'Questions are selected independently for each monitored model. When the library is empty, probes use "hi".'
              )
        }
        contentClassName='sm:max-w-2xl'
        contentHeight='min(560px, calc(100vh - 12rem))'
        showCloseButton
        footer={
          draft ? undefined : (
            <Button type='button' variant='outline' onClick={handleClose}>
              {t('Close')}
            </Button>
          )
        }
      >
        {draft ? (
          <QuestionEditor
            draft={draft}
            saving={saving}
            onChange={setDraft}
            onBack={() => setDraft(null)}
            onSave={() => void handleSave()}
          />
        ) : (
          <QuestionList
            questions={questions}
            loading={loading}
            onNew={() => setDraft(newQuestion())}
            onEdit={(question) => setDraft({ ...question })}
            onDelete={setPendingDelete}
          />
        )}
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(next) => {
          if (!next) setPendingDelete(null)
        }}
        title={t('Delete question')}
        desc={t('Are you sure you want to delete this question?')}
        confirmText={t('Delete')}
        destructive
        isLoading={deleting}
        handleConfirm={() => void handleDelete()}
      />
    </>
  )
}
