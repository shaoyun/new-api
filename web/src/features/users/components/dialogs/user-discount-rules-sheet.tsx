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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerContentClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { handleServerError } from '@/lib/handle-server-error'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import {
  deleteUserDiscountRules,
  getUserDiscountOptions,
  getUserDiscountRules,
  upsertUserDiscountRules,
} from '../../api'
import {
  DISCOUNT_RULE_FORM_DEFAULTS,
  type DiscountRuleFormValues,
  discountRuleFormSchema,
} from '../../lib/discount-rules'
import type { UserDiscountRule } from '../../types'
import { UserDiscountRuleForm } from './user-discount-rule-form'
import { UserDiscountRulesTable } from './user-discount-rules-table'

interface UserDiscountRulesSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username?: string }
}

export function UserDiscountRulesSheet(props: UserDiscountRulesSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const userId = props.user.id
  const rulesQueryKey = ['user-discount-rules', userId]
  const addSectionRef = useRef<HTMLDivElement>(null)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [deleteIds, setDeleteIds] = useState<number[] | null>(null)

  const form = useForm<DiscountRuleFormValues>({
    resolver: zodResolver(discountRuleFormSchema),
    defaultValues: DISCOUNT_RULE_FORM_DEFAULTS,
    mode: 'onChange',
  })

  const optionsQuery = useQuery({
    queryKey: ['user-discount-options', userId],
    queryFn: async () =>
      requireServerSuccess(await getUserDiscountOptions(userId)).data,
    enabled: props.open,
  })

  const rulesQuery = useQuery({
    queryKey: rulesQueryKey,
    queryFn: async () =>
      requireServerSuccess(await getUserDiscountRules(userId)).data ?? [],
    enabled: props.open,
  })

  const rules = useMemo(() => rulesQuery.data ?? [], [rulesQuery.data])

  // Drop selections whose rules no longer exist after a refetch; keep table order.
  const liveSelectedIds = useMemo(() => {
    const selected = new Set(selectedIds)
    return rules.filter((rule) => selected.has(rule.id)).map((rule) => rule.id)
  }, [rules, selectedIds])

  useEffect(() => {
    if (props.open) return
    form.reset(DISCOUNT_RULE_FORM_DEFAULTS)
    setSelectedIds([])
    setDeleteIds(null)
  }, [props.open, form])

  const refreshAfterChange = async () => {
    // The users list carries discount_rule_count.
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: rulesQueryKey }),
      queryClient.invalidateQueries({ queryKey: ['users'] }),
    ])
  }

  const saveMutation = useMutation({
    mutationFn: async (values: DiscountRuleFormValues) => {
      const response = await upsertUserDiscountRules(userId, {
        group: values.group,
        models: values.models,
        discount: values.discount as number,
      })
      if (!response.success) {
        throw createServerError(response, t('Failed to save discount rules'))
      }
      return response.data?.updated ?? values.models.length
    },
    onSuccess: async (updated) => {
      toast.success(t('Saved {{count}} discount rules', { count: updated }))
      form.reset(DISCOUNT_RULE_FORM_DEFAULTS)
      await refreshAfterChange()
    },
    onError: (error: Error) => handleServerError(error),
  })

  const deleteMutation = useMutation({
    mutationFn: async (ids: number[]) => {
      const response = await deleteUserDiscountRules(userId, ids)
      if (!response.success) {
        throw createServerError(response, t('Failed to delete discount rules'))
      }
      return response.data?.deleted ?? ids.length
    },
    onSuccess: async (deleted) => {
      toast.success(t('Deleted {{count}} discount rules', { count: deleted }))
      setSelectedIds([])
      setDeleteIds(null)
      await refreshAfterChange()
    },
    onError: (error: Error) => handleServerError(error),
  })

  const handleEdit = (rule: UserDiscountRule) => {
    form.reset(
      { group: rule.group, models: [rule.model], discount: rule.discount },
      { keepDefaultValues: true }
    )
    void form.trigger()
    addSectionRef.current?.scrollIntoView({ block: 'start' })
    // Wait a frame so the clicked row button does not keep focus.
    window.requestAnimationFrame(() => form.setFocus('discount'))
  }

  return (
    <>
      <Sheet open={props.open} onOpenChange={props.onOpenChange}>
        <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>{t('Discount rules')}</SheetTitle>
            <SheetDescription>
              {props.user.username || '-'} (ID: {userId})
            </SheetDescription>
          </SheetHeader>

          <div className={sideDrawerFormClassName()}>
            <SideDrawerSection>
              <div ref={addSectionRef} className='flex flex-col gap-4'>
                <SideDrawerSectionHeader
                  title={t('Add or update rules')}
                  description={t(
                    'Pick a group and one or more models; saving overwrites existing rules for the same models.'
                  )}
                />
                <UserDiscountRuleForm
                  form={form}
                  groups={optionsQuery.data?.groups ?? []}
                  rules={rules}
                  saving={saveMutation.isPending}
                  onSubmit={(values) => saveMutation.mutate(values)}
                />
              </div>
            </SideDrawerSection>

            <SideDrawerSection>
              <SideDrawerSectionHeader
                title={t('Existing rules')}
                description={t('{{count}} discount rules', {
                  count: rules.length,
                })}
              />
              <UserDiscountRulesTable
                rules={rules}
                loading={rulesQuery.isLoading}
                selectedIds={liveSelectedIds}
                onSelectedIdsChange={setSelectedIds}
                onEdit={handleEdit}
                onDelete={setDeleteIds}
              />
            </SideDrawerSection>
          </div>
        </SheetContent>
      </Sheet>

      <ConfirmDialog
        open={deleteIds !== null}
        onOpenChange={(open) => !open && setDeleteIds(null)}
        title={t('Delete discount rules')}
        desc={t('Delete {{count}} discount rules? This cannot be undone.', {
          count: deleteIds?.length ?? 0,
        })}
        confirmText={t('Delete')}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => deleteIds && deleteMutation.mutate(deleteIds)}
      />
    </>
  )
}
