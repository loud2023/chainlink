package cosmos

import (
	"github.com/smartcontractkit/sqlx"

	cosmosdb "github.com/smartcontractkit/chainlink-cosmos/pkg/cosmos/db"

	"github.com/smartcontractkit/chainlink/core/chains"
	"github.com/smartcontractkit/chainlink/core/chains/cosmos/types"
	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/chainlink/core/services/pg"
)

// NewORM returns an ORM backed by db.
// https://app.shortcut.com/chainlinklabs/story/33622/remove-legacy-config
func NewORM(db *sqlx.DB, lggr logger.Logger, cfg pg.QConfig) types.ORM {
	q := pg.NewQ(db, lggr.Named("ORM"), cfg)
	return chains.NewORM[string, *cosmosdb.ChainCfg, cosmosdb.Node](q, "cosmos", "tendermint_url")
}

func NewORMImmut(cfgs chains.ChainConfig[string, *cosmosdb.ChainCfg, cosmosdb.Node]) types.ORM {
	return chains.NewORMImmut(cfgs)
}
