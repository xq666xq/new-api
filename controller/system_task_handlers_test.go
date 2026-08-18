package controller

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteChannelMonitorProbeBatchRunsWithBoundedConcurrency(t *testing.T) {
	const concurrency = 4
	jobs := make([]channelMonitorProbeJob, 8)
	for index := range jobs {
		jobs[index].modelName = fmt.Sprintf("model-%d", index)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, len(jobs))
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	type batchOutcome struct {
		results []*model.ChannelMonitorResult
		err     error
	}
	completed := make(chan batchOutcome, 1)

	go func() {
		results, err := executeChannelMonitorProbeBatch(
			ctx,
			jobs,
			concurrency,
			func(ctx context.Context, job channelMonitorProbeJob) (*model.ChannelMonitorResult, error) {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					observed := maximum.Load()
					if current <= observed || maximum.CompareAndSwap(observed, current) {
						break
					}
				}
				started <- struct{}{}
				select {
				case <-release:
					return &model.ChannelMonitorResult{ModelName: job.modelName}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		)
		completed <- batchOutcome{results: results, err: err}
	}()

	for range concurrency {
		select {
		case <-started:
		case <-ctx.Done():
			require.NoError(t, ctx.Err())
		}
	}
	assert.Equal(t, int32(concurrency), active.Load())
	assert.Equal(t, int32(concurrency), maximum.Load())
	assert.Empty(t, started)

	close(release)
	var outcome batchOutcome
	select {
	case outcome = <-completed:
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	}
	require.NoError(t, outcome.err)
	require.Len(t, outcome.results, len(jobs))
	for index, result := range outcome.results {
		require.NotNil(t, result)
		assert.Equal(t, jobs[index].modelName, result.ModelName)
	}
	assert.LessOrEqual(t, maximum.Load(), int32(concurrency))
}

func TestExecuteChannelMonitorProbeBatchZeroStartsEveryJob(t *testing.T) {
	jobs := []channelMonitorProbeJob{
		{modelName: "model-a"},
		{modelName: "model-b"},
		{modelName: "model-c"},
		{modelName: "model-d"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, len(jobs))
	release := make(chan struct{})
	type batchOutcome struct {
		results []*model.ChannelMonitorResult
		err     error
	}
	completed := make(chan batchOutcome, 1)

	go func() {
		results, err := executeChannelMonitorProbeBatch(
			ctx,
			jobs,
			0,
			func(ctx context.Context, job channelMonitorProbeJob) (*model.ChannelMonitorResult, error) {
				started <- struct{}{}
				select {
				case <-release:
					return &model.ChannelMonitorResult{ModelName: job.modelName}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		)
		completed <- batchOutcome{results: results, err: err}
	}()

	for range jobs {
		select {
		case <-started:
		case <-ctx.Done():
			require.NoError(t, ctx.Err())
		}
	}
	close(release)

	select {
	case outcome := <-completed:
		require.NoError(t, outcome.err)
		require.Len(t, outcome.results, len(jobs))
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	}
}

func TestExecuteChannelMonitorProbeBatchProbesEveryJobDespiteFailure(t *testing.T) {
	jobs := []channelMonitorProbeJob{
		{modelName: "model-a"},
		{modelName: "model-b"},
		{modelName: "model-c"},
	}
	wantErr := errors.New("persist probe result")
	var calls atomic.Int32

	results, err := executeChannelMonitorProbeBatch(
		context.Background(),
		jobs,
		1,
		func(_ context.Context, job channelMonitorProbeJob) (*model.ChannelMonitorResult, error) {
			calls.Add(1)
			if job.modelName == "model-a" {
				return nil, wantErr
			}
			return &model.ChannelMonitorResult{ModelName: job.modelName}, nil
		},
	)

	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, int32(len(jobs)), calls.Load())
	require.Len(t, results, len(jobs))
	assert.Nil(t, results[0])
	require.NotNil(t, results[1])
	assert.Equal(t, "model-b", results[1].ModelName)
	require.NotNil(t, results[2])
	assert.Equal(t, "model-c", results[2].ModelName)
}

func TestChannelMonitorModelsForSweepHonorsBannedOnlyMode(t *testing.T) {
	config := &model.ChannelMonitorConfig{MonitorMode: model.ChannelMonitorModeBannedOnly}
	require.NoError(t, config.SetMonitoredModels([]string{"active", "banned", "confirming"}))
	states := map[string]*model.ChannelManagedState{
		"active":     {BanState: model.ManagedBanStateActive},
		"banned":     {BanState: model.ManagedBanStateBanned},
		"confirming": {BanState: model.ManagedBanStateActive, ConfirmCount: 1},
	}

	assert.Equal(t, []string{"banned", "confirming"}, channelMonitorModelsForSweep(config, states))

	config.MonitorMode = model.ChannelMonitorModeDefault
	assert.Equal(t, []string{"active", "banned", "confirming"}, channelMonitorModelsForSweep(config, states))

	config.MonitorMode = model.ChannelMonitorModeBannedOnly
	config.NextCheckAt = -1
	assert.Equal(t, []string{"active", "banned", "confirming"}, channelMonitorModelsForSweep(config, states))
}
