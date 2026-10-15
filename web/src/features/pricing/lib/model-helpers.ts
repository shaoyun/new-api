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
import { EXCLUDED_GROUPS, FILTER_ALL, QUOTA_TYPE_VALUES } from '../constants'
import type { PricingModel } from '../types'

// ----------------------------------------------------------------------------
// Model Helper Utilities
// ----------------------------------------------------------------------------

/**
 * Get available groups for a model
 */
export function getAvailableGroups(
  model: PricingModel,
  usableGroup: Record<string, { desc: string; ratio: number }>
): string[] {
  const modelEnableGroups = Array.isArray(model.enable_groups)
    ? model.enable_groups
    : []

  return Object.keys(usableGroup)
    .filter((g) => !EXCLUDED_GROUPS.includes(g))
    .filter((g) => modelEnableGroups.includes(g))
}

/**
 * Read a configured group ratio while preserving valid zero ratios.
 */
export function getConfiguredGroupRatio(
  groupRatio: Record<string, number>,
  group: string
): number {
  const ratio = groupRatio[group]
  return typeof ratio === 'number' && Number.isFinite(ratio) ? ratio : 1
}

/**
 * Resolve the group whose ratio sets the model square summary price.
 *
 * When no specific group is selected, the model square shows the best price
 * available to the viewer (the enabled group with the lowest effective ratio).
 * When a group filter is active, it shows that group's price instead. On a
 * price tie the group the viewer has a discount for wins. Returns undefined
 * when no group has a usable ratio.
 */
export function getDisplayGroup(
  model: PricingModel,
  selectedGroup?: string
): string | undefined {
  const modelEnableGroups = Array.isArray(model.enable_groups)
    ? model.enable_groups
    : []

  if (
    selectedGroup &&
    selectedGroup !== FILTER_ALL &&
    modelEnableGroups.includes(selectedGroup)
  ) {
    return selectedGroup
  }

  const groupRatio = model.group_ratio || {}
  let displayGroup: string | undefined
  let minRatio = Number.POSITIVE_INFINITY

  for (const group of modelEnableGroups) {
    const ratio = groupRatio[group]
    if (typeof ratio !== 'number' || !Number.isFinite(ratio)) continue
    const isCheaper = ratio < minRatio
    const winsTie =
      ratio === minRatio &&
      model.user_discounts?.[group] !== undefined &&
      displayGroup !== undefined &&
      model.user_discounts[displayGroup] === undefined
    if (isCheaper || winsTie) {
      minRatio = ratio
      displayGroup = group
    }
  }

  return displayGroup
}

/**
 * Resolve the group ratio used by model square summary prices.
 */
export function getDisplayGroupRatio(
  model: PricingModel,
  selectedGroup?: string
): number {
  const displayGroup = getDisplayGroup(model, selectedGroup)
  if (displayGroup === undefined) return 1
  return getConfiguredGroupRatio(model.group_ratio || {}, displayGroup)
}

/**
 * Resolve the viewer's discount for the group that sets the displayed price,
 * so the badge always describes the price shown. Undefined when that group
 * has no discount rule.
 */
export function getDisplayGroupDiscount(
  model: PricingModel,
  selectedGroup?: string
): number | undefined {
  const displayGroup = getDisplayGroup(model, selectedGroup)
  if (displayGroup === undefined) return undefined
  return model.user_discounts?.[displayGroup]
}

/**
 * Replace model placeholder in endpoint path
 */
export function replaceModelInPath(path: string, modelName: string): string {
  return path.replaceAll('{model}', modelName)
}

/**
 * Check if model is token-based pricing
 */
export function isTokenBasedModel(model: PricingModel): boolean {
  return model.quota_type === QUOTA_TYPE_VALUES.TOKEN
}
