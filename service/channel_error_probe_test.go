package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestBumpChannelModelErrorProbe pins the "stable errors only" contract of the
// error-triggered probe counter: a probe fires only on a consecutive run of
// errors that reaches the threshold within the window, a success (reset) breaks
// the streak, the window elapsing restarts it, and the counter is per
// (channel, model).
func TestBumpChannelModelErrorProbe(t *testing.T) {
	const (
		channelID = 4201
		modelName = "gpt-probe-test"
		threshold = 2
	)
	window := 60 * time.Second
	base := time.Unix(1_700_000_000, 0)

	// Isolate this pair from any other test state.
	ResetChannelModelErrorProbe(channelID, modelName)
	t.Cleanup(func() { ResetChannelModelErrorProbe(channelID, modelName) })

	t.Run("threshold reached fires exactly once then resets", func(t *testing.T) {
		ResetChannelModelErrorProbe(channelID, modelName)
		// First error: below threshold, no probe.
		assert.False(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base))
		// Second consecutive error within window: reaches threshold, fires.
		assert.True(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base.Add(time.Second)))
		// After firing the streak is cleared, so the next error is the first of a
		// fresh streak and does not fire.
		assert.False(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base.Add(2*time.Second)))
	})

	t.Run("success reset breaks the streak", func(t *testing.T) {
		ResetChannelModelErrorProbe(channelID, modelName)
		assert.False(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base))
		// A successful forward resets the streak.
		ResetChannelModelErrorProbe(channelID, modelName)
		// The next error is again only the first of a new streak: no probe.
		assert.False(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base.Add(time.Second)))
	})

	t.Run("window elapsing restarts the streak", func(t *testing.T) {
		ResetChannelModelErrorProbe(channelID, modelName)
		assert.False(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base))
		// Second error arrives after the window: the stale streak is discarded and
		// this becomes a fresh first error, so no probe fires.
		assert.False(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base.Add(window+time.Second)))
		// One more within the new window reaches the threshold.
		assert.True(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base.Add(window+2*time.Second)))
	})

	t.Run("counter is per channel-model pair", func(t *testing.T) {
		const otherModel = "gpt-probe-other"
		ResetChannelModelErrorProbe(channelID, modelName)
		ResetChannelModelErrorProbe(channelID, otherModel)
		t.Cleanup(func() { ResetChannelModelErrorProbe(channelID, otherModel) })
		// One error on each model: neither reaches the threshold, because the two
		// pairs count independently.
		assert.False(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base))
		assert.False(t, bumpChannelModelErrorProbeAt(channelID, otherModel, threshold, window, base))
		// A second error on the first model fires only that model's probe.
		assert.True(t, bumpChannelModelErrorProbeAt(channelID, modelName, threshold, window, base.Add(time.Second)))
	})

	t.Run("threshold of one fires on the first error", func(t *testing.T) {
		ResetChannelModelErrorProbe(channelID, modelName)
		assert.True(t, bumpChannelModelErrorProbeAt(channelID, modelName, 1, window, base))
	})
}
