package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
)

// Error-triggered probe counter.
//
// When a managed channel is hit by a normal-forwarding error that would
// otherwise auto-disable it, we do not disable it immediately. Instead we count
// consecutive errors per (channel, model) and, once the count reaches the
// configured threshold within the configured window, advance that channel's
// next monitor probe and hand the ban/recover decision to the managed policy.
//
// "Consecutive" is enforced two ways: a successful forward for the same pair
// resets its counter (ResetChannelModelErrorProbe), and the window elapsing
// since the streak's first error also resets it. This keeps a single flaky
// request from ever accumulating toward a probe — only a stable run of errors
// does.
//
// The counter is in-memory only: advancing a probe is idempotent
// (next_check_at=0 can be set repeatedly with no harm), so multi-instance
// deployments counting independently at worst trigger a probe a little sooner,
// which is harmless for a health check. There is no correctness requirement for
// a shared count, so we avoid the Redis round-trip on the hot error path.
var (
	channelErrorProbeStore   sync.Map // key: "channelId\x00model" -> channelErrorProbeEntry
	channelErrorProbeCleanup sync.Once
)

type channelErrorProbeEntry struct {
	count   int
	firstAt time.Time
}

func channelErrorProbeKey(channelId int, modelName string) string {
	return fmt.Sprintf("%d\x00%s", channelId, modelName)
}

// startChannelErrorProbeCleanup periodically drops entries whose window has long
// elapsed so the map does not grow unbounded for pairs that error once and then
// go quiet (a quiet pair never gets a reset call).
func startChannelErrorProbeCleanup() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Hour)
			now := time.Now()
			windowSeconds := operation_setting.GetManagedPolicySetting().ErrorProbeWindowSeconds
			// Use a generous multiple of the window as the staleness cutoff so an
			// entry mid-streak is never evicted out from under an active counter.
			cutoff := time.Duration(windowSeconds) * time.Second * 4
			channelErrorProbeStore.Range(func(key, value any) bool {
				if entry, ok := value.(channelErrorProbeEntry); ok {
					if now.Sub(entry.firstAt) >= cutoff {
						channelErrorProbeStore.Delete(key)
					}
				}
				return true
			})
		}
	})
}

// BumpChannelModelErrorProbe records one normal-forwarding error for the given
// (channel, model) pair and reports whether a monitor probe should now be
// triggered. It returns true exactly on the error that reaches the configured
// threshold within the window, and resets the counter at that moment so the next
// probe requires a fresh streak. A stale streak (first error older than the
// window) restarts with this error as the new first.
func BumpChannelModelErrorProbe(channelId int, modelName string) bool {
	channelErrorProbeCleanup.Do(startChannelErrorProbeCleanup)

	setting := operation_setting.GetManagedPolicySetting()
	threshold := setting.ErrorProbeThreshold
	window := time.Duration(setting.ErrorProbeWindowSeconds) * time.Second
	return bumpChannelModelErrorProbeAt(channelId, modelName, threshold, window, time.Now())
}

// bumpChannelModelErrorProbeAt is the pure core of BumpChannelModelErrorProbe
// with the threshold, window, and clock injected so the streak/window/threshold
// invariants can be tested deterministically without sleeps or mutating global
// settings.
func bumpChannelModelErrorProbeAt(channelId int, modelName string, threshold int, window time.Duration, now time.Time) bool {
	key := channelErrorProbeKey(channelId, modelName)

	entry := channelErrorProbeEntry{count: 0, firstAt: now}
	if value, ok := channelErrorProbeStore.Load(key); ok {
		if prev, ok := value.(channelErrorProbeEntry); ok {
			if now.Sub(prev.firstAt) < window {
				entry = prev
			}
			// else: streak expired, keep the fresh entry started at `now`.
		}
	}
	entry.count++

	if entry.count >= threshold {
		channelErrorProbeStore.Delete(key)
		return true
	}
	channelErrorProbeStore.Store(key, entry)
	return false
}

// ResetChannelModelErrorProbe clears the error streak for a (channel, model)
// pair after a successful forward, enforcing the "consecutive" requirement: any
// success breaks the streak so a probe is only triggered by an unbroken run of
// errors.
func ResetChannelModelErrorProbe(channelId int, modelName string) {
	channelErrorProbeStore.Delete(channelErrorProbeKey(channelId, modelName))
}

// ResetChannelModelErrorProbeIfEnabled is the success-path entry point. It is a
// no-op when the feature is off (or the model is unknown) so the hot,
// high-volume success path pays nothing — no map lookup, no delete — unless
// error-triggered probing is actually enabled.
func ResetChannelModelErrorProbeIfEnabled(channelId int, modelName string) {
	if modelName == "" {
		return
	}
	if !operation_setting.GetManagedPolicySetting().ErrorTriggerProbeEnabled {
		return
	}
	ResetChannelModelErrorProbe(channelId, modelName)
}

// TryDeferChannelDisableToProbe decides, for a normal-forwarding error, whether
// to hand a managed channel's fate to the monitor/managed policy instead of
// letting the caller auto-disable the whole channel.
//
// It is deliberately independent of the global auto-disable switch and of the
// channel's AutoBan flag: the whole point of the feature is "for managed
// channels, don't ban on a forwarding error — trigger a probe and let the policy
// decide". It only requires the error to be a genuine upstream fault (same
// classification as auto-disable, via isChannelErrorDisableWorthy) so a user 400
// or skip-retry error never feeds the counter.
//
// It returns true when it has taken ownership of the error (the caller must NOT
// auto-disable): this happens for a probe-worthy error on a managed channel whose
// monitor config is enabled and actually monitors the erroring model. The error
// is counted, and once a stable streak reaches the threshold the channel's next
// probe is advanced so the managed ban/recover state machine — not this single
// error — decides the channel's fate.
//
// It returns false (caller keeps its own auto-disable behavior) when the feature
// is off, the error is not a real fault, the model name is unknown, or the
// channel is not a managed + enabled + monitored pair.
func TryDeferChannelDisableToProbe(channelId int, modelName string, err *types.NewAPIError) bool {
	if !operation_setting.GetManagedPolicySetting().ErrorTriggerProbeEnabled {
		return false
	}
	if modelName == "" {
		return false
	}
	if !isChannelErrorDisableWorthy(err) {
		return false
	}
	config, configErr := model.GetChannelMonitorConfig(channelId)
	if configErr != nil {
		common.SysError(fmt.Sprintf("error-triggered probe: load monitor config for channel %d failed: %v", channelId, configErr))
		return false
	}
	if config == nil || !config.Enabled || !config.Managed {
		return false
	}
	monitored := false
	for _, name := range config.GetMonitoredModels() {
		if name == modelName {
			monitored = true
			break
		}
	}
	if !monitored {
		return false
	}

	if BumpChannelModelErrorProbe(channelId, modelName) {
		if _, err := model.AdvanceChannelMonitorConfigDue(channelId); err != nil {
			common.SysError(fmt.Sprintf("error-triggered probe: advance probe for channel %d failed: %v", channelId, err))
		} else {
			common.SysLog(fmt.Sprintf("error-triggered probe: channel %d model %s reached error threshold, advancing monitor probe", channelId, modelName))
		}
	}
	return true
}
