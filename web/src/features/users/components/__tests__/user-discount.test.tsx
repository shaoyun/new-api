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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import {
  USER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  transformUserToFormDefaults,
  userFormSchema,
} from '../../lib/user-form'
import type { User } from '../../types'
import { UsersMutateDrawer } from '../users-mutate-drawer'
import { UsersProvider } from '../users-provider'

const formData = { ...USER_FORM_DEFAULT_VALUES, username: 'alice' }

const listRow: User = {
  id: 1,
  username: 'alice',
  display_name: 'Alice',
  role: 1,
  status: 1,
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
  discount: 0.8,
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

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

describe('user discount seeding from the list row', () => {
  it('seeds the form discount from a list row with a discount', () => {
    expect(transformUserToFormDefaults(listRow).discount).toBe(0.8)
  })

  it('seeds an empty form discount from a list row without a discount', () => {
    expect(
      transformUserToFormDefaults({ ...listRow, discount: 0 }).discount
    ).toBeUndefined()
  })
})

function renderEditDrawer(loadUser: () => Promise<unknown>) {
  useAuthStore
    .getState()
    .auth.setUser({ id: 9, username: 'operator', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/group/') return { data: { success: true, data: [] } }
    if (url === `/api/user/${listRow.id}`) return loadUser()
    return { data: { success: true, data: { resources: [], roles: [] } } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <UsersProvider>
        <UsersMutateDrawer
          open
          onOpenChange={() => undefined}
          currentRow={listRow}
        />
      </UsersProvider>
    </QueryClientProvider>
  )
}

describe('user discount edit drawer', () => {
  it('shows the stored discount and keeps save disabled when the detail load fails', async () => {
    renderEditDrawer(() => Promise.reject(new Error('network down')))

    const input = await screen.findByLabelText('User discount')
    await waitFor(() => expect(input).toHaveValue(0.8))
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
  })

  it('enables save once the user detail has loaded', async () => {
    renderEditDrawer(async () => ({
      data: { success: true, data: { ...listRow, discount: 0.7 } },
    }))

    const input = await screen.findByLabelText('User discount')
    await waitFor(() => expect(input).toHaveValue(0.7))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()
    )
  })
})
