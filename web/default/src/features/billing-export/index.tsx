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
import { Download, Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { MultiSelect, type Option } from '@/components/multi-select'
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

import { fetchBillingExport, fetchCustomerOptions } from './api'

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
  const [customerOptions, setCustomerOptions] = useState<Option[]>([])
  const [selectedUsernames, setSelectedUsernames] = useState<string[]>([])
  const [isLoadingCustomers, setIsLoadingCustomers] = useState(true)
  const [isExporting, setIsExporting] = useState(false)

  useEffect(() => {
    let cancelled = false
    const loadCustomers = async () => {
      setIsLoadingCustomers(true)
      try {
        const { options, truncated } = await fetchCustomerOptions()
        if (cancelled) return
        setCustomerOptions(options)
        if (truncated) {
          toast.warning(
            t('Customer list is large; some customers may not be listed.')
          )
        }
      } catch {
        if (!cancelled) toast.error(t('Failed to load customers'))
      } finally {
        if (!cancelled) setIsLoadingCustomers(false)
      }
    }
    void loadCustomers()
    return () => {
      cancelled = true
    }
  }, [t])

  const handleExport = async () => {
    const startTimestamp = localDateTimeToUnixSeconds(startTime)
    const endTimestamp = localDateTimeToUnixSeconds(endTime)
    const usernames = selectedUsernames

    if (startTimestamp === null || endTimestamp === null) {
      toast.error(t('Please select a valid time range'))
      return
    }
    if (endTimestamp < startTimestamp) {
      toast.error(t('End time must be later than start time'))
      return
    }
    if (usernames.length === 0) {
      toast.error(t('Please select at least one customer'))
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
              'Records are summarized by day, user, token and model. Both successful and failed requests are included. The file is exported as CSV.'
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
            <Label htmlFor='billing-export-usernames'>{t('Customers')}</Label>
            <MultiSelect
              id='billing-export-usernames'
              options={customerOptions}
              selected={selectedUsernames}
              onChange={setSelectedUsernames}
              disabled={isLoadingCustomers}
              maxVisibleChips={12}
              showBulkActions
              showClear
              placeholder={
                isLoadingCustomers
                  ? t('Loading customers...')
                  : t('Select one or more customers')
              }
              emptyText={t('No matching customers')}
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
