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
import { useEffect, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsControlGroup,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { validateJsonString } from './utils'

// Channel keys mirror setting/model_setting/claude_beta_filter.go.
const CHANNEL_KEYS = ['aws', 'anthropic'] as const

type ChannelKey = (typeof CHANNEL_KEYS)[number]

type BetaFilterRule = {
  enabled: boolean
  whitelist: string[]
  model_override: Record<string, string[]>
}

// validateModelOverride ensures the text is not just valid JSON but the exact
// shape the backend expects: an object mapping model name -> array of strings.
// A well-formed-but-wrong shape (e.g. { "m": "flag" }) would otherwise be saved
// and then silently fail to unmarshal into map[string][]string on the Go side.
function validateModelOverride(value: string, ctx: z.RefinementCtx) {
  const jsonResult = validateJsonString(value)
  if (!jsonResult.valid) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: jsonResult.message || 'Invalid JSON',
    })
    return
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(value || '{}')
  } catch {
    return
  }
  if (
    parsed === null ||
    typeof parsed !== 'object' ||
    Array.isArray(parsed)
  ) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: 'Must be a JSON object mapping model name to an array of flags',
    })
    return
  }
  for (const [model, flags] of Object.entries(parsed as Record<string, unknown>)) {
    if (
      !Array.isArray(flags) ||
      flags.some((flag) => typeof flag !== 'string')
    ) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: `"${model}" must map to an array of strings`,
      })
      return
    }
  }
}

const schema = z.object({
  aws_enabled: z.boolean(),
  aws_whitelist: z.string(),
  aws_model_override: z.string().superRefine(validateModelOverride),
  anthropic_enabled: z.boolean(),
  anthropic_whitelist: z.string(),
  anthropic_model_override: z.string().superRefine(validateModelOverride),
})

type BetaFilterFormValues = z.infer<typeof schema>

type BetaFilterSettingsCardProps = {
  defaultValues: { rules: string }
}

function parseRules(raw: string): Record<string, BetaFilterRule> {
  try {
    const parsed = JSON.parse(raw || '{}')
    if (parsed && typeof parsed === 'object' && parsed.rules) {
      return parsed.rules as Record<string, BetaFilterRule>
    }
    if (parsed && typeof parsed === 'object') {
      return parsed as Record<string, BetaFilterRule>
    }
  } catch {
    /* fall through to empty */
  }
  return {}
}

function listToText(list: string[] | undefined): string {
  return (list ?? []).join('\n')
}

function textToList(text: string): string[] {
  return text
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

function buildFormValues(raw: string): BetaFilterFormValues {
  const rules = parseRules(raw)
  const read = (key: ChannelKey) => {
    const rule = rules[key] ?? { enabled: false, whitelist: [], model_override: {} }
    return {
      enabled: Boolean(rule.enabled),
      whitelist: listToText(rule.whitelist),
      override: JSON.stringify(rule.model_override ?? {}, null, 2),
    }
  }
  const aws = read('aws')
  const anthropic = read('anthropic')
  return {
    aws_enabled: aws.enabled,
    aws_whitelist: aws.whitelist,
    aws_model_override: aws.override,
    anthropic_enabled: anthropic.enabled,
    anthropic_whitelist: anthropic.whitelist,
    anthropic_model_override: anthropic.override,
  }
}

function serializeRules(values: BetaFilterFormValues): string {
  const build = (
    enabled: boolean,
    whitelist: string,
    override: string
  ): BetaFilterRule => {
    let modelOverride: Record<string, string[]> = {}
    try {
      const parsed = JSON.parse(override || '{}')
      if (parsed && typeof parsed === 'object') {
        modelOverride = parsed as Record<string, string[]>
      }
    } catch {
      modelOverride = {}
    }
    return {
      enabled,
      whitelist: textToList(whitelist),
      model_override: modelOverride,
    }
  }
  const rules: Record<string, BetaFilterRule> = {
    aws: build(values.aws_enabled, values.aws_whitelist, values.aws_model_override),
    anthropic: build(
      values.anthropic_enabled,
      values.anthropic_whitelist,
      values.anthropic_model_override
    ),
  }
  // The backend stores/reads the bare rules map under the "claude_beta_filter.rules"
  // option (configToMap marshals the Rules field directly, no outer wrapper).
  // Serialize unwrapped so UpdateConfigFromMap unmarshals into map[string]BetaFilterRule
  // correctly; wrapping in { rules } would collapse to a single bogus "rules" key
  // and silently disable the filter.
  return JSON.stringify(rules)
}

export function BetaFilterSettingsCard({
  defaultValues,
}: BetaFilterSettingsCardProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  // Baseline is derived through the SAME form->serialize path as onSubmit's
  // `next`, so key order and whitespace/dedup normalization match and the
  // "no changes to save" guard is reliable.
  const savedRef = useRef<string>(
    serializeRules(buildFormValues(defaultValues.rules))
  )

  const form = useForm<BetaFilterFormValues>({
    resolver: zodResolver(schema),
    defaultValues: buildFormValues(defaultValues.rules),
  })

  useEffect(() => {
    savedRef.current = serializeRules(buildFormValues(defaultValues.rules))
    form.reset(buildFormValues(defaultValues.rules))
  }, [defaultValues, form])

  const onSubmit = async (values: BetaFilterFormValues) => {
    const next = serializeRules(values)
    if (next === savedRef.current) {
      toast.info(t('No changes to save'))
      return
    }
    await updateOption.mutateAsync({ key: 'claude_beta_filter.rules', value: next })
    savedRef.current = next
  }

  return (
    <SettingsSection title={t('Claude Beta Flag Filter')}>
      <Form {...form}>
        {/* eslint-disable-next-line react-hooks/refs */}
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <p className='text-sm text-muted-foreground'>
            {t(
              'Filter anthropic-beta flags per channel type before forwarding upstream. When enabled, only whitelisted flags are sent; this prevents unsupported flags from reaching an upstream that ultimately relays to AWS Bedrock and returns a 400 error. Anthropic-typed channels that proxy to Bedrock should share the AWS whitelist.'
            )}
          </p>

          {/* AWS Bedrock channel */}
          <SettingsControlGroup>
            <FormField
              control={form.control}
              name='aws_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('AWS Bedrock channel filter')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Enable anthropic-beta filtering for AWS Bedrock (type 33) channels.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='aws_whitelist'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('AWS whitelist')}</FormLabel>
                  <FormControl>
                    <Textarea rows={6} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('One beta flag per line. Flags not listed are dropped.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='aws_model_override'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('AWS per-model override')}</FormLabel>
                  <FormControl>
                    <Textarea rows={4} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('Example')}{' '}
                    {`{ "claude-opus-4-6": ["claude-code-20250219"] }`}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsControlGroup>

          {/* Anthropic-typed channel */}
          <SettingsControlGroup>
            <FormField
              control={form.control}
              name='anthropic_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Anthropic channel filter')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Enable anthropic-beta filtering for Anthropic (type 14) channels. Use the AWS whitelist here when the channel ultimately relays to Bedrock.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='anthropic_whitelist'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Anthropic whitelist')}</FormLabel>
                  <FormControl>
                    <Textarea rows={6} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('One beta flag per line. Flags not listed are dropped.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='anthropic_model_override'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Anthropic per-model override')}</FormLabel>
                  <FormControl>
                    <Textarea rows={4} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('Example')}{' '}
                    {`{ "claude-opus-4-6": ["claude-code-20250219"] }`}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsControlGroup>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
