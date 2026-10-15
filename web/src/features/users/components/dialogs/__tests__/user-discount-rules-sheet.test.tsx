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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import {
  afterAll,
  afterEach,
  beforeAll,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'

import type { UserDiscountRule } from '../../../types'
import { UserDiscountRulesSheet } from '../user-discount-rules-sheet'

const USER_ID = 5
const RULES_URL = `/api/user/${USER_ID}/discount_rules`
const OPTIONS_URL = `/api/user/${USER_ID}/discount_options`

const discountOptions = {
  groups: [
    { group: 'default', desc: 'Default', ratio: 1, models: ['gpt-4o-mini'] },
    { group: 'vip', desc: 'VIP', ratio: 0.9, models: ['claude-3', 'gpt-4o'] },
  ],
}

function rule(
  id: number,
  group: string,
  model: string,
  discount: number
): UserDiscountRule {
  return {
    id,
    user_id: USER_ID,
    group,
    model,
    discount,
    created_at: 1_700_000_000,
    updated_at: 1_700_000_000,
  }
}

const existingRules = [
  rule(11, 'vip', 'gpt-4o', 0.9),
  rule(12, 'default', 'gpt-4o-mini', 0.7),
]

const clients: QueryClient[] = []
const originalGetAnimations = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'getAnimations'
)

beforeAll(() => {
  // Base UI waits for exit animations; jsdom has none.
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})

afterAll(() => {
  if (originalGetAnimations) {
    Object.defineProperty(
      HTMLElement.prototype,
      'getAnimations',
      originalGetAnimations
    )
    return
  }
  Reflect.deleteProperty(HTMLElement.prototype, 'getAnimations')
})

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

function renderSheet(options: { rules?: UserDiscountRule[][] } = {}) {
  // Each successive rules fetch returns the next list (last one repeats).
  const ruleLists = options.rules ?? [existingRules]
  let rulesFetches = 0
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === OPTIONS_URL) {
      return { data: { success: true, data: discountOptions } }
    }
    if (url === RULES_URL) {
      const list = ruleLists[Math.min(rulesFetches, ruleLists.length - 1)]
      rulesFetches += 1
      return { data: { success: true, data: list } }
    }
    throw new Error(`unexpected GET ${url}`)
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true, data: { updated: 1 } } })
  const del = vi
    .spyOn(api, 'delete')
    .mockResolvedValue({ data: { success: true, data: { deleted: 2 } } })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <UserDiscountRulesSheet
          open
          onOpenChange={() => undefined}
          user={{ id: USER_ID, username: 'alice' }}
        />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return { get, put, del }
}

async function chooseGroup(
  user: ReturnType<typeof userEvent.setup>,
  group: string
) {
  await user.click(screen.getByRole('combobox', { name: 'Group' }))
  await user.click(
    await screen.findByRole('option', { name: new RegExp(`^${group}`) })
  )
}

async function openModelPicker(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('combobox', { name: 'Select models' }))
}

function discountInput() {
  return screen.getByRole('spinbutton', { name: 'Discount' })
}

function saveButton() {
  return screen.getByRole('button', { name: 'Save rules' })
}

function ruleRow(group: string, model: string) {
  const checkbox = screen.getByRole('checkbox', {
    name: `Select ${group} / ${model}`,
  })
  const row = checkbox.closest('tr')
  expect(row).not.toBeNull()
  return row as HTMLElement
}

describe('user discount rules sheet: adding rules', () => {
  it('lists only the selected group models and saves every selected model with the discount', async () => {
    const user = userEvent.setup()
    const { put } = renderSheet()
    await screen.findByText('gpt-4o-mini')

    await chooseGroup(user, 'vip')
    await openModelPicker(user)
    const optionNames = (await screen.findAllByRole('option')).map(
      (option) => option.textContent
    )
    expect(optionNames).toEqual(['claude-3', 'gpt-4o'])
    await user.click(screen.getByRole('button', { name: 'Select all' }))
    await user.keyboard('{Escape}')
    await user.type(discountInput(), '0.8')
    await user.click(saveButton())

    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(RULES_URL, {
        group: 'vip',
        models: ['claude-3', 'gpt-4o'],
        discount: 0.8,
      })
    )
  })

  it('shows the overwrite hint once a selected model already has a rule', async () => {
    const user = userEvent.setup()
    renderSheet()
    await screen.findByText('gpt-4o-mini')

    await chooseGroup(user, 'vip')
    await openModelPicker(user)
    await user.click(await screen.findByRole('option', { name: 'claude-3' }))
    expect(screen.queryByText(/Will overwrite/)).not.toBeInTheDocument()

    await user.click(screen.getByRole('option', { name: 'gpt-4o' }))

    expect(
      screen.getByText(/^Will overwrite 1 existing rules?$/)
    ).toBeInTheDocument()
  })

  it.each(['0', '1', '1.5', ''])(
    'keeps save disabled and marks the discount invalid for %j',
    async (value) => {
      const user = userEvent.setup()
      renderSheet()
      await screen.findByText('gpt-4o-mini')
      await chooseGroup(user, 'vip')
      await openModelPicker(user)
      await user.click(await screen.findByRole('option', { name: 'claude-3' }))
      await user.keyboard('{Escape}')

      const input = discountInput()
      await user.type(input, '0.5')
      await user.clear(input)
      if (value) await user.type(input, value)

      await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'))
      expect(saveButton()).toBeDisabled()
    }
  )

  it('clears the model selection when the group changes', async () => {
    const user = userEvent.setup()
    renderSheet()
    await screen.findByText('gpt-4o-mini')
    await chooseGroup(user, 'vip')
    await openModelPicker(user)
    await user.click(await screen.findByRole('option', { name: 'claude-3' }))
    await user.keyboard('{Escape}')

    await chooseGroup(user, 'default')
    await openModelPicker(user)

    const optionNames = (await screen.findAllByRole('option')).map(
      (option) => option.textContent
    )
    expect(optionNames).toEqual(['gpt-4o-mini'])
    expect(screen.getByRole('option', { name: 'gpt-4o-mini' })).toHaveAttribute(
      'aria-selected',
      'false'
    )
  })

  it('shows the refreshed rule list after a successful save', async () => {
    const user = userEvent.setup()
    renderSheet({
      rules: [
        existingRules,
        [...existingRules, rule(13, 'vip', 'claude-3', 0.8)],
      ],
    })
    await screen.findByText('gpt-4o-mini')
    await chooseGroup(user, 'vip')
    await openModelPicker(user)
    await user.click(await screen.findByRole('option', { name: 'claude-3' }))
    await user.keyboard('{Escape}')
    await user.type(discountInput(), '0.8')

    await user.click(saveButton())

    expect(
      await screen.findByRole('checkbox', { name: 'Select vip / claude-3' })
    ).toBeInTheDocument()
  })

  it('reports the server message and keeps the input when saving fails', async () => {
    const user = userEvent.setup()
    const { put } = renderSheet()
    const toastError = vi.spyOn(toast, 'error').mockReturnValue('toast')
    put.mockResolvedValue({
      data: { success: false, message: 'group is not usable' },
    })
    await screen.findByText('gpt-4o-mini')
    await chooseGroup(user, 'vip')
    await openModelPicker(user)
    await user.click(await screen.findByRole('option', { name: 'claude-3' }))
    await user.keyboard('{Escape}')
    await user.type(discountInput(), '0.8')

    await user.click(saveButton())

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith('group is not usable')
    )
    expect(discountInput()).toHaveValue(0.8)
  })
})

describe('user discount rules sheet: existing rules', () => {
  it('prefills the add form from a rule and saves the edit through the same update', async () => {
    const user = userEvent.setup()
    const { put } = renderSheet()
    await screen.findByText('gpt-4o-mini')

    await user.click(
      within(ruleRow('vip', 'gpt-4o')).getByRole('button', {
        name: 'Edit rule',
      })
    )

    expect(screen.getByRole('combobox', { name: 'Group' })).toHaveValue('vip')
    await waitFor(() => expect(discountInput()).toHaveValue(0.9))
    await waitFor(() => expect(discountInput()).toHaveFocus())
    await user.clear(discountInput())
    await user.type(discountInput(), '0.6')
    await user.click(saveButton())

    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(RULES_URL, {
        group: 'vip',
        models: ['gpt-4o'],
        discount: 0.6,
      })
    )
  })

  it('deletes every checked rule after confirmation', async () => {
    const user = userEvent.setup()
    const { del } = renderSheet()
    await screen.findByText('gpt-4o-mini')

    await user.click(
      screen.getByRole('checkbox', { name: 'Select vip / gpt-4o' })
    )
    await user.click(
      screen.getByRole('checkbox', { name: 'Select default / gpt-4o-mini' })
    )
    await user.click(screen.getByRole('button', { name: 'Delete selected' }))
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    await waitFor(() =>
      expect(del).toHaveBeenCalledWith(RULES_URL, { data: { ids: [11, 12] } })
    )
  })

  it('keeps bulk delete disabled while no rule is checked', async () => {
    renderSheet()
    await screen.findByText('gpt-4o-mini')

    expect(
      screen.getByRole('button', { name: 'Delete selected' })
    ).toBeDisabled()
  })

  it('filters the rule list by group or model text', async () => {
    const user = userEvent.setup()
    renderSheet()
    await screen.findByText('gpt-4o-mini')

    await user.type(
      screen.getByRole('textbox', { name: 'Filter by group or model' }),
      'mini'
    )

    expect(
      screen.getByRole('checkbox', { name: 'Select default / gpt-4o-mini' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('checkbox', { name: 'Select vip / gpt-4o' })
    ).not.toBeInTheDocument()
  })

  it('shows the empty state when the user has no rules', async () => {
    renderSheet({ rules: [[]] })

    expect(await screen.findByText('No discount rules')).toBeInTheDocument()
  })
})
