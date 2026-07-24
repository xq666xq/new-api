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

export const channelMonitorTableClassName = 'min-w-[1450px] table-fixed'

export const channelMonitorColumns = [
  { key: 'channel', width: 240, className: 'w-[240px]' },
  { key: 'models', width: 300, className: 'w-[300px]' },
  { key: 'monitoring', width: 180, className: 'w-[180px]' },
  { key: 'hosting', width: 96, className: 'w-[96px]' },
  { key: 'enabled', width: 84, className: 'w-[84px]' },
  { key: 'strategy', width: 220, className: 'w-[220px]' },
  { key: 'remark', width: 190, className: 'w-[190px]' },
  { key: 'actions', width: 140, className: 'w-[140px]' },
] as const
