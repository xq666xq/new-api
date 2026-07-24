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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { ChannelStatus } from '@/features/channel-status'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/channel-status/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    // Any authenticated user may view channel status. Admins see per-channel
    // rows (with channel identity); normal members see the model-aggregated
    // view with channel identity stripped (see the page's role branch).
    if (!auth.user) {
      throw redirect({ to: '/403' })
    }
  },
  component: ChannelStatus,
})
