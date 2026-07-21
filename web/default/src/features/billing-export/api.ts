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
import { api } from '@/lib/api'

/** A selectable customer: value is the username, label is a friendly name. */
export interface CustomerOption {
  value: string
  label: string
}

// Server caps page_size at 100. Bound total fetches so an unexpectedly large
// user table can't trigger a runaway loop; surface truncation to the caller.
const USERS_PAGE_SIZE = 100
const USERS_MAX_PAGES = 50 // up to 5000 users

interface UsersListResponse {
  success: boolean
  message?: string
  data?: {
    items: Array<{ username: string; display_name?: string }>
    total: number
  }
}

/**
 * Fetch the full customer list for the export selector. Pages through the
 * admin users endpoint until every user is loaded (or the safety cap is hit).
 * Returns the options plus whether the list was truncated so the UI can warn.
 */
export async function fetchCustomerOptions(): Promise<{
  options: CustomerOption[]
  truncated: boolean
}> {
  const options: CustomerOption[] = []
  let page = 1
  let total = Infinity
  let truncated = false

  while (options.length < total) {
    if (page > USERS_MAX_PAGES) {
      truncated = true
      break
    }
    const res = await api.get<UsersListResponse>(
      `/api/user/?p=${page}&page_size=${USERS_PAGE_SIZE}`,
      { skipBusinessError: true }
    )
    const data = res.data?.data
    const items = data?.items ?? []
    total = data?.total ?? options.length
    for (const item of items) {
      options.push({
        value: item.username,
        label: item.display_name
          ? `${item.display_name} (${item.username})`
          : item.username,
      })
    }
    if (items.length === 0) break
    page += 1
  }

  return { options, truncated }
}

export interface BillingExportParams {
  /** Inclusive start of the window, in seconds since epoch. */
  startTimestamp: number
  /** Inclusive end of the window, in seconds since epoch. */
  endTimestamp: number
  /** Usernames to include (already trimmed of blanks). */
  usernames: string[]
}

/**
 * Fetch the admin billing export as a CSV Blob. The backend streams
 * `GET /api/log/export` as a `text/csv` attachment; we request it as a blob so
 * the browser can save it directly.
 */
export async function fetchBillingExport(
  params: BillingExportParams
): Promise<Blob> {
  const response = await api.get('/api/log/export', {
    responseType: 'blob',
    // The response is a CSV blob, not the { success, message, data } envelope.
    skipBusinessError: true,
    params: {
      start_timestamp: params.startTimestamp,
      end_timestamp: params.endTimestamp,
      usernames: params.usernames.join(','),
    },
  })
  return response.data as Blob
}
