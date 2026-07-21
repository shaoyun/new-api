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
