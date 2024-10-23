package ethereum

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/pkg/errors"
	log "github.com/xlab/suplog"

	"github.com/monk07-01/peggo1.14/orchestrator/ethereum/committer"
	"github.com/monk07-01/peggo1.14/orchestrator/ethereum/peggy"
	"github.com/monk07-01/peggo1.14/orchestrator/ethereum/provider"
	peggyevents "github.com/monk07-01/peggo1.14/solidity/wrappers/Peggy.sol"
	peggyevents2 "github.com/monk07-01/peggo1.14/solidity/wrappers/PeggySubgraph.sol"
	peggytypes "github.com/InjectiveLabs/sdk-go/chain/peggy/types"
)

type NetworkConfig struct {
	EthNodeRPC            string
	GasPriceAdjustment    float64
	MaxGasPrice           string
	PendingTxWaitDuration string
	EthNodeAlchemyWS      string
}

// Network is the orchestrator's reference endpoint to the Ethereum network
type Network interface {
	GetHeaderByNumber(ctx context.Context, number *big.Int) (*gethtypes.Header, error)
	GetPeggyID(ctx context.Context) (gethcommon.Hash, error)

	GetSendToCosmosEvents(startBlock, endBlock uint64) ([]*peggyevents2.WrappersSendToCosmosEvent, error)
	GetSendToBfhdexEvents(startBlock, endBlock uint64) ([]*peggyevents2.WrappersSendToBfhdexEvent, error)
	GetPeggyERC20DeployedEvents(startBlock, endBlock uint64) ([]*peggyevents.WrappersERC20DeployedEvent, error)
	GetValsetUpdatedEvents(startBlock, endBlock uint64) ([]*peggyevents.WrappersValsetUpdatedEvent, error)
	GetTransactionBatchExecutedEvents(startBlock, endBlock uint64) ([]*peggyevents.WrappersTransactionBatchExecutedEvent, error)

	GetValsetNonce(ctx context.Context) (*big.Int, error)
	SendEthValsetUpdate(ctx context.Context,
		oldValset *peggytypes.Valset,
		newValset *peggytypes.Valset,
		confirms []*peggytypes.MsgValsetConfirm,
	) (*gethcommon.Hash, error)

	GetTxBatchNonce(ctx context.Context, erc20ContractAddress gethcommon.Address) (*big.Int, error)
	SendTransactionBatch(ctx context.Context,
		currentValset *peggytypes.Valset,
		batch *peggytypes.OutgoingTxBatch,
		confirms []*peggytypes.MsgConfirmBatch,
	) (*gethcommon.Hash, error)

	TokenDecimals(ctx context.Context, tokenContract gethcommon.Address) (uint8, error)
}

type network struct {
	peggy.PeggyContract

	FromAddr gethcommon.Address
}

func NewNetwork(
	peggyContractAddr,
	fromAddr gethcommon.Address,
	signerFn bind.SignerFn,
	cfg NetworkConfig,
) (Network, error) {
	evmRPC, err := rpc.Dial(cfg.EthNodeRPC)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to ethereum RPC: %s", cfg.EthNodeRPC)
	}

	ethCommitter, err := committer.NewEthCommitter(
		fromAddr,
		cfg.GasPriceAdjustment,
		cfg.MaxGasPrice,
		signerFn,
		provider.NewEVMProvider(evmRPC),
	)
	if err != nil {
		return nil, err
	}

	pendingTxDuration, err := time.ParseDuration(cfg.PendingTxWaitDuration)
	if err != nil {
		return nil, err
	}

	peggyContract, err := peggy.NewPeggyContract(ethCommitter, peggyContractAddr, peggy.PendingTxInputList{}, pendingTxDuration)
	if err != nil {
		return nil, err
	}

	// If Alchemy Websocket URL is set, then Subscribe to Pending Transaction of Peggy Contract.
	if cfg.EthNodeAlchemyWS != "" {
		log.WithFields(log.Fields{
			"url": cfg.EthNodeAlchemyWS,
		}).Infoln("subscribing to Alchemy websocket")
		go peggyContract.SubscribeToPendingTxs(cfg.EthNodeAlchemyWS)
	}

	n := &network{
		PeggyContract: peggyContract,
		FromAddr:      fromAddr,
	}

	return n, nil
}

func (n *network) TokenDecimals(ctx context.Context, tokenContract gethcommon.Address) (uint8, error) {
	msg := ethereum.CallMsg{
		To:   &tokenContract,
		Data: gethcommon.Hex2Bytes("313ce567"), // decimals() method signature
	}

	res, err := n.Provider().CallContract(ctx, msg, nil)
	if err != nil {
		return 0, err
	}

	if len(res) == 0 {
		return 0, errors.Errorf("no decimals found for token contract %s", tokenContract.Hex())
	}

	return uint8(big.NewInt(0).SetBytes(res).Uint64()), nil
}

func (n *network) GetHeaderByNumber(ctx context.Context, number *big.Int) (*gethtypes.Header, error) {
	return n.Provider().HeaderByNumber(ctx, number)
}

func (n *network) GetPeggyID(ctx context.Context) (gethcommon.Hash, error) {
	return n.PeggyContract.GetPeggyID(ctx, n.FromAddr)
}

func (n *network) GetValsetNonce(ctx context.Context) (*big.Int, error) {
	return n.PeggyContract.GetValsetNonce(ctx, n.FromAddr)
}

func (n *network) GetTxBatchNonce(ctx context.Context, erc20ContractAddress gethcommon.Address) (*big.Int, error) {
	return n.PeggyContract.GetTxBatchNonce(ctx, erc20ContractAddress, n.FromAddr)
}

func (n *network) GetSendToCosmosEvents(startBlock, endBlock uint64) ([]*peggyevents2.WrappersSendToCosmosEvent, error) {
	peggyFilterer, err := peggyevents2.NewWrappersFilterer(n.Address(), n.Provider())
	if err != nil {
		return nil, errors.Wrap(err, "failed to init Peggy events filterer")
	}

	iter, err := peggyFilterer.FilterSendToCosmosEvent(&bind.FilterOpts{
		Start: startBlock,
		End:   &endBlock,
	}, nil, nil, nil)
	if err != nil {
		if !isUnknownBlockErr(err) {
			return nil, errors.Wrap(err, "failed to scan past SendToCosmos events from Ethereum")
		} else if iter == nil {
			return nil, errors.New("no iterator returned")
		}
	}

	defer iter.Close()

	var sendToCosmosEvents []*peggyevents2.WrappersSendToCosmosEvent
	for iter.Next() {
		sendToCosmosEvents = append(sendToCosmosEvents, iter.Event)
	}

	return sendToCosmosEvents, nil
}

func (n *network) GetSendToBfhdexEvents(startBlock, endBlock uint64) ([]*peggyevents2.WrappersSendToBfhdexEvent, error) {
	peggyFilterer, err := peggyevents2.NewWrappersFilterer(n.Address(), n.Provider())
	if err != nil {
		return nil, errors.Wrap(err, "failed to init Peggy events filterer")
	}

	iter, err := peggyFilterer.FilterSendToBfhdexEvent(&bind.FilterOpts{
		Start: startBlock,
		End:   &endBlock,
	}, nil, nil, nil)
	if err != nil {
		if !isUnknownBlockErr(err) {
			return nil, errors.Wrap(err, "failed to scan past SendToCosmos events from Ethereum")
		} else if iter == nil {
			return nil, errors.New("no iterator returned")
		}
	}

	defer iter.Close()

	var sendToBfhdexEvents []*peggyevents2.WrappersSendToBfhdexEvent
	for iter.Next() {
		sendToBfhdexEvents = append(sendToBfhdexEvents, iter.Event)
	}

	return sendToBfhdexEvents, nil
}

func (n *network) GetPeggyERC20DeployedEvents(startBlock, endBlock uint64) ([]*peggyevents.WrappersERC20DeployedEvent, error) {
	peggyFilterer, err := peggyevents.NewWrappersFilterer(n.Address(), n.Provider())
	if err != nil {
		return nil, errors.Wrap(err, "failed to init Peggy events filterer")
	}

	iter, err := peggyFilterer.FilterERC20DeployedEvent(&bind.FilterOpts{
		Start: startBlock,
		End:   &endBlock,
	}, nil)
	if err != nil {
		if !isUnknownBlockErr(err) {
			return nil, errors.Wrap(err, "failed to scan past TransactionBatchExecuted events from Ethereum")
		} else if iter == nil {
			return nil, errors.New("no iterator returned")
		}
	}

	defer iter.Close()

	var transactionBatchExecutedEvents []*peggyevents.WrappersERC20DeployedEvent
	for iter.Next() {
		transactionBatchExecutedEvents = append(transactionBatchExecutedEvents, iter.Event)
	}

	return transactionBatchExecutedEvents, nil
}

func (n *network) GetValsetUpdatedEvents(startBlock, endBlock uint64) ([]*peggyevents.WrappersValsetUpdatedEvent, error) {
	peggyFilterer, err := peggyevents.NewWrappersFilterer(n.Address(), n.Provider())
	if err != nil {
		return nil, errors.Wrap(err, "failed to init Peggy events filterer")
	}

	iter, err := peggyFilterer.FilterValsetUpdatedEvent(&bind.FilterOpts{
		Start: startBlock,
		End:   &endBlock,
	}, nil)
	if err != nil {
		if !isUnknownBlockErr(err) {
			return nil, errors.Wrap(err, "failed to scan past ValsetUpdatedEvent events from Ethereum")
		} else if iter == nil {
			return nil, errors.New("no iterator returned")
		}
	}

	defer iter.Close()

	var valsetUpdatedEvents []*peggyevents.WrappersValsetUpdatedEvent
	for iter.Next() {
		valsetUpdatedEvents = append(valsetUpdatedEvents, iter.Event)
	}

	return valsetUpdatedEvents, nil
}

func (n *network) GetTransactionBatchExecutedEvents(startBlock, endBlock uint64) ([]*peggyevents.WrappersTransactionBatchExecutedEvent, error) {
	peggyFilterer, err := peggyevents.NewWrappersFilterer(n.Address(), n.Provider())
	if err != nil {
		return nil, errors.Wrap(err, "failed to init Peggy events filterer")
	}

	iter, err := peggyFilterer.FilterTransactionBatchExecutedEvent(&bind.FilterOpts{
		Start: startBlock,
		End:   &endBlock,
	}, nil, nil)
	if err != nil {
		if !isUnknownBlockErr(err) {
			return nil, errors.Wrap(err, "failed to scan past TransactionBatchExecuted events from Ethereum")
		} else if iter == nil {
			return nil, errors.New("no iterator returned")
		}
	}

	defer iter.Close()

	var transactionBatchExecutedEvents []*peggyevents.WrappersTransactionBatchExecutedEvent
	for iter.Next() {
		transactionBatchExecutedEvents = append(transactionBatchExecutedEvents, iter.Event)
	}

	return transactionBatchExecutedEvents, nil
}

func isUnknownBlockErr(err error) bool {
	// Geth error
	if strings.Contains(err.Error(), "unknown block") {
		return true
	}

	// Parity error
	if strings.Contains(err.Error(), "One of the blocks specified in filter") {
		return true
	}

	return false
}
