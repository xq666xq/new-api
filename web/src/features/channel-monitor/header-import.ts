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
import type { HeaderEntry } from './types'

export type ImportedHeaderEntry = Omit<HeaderEntry, 'id'>

/**
 * Parses a captured header object. Scalar values are converted to strings;
 * nested objects and arrays are serialized as compact JSON so structured
 * headers such as X-Codex-Turn-Metadata remain valid HTTP header values.
 */
export function parseImportedHeaders(
  raw: string,
  invalidMessage: string
): ImportedHeaderEntry[] {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    throw new Error(invalidMessage)
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(invalidMessage)
  }

  return Object.entries(parsed).map(([key, value]) => ({
    key,
    value:
      typeof value === 'string'
        ? value
        : (JSON.stringify(value) ?? String(value)),
  }))
}
