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
import { useState } from 'react'

import { Download, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import { fetchBillingExport } from './api'

// datetime-local (step=1) yields "YYYY-MM-DDTHH:mm:ss" interpreted in the
// browser's local timezone. Convert to Unix seconds; return null if unparseable.
function localDateTimeToUnixSeconds(value: string): number | null {
  if (!value) return null
  const ms = new Date(value).getTime()
  if (Number.isNaN(ms)) return null
  return Math.floor(ms / 1000)
}

// Default range: today 00:00:00 local -> now, formatted for datetime-local.
function pad(n: number): string {
  return String(n).padStart(2, '0')
}

function toLocalInputValue(date: Date): string {
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  )
}

function BillingExportContent() {
  const { t } = useTranslation()

  const now = new Date()
  const dayStart = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate(),
    0,
    0,
    0
  )

  const [startTime, setStartTime] = useState(toLocalInputValue(dayStart))
  const [endTime, setEndTime] = useState(toLocalInputValue(now))
  const [usernamesRaw, setUsernamesRaw] = useState('')
  const [isExporting, setIsExporting] = useState(false)

  const handleExport = async () => {
    const startTimestamp = localDateTimeToUnixSeconds(startTime)
    const endTimestamp = localDateTimeToUnixSeconds(endTime)
    const usernames = usernamesRaw
      .split(/[\n,]/)
      .map((s) => s.trim())
      .filter(Boolean)

    if (startTimestamp === null || endTimestamp === null) {
      toast.error(t('Please select a valid time range'))
      return
    }
    if (endTimestamp < startTimestamp) {
      toast.error(t('End time must be later than start time'))
      return
    }
    if (usernames.length === 0) {
      toast.error(t('Please enter at least one customer username'))
      return
    }

    setIsExporting(true)
    try {
      const blob = await fetchBillingExport({
        startTimestamp,
        endTimestamp,
        usernames,
      })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `billing_export_${startTimestamp}_${endTimestamp}.csv`
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(url)
      toast.success(t('Billing export started downloading'))
    } catch {
      toast.error(t('Failed to export billing records'))
    } finally {
      setIsExporting(false)
    }
  }

  return (
    <div className='mx-auto w-full max-w-2xl'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Export Conditions')}</CardTitle>
          <CardDescription>
            {t(
              'Both successful and failed requests are included. The file is exported as CSV.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className='flex flex-col gap-6'>
          <div className='grid gap-4 sm:grid-cols-2'>
            <div className='flex flex-col gap-2'>
              <Label htmlFor='billing-export-start'>{t('Start Time')}</Label>
              <Input
                id='billing-export-start'
                type='datetime-local'
                step={1}
                value={startTime}
                onChange={(e) => setStartTime(e.target.value)}
              />
            </div>
            <div className='flex flex-col gap-2'>
              <Label htmlFor='billing-export-end'>{t('End Time')}</Label>
              <Input
                id='billing-export-end'
                type='datetime-local'
                step={1}
                value={endTime}
                onChange={(e) => setEndTime(e.target.value)}
              />
            </div>
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='billing-export-usernames'>
              {t('Customer Usernames')}
            </Label>
            <Textarea
              id='billing-export-usernames'
              rows={4}
              placeholder={t(
                'Enter usernames, separated by comma or newline'
              )}
              value={usernamesRaw}
              onChange={(e) => setUsernamesRaw(e.target.value)}
            />
          </div>

          <div>
            <Button onClick={handleExport} disabled={isExporting}>
              {isExporting ? (
                <Loader2 className='h-4 w-4 animate-spin' />
              ) : (
                <Download className='h-4 w-4' />
              )}
              {isExporting ? t('Exporting...') : t('Export CSV')}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

export function BillingExport() {
  const { t } = useTranslation()
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Billing Export')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <BillingExportContent />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
