package pricefeed

import (
	"math/big"
	"testing"

	// cosmtypes "github.com/cosmos/cosmos-sdk/types"
	math "cosmossdk.io/math"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestFeeThresholdTwoDecimals(t *testing.T) {
	// https://api.coingecko.com/api/v3/simple/token_price/ethereum?contract_addresses=0xe28b3b32b6c345a34ff64674606124dd5aceca30&vs_currencies=usd

	bfhTokenContract := common.HexToAddress("0xe28b3b32b6c345a34ff64674606124dd5aceca30")
	coingeckoFeed := NewCoingeckoPriceFeed(100, &Config{})
	currentTokenPrice, _ := coingeckoFeed.QueryUSDPrice(bfhTokenContract) // "usd":9.35

	minFeeInUSD := float64(23.5) // 23.5 USD to submit batch tx
	minBfh := minFeeInUSD / currentTokenPrice
	var DecimalReduction = math.NewIntFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

	// FeeAccumulated is greater than ExpectedFee
	totalFeeInBFH := math.NewInt(int64(minBfh) + 1).Mul(DecimalReduction)
	isFeeLimitExceeded := coingeckoFeed.CheckFeeThreshold(bfhTokenContract, totalFeeInBFH, minFeeInUSD)
	assert.True(t, isFeeLimitExceeded, "FeeAccumulated is less than ExpectedFee")

	// FeeAccumulated is less than ExpectedFee
	totalFeeInBFH = math.NewInt(int64(minBfh) - 1).Mul(DecimalReduction)
	isFeeLimitExceeded = coingeckoFeed.CheckFeeThreshold(bfhTokenContract, totalFeeInBFH, minFeeInUSD)
	assert.False(t, isFeeLimitExceeded, "FeeAccumulated is greater than ExpectedFee")
}

func TestFeeThresholdNineDecimals(t *testing.T) {
	// https://api.coingecko.com/api/v3/simple/token_price/ethereum?contract_addresses=0x95ad61b0a150d79219dcf64e1e6cc01f0b64c4ce&vs_currencies=usd
	shibTokenContract := common.HexToAddress("0x95ad61b0a150d79219dcf64e1e6cc01f0b64c4ce")
	coingeckoFeed := NewCoingeckoPriceFeed(100, &Config{})
	currentTokenPrice, _ := coingeckoFeed.QueryUSDPrice(shibTokenContract) // "usd":0.000008853

	minFeeInUSD := float64(23.5) // 23.5 USD to submit batch tx
	minShib := minFeeInUSD / currentTokenPrice
	var DecimalReduction = math.NewIntFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

	// FeeAccumulated is greater than ExpectedFee
	totalFeeInSHIB := math.NewInt(int64(minShib) + 1).Mul(DecimalReduction)
	isFeeLimitExceeded := coingeckoFeed.CheckFeeThreshold(shibTokenContract, totalFeeInSHIB, minFeeInUSD)
	assert.True(t, isFeeLimitExceeded, "FeeAccumulated is less than ExpectedFee")

	// FeeAccumulated is less than ExpectedFee
	totalFeeInSHIB = math.NewInt(int64(minShib) - 1).Mul(DecimalReduction)
	isFeeLimitExceeded = coingeckoFeed.CheckFeeThreshold(shibTokenContract, totalFeeInSHIB, minFeeInUSD)
	assert.False(t, isFeeLimitExceeded, "FeeAccumulated is greater than ExpectedFee")
}
