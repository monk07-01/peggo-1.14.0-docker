package orchestrator

import (
	"context"
	"time"

	"github.com/avast/retry-go"
	cosmostypes "github.com/cosmos/cosmos-sdk/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	log "github.com/xlab/suplog"

	"github.com/InjectiveLabs/metrics"
	"github.com/monk07-01/peggo1.14/orchestrator/cosmos"
	"github.com/monk07-01/peggo1.14/orchestrator/ethereum"
	"github.com/monk07-01/peggo1.14/orchestrator/loops"
)

const (
	defaultLoopDur = 60 * time.Second
)

// PriceFeed provides token price for a given contract address
type PriceFeed interface {
	QueryUSDPrice(address gethcommon.Address) (float64, error)
}

type Config struct {
	CosmosAddr           cosmostypes.AccAddress
	EthereumAddr         gethcommon.Address
	MinBatchFeeUSD       float64
	ERC20ContractMapping map[gethcommon.Address]string
	RelayValsetOffsetDur time.Duration
	RelayBatchOffsetDur  time.Duration
	RelayValsets         bool
	RelayBatches         bool
	RelayerMode          bool
}

type Orchestrator struct {
	logger      log.Logger
	svcTags     metrics.Tags
	cfg         Config
	maxAttempts uint

	bfhdex cosmos.Network
	ethereum  ethereum.Network
	priceFeed PriceFeed
}

func NewOrchestrator(
	bfh cosmos.Network,
	eth ethereum.Network,
	priceFeed PriceFeed,
	cfg Config,
) (*Orchestrator, error) {
	o := &Orchestrator{
		logger:      log.DefaultLogger,
		svcTags:     metrics.Tags{"svc": "peggy_orchestrator"},
		bfhdex:   bfh,
		ethereum:    eth,
		priceFeed:   priceFeed,
		cfg:         cfg,
		maxAttempts: 10,
	}

	return o, nil
}

// Run starts all major loops required to make
// up the Orchestrator, all of these are async loops.
func (s *Orchestrator) Run(ctx context.Context, bfh cosmos.Network, eth ethereum.Network) error {
	if s.cfg.RelayerMode {
		return s.startRelayerMode(ctx, bfh, eth)
	}

	return s.startValidatorMode(ctx, bfh, eth)
}

// startValidatorMode runs all orchestrator processes. This is called
// when peggo is run alongside a validator bfhdex node.
func (s *Orchestrator) startValidatorMode(ctx context.Context, bfh cosmos.Network, eth ethereum.Network) error {
	log.Infoln("running orchestrator in validator mode")

	lastObservedEthBlock, _ := s.getLastClaimBlockHeight(ctx, bfh)
	if lastObservedEthBlock == 0 {
		peggyParams, err := bfh.PeggyParams(ctx)
		if err != nil {
			s.logger.WithError(err).Fatalln("unable to query peggy module params, is dex-cored running?")
		}

		lastObservedEthBlock = peggyParams.BridgeContractStartHeight
	}

	// get peggy ID from contract
	peggyContractID, err := eth.GetPeggyID(ctx)
	if err != nil {
		s.logger.WithError(err).Fatalln("unable to query peggy ID from contract")
	}

	var pg loops.ParanoidGroup

	pg.Go(func() error { return s.runOracle(ctx, lastObservedEthBlock) })
	pg.Go(func() error { return s.runSigner(ctx, peggyContractID) })
	pg.Go(func() error { return s.runBatchCreator(ctx) })
	pg.Go(func() error { return s.runRelayer(ctx) })

	return pg.Wait()
}

// startRelayerMode runs orchestrator processes that only relay specific
// messages that do not require a validator's signature. This mode is run
// alongside a non-validator bfhdex node
func (s *Orchestrator) startRelayerMode(ctx context.Context, bfh cosmos.Network, eth ethereum.Network) error {
	log.Infoln("running orchestrator in relayer mode")

	var pg loops.ParanoidGroup

	pg.Go(func() error { return s.runBatchCreator(ctx) })
	pg.Go(func() error { return s.runRelayer(ctx) })

	return pg.Wait()
}

func (s *Orchestrator) getLastClaimBlockHeight(ctx context.Context, bfh cosmos.Network) (uint64, error) {
	claim, err := bfh.LastClaimEventByAddr(ctx, s.cfg.CosmosAddr)
	if err != nil {
		return 0, err
	}

	return claim.EthereumEventHeight, nil
}

func (s *Orchestrator) retry(ctx context.Context, fn func() error) error {
	return retry.Do(fn,
		retry.Context(ctx),
		retry.Attempts(s.maxAttempts),
		retry.OnRetry(func(n uint, err error) {
			s.logger.WithError(err).Warningf("loop error, retrying... (#%d)", n+1)
		}))
}
