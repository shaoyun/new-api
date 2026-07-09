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
import { useQuery } from '@tanstack/react-query'
import { Copy, Check, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { cn } from '@/lib/utils'

import { getRequestDebugInfo } from '../../api'

interface RequestDebugDialogProps {
  requestId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

function prettifyBody(body: string): string {
  if (!body) return ''
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

function CopyButton(props: { text: string }) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  return (
    <Button
      variant='ghost'
      size='sm'
      className='absolute top-1.5 right-1.5 h-5 w-5 p-0'
      onClick={() => copyToClipboard(props.text)}
      title={t('Copy to clipboard')}
      aria-label={t('Copy to clipboard')}
    >
      {copiedText === props.text ? (
        <Check className='size-3 text-green-600' />
      ) : (
        <Copy className='size-3' />
      )}
    </Button>
  )
}

export function RequestDebugDialog(props: RequestDebugDialogProps) {
  const { t } = useTranslation()

  const { data, isLoading, isError } = useQuery({
    queryKey: ['request-debug', props.requestId],
    queryFn: async () => {
      const res = await getRequestDebugInfo(props.requestId)
      if (!res.success) throw new Error(res.message || 'failed')
      return res.data ?? null
    },
    enabled: props.open && !!props.requestId,
    staleTime: 30_000,
    retry: false,
  })

  const payload = data ?? null
  const headerEntries = payload?.headers
    ? Object.entries(payload.headers)
    : []
  const prettyBody = prettifyBody(payload?.body ?? '')

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Request Debug')}
      description={t('Inspect the captured request headers and body')}
      contentClassName='min-w-0 overflow-hidden sm:max-w-2xl'
      descriptionClassName='sr-only'
      contentHeight='min(72dvh, 720px)'
      bodyClassName='pr-2 sm:pr-4'
    >
      <div className='w-full max-w-full min-w-0 space-y-3 overflow-x-hidden py-1'>
        {isLoading && (
          <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
        )}

        {!isLoading && (isError || !payload) && (
          <div className='flex items-start gap-1.5 rounded-md border border-amber-200 bg-amber-50 p-2.5 text-xs text-amber-700 dark:border-amber-900 dark:bg-amber-950/20 dark:text-amber-400'>
            <Info className='mt-0.5 size-3.5 shrink-0' aria-hidden='true' />
            <span>
              {t(
                'Request debug data was not captured or has expired. Enable Request Debugging in system settings to capture new requests.'
              )}
            </span>
          </div>
        )}

        {!isLoading && payload && (
          <>
            <div className='min-w-0 space-y-1'>
              <div className='grid grid-cols-[5rem_minmax(0,1fr)] gap-2 text-xs'>
                <span className='text-muted-foreground'>{t('Method')}</span>
                <span className='font-mono break-all'>{payload.method}</span>
              </div>
              <div className='grid grid-cols-[5rem_minmax(0,1fr)] gap-2 text-xs'>
                <span className='text-muted-foreground'>{t('Path')}</span>
                <span className='font-mono break-all'>{payload.path}</span>
              </div>
              {payload.model && (
                <div className='grid grid-cols-[5rem_minmax(0,1fr)] gap-2 text-xs'>
                  <span className='text-muted-foreground'>{t('Model')}</span>
                  <span className='font-mono break-all'>{payload.model}</span>
                </div>
              )}
            </div>

            {headerEntries.length > 0 && (
              <div className='space-y-1.5'>
                <Label className='text-xs font-semibold'>
                  {t('Request Headers')}
                </Label>
                <div className='bg-muted/30 min-w-0 space-y-1 overflow-hidden rounded-md border p-2.5'>
                  {headerEntries.map(([key, value]) => (
                    <div
                      key={key}
                      className='grid grid-cols-[minmax(0,10rem)_minmax(0,1fr)] gap-2 text-[11px]'
                    >
                      <span className='text-muted-foreground font-mono break-all'>
                        {key}
                      </span>
                      <span className='font-mono break-all'>{value}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            <div className='space-y-1.5'>
              <Label className='flex items-center gap-2 text-xs font-semibold'>
                {t('Request Body')}
                {payload.body_truncated && (
                  <span className='text-amber-600 dark:text-amber-400'>
                    ({t('truncated')})
                  </span>
                )}
              </Label>
              <div className='bg-muted/30 relative min-w-0 overflow-hidden rounded-md border p-2.5'>
                {payload.body ? (
                  <>
                    <CopyButton text={prettyBody} />
                    <pre className='max-h-80 min-w-0 overflow-auto pr-6 font-mono text-[11px] leading-relaxed break-all whitespace-pre-wrap'>
                      {prettyBody}
                    </pre>
                  </>
                ) : (
                  <span className={cn('text-muted-foreground/60 text-xs')}>
                    {t('No request body')}
                  </span>
                )}
              </div>
            </div>
          </>
        )}
      </div>
    </Dialog>
  )
}
