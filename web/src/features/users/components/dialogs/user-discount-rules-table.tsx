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
import { Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable, StaticRowActions } from '@/components/data-table'
import { GroupBadge } from '@/components/group-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestamp } from '@/lib/format'

import type { UserDiscountRule } from '../../types'

interface UserDiscountRulesTableProps {
  rules: UserDiscountRule[]
  loading: boolean
  selectedIds: number[]
  onSelectedIdsChange: (ids: number[]) => void
  onEdit: (rule: UserDiscountRule) => void
  onDelete: (ids: number[]) => void
}

export function UserDiscountRulesTable(props: UserDiscountRulesTableProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [filter, setFilter] = useState('')

  const visibleRules = useMemo(() => {
    const query = filter.trim().toLowerCase()
    if (!query) return props.rules
    return props.rules.filter(
      (rule) =>
        rule.group.toLowerCase().includes(query) ||
        rule.model.toLowerCase().includes(query)
    )
  }, [props.rules, filter])

  const selected = new Set(props.selectedIds)
  const visibleSelectedCount = visibleRules.filter((rule) =>
    selected.has(rule.id)
  ).length
  const allVisibleSelected =
    visibleRules.length > 0 && visibleSelectedCount === visibleRules.length

  const toggleVisible = (checked: boolean) => {
    const visibleIds = new Set(visibleRules.map((rule) => rule.id))
    const kept = props.selectedIds.filter((id) => !visibleIds.has(id))
    props.onSelectedIdsChange(checked ? [...kept, ...visibleIds] : kept)
  }

  const toggleRule = (id: number, checked: boolean) => {
    props.onSelectedIdsChange(
      checked
        ? [...props.selectedIds, id]
        : props.selectedIds.filter((selectedId) => selectedId !== id)
    )
  }

  let emptyContent = t('No discount rules')
  if (props.loading) emptyContent = t('Loading...')
  else if (props.rules.length > 0) emptyContent = t('No matching rules')

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
        <Input
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          placeholder={t('Filter by group or model')}
          aria-label={t('Filter by group or model')}
          className='sm:max-w-xs'
        />
        <Button
          type='button'
          variant='outline'
          className='sm:ml-auto'
          disabled={props.selectedIds.length === 0}
          onClick={() => props.onDelete(props.selectedIds)}
        >
          <Trash2 aria-hidden='true' />
          {t('Delete selected')}
        </Button>
      </div>

      <StaticDataTable
        data={visibleRules}
        getRowKey={(rule) => rule.id}
        emptyClassName='text-muted-foreground py-8'
        emptyContent={emptyContent}
        columns={[
          {
            id: 'select',
            className: 'w-8',
            header: (
              <Checkbox
                checked={allVisibleSelected}
                indeterminate={visibleSelectedCount > 0 && !allVisibleSelected}
                disabled={visibleRules.length === 0}
                onCheckedChange={(checked) => toggleVisible(!!checked)}
                aria-label={t('Select all rules')}
              />
            ),
            cell: (rule) => (
              <Checkbox
                checked={selected.has(rule.id)}
                onCheckedChange={(checked) => toggleRule(rule.id, !!checked)}
                aria-label={t('Select {{group}} / {{model}}', {
                  group: rule.group,
                  model: rule.model,
                })}
              />
            ),
          },
          {
            id: 'group',
            header: t('Group'),
            cell: (rule) => (
              <GroupBadge group={rule.group} className='font-normal' />
            ),
          },
          {
            id: 'model',
            header: t('Model'),
            cell: (rule) => <span className='font-mono'>{rule.model}</span>,
          },
          {
            id: 'discount',
            header: t('Discount'),
            cell: (rule) => `${formatNumber(rule.discount, locale)}x`,
          },
          {
            id: 'updated_at',
            header: t('Updated At'),
            cellClassName: 'text-muted-foreground',
            cell: (rule) => formatTimestamp(rule.updated_at),
          },
          {
            id: 'actions',
            header: <span className='sr-only'>{t('Actions')}</span>,
            className: 'text-right',
            cell: (rule) => (
              <StaticRowActions
                editLabel={t('Edit rule')}
                deleteLabel={t('Delete rule')}
                menuLabel={t('Open menu')}
                onEdit={() => props.onEdit(rule)}
                onDelete={() => props.onDelete([rule.id])}
              />
            ),
          },
        ]}
      />
    </div>
  )
}
