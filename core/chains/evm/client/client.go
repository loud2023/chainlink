package client

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/smartcontractkit/chainlink/core/assets"
	"github.com/smartcontractkit/chainlink/core/chains/evm/label"
	evmtypes "github.com/smartcontractkit/chainlink/core/chains/evm/types"
	"github.com/smartcontractkit/chainlink/core/config"
	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/chainlink/core/utils"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/pkg/errors"
)

const queryTimeout = 10 * time.Second

//go:generate mockery --quiet --name Client --output ../mocks/ --case=underscore
//go:generate mockery --quiet --name Subscription --output ../mocks/ --case=underscore

// Client is the interface used to interact with an ethereum node.
type Client interface {
	Dial(ctx context.Context) error
	Close()
	ChainID() *big.Int
	// NodeStates returns a map of node Name->node state
	// It might be nil or empty, e.g. for mock clients etc
	NodeStates() map[string]string

	GetERC20Balance(ctx context.Context, address common.Address, contractAddress common.Address) (*big.Int, error)
	GetLINKBalance(ctx context.Context, linkAddress common.Address, address common.Address) (*assets.Link, error)
	GetEthBalance(ctx context.Context, account common.Address, blockNumber *big.Int) (*assets.Eth, error)

	// Wrapped RPC methods
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
	BatchCallContext(ctx context.Context, b []rpc.BatchElem) error
	// BatchCallContextAll calls BatchCallContext for every single node including
	// sendonlys.
	// CAUTION: This should only be used for mass re-transmitting transactions, it
	// might have unexpected effects to use it for anything else.
	BatchCallContextAll(ctx context.Context, b []rpc.BatchElem) error

	// HeadByNumber and HeadByHash is a reimplemented version due to a
	// difference in how block header hashes are calculated by Parity nodes
	// running on Kovan, Avalanche and potentially others. We have to return our own wrapper type to capture the
	// correct hash from the RPC response.
	HeadByNumber(ctx context.Context, n *big.Int) (*evmtypes.Head, error)
	HeadByHash(ctx context.Context, n common.Hash) (*evmtypes.Head, error)
	SubscribeNewHead(ctx context.Context, ch chan<- *evmtypes.Head) (ethereum.Subscription, error)

	SendTransactionAndReturnErrorType(ctx context.Context, tx *types.Transaction, fromAddress common.Address) (evmtypes.TxmErrorType, error)

	// Wrapped Geth client methods
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)
	EstimateGas(ctx context.Context, call ethereum.CallMsg) (uint64, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)

	// bind.ContractBackend methods
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
	HeaderByHash(context.Context, common.Hash) (*types.Header, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
}

// This interface only exists so that we can generate a mock for it.  It is
// identical to `ethereum.Subscription`.
type Subscription interface {
	Err() <-chan error
	Unsubscribe()
}

func ContextWithDefaultTimeoutFromChan(chStop <-chan struct{}) (ctx context.Context, cancel context.CancelFunc) {
	return utils.ContextFromChanWithDeadline(chStop, queryTimeout)
}

// client represents an abstract client that manages connections to
// multiple nodes for a single chain id
type client struct {
	logger logger.Logger
	pool   *Pool
}

var _ Client = (*client)(nil)

// NewClientWithNodes instantiates a client from a list of nodes
// Currently only supports one primary
func NewClientWithNodes(logger logger.Logger, cfg PoolConfig, primaryNodes []Node, sendOnlyNodes []SendOnlyNode, chainID *big.Int, chainType config.ChainType) (*client, error) {
	pool := NewPool(logger, cfg, primaryNodes, sendOnlyNodes, chainID, chainType)
	return &client{
		logger: logger,
		pool:   pool,
	}, nil
}

// Dial opens websocket connections if necessary and sanity-checks that the
// node's remote chain ID matches the local one
func (client *client) Dial(ctx context.Context) error {
	if err := client.pool.Dial(ctx); err != nil {
		return errors.Wrap(err, "failed to dial pool")
	}
	return nil
}

func (client *client) Close() {
	client.pool.Close()
}

func (client *client) NodeStates() (states map[string]string) {
	states = make(map[string]string)
	for _, n := range client.pool.nodes {
		states[n.Name()] = n.State().String()
	}
	for _, s := range client.pool.sendonlys {
		states[s.Name()] = s.State().String()
	}
	return
}

// CallArgs represents the data used to call the balance method of a contract.
// "To" is the address of the ERC contract. "Data" is the message sent
// to the contract. "From" is the sender address.
type CallArgs struct {
	From common.Address `json:"from"`
	To   common.Address `json:"to"`
	Data hexutil.Bytes  `json:"data"`
}

// GetERC20Balance returns the balance of the given address for the token contract address.
func (client *client) GetERC20Balance(ctx context.Context, address common.Address, contractAddress common.Address) (*big.Int, error) {
	result := ""
	numLinkBigInt := new(big.Int)
	functionSelector := evmtypes.HexToFunctionSelector("0x70a08231") // balanceOf(address)
	data := utils.ConcatBytes(functionSelector.Bytes(), common.LeftPadBytes(address.Bytes(), utils.EVMWordByteLen))
	args := CallArgs{
		To:   contractAddress,
		Data: data,
	}
	err := client.CallContext(ctx, &result, "eth_call", args, "latest")
	if err != nil {
		return numLinkBigInt, err
	}
	numLinkBigInt.SetString(result, 0)
	return numLinkBigInt, nil
}

// GetLINKBalance returns the balance of LINK at the given address
func (client *client) GetLINKBalance(ctx context.Context, linkAddress common.Address, address common.Address) (*assets.Link, error) {
	balance, err := client.GetERC20Balance(ctx, address, linkAddress)
	if err != nil {
		return assets.NewLinkFromJuels(0), err
	}
	return (*assets.Link)(balance), nil
}

func (client *client) GetEthBalance(ctx context.Context, account common.Address, blockNumber *big.Int) (*assets.Eth, error) {
	balance, err := client.BalanceAt(ctx, account, blockNumber)
	if err != nil {
		return assets.NewEth(0), err
	}
	return (*assets.Eth)(balance), nil
}

// We wrap the GethClient's `TransactionReceipt` method so that we can ignore the error that arises
// when we're talking to a Parity node that has no receipt yet.
func (client *client) TransactionReceipt(ctx context.Context, txHash common.Hash) (receipt *types.Receipt, err error) {
	receipt, err = client.pool.TransactionReceipt(ctx, txHash)

	if err != nil && strings.Contains(err.Error(), "missing required field") {
		return nil, ethereum.NotFound
	}
	return
}

func (client *client) ChainID() *big.Int {
	return client.pool.chainID
}

func (client *client) HeaderByNumber(ctx context.Context, n *big.Int) (*types.Header, error) {
	return client.pool.HeaderByNumber(ctx, n)
}

func (client *client) HeaderByHash(ctx context.Context, h common.Hash) (*types.Header, error) {
	return client.pool.HeaderByHash(ctx, h)
}

func (client *client) SendTransactionAndReturnErrorType(ctx context.Context, tx *types.Transaction, fromAddress common.Address) (evmtypes.TxmErrorType, error) {
	err := client.pool.SendTransaction(ctx, tx)
	sendError := NewSendError(err)
	if sendError.Fatal() {
		client.logger.Criticalw("Fatal error sending transaction", "err", sendError, "etx", tx)
		// Attempt is thrown away in this case; we don't need it since it never got accepted by a node
		return evmtypes.Fatal, err
	}
	if sendError == nil || sendError.IsNonceTooLowError() || sendError.IsTransactionAlreadyMined() || sendError.IsReplacementUnderpriced() {
		// Either the transaction was successful or one of the following four scenarios happened:
		//
		// SCENARIO 1
		//
		// This is resuming a previous crashed run. In this scenario, it is
		// likely that our previous transaction was the one who was confirmed,
		// in which case we hand it off to the eth confirmer to get the
		// receipt.
		//
		// SCENARIO 2
		//
		// It is also possible that an external wallet can have messed with the
		// account and sent a transaction on this nonce.
		//
		// In this case, the onus is on the node operator since this is
		// explicitly unsupported.
		//
		// If it turns out to have been an external wallet, we will never get a
		// receipt for this transaction and it will eventually be marked as
		// errored.
		//
		// The end result is that we will NOT SEND a transaction for this
		// nonce.
		//
		// SCENARIO 3
		//
		// The network/eth client can be assumed to have at-least-once delivery
		// behavior. It is possible that the eth client could have already
		// sent this exact same transaction even if this is our first time
		// calling SendTransaction().
		//
		// SCENARIO 4 (most likely)
		//
		// A sendonly node got the transaction in first.
		//
		// In all scenarios, the correct thing to do is assume success for now
		// and hand off to the eth confirmer to get the receipt (or mark as
		// failed).
		return evmtypes.Successful, err
	}
	if sendError.IsTransactionAlreadyInMempool() {
		client.logger.Debugw("Transaction already in mempool", "txHash", tx.Hash, "nodeErr", sendError.Error())
		return evmtypes.Successful, err
	}
	if sendError.IsTemporarilyUnderpriced() {
		// If we can't even get the transaction into the mempool at all, assume
		// success (even though the transaction will never confirm) and hand
		// off to the ethConfirmer to bump gas periodically until we _can_ get
		// it in
		client.logger.Infow("Transaction temporarily underpriced", "err", sendError.Error())
		return evmtypes.Successful, err
	}
	if sendError.IsTerminallyUnderpriced() {
		return evmtypes.Underpriced, err
	}
	if sendError.L2FeeTooLow() || sendError.IsL2FeeTooHigh() || sendError.IsL2Full() {
		if client.pool.ChainType().IsL2() {
			return evmtypes.Underpriced, err
		}
		return evmtypes.Unsupported, errors.Wrap(sendError, "this error type only handled for L2s")
	}
	if sendError.IsNonceTooHighError() {
		// Nethermind specific error. Nethermind throws a NonceGap error when the tx nonce is
		// greater than current_nonce + tx_count_in_mempool, instead of keeping the tx in mempool.
		// This can happen if previous transactions haven't reached the client yet.
		// The correct thing to do is assume success for now and let the eth_confirmer retry until
		// the nonce gap gets filled by the previous transactions.
		client.logger.Warnw("Transaction has a nonce gap.", "err", sendError.Error())
		return evmtypes.Retryable, err
	}
	if sendError.IsInsufficientEth() {
		client.logger.Criticalw(fmt.Sprintf("Tx %x with type 0x%d was rejected due to insufficient eth: %s\n"+
			"ACTION REQUIRED: Chainlink wallet with address 0x%x is OUT OF FUNDS",
			tx.Hash(), tx.Type(), sendError.Error(), fromAddress,
		), "err", sendError)
		// NOTE: This bails out of the entire cycle and essentially "blocks" on
		// any transaction that gets insufficient_eth. This is OK if a
		// transaction with a large VALUE blocks because this always comes last
		// in the processing list.
		// If it blocks because of a transaction that is expensive due to large
		// gas limit, we could have smaller transactions "above" it that could
		// theoretically be sent, but will instead be blocked.
		return evmtypes.Retryable, err
	}
	if sendError.IsTimeout() {
		// In the case of timeout, we fall back to the backoff retry loop and
		// subsequent tries ought to resend the exact same in-progress transaction
		// attempt and get a definitive answer on what happened
		return evmtypes.Retryable, errors.Wrapf(sendError, "timeout while sending transaction %s", tx.Hash().Hex())
	}
	if sendError.IsTxFeeExceedsCap() {
		client.logger.Criticalw(fmt.Sprintf("Sending transaction failed(treating this error as of type Unknown); %s", label.RPCTxFeeCapConfiguredIncorrectlyWarning),
			"etx", tx,
			"err", sendError,
			"id", "RPCTxFeeCapExceeded",
		)
		// Note that we may have broadcast to multiple nodes and had it
		// accepted by one of them! It is not guaranteed that all nodes share
		// the same tx fee cap. That is why we must treat this as an unknown
		// error that may have been confirmed.
		//
		// If there is only one RPC node, or all RPC nodes have the same
		// configured cap, this transaction will get stuck and keep repeating
		// forever until the issue is resolved.
	}
	return evmtypes.Unknown, err
}

// SendTransaction also uses the sendonly HTTP RPC URLs if set
func (client *client) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return client.pool.SendTransaction(ctx, tx)
}

func (client *client) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return client.pool.PendingNonceAt(ctx, account)
}

func (client *client) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	return client.pool.NonceAt(ctx, account, blockNumber)
}

func (client *client) PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	return client.pool.PendingCodeAt(ctx, account)
}

func (client *client) EstimateGas(ctx context.Context, call ethereum.CallMsg) (gas uint64, err error) {
	return client.pool.EstimateGas(ctx, call)
}

// SuggestGasPrice calls the RPC node to get a suggested gas price.
// WARNING: It is not recommended to ever use this result for anything
// important. There are a number of issues with asking the RPC node to provide a
// gas estimate; it is not reliable. Unless you really have a good reason to
// use this, you should probably use core node's internal gas estimator
// instead.
func (client *client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return client.pool.SuggestGasPrice(ctx)
}

func (client *client) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return client.pool.CallContract(ctx, msg, blockNumber)
}

func (client *client) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	return client.pool.CodeAt(ctx, account, blockNumber)
}

func (client *client) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	return client.pool.BlockByNumber(ctx, number)
}

func (client *client) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return client.pool.BlockByHash(ctx, hash)
}

func (client *client) HeadByNumber(ctx context.Context, number *big.Int) (head *evmtypes.Head, err error) {
	hex := ToBlockNumArg(number)
	err = client.pool.CallContext(ctx, &head, "eth_getBlockByNumber", hex, false)
	if err != nil {
		return nil, err
	}
	if head == nil {
		err = ethereum.NotFound
		return
	}
	head.EVMChainID = utils.NewBig(client.ChainID())
	return
}

func (client *client) HeadByHash(ctx context.Context, hash common.Hash) (head *evmtypes.Head, err error) {
	err = client.pool.CallContext(ctx, &head, "eth_getBlockByHash", hash.Hex(), false)
	if err != nil {
		return nil, err
	}
	if head == nil {
		err = ethereum.NotFound
		return
	}
	head.EVMChainID = utils.NewBig(client.ChainID())
	return
}

func ToBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	return hexutil.EncodeBig(number)
}

func (client *client) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	return client.pool.BalanceAt(ctx, account, blockNumber)
}

func (client *client) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	return client.pool.FilterLogs(ctx, q)
}

func (client *client) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	client.logger.Debugw("evmclient.Client#SubscribeFilterLogs(...)",
		"q", q,
	)
	return client.pool.SubscribeFilterLogs(ctx, q, ch)
}

func (client *client) SubscribeNewHead(ctx context.Context, ch chan<- *evmtypes.Head) (ethereum.Subscription, error) {
	csf := newChainIDSubForwarder(client.ChainID(), ch)
	err := csf.start(client.pool.EthSubscribe(ctx, csf.srcCh, "newHeads"))
	if err != nil {
		return nil, err
	}
	return csf, nil
}

func (client *client) EthSubscribe(ctx context.Context, channel chan<- *evmtypes.Head, args ...interface{}) (ethereum.Subscription, error) {
	return client.pool.EthSubscribe(ctx, channel, args...)
}

func (client *client) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	return client.pool.CallContext(ctx, result, method, args...)
}

func (client *client) BatchCallContext(ctx context.Context, b []rpc.BatchElem) error {
	return client.pool.BatchCallContext(ctx, b)
}

func (client *client) BatchCallContextAll(ctx context.Context, b []rpc.BatchElem) error {
	return client.pool.BatchCallContextAll(ctx, b)
}

// SuggestGasTipCap calls the RPC node to get a suggested gas tip cap.
// WARNING: It is not recommended to ever use this result for anything
// important. There are a number of issues with asking the RPC node to provide a
// gas estimate; it is not reliable. Unless you really have a good reason to
// use this, you should probably use core node's internal gas estimator
// instead.
func (client *client) SuggestGasTipCap(ctx context.Context) (tipCap *big.Int, err error) {
	return client.pool.SuggestGasTipCap(ctx)
}
