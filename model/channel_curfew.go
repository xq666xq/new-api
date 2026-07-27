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

package model

import (
	"errors"

	"gorm.io/gorm"
)

// Channel-monitor curfew phase persistence. The curfew boundary notifier compares
// the live curfew state against the last phase it acted on, so it fires exactly
// once at each boundary (curfew start / end) and survives restarts and
// multi-master deployments. The value lives in the shared option table as a
// single string; it is intentionally read/written directly (not through the
// OptionMap config pipeline) because it is internal runtime bookkeeping, not an
// operator-facing setting.
const (
	channelCurfewPhaseOptionKey = "channel_monitor_curfew_phase"

	// CurfewPhaseActive means the last observed state was inside the quiet window;
	// CurfewPhaseInactive means outside it. An empty stored value means "never
	// seeded" — the notifier records the current phase without sending, so a fresh
	// install or a just-enabled integration never emits a spurious boundary card.
	CurfewPhaseActive   = "active"
	CurfewPhaseInactive = "inactive"
)

// GetChannelCurfewPhase returns the last persisted curfew phase, or "" when it has
// never been recorded. A missing row is not an error; any other DB error is
// surfaced so the caller can skip acting rather than risk a false transition.
func GetChannelCurfewPhase() (string, error) {
	var option Option
	err := DB.Where(&Option{Key: channelCurfewPhaseOptionKey}).First(&option).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return option.Value, nil
}

// SetChannelCurfewPhase upserts the curfew phase. It uses GORM's struct-based
// query so the reserved "key" column is quoted correctly across SQLite, MySQL,
// and PostgreSQL, and does not touch the in-memory OptionMap since this is
// internal state, not a published option.
func SetChannelCurfewPhase(phase string) error {
	var option Option
	if err := DB.Where(&Option{Key: channelCurfewPhaseOptionKey}).FirstOrCreate(&option, Option{Key: channelCurfewPhaseOptionKey}).Error; err != nil {
		return err
	}
	option.Value = phase
	return DB.Save(&option).Error
}
