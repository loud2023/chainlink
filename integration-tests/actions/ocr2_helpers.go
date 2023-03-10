package actions

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/smartcontractkit/libocr/offchainreporting2/confighelper"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/chainlink/integration-tests/client"
	"github.com/smartcontractkit/chainlink/integration-tests/contracts"
)

func BuildGeneralOCR2Config(
	t *testing.T,
	chainlinkNodes []*client.Chainlink,
	deltaProgress time.Duration,
	deltaResend time.Duration,
	deltaRound time.Duration,
	deltaGrace time.Duration,
	deltaStage time.Duration,
	rMax uint8,
	s []int,
	reportingPluginConfig []byte,
	maxDurationQuery time.Duration,
	maxDurationObservation time.Duration,
	maxDurationReport time.Duration,
	maxDurationShouldAcceptFinalizedReport time.Duration,
	maxDurationShouldTransmitAcceptedReport time.Duration,
	f int,
	onchainConfig []byte,
) contracts.OCRConfig {
	l := GetTestLogger(t)
	_, oracleIdentities := getOracleIdentities(t, chainlinkNodes)

	signerOnchainPublicKeys, transmitterAccounts, f_, onchainConfig_, offchainConfigVersion, offchainConfig, err := confighelper.ContractSetConfigArgsForTests(
		deltaProgress,
		deltaResend,
		deltaRound,
		deltaGrace,
		deltaStage,
		rMax,
		s,
		oracleIdentities,
		reportingPluginConfig,
		maxDurationQuery,
		maxDurationObservation,
		maxDurationReport,
		maxDurationShouldAcceptFinalizedReport,
		maxDurationShouldTransmitAcceptedReport,
		f,
		onchainConfig,
	)
	require.NoError(t, err, "Shouldn't fail setting contract config")

	var signers []common.Address
	for _, signer := range signerOnchainPublicKeys {
		require.Equal(t, 20, len(signer), "OnChainPublicKey has wrong length for address")
		signers = append(signers, common.BytesToAddress(signer))
	}

	var transmitters []common.Address
	for _, transmitter := range transmitterAccounts {
		require.True(t, common.IsHexAddress(string(transmitter)), "TransmitAccount is not a valid Ethereum address")
		transmitters = append(transmitters, common.HexToAddress(string(transmitter)))
	}

	l.Info().Msg("Done building OCR2 config")
	return contracts.OCRConfig{
		Signers:               signers,
		Transmitters:          transmitters,
		F:                     f_,
		OnchainConfig:         onchainConfig_,
		OffChainConfigVersion: offchainConfigVersion,
		OffChainConfig:        offchainConfig,
	}
}
