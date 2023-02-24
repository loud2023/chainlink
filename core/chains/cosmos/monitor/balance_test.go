package monitor

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/chainlink-terra/pkg/cosmos/client/mocks"
	"github.com/smartcontractkit/chainlink/core/internal/testutils"
	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/chainlink/core/services/keystore/keys/cosmoskey"
)

func TestBalanceMonitor(t *testing.T) {
	const chainID = "Chainlinktest-42"
	ks := keystore{cosmoskey.New(), cosmoskey.New(), cosmoskey.New()}
	bals := []sdk.Coin{
		sdk.NewInt64Coin("uatom", 0),
		sdk.NewInt64Coin("uatom", 1),
		sdk.NewInt64Coin("uatom", 100000000000),
	}
	expBals := []string{
		"0.000000000000000000atom",
		"0.000001000000000000atom",
		"100000.000000000000000000atom",
	}
	client := new(mocks.ReaderWriter)
	client.Test(t)
	type update struct{ acc, bal string }
	var exp []update
	for i := range bals {
		acc := sdk.AccAddress(ks[i].PublicKey().Address())
		client.On("Balance", acc, bals[i].Denom).Return(&bals[i], nil)
		exp = append(exp, update{acc.String(), expBals[i]})
	}
	cfg := &config{blockRate: time.Second}
	b := newBalanceMonitor(chainID, cfg, logger.TestLogger(t), ks, nil)
	var got []update
	done := make(chan struct{})
	b.updateFn = func(acc sdk.AccAddress, bal *sdk.DecCoin) {
		select {
		case <-done:
			return
		default:
		}
		got = append(got, update{acc.String(), bal.String()})
		if len(got) == len(exp) {
			close(done)
		}
	}
	b.reader = client

	require.NoError(t, b.Start(testutils.Context(t)))
	t.Cleanup(func() {
		assert.NoError(t, b.Close())
		client.AssertExpectations(t)
	})
	select {
	case <-time.After(testutils.WaitTimeout(t)):
		t.Fatal("timed out waiting for balance monitor")
	case <-done:
	}

	assert.EqualValues(t, exp, got)
}

type config struct {
	blockRate time.Duration
}

func (c *config) BlockRate() time.Duration {
	return c.blockRate
}

type keystore []cosmoskey.Key

func (k keystore) GetAll() ([]cosmoskey.Key, error) {
	return k, nil
}
