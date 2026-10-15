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
import { useMemo } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { DiscountRuleFormValues } from '../../lib/discount-rules'
import type { UserDiscountGroupOption, UserDiscountRule } from '../../types'

interface UserDiscountRuleFormProps {
  form: UseFormReturn<DiscountRuleFormValues>
  groups: UserDiscountGroupOption[]
  rules: UserDiscountRule[]
  saving: boolean
  onSubmit: (values: DiscountRuleFormValues) => void
}

export function UserDiscountRuleForm(props: UserDiscountRuleFormProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const group = props.form.watch('group')
  const models = props.form.watch('models')

  const groupOptions = useMemo(
    () =>
      props.groups.map((option) => ({
        value: option.group,
        label: option.group,
        description: `${option.desc ? `${option.desc} · ` : ''}${formatNumber(option.ratio, locale)}x`,
      })),
    [props.groups, locale]
  )

  const modelOptions = useMemo(
    () =>
      (props.groups.find((option) => option.group === group)?.models ?? []).map(
        (model) => ({ value: model, label: model })
      ),
    [props.groups, group]
  )

  // Rules the save would replace: same group, model among the selection.
  const overwriteCount = useMemo(() => {
    const selected = new Set(models)
    return props.rules.filter(
      (rule) => rule.group === group && selected.has(rule.model)
    ).length
  }, [props.rules, group, models])

  return (
    <Form {...props.form}>
      <form
        onSubmit={props.form.handleSubmit(props.onSubmit)}
        className='flex flex-col gap-4'
      >
        <div className='grid gap-2'>
          <Label htmlFor='discount-rule-group'>{t('Group')}</Label>
          <Combobox
            id='discount-rule-group'
            aria-label={t('Group')}
            options={groupOptions}
            value={group}
            onValueChange={(value) => {
              if (value === null || value === group) return
              props.form.setValue('group', value, { shouldValidate: true })
              // Model names are only meaningful within their group.
              props.form.setValue('models', [], { shouldValidate: true })
            }}
            placeholder={t('Select a group')}
            className='w-full'
          />
        </div>

        <div className='grid gap-2'>
          <Label htmlFor='discount-rule-models'>{t('Models')}</Label>
          <MultiSelect
            id='discount-rule-models'
            options={modelOptions}
            selected={models}
            onChange={(values) =>
              props.form.setValue('models', values, { shouldValidate: true })
            }
            placeholder={t('Select models')}
            emptyText={
              group ? t('No matching items') : t('Select a group first')
            }
            disabled={!group}
            maxVisibleChips={12}
            showBulkActions
            showClear
          />
        </div>

        <FormField
          control={props.form.control}
          name='discount'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Discount')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  inputMode='decimal'
                  min={0}
                  max={1}
                  step={0.01}
                  name={field.name}
                  ref={field.ref}
                  onBlur={field.onBlur}
                  value={field.value ?? ''}
                  onChange={(event) =>
                    field.onChange(
                      event.target.value === ''
                        ? undefined
                        : Number(event.target.value)
                    )
                  }
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Multiplier applied on top of the group ratio, e.g. 0.8 means 20% off. Must be greater than 0 and less than 1.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        {overwriteCount > 0 && (
          <p role='status' className='text-warning text-sm'>
            {t('Will overwrite {{count}} existing rules', {
              count: overwriteCount,
            })}
          </p>
        )}

        <div className='flex justify-end'>
          <Button
            type='submit'
            disabled={props.saving || !props.form.formState.isValid}
          >
            {props.saving ? t('Saving...') : t('Save rules')}
          </Button>
        </div>
      </form>
    </Form>
  )
}
