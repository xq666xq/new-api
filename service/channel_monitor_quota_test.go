package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorSkipsRelayQuotaSideEffects(t *testing.T) {
	info := &relaycommon.RelayInfo{IsChannelMonitor: true}

	require.NoError(t, PostConsumeQuota(info, 100, 0, false))
	require.NotPanics(t, func() {
		PostTextConsumeQuota(nil, info, &dto.Usage{TotalTokens: 1}, nil)
	})
	require.NotPanics(t, func() {
		PostAudioConsumeQuota(nil, info, &dto.Usage{TotalTokens: 1}, "")
	})
}
