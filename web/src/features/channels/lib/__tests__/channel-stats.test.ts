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
import { describe, expect, test } from 'vitest'

import type { Channel } from '../../types'
import {
  aggregateChannelsByTag,
  getCacheHitRate,
  getSuccessRate,
  getSuccessRateVariant,
  type TagRow,
} from '../channel-utils'

function channel(id: number, tag: string, stats: Channel['stats']): Channel {
  return {
    id,
    tag,
    group: '',
    used_quota: 0,
    response_time: 0,
    priority: 0,
    weight: 0,
    stats,
  } as Channel
}

describe('channel stats', () => {
  test('rates are null without traffic', () => {
    expect(getSuccessRate(undefined)).toBeNull()
    expect(getSuccessRate({ success: 0, fail: 0, input_tokens: 0, cache_tokens: 0 })).toBeNull()
    expect(getCacheHitRate({ success: 1, fail: 0, input_tokens: 0, cache_tokens: 0 })).toBeNull()
  })

  test('computes rates and caps cache hit at 100%', () => {
    const stats = { success: 99, fail: 1, input_tokens: 100, cache_tokens: 150 }
    expect(getSuccessRate(stats)).toBeCloseTo(0.99)
    expect(getCacheHitRate(stats)).toBe(1)
  })

  test('success rate color thresholds', () => {
    expect(getSuccessRateVariant(0.99)).toBe('success')
    expect(getSuccessRateVariant(0.95)).toBe('warning')
    expect(getSuccessRateVariant(0.9499)).toBe('danger')
  })

  test('tag rows sum raw counts instead of averaging rates', () => {
    const rows = aggregateChannelsByTag([
      channel(1, 'a', { success: 99, fail: 1, input_tokens: 100, cache_tokens: 50 }),
      channel(2, 'a', { success: 0, fail: 100, input_tokens: 900, cache_tokens: 0 }),
      channel(3, 'a', null),
    ])
    const tag = rows[0] as TagRow
    expect(tag.stats).toEqual({ success: 99, fail: 101, input_tokens: 1000, cache_tokens: 50 })
    expect(getSuccessRate(tag.stats)).toBeCloseTo(99 / 200)
  })

  test('tag row without any child traffic has no stats', () => {
    const rows = aggregateChannelsByTag([channel(1, 'b', null)])
    expect((rows[0] as TagRow).stats).toBeNull()
  })
})
