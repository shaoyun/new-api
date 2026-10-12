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
import { describe, expect, it } from 'vitest'

import {
  USER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  transformUserToFormDefaults,
  userFormSchema,
} from '../../lib/user-form'
import type { User } from '../../types'

const formData = { ...USER_FORM_DEFAULT_VALUES, username: 'alice' }

describe('user discount form logic', () => {
  it('sends the entered discount when editing a user', () => {
    const payload = transformFormDataToPayload(
      { ...formData, discount: 0.8 },
      1
    )

    expect(payload.discount).toBe(0.8)
  })

  it('sends discount 0 when editing with an empty discount so it can be cleared', () => {
    const payload = transformFormDataToPayload(
      { ...formData, discount: undefined },
      1
    )

    expect(payload.discount).toBe(0)
  })

  it('omits discount when creating a user', () => {
    const payload = transformFormDataToPayload({ ...formData, discount: 0.8 })

    expect(payload).not.toHaveProperty('discount')
  })

  it('rejects a discount above 1', () => {
    const result = userFormSchema.safeParse({ ...formData, discount: 1.2 })

    expect(result.success).toBe(false)
  })

  it('rejects a negative discount', () => {
    const result = userFormSchema.safeParse({ ...formData, discount: -0.1 })

    expect(result.success).toBe(false)
  })

  it('accepts an empty discount', () => {
    const result = userFormSchema.safeParse({
      ...formData,
      discount: undefined,
    })

    expect(result.success).toBe(true)
  })

  it('fills the form discount from the loaded user', () => {
    const user: User = {
      id: 1,
      username: 'alice',
      display_name: 'Alice',
      role: 1,
      status: 1,
      quota: 0,
      used_quota: 0,
      request_count: 0,
      group: 'default',
      discount: 0.75,
    }

    expect(transformUserToFormDefaults(user).discount).toBe(0.75)
  })
})
