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
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'

interface ModelDiscountBadgeProps {
  discount: number
  className?: string
}

/** Marks a price the viewer gets through a per-user group-model discount. */
export function ModelDiscountBadge(props: ModelDiscountBadgeProps) {
  const { t } = useTranslation()
  // Discounts can carry more than two decimals, so avoid rounding them away.
  const factor = Number(props.discount.toFixed(4))

  return (
    <StatusBadge
      label={`${t('Discount')} ${factor}x`}
      variant='success'
      copyable={false}
      size='sm'
      className={props.className}
    />
  )
}
