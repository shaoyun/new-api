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
import { z } from 'zod'

/** i18n key; FormMessage translates string errors. */
export const DISCOUNT_RANGE_MESSAGE =
  'Enter a discount greater than 0 and less than 1'

// The discount stays optional in the form type so the input can start empty;
// the refinement rejects empty, non-finite and out-of-range values, matching
// the backend's exclusive (0, 1) range.
export const discountRuleFormSchema = z.object({
  group: z.string().min(1),
  models: z.array(z.string()).min(1),
  discount: z
    .number()
    .optional()
    .refine(
      (value) =>
        value !== undefined && Number.isFinite(value) && value > 0 && value < 1,
      { message: DISCOUNT_RANGE_MESSAGE }
    ),
})

export type DiscountRuleFormValues = z.infer<typeof discountRuleFormSchema>

export const DISCOUNT_RULE_FORM_DEFAULTS: DiscountRuleFormValues = {
  group: '',
  models: [],
  discount: undefined,
}
