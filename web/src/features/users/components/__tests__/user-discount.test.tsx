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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import {
  USER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
} from '../../lib/user-form'
import type { User } from '../../types'
import { useUsersColumns } from '../users-columns'
import { UsersMutateDrawer } from '../users-mutate-drawer'
import { UsersProvider } from '../users-provider'

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
}

const clients: QueryClient[] = []

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

describe('user edit payload without the flat discount', () => {
  it('omits discount when updating a user', () => {
    const payload = transformFormDataToPayload(
      { ...USER_FORM_DEFAULT_VALUES, username: 'alice' },
      1
    )

    expect(payload).not.toHaveProperty('discount')
  })
})

describe('user edit drawer without the flat discount', () => {
  it('renders no discount input once the user detail has loaded', async () => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 9, username: 'operator', role: 100 })
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/group/') return { data: { success: true, data: [] } }
      if (url === `/api/user/${listRow.id}`) {
        return { data: { success: true, data: listRow } }
      }
      return { data: { success: true, data: { resources: [], roles: [] } } }
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    clients.push(client)
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

    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()
    )
    expect(screen.queryByLabelText(/discount/i)).not.toBeInTheDocument()
  })
})

function GroupColumnTable(props: { user: User }) {
  const columns = useUsersColumns().filter(
    (column) => 'accessorKey' in column && column.accessorKey === 'group'
  )
  const table = useReactTable({
    columns,
    data: [props.user],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <table>
      <tbody>
        {table.getRowModel().rows.map((row) => (
          <tr key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <td key={cell.id}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function renderGroupCell(user: User) {
  render(
    <TooltipProvider>
      <GroupColumnTable user={user} />
    </TooltipProvider>
  )
}

describe('users list discount rule badge', () => {
  it('shows the rule count beside the group when the user has rules', () => {
    renderGroupCell({ ...listRow, discount_rule_count: 3 })

    expect(screen.getByText('3 discount rules')).toBeInTheDocument()
  })

  it.each([
    ['zero rules', { discount_rule_count: 0 }],
    ['no rule count', {}],
  ])('hides the rule badge for a user with %s', (_name, extra) => {
    renderGroupCell({ ...listRow, ...extra })

    expect(screen.queryByText(/discount rules/)).not.toBeInTheDocument()
  })
})
