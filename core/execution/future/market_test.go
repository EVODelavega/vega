// Copyright (C) 2023 Gobalsky Labs Limited
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package future_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	bmocks "code.vegaprotocol.io/vega/core/broker/mocks"
	"code.vegaprotocol.io/vega/core/collateral"
	"code.vegaprotocol.io/vega/core/datasource"
	dstypes "code.vegaprotocol.io/vega/core/datasource/common"
	dserrors "code.vegaprotocol.io/vega/core/datasource/errors"
	"code.vegaprotocol.io/vega/core/datasource/external/signedoracle"
	"code.vegaprotocol.io/vega/core/datasource/spec"
	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/execution/common"
	"code.vegaprotocol.io/vega/core/execution/common/mocks"
	"code.vegaprotocol.io/vega/core/execution/future"
	"code.vegaprotocol.io/vega/core/fee"
	fmocks "code.vegaprotocol.io/vega/core/fee/mocks"
	"code.vegaprotocol.io/vega/core/idgeneration"
	"code.vegaprotocol.io/vega/core/integration/stubs"
	"code.vegaprotocol.io/vega/core/liquidity/v2"
	"code.vegaprotocol.io/vega/core/matching"
	"code.vegaprotocol.io/vega/core/monitor"
	"code.vegaprotocol.io/vega/core/positions"
	"code.vegaprotocol.io/vega/core/risk"
	"code.vegaprotocol.io/vega/core/settlement"
	"code.vegaprotocol.io/vega/core/types"
	vegacontext "code.vegaprotocol.io/vega/libs/context"
	"code.vegaprotocol.io/vega/libs/crypto"
	vgcrypto "code.vegaprotocol.io/vega/libs/crypto"
	"code.vegaprotocol.io/vega/libs/num"
	"code.vegaprotocol.io/vega/logging"
	proto "code.vegaprotocol.io/vega/protos/vega"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	datapb "code.vegaprotocol.io/vega/protos/vega/data/v1"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	MAXMOVEUP   = num.DecimalFromFloat(1000)
	MINMOVEDOWN = num.DecimalFromFloat(500)
)

func peggedOrderCounterForTest(int64) {}

var defaultCollateralAssets = []types.Asset{
	{
		ID: "ETH",
		Details: &types.AssetDetails{
			Symbol:  "ETH",
			Quantum: num.DecimalOne(),
		},
	},
	{
		ID: "VOTE",
		Details: &types.AssetDetails{
			Name:     "VOTE",
			Symbol:   "VOTE",
			Decimals: 5,
			Quantum:  num.DecimalOne(),
			Source: &types.AssetDetailsBuiltinAsset{
				BuiltinAsset: &types.BuiltinAsset{},
			},
		},
	},
}

var defaultPriceMonitorSettings = &types.PriceMonitoringSettings{
	Parameters: &types.PriceMonitoringParameters{
		Triggers: []*types.PriceMonitoringTrigger{
			{
				Horizon:          600,
				HorizonDec:       num.MustDecimalFromString("600"),
				Probability:      num.DecimalFromFloat(0.99),
				AuctionExtension: 120,
			},
		},
	},
}

func newTestIDGenerator() common.IDGenerator {
	return idgeneration.New(vgcrypto.RandomHash())
}

type marketW struct {
	*future.Market
}

func (m *marketW) SubmitOrder(
	ctx context.Context,
	order *types.Order,
) (*types.OrderConfirmation, error) {
	conf, err := m.Market.SubmitOrder(ctx, order.IntoSubmission(), order.Party, vgcrypto.RandomHash())
	if err == nil {
		*order = *conf.Order.Clone()
	}
	return conf, err
}

func (m *marketW) SubmitOrderWithHash(
	ctx context.Context,
	order *types.Order,
	hash string,
) (*types.OrderConfirmation, error) {
	conf, err := m.Market.SubmitOrder(ctx, order.IntoSubmission(), order.Party, hash)
	if err == nil {
		*order = *conf.Order.Clone()
	}
	return conf, err
}

type testMarket struct {
	t *testing.T

	market           *marketW
	log              *logging.Logger
	ctrl             *gomock.Controller
	collateralEngine *collateral.Engine
	broker           *bmocks.MockBroker
	timeService      *mocks.MockTimeService
	now              time.Time
	asset            string
	mas              *monitor.AuctionState
	eventCount       uint64
	orderEventCount  uint64
	events           []events.Event
	orderEvents      []events.Event
	mktCfg           *types.Market
	oracleEngine     *spec.Engine
	stateVar         *stubs.StateVarStub
	builtinOracle    *spec.Builtin

	// Options
	Assets []types.Asset
}

func newTestMarket(t *testing.T, now time.Time) *testMarket {
	t.Helper()
	ctrl := gomock.NewController(t)
	tm := &testMarket{
		t:    t,
		ctrl: ctrl,
		log:  logging.NewTestLogger(),
		now:  now,
	}

	// Setup Mocking Expectations
	tm.broker = bmocks.NewMockBroker(ctrl)
	tm.timeService = mocks.NewMockTimeService(ctrl)
	tm.timeService.EXPECT().GetTimeNow().DoAndReturn(
		func() time.Time {
			return tm.now
		}).AnyTimes()

	// eventFn records and count events and orderEvents
	eventFn := func(evt events.Event) {
		if evt.Type() == events.OrderEvent {
			tm.orderEventCount++
			tm.orderEvents = append(tm.orderEvents, evt)
		}
		tm.eventCount++
		tm.events = append(tm.events, evt)
	}
	// eventsFn is the same as eventFn above but handles []event
	eventsFn := func(evts []events.Event) {
		for _, evt := range evts {
			eventFn(evt)
		}
	}

	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes().Do(eventFn)
	tm.broker.EXPECT().SendBatch(gomock.Any()).AnyTimes().Do(eventsFn)
	tm.oracleEngine = spec.NewEngine(tm.log, spec.NewDefaultConfig(), tm.timeService, tm.broker)
	tm.builtinOracle = spec.NewBuiltin(tm.oracleEngine, tm.timeService)
	return tm
}

func (tm *testMarket) Run(ctx context.Context, mktCfg types.Market) *testMarket {
	collateralEngine := collateral.New(tm.log, collateral.NewDefaultConfig(), tm.timeService, tm.broker)
	// create asset with same decimal places as the market asset
	mktAssets, _ := mktCfg.GetAssets()
	cfgAsset := NewAssetStub(mktAssets[0], mktCfg.DecimalPlaces)
	assets := tm.Assets
	if len(assets) == 0 {
		assets = defaultCollateralAssets
	}
	for _, asset := range assets {
		err := collateralEngine.EnableAsset(ctx, asset)
		require.NoError(tm.t, err)
	}

	var (
		riskConfig       = risk.NewDefaultConfig()
		positionConfig   = positions.NewDefaultConfig()
		settlementConfig = settlement.NewDefaultConfig()
		matchingConfig   = matching.NewDefaultConfig()
		feeConfig        = fee.NewDefaultConfig()
		liquidityConfig  = liquidity.NewDefaultConfig()
	)
	positionConfig.StreamPositionVerbose = true

	oracleEngine := spec.NewEngine(tm.log, spec.NewDefaultConfig(), tm.timeService, tm.broker)

	mas := monitor.NewAuctionState(&mktCfg, tm.now)
	monitor.NewAuctionState(&mktCfg, tm.now)

	statevarEngine := stubs.NewStateVar()
	epochEngine := mocks.NewMockEpochEngine(tm.ctrl)
	epochEngine.EXPECT().NotifyOnEpoch(gomock.Any(), gomock.Any()).Times(1)

	teams := mocks.NewMockTeams(tm.ctrl)
	bc := mocks.NewMockAccountBalanceChecker(tm.ctrl)
	broker := bmocks.NewMockBroker(tm.ctrl)
	marketActivityTracker := common.NewMarketActivityTracker(logging.NewTestLogger(), teams, bc, broker, collateralEngine)
	epochEngine.NotifyOnEpoch(marketActivityTracker.OnEpochEvent, marketActivityTracker.OnEpochRestore)

	referralDiscountReward := fmocks.NewMockReferralDiscountRewardService(tm.ctrl)
	volumeDiscount := fmocks.NewMockVolumeDiscountService(tm.ctrl)
	volumeRebate := fmocks.NewMockVolumeRebateService(tm.ctrl)
	referralDiscountReward.EXPECT().GetReferrer(gomock.Any()).Return(types.PartyID(""), errors.New("no referrer")).AnyTimes()
	referralDiscountReward.EXPECT().ReferralDiscountFactorsForParty(gomock.Any()).Return(types.EmptyFactors).AnyTimes()
	referralDiscountReward.EXPECT().RewardsFactorsMultiplierAppliedForParty(gomock.Any()).Return(types.EmptyFactors).AnyTimes()
	volumeDiscount.EXPECT().VolumeDiscountFactorForParty(gomock.Any()).Return(types.EmptyFactors).AnyTimes()
	volumeRebate.EXPECT().VolumeRebateFactorForParty(gomock.Any()).Return(num.DecimalZero()).AnyTimes()
	banking := mocks.NewMockBanking(tm.ctrl)
	parties := mocks.NewMockParties(tm.ctrl)

	mktEngine, err := future.NewMarket(ctx,
		tm.log, riskConfig, positionConfig, settlementConfig, matchingConfig,
		feeConfig, liquidityConfig, collateralEngine, oracleEngine, &mktCfg, tm.timeService, tm.broker, mas, statevarEngine, marketActivityTracker, cfgAsset,
		peggedOrderCounterForTest, referralDiscountReward, volumeDiscount, volumeRebate, banking, parties,
	)
	require.NoError(tm.t, err)

	settlementAssets, err := mktCfg.GetAssets()
	require.NoError(tm.t, err)

	_, _, err = collateralEngine.CreateMarketAccounts(ctx, mktEngine.GetID(), settlementAssets[0])
	require.NoError(tm.t, err)

	tm.market = &marketW{mktEngine}
	tm.market.UpdateRiskFactorsForTest()
	tm.collateralEngine = collateralEngine
	tm.asset = settlementAssets[0]
	tm.mas = mas
	tm.mktCfg = &mktCfg
	tm.stateVar = statevarEngine

	// Reset event counters
	tm.eventCount = 0
	tm.orderEventCount = 0

	return tm
}

func (tm *testMarket) EndOpeningAuction(t *testing.T, auctionEnd time.Time, setMarkPrice bool) {
	t.Helper()
	var (
		party0 = "clearing-auction-party0"
		party1 = "clearing-auction-party1"
		party2 = "lpprov-party"
	)

	// parties used for clearing opening auction
	tm.WithAccountAndAmount(party0, 1000000).
		WithAccountAndAmount(party1, 1000000).
		WithAccountAndAmount(party2, 90000000000) // LP needs a lot of balance

	auctionOrders := []*types.Order{
		// Limit Orders
		{
			Type:        types.OrderTypeLimit,
			Size:        5,
			Remaining:   5,
			Price:       num.NewUint(1000),
			Side:        types.SideBuy,
			Party:       party0,
			TimeInForce: types.OrderTimeInForceGTC,
		},
		{
			Type:        types.OrderTypeLimit,
			Size:        5,
			Remaining:   5,
			Price:       num.NewUint(1000),
			Side:        types.SideSell,
			Party:       party1,
			TimeInForce: types.OrderTimeInForceGTC,
		},
		{
			Type:        types.OrderTypeLimit,
			Size:        1,
			Remaining:   1,
			Price:       num.NewUint(900),
			Side:        types.SideBuy,
			Party:       party0,
			TimeInForce: types.OrderTimeInForceGTC,
		},
		{
			Type:        types.OrderTypeLimit,
			Size:        1,
			Remaining:   1,
			Price:       num.NewUint(1100),
			Side:        types.SideSell,
			Party:       party1,
			TimeInForce: types.OrderTimeInForceGTC,
		},
	}
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	// submit the auctions orders & LP
	tm.WithSubmittedOrders(t, auctionOrders...)
	// update the time to get out of auction
	if setMarkPrice {
		// now set the markprice
		mpOrders := []*types.Order{
			{
				Type:        types.OrderTypeLimit,
				Size:        1,
				Remaining:   1,
				Price:       num.NewUint(900),
				Side:        types.SideSell,
				Party:       party1,
				TimeInForce: types.OrderTimeInForceGTC,
			},
			{
				Type:        types.OrderTypeLimit,
				Size:        1,
				Remaining:   1,
				Price:       num.NewUint(2500),
				Side:        types.SideBuy,
				Party:       party0,
				TimeInForce: types.OrderTimeInForceGTC,
			},
		}
		// submit the auctions orders
		tm.WithSubmittedOrders(t, mpOrders...)
	}

	tm.now = auctionEnd
	tm.market.OnTick(ctx, auctionEnd)

	assert.Equal(t,
		tm.market.GetMarketData().MarketTradingMode,
		types.MarketTradingModeContinuous,
	)
}

func (tm *testMarket) EndOpeningAuction2(t *testing.T, auctionEnd time.Time, setMarkPrice bool) {
	t.Helper()
	var (
		party0 = "clearing-auction-party0"
		party1 = "clearing-auction-party1"
	)

	// parties used for clearing opening auction
	tm.WithAccountAndAmount(party0, 1000000).
		WithAccountAndAmount(party1, 1000000)

	auctionOrders := []*types.Order{
		// Limit Orders
		{
			Type:        types.OrderTypeLimit,
			Size:        5,
			Remaining:   5,
			Price:       num.NewUint(1000),
			Side:        types.SideBuy,
			Party:       party0,
			TimeInForce: types.OrderTimeInForceGTC,
		},
		{
			Type:        types.OrderTypeLimit,
			Size:        5,
			Remaining:   5,
			Price:       num.NewUint(1000),
			Side:        types.SideSell,
			Party:       party1,
			TimeInForce: types.OrderTimeInForceGTC,
		},
		{
			Type:        types.OrderTypeLimit,
			Size:        1,
			Remaining:   1,
			Price:       num.NewUint(900),
			Side:        types.SideBuy,
			Party:       party0,
			TimeInForce: types.OrderTimeInForceGTC,
		},
		{
			Type:        types.OrderTypeLimit,
			Size:        1,
			Remaining:   1,
			Price:       num.NewUint(1200),
			Side:        types.SideSell,
			Party:       party1,
			TimeInForce: types.OrderTimeInForceGTC,
		},
	}

	// submit the auctions orders
	tm.WithSubmittedOrders(t, auctionOrders...)

	// update the time to get out of auction
	tm.market.OnTick(context.Background(), auctionEnd)

	assert.Equal(t,
		tm.market.GetMarketData().MarketTradingMode,
		types.MarketTradingModeContinuous,
	)

	if setMarkPrice {
		// now set the markprice
		mpOrders := []*types.Order{
			{
				Type:        types.OrderTypeLimit,
				Size:        1,
				Remaining:   1,
				Price:       num.NewUint(900),
				Side:        types.SideSell,
				Party:       party1,
				TimeInForce: types.OrderTimeInForceGTC,
			},
			{
				Type:        types.OrderTypeLimit,
				Size:        1,
				Remaining:   1,
				Price:       num.NewUint(1200),
				Side:        types.SideBuy,
				Party:       party0,
				TimeInForce: types.OrderTimeInForceGTC,
			},
		}
		// submit the auctions orders
		tm.WithSubmittedOrders(t, mpOrders...)
	}
}

func mustOrderFromProto(o *vegapb.Order) *types.Order {
	order, _ := types.OrderFromProto(o)
	return order
}

func (tm *testMarket) lastOrderUpdate(id string) *types.Order {
	var order *types.Order
	cancel := false
	for _, e := range tm.events {
		switch evt := e.(type) {
		case *events.Order:
			ord := evt.Order()
			if ord.Id == id {
				order = mustOrderFromProto(ord)
			}
		case *events.CancelledOrders:
			cancel = true
		}
	}
	if cancel {
		order.Status = types.OrderStatusCancelled
	}
	return order
}

func (tm *testMarket) StartOpeningAuction() *testMarket {
	err := tm.market.StartOpeningAuction(context.Background())
	require.NoError(tm.t, err)
	return tm
}

func (tm *testMarket) WithAccountAndAmount(id string, amount uint64) *testMarket {
	addAccountWithAmount(tm, id, amount)
	return tm
}

func (tm *testMarket) PartyGeneralAccount(t *testing.T, party string) *types.Account {
	t.Helper()
	acc, err := tm.collateralEngine.GetPartyGeneralAccount(party, tm.asset)
	require.NoError(t, err)
	require.NotNil(t, acc)
	return acc
}

func (tm *testMarket) PartyMarginAccount(t *testing.T, party string) *types.Account {
	t.Helper()
	acc, err := tm.collateralEngine.GetPartyMarginAccount(tm.market.GetID(), party, tm.asset)
	require.NoError(t, err)
	require.NotNil(t, acc)
	return acc
}

func getTestMarket(
	t *testing.T,
	now time.Time,
	pMonitorSettings *types.PriceMonitoringSettings,
	openingAuctionDuration *types.AuctionDuration,
) *testMarket {
	t.Helper()
	return getTestMarket2(t, now, pMonitorSettings, openingAuctionDuration, true, 0.99)
}

func getTestMarketWithDP(
	t *testing.T,
	now time.Time,
	pMonitorSettings *types.PriceMonitoringSettings,
	openingAuctionDuration *types.AuctionDuration,
	decimalPlaces uint64,
	lpRange float64,
) *testMarket {
	t.Helper()
	return getTestMarket2WithDP(t, now, pMonitorSettings, openingAuctionDuration, true, decimalPlaces, lpRange)
}

func getTestMarket2(
	t *testing.T,
	now time.Time,
	pMonitorSettings *types.PriceMonitoringSettings,
	openingAuctionDuration *types.AuctionDuration,
	startOpeningAuction bool,
	lpRange float64,
) *testMarket {
	t.Helper()
	return getTestMarket2WithDP(t, now, pMonitorSettings, openingAuctionDuration, startOpeningAuction, 1, lpRange)
}

func getTestMarket2WithDP(
	t *testing.T,
	now time.Time,
	pMonitorSettings *types.PriceMonitoringSettings,
	openingAuctionDuration *types.AuctionDuration,
	startOpeningAuction bool,
	decimalPlaces uint64,
	lpRange float64,
) *testMarket {
	t.Helper()
	ctrl := gomock.NewController(t)
	log := logging.NewTestLogger()
	riskConfig := risk.NewDefaultConfig()
	positionConfig := positions.NewDefaultConfig()
	settlementConfig := settlement.NewDefaultConfig()
	matchingConfig := matching.NewDefaultConfig()
	feeConfig := fee.NewDefaultConfig()
	liquidityConfig := liquidity.NewDefaultConfig()
	broker := bmocks.NewMockBroker(ctrl)
	timeService := mocks.NewMockTimeService(ctrl)

	tm := &testMarket{
		log:         log,
		ctrl:        ctrl,
		broker:      broker,
		timeService: timeService,
		now:         now,
	}

	timeService.EXPECT().GetTimeNow().DoAndReturn(
		func() time.Time {
			return tm.now
		}).AnyTimes()

	handleEvent := func(evt events.Event) {
		te := evt.Type()
		if te == events.OrderEvent {
			tm.orderEventCount++
			tm.orderEvents = append(tm.orderEvents, evt)
		}
		tm.eventCount++
		tm.events = append(tm.events, evt)
	}

	// catch all expected calls
	broker.EXPECT().SendBatch(gomock.Any()).AnyTimes().Do(
		func(evts []events.Event) {
			for _, evt := range evts {
				handleEvent(evt)
			}
		},
	)

	broker.EXPECT().Send(gomock.Any()).AnyTimes().Do(handleEvent)

	collateralEngine := collateral.New(log, collateral.NewDefaultConfig(), timeService, broker)
	err := collateralEngine.EnableAsset(context.Background(), types.Asset{
		ID: "ETH",
		Details: &types.AssetDetails{
			Symbol:   "ETH",
			Decimals: 0, // no decimals
			Quantum:  num.DecimalOne(),
		},
	})
	require.NoError(t, err)
	// create asset stub to match the test asset:
	cfgAsset := NewAssetStub("ETH", 0)

	oracleEngine := spec.NewEngine(log, spec.NewDefaultConfig(), timeService, broker)
	tm.oracleEngine = oracleEngine
	tm.builtinOracle = spec.NewBuiltin(tm.oracleEngine, tm.timeService)

	// add the token asset
	tokAsset := types.Asset{
		ID: "VOTE",
		Details: &types.AssetDetails{
			Name:     "VOTE",
			Symbol:   "VOTE",
			Decimals: 5,
			Quantum:  num.DecimalOne(),
			Source: &types.AssetDetailsBuiltinAsset{
				BuiltinAsset: &types.BuiltinAsset{},
			},
		},
	}

	err = collateralEngine.EnableAsset(context.Background(), tokAsset)
	if pMonitorSettings == nil {
		pMonitorSettings = &types.PriceMonitoringSettings{
			Parameters: &types.PriceMonitoringParameters{
				Triggers: []*types.PriceMonitoringTrigger{},
			},
		}
	}
	require.NoError(t, err)
	mkt := getMarketWithDP(pMonitorSettings, openingAuctionDuration, decimalPlaces, lpRange)
	// ensure nextMTM is happening every block
	mktCfg := &mkt
	mktCfg.DecimalPlaces = cfgAsset.DecimalPlaces()

	mas := monitor.NewAuctionState(mktCfg, now)
	statevar := stubs.NewStateVar()

	epoch := mocks.NewMockEpochEngine(ctrl)
	epoch.EXPECT().NotifyOnEpoch(gomock.Any(), gomock.Any()).Times(1)
	teams := mocks.NewMockTeams(tm.ctrl)
	bc := mocks.NewMockAccountBalanceChecker(tm.ctrl)
	marketActivityTracker := common.NewMarketActivityTracker(logging.NewTestLogger(), teams, bc, broker, collateralEngine)
	epoch.NotifyOnEpoch(marketActivityTracker.OnEpochEvent, marketActivityTracker.OnEpochRestore)

	referralDiscountReward := fmocks.NewMockReferralDiscountRewardService(tm.ctrl)
	volumeDiscount := fmocks.NewMockVolumeDiscountService(tm.ctrl)
	volumeRebate := fmocks.NewMockVolumeRebateService(tm.ctrl)

	referralDiscountReward.EXPECT().GetReferrer(gomock.Any()).Return(types.PartyID(""), errors.New("no referrer")).AnyTimes()
	referralDiscountReward.EXPECT().ReferralDiscountFactorsForParty(gomock.Any()).Return(types.EmptyFactors).AnyTimes()
	referralDiscountReward.EXPECT().RewardsFactorsMultiplierAppliedForParty(gomock.Any()).Return(types.EmptyFactors).AnyTimes()
	volumeDiscount.EXPECT().VolumeDiscountFactorForParty(gomock.Any()).Return(types.EmptyFactors).AnyTimes()
	volumeRebate.EXPECT().VolumeRebateFactorForParty(gomock.Any()).Return(num.DecimalZero()).AnyTimes()
	banking := mocks.NewMockBanking(ctrl)
	parties := mocks.NewMockParties(ctrl)

	mktEngine, err := future.NewMarket(context.Background(),
		log, riskConfig, positionConfig, settlementConfig, matchingConfig,
		feeConfig, liquidityConfig, collateralEngine, oracleEngine, mktCfg, timeService, broker, mas, statevar, marketActivityTracker, cfgAsset,
		peggedOrderCounterForTest, referralDiscountReward, volumeDiscount, volumeRebate, banking, parties)
	if err != nil {
		t.Fatalf("couldn't create a market: %v", err)
	}
	// ensure MTM settlements happen every block
	mktEngine.OnMarkPriceUpdateMaximumFrequency(context.Background(), time.Duration(0))
	mktEngine.UpdateRiskFactorsForTest()

	if startOpeningAuction {
		d := time.Second
		if openingAuctionDuration != nil {
			d = time.Duration(openingAuctionDuration.Duration) * time.Second
		}
		mktEngine.OnMarketAuctionMinimumDurationUpdate(context.Background(), d)
		err := mktEngine.StartOpeningAuction(context.Background())
		require.NoError(t, err)
	}

	settlementAssets, err := mkt.GetAssets()
	assert.NoError(t, err)

	// ignore response ids here + this cannot fail
	_, _, err = collateralEngine.CreateMarketAccounts(context.Background(), mktEngine.GetID(), settlementAssets[0])
	assert.NoError(t, err)

	tm.market = &marketW{mktEngine}
	tm.collateralEngine = collateralEngine
	tm.asset = settlementAssets[0]
	tm.mas = mas
	tm.mktCfg = mktCfg
	tm.stateVar = statevar

	// Reset event counters
	tm.eventCount = 0
	tm.orderEventCount = 0

	return tm
}

func getMarket(pMonitorSettings *types.PriceMonitoringSettings, openingAuctionDuration *types.AuctionDuration) types.Market {
	return getMarketWithDP(pMonitorSettings, openingAuctionDuration, 1, 0.99)
}

func getMarketWithDP(pMonitorSettings *types.PriceMonitoringSettings, openingAuctionDuration *types.AuctionDuration, decimalPlaces uint64, _ float64) types.Market {
	pubKeys := []*dstypes.Signer{
		dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
	}

	mkt := types.Market{
		ID:            vgcrypto.RandomHash(),
		DecimalPlaces: decimalPlaces,
		Fees: &types.Fees{
			Factors: &types.FeeFactors{
				InfrastructureFee: num.DecimalFromFloat(0.001),
				MakerFee:          num.DecimalFromFloat(0.004),
			},
			LiquidityFeeSettings: &types.LiquidityFeeSettings{
				Method: types.LiquidityFeeMethodMarginalCost,
			},
		},
		TradableInstrument: &types.TradableInstrument{
			Instrument: &types.Instrument{
				ID:   "Crypto/ETHUSD/Futures/Dec19",
				Code: "CRYPTO:ETHUSD/DEC19",
				Name: "December 2019 ETH vs USD future",
				Metadata: &types.InstrumentMetadata{
					Tags: []string{
						"asset_class:fx/crypto",
						"product:futures",
					},
				},
				Product: &types.InstrumentFuture{
					Future: &types.Future{
						SettlementAsset: "ETH",
						QuoteName:       "USD",
						DataSourceSpecForSettlementData: &datasource.Spec{
							ID: "1",
							Data: datasource.NewDefinition(
								datasource.ContentTypeOracle,
							).SetOracleConfig(
								&signedoracle.SpecConfiguration{
									Signers: pubKeys,
									Filters: []*dstypes.SpecFilter{
										{
											Key: &dstypes.SpecPropertyKey{
												Name: "prices.ETH.value",
												Type: datapb.PropertyKey_TYPE_INTEGER,
											},
											Conditions: []*dstypes.SpecCondition{},
										},
									},
								},
							),
						},
						DataSourceSpecForTradingTermination: &datasource.Spec{
							ID: "2",
							Data: datasource.NewDefinition(
								datasource.ContentTypeOracle,
							).SetOracleConfig(
								&signedoracle.SpecConfiguration{
									Signers: pubKeys,
									Filters: []*dstypes.SpecFilter{
										{
											Key: &dstypes.SpecPropertyKey{
												Name: "trading.terminated",
												Type: datapb.PropertyKey_TYPE_BOOLEAN,
											},
											Conditions: []*dstypes.SpecCondition{},
										},
									},
								},
							),
						},
						DataSourceSpecBinding: &datasource.SpecBindingForFuture{
							SettlementDataProperty:     "prices.ETH.value",
							TradingTerminationProperty: "trading.terminated",
						},
					},
				},
			},
			MarginCalculator: &types.MarginCalculator{
				ScalingFactors: &types.ScalingFactors{
					SearchLevel:       num.DecimalFromFloat(1.1),
					InitialMargin:     num.DecimalFromFloat(1.2),
					CollateralRelease: num.DecimalFromFloat(1.4),
				},
			},
			RiskModel: &types.TradableInstrumentSimpleRiskModel{
				SimpleRiskModel: &types.SimpleRiskModel{
					Params: &types.SimpleModelParams{
						FactorLong:           num.DecimalFromFloat(0.15),
						FactorShort:          num.DecimalFromFloat(0.25),
						MaxMoveUp:            MAXMOVEUP,
						MinMoveDown:          MINMOVEDOWN,
						ProbabilityOfTrading: num.DecimalFromFloat(0.1),
					},
				},
			},
		},
		OpeningAuction:          openingAuctionDuration,
		PriceMonitoringSettings: pMonitorSettings,
		LiquidityMonitoringParameters: &types.LiquidityMonitoringParameters{
			TargetStakeParameters: &types.TargetStakeParameters{
				TimeWindow:    3600, // seconds = 1h
				ScalingFactor: num.DecimalFromFloat(10),
			},
		},
		LiquiditySLAParams: &types.LiquiditySLAParams{
			PriceRange:                  num.DecimalOne(),
			CommitmentMinTimeFraction:   num.DecimalFromFloat(0.5),
			SlaCompetitionFactor:        num.DecimalOne(),
			PerformanceHysteresisEpochs: 1,
		},
		LinearSlippageFactor:    num.DecimalFromFloat(0.1),
		QuadraticSlippageFactor: num.DecimalFromFloat(0.1),
		MarkPriceConfiguration: &types.CompositePriceConfiguration{
			DecayWeight:        num.DecimalZero(),
			DecayPower:         num.DecimalZero(),
			CashAmount:         num.UintZero(),
			CompositePriceType: types.CompositePriceTypeByLastTrade,
		},
		TickSize: num.UintOne(),
	}

	return mkt
}

func addAccount(t *testing.T, market *testMarket, party string) {
	t.Helper()
	_, err := market.collateralEngine.Deposit(context.Background(), party, market.asset, num.NewUint(1000000000))
	if err != nil {
		t.Fatalf("couldn't deposit asset \"%s\" for party \"%s\": %v", market.asset, party, err)
	}
}

func addAccountWithAmount(market *testMarket, party string, amnt uint64) *types.LedgerMovement {
	r, _ := market.collateralEngine.Deposit(context.Background(), party, market.asset, num.NewUint(amnt))
	return r
}

// WithSubmittedLiquidityProvision Submits a Liquidity Provision and asserts that it was created without errors.
func (tm *testMarket) WithSubmittedLiquidityProvision(t *testing.T, party string, amount uint64, fee string) *testMarket {
	t.Helper()
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	f, _ := num.DecimalFromString(fee)
	lps := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(amount),
		Fee:              f,
	}

	require.NoError(t,
		tm.market.SubmitLiquidityProvision(ctx, lps, party, vgcrypto.RandomHash()),
	)

	return tm
}

// WithSubmittedOrder returns a market with Submitted orders defined in `orders`.
// If one submission fails, it will make the test fail and stop.
func (tm *testMarket) WithSubmittedOrders(t *testing.T, orders ...*types.Order) *testMarket {
	t.Helper()
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	for i, order := range orders {
		order.MarketID = tm.market.GetID()
		_, err := tm.market.SubmitOrder(ctx, order)
		require.NoError(t, err, "Submitting Order(@index#%d): '%s' failed", i, order.String())
	}
	return tm
}

func (tm *testMarket) EventHasBeenEmitted(t *testing.T, e events.Event) {
	t.Helper()
	for _, event := range tm.events {
		if reflect.DeepEqual(e, event) {
			return
		}
	}
	t.Fatalf("Expected event: '%s', has not been emitted", e)
}

func TestMarketClosing(t *testing.T) {
	pubKeys := []*dstypes.Signer{
		dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
	}

	party1 := "party1"
	party2 := "party2"
	lp1 := "lp1"
	lp2 := "lp2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{Duration: 1})
	defer tm.ctrl.Finish()
	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccount(t, tm, lp1)
	addAccount(t, tm, lp2)

	// submit liquidity with varying fee levels
	commitment1 := num.NewUint(30000)
	fee1 := num.DecimalFromFloat(0.01)
	commitment2 := num.NewUint(20000)
	fee2 := num.DecimalFromFloat(0.02)
	lps := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: commitment1,
		Fee:              fee1,
	}

	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lps, lp1, vgcrypto.RandomHash()))
	lps.Fee = fee2
	lps.CommitmentAmount = commitment2
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lps, lp2, vgcrypto.RandomHash()))

	// generate trades so that fees need to be distributed among LPs
	orders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "order1", types.SideSell, lp1, 1000, 110),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "order1", types.SideBuy, lp1, 1000, 90),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "order1", types.SideSell, party1, 1, 100),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "order2", types.SideBuy, party2, 1, 100),
	}
	for _, o := range orders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}

	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)
	md := tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)

	properties := map[string]string{}
	properties["trading.terminated"] = "true"
	err := tm.oracleEngine.BroadcastData(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)

	closingAt = closingAt.Add(time.Second)
	tm.now = closingAt
	closed := tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), closingAt)

	// there's no settlement data yet
	assert.False(t, closed)
	assert.Equal(t, types.MarketStateTradingTerminated, tm.market.State())

	// let time pass still no settlement data
	closingAt = closingAt.Add(time.Second)
	tm.now = closingAt
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), closingAt)
	assert.False(t, closed)
	assert.Equal(t, types.MarketStateTradingTerminated, tm.market.State())

	// now update the market with different trading terminated key
	tm.mktCfg.TradableInstrument.Instrument.GetFuture().DataSourceSpecForTradingTermination = &datasource.Spec{
		ID: "2",
		Data: datasource.NewDefinition(
			datasource.ContentTypeOracle,
		).SetOracleConfig(&signedoracle.SpecConfiguration{
			Signers: pubKeys,
			Filters: []*dstypes.SpecFilter{
				{
					Key: &dstypes.SpecPropertyKey{
						Name: "tradingTerminated",
						Type: datapb.PropertyKey_TYPE_BOOLEAN,
					},
				},
			},
		}),
	}
	tm.mktCfg.TradableInstrument.Instrument.GetFuture().DataSourceSpecBinding.TradingTerminationProperty = "tradingTerminated"
	err = tm.market.Update(context.Background(), tm.mktCfg, tm.oracleEngine)
	require.NoError(t, err)

	// now update the market again with the *same* spec ID
	err = tm.market.Update(context.Background(), tm.mktCfg, tm.oracleEngine)
	require.NoError(t, err)

	properties = map[string]string{}
	properties["tradingTerminated"] = "true"
	err = tm.oracleEngine.BroadcastData(context.Background(), dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)

	// let the oracle update settlement data
	delete(properties, "tradingTerminated")
	properties["prices.ETH.value"] = "100"
	err = tm.oracleEngine.BroadcastData(context.Background(), dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)

	assert.Equal(t, closingAt.UnixNano(), tm.market.Mkt().MarketTimestamps.Close)
	assert.Equal(t, types.MarketStateSettled, tm.market.State())

	closingAt = closingAt.Add(time.Second)
	tm.now = closingAt
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), closingAt)
	assert.True(t, closed)
	assert.Equal(t, types.MarketStateSettled, tm.market.State())

	// call on epoch event to replicate system behaviour
	tm.market.OnEpochEvent(context.Background(), types.Epoch{Action: proto.EpochAction_EPOCH_ACTION_END})
}

func TestMarketClosingAfterUpdate(t *testing.T) {
	// given
	party1 := "party1"
	party2 := "party2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, nil)
	defer tm.ctrl.Finish()

	pubKeys := []*dstypes.Signer{
		dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
	}

	// setup
	addAccount(t, tm, party1)
	addAccount(t, tm, party2)

	// then
	assert.Equal(t, types.MarketStateActive.String(), tm.market.State().String())

	// when
	err := tm.oracleEngine.BroadcastData(context.Background(), dstypes.Data{
		Signers: pubKeys,
		Data: map[string]string{
			"trading.terminated": "true",
		},
	})

	// then
	require.NoError(t, err)

	// given
	closingTimePlus1Sec := closingAt.Add(1 * time.Second)

	// when
	tm.now = closingTimePlus1Sec
	closed := tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), closingTimePlus1Sec)

	// then
	require.False(t, closed)
	assert.Equal(t, types.MarketStateTradingTerminated.String(), tm.market.State().String())

	// Update the market's oracle spec for settlement data.

	// given
	updatedMkt := tm.mktCfg.DeepClone()
	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecForSettlementData.Data.UpdateFilters(
		[]*dstypes.SpecFilter{
			{
				Key: &dstypes.SpecPropertyKey{
					Name: "prices.ETHEREUM.value",
					Type: datapb.PropertyKey_TYPE_INTEGER,
				},
			},
		},
	)

	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecBinding.SettlementDataProperty = "prices.ETHEREUM.value"

	// when
	err = tm.market.Update(context.Background(), updatedMkt, tm.oracleEngine)

	// Sending an oracle data aiming at older oracle spec of the market.

	// then
	require.NoError(t, err)

	// when
	err = tm.oracleEngine.BroadcastData(context.Background(), dstypes.Data{
		Signers: pubKeys,
		Data: map[string]string{
			"prices.ETH.value": "10",
		},
	})

	// then
	require.NoError(t, err)

	// Verify the market didn't catch the oracle data since the oracle spec has
	// been updated.

	// given
	closingTimePlus2Sec := closingAt.Add(2 * time.Second)

	// when
	tm.now = closingTimePlus2Sec
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), closingTimePlus2Sec)

	// then
	require.False(t, closed)
	assert.Equal(t, types.MarketStateTradingTerminated.String(), tm.market.State().String())

	// Verify the market did catch the oracle data according to the oracle spec
	// update.

	// when
	err = tm.oracleEngine.BroadcastData(context.Background(), dstypes.Data{
		Signers: pubKeys,
		Data: map[string]string{
			"prices.ETHEREUM.value": "100",
		},
	})

	// then
	require.NoError(t, err)

	// given
	closingTimePlus3Sec := closingAt.Add(2 * time.Second)

	// when
	tm.now = closingTimePlus3Sec
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), closingTimePlus3Sec)

	// then
	require.True(t, closed)
	assert.Equal(t, types.MarketStateSettled.String(), tm.market.State().String())
}

func TestUnsubscribeTradingTerminatedOracle(t *testing.T) {
	// given
	party1 := "party1"
	party2 := "party2"
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	defer tm.ctrl.Finish()

	// setup
	addAccount(t, tm, party1)
	addAccount(t, tm, party2)

	// then
	assert.Equal(t, types.MarketStateActive.String(), tm.market.State().String())

	// when
	err := tm.oracleEngine.BroadcastData(context.Background(), dstypes.Data{
		Signers: []*dstypes.Signer{
			dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
		},
		Data: map[string]string{
			"trading.terminated": "true",
		},
	})

	// then
	require.NoError(t, err)

	count := tm.eventCount

	for i := 0; i < 10; i++ {
		err := tm.oracleEngine.BroadcastData(context.Background(), dstypes.Data{
			Signers: []*dstypes.Signer{
				dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
			},
			Data: map[string]string{
				"trading.terminated": "true",
			},
		})

		// then
		require.NoError(t, err)
	}

	require.Equal(t, count, tm.eventCount)
}

func TestMarketLiquidityFeeAfterUpdate(t *testing.T) {
	// given
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	defer tm.ctrl.Finish()

	// then
	// We need to ensure this has been updated
	require.NotEqual(t, tm.market.GetLiquidityFee(), num.DecimalZero())

	// given
	previousLiqFee := tm.market.GetLiquidityFee()
	updatedMkt := tm.mktCfg.DeepClone()
	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecForSettlementData.Data.UpdateFilters(
		[]*dstypes.SpecFilter{
			{
				Key: &dstypes.SpecPropertyKey{
					Name: "prices.ETHEREUM.value",
					Type: datapb.PropertyKey_TYPE_INTEGER,
				},
			},
		},
	)

	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecBinding.SettlementDataProperty = "prices.ETHEREUM.value"

	// when
	err := tm.market.Update(context.Background(), updatedMkt, tm.oracleEngine)

	// then
	require.NoError(t, err)
	assert.Equal(t, previousLiqFee, tm.market.GetLiquidityFee())
}

func TestLiquidityFeeWhenTargetStakeDropsDueToFlowOfTime(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	lp1 := "lp1"
	lp2 := "lp2"
	maxOI := uint64(124)
	matchingPrice := uint64(111)
	now := time.Unix(10, 0)
	tm := getTestMarket2(t, now, nil, &types.AuctionDuration{
		Duration: 1,
		// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	}, true, 1)
	tm.market.OnMarketTargetStakeTimeWindowUpdate(5 * time.Second)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccountWithAmount(tm, lp1, 100000000000)
	addAccountWithAmount(tm, lp2, 100000000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, lp1, 1, 10)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, lp2, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	orders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "ord1", types.SideSell, party1, maxOI, matchingPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "ord2", types.SideBuy, party2, maxOI, matchingPrice),
	}
	for _, o := range orders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}

	// submit liquidity with varying fee levels
	commitment1 := num.NewUint(30000)
	fee1 := num.DecimalFromFloat(0.01)
	commitment2 := num.NewUint(20000)
	fee2 := num.DecimalFromFloat(0.02)
	lps := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: commitment1,
		Fee:              fee1,
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lps, lp1, vgcrypto.RandomHash()))
	lps.Fee = fee2
	lps.CommitmentAmount = commitment2
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lps, lp2, vgcrypto.RandomHash()))

	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)
	md := tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)
	require.Equal(t, maxOI, md.OpenInterest)
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)
	// move time and decrase open interest
	orders = []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "ord1", types.SideBuy, party1, maxOI-100, matchingPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "ord2", types.SideSell, party2, maxOI-100, matchingPrice),
	}
	for _, o := range orders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)
	targetStake1 := md.TargetStake
	require.Equal(t, fee2, tm.market.GetLiquidityFee())

	// move time beyond taret stake window (so max OI drops and hence target stake)
	now = now.Add(6 * time.Second)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)
	targetStake2 := md.TargetStake

	require.Less(t, targetStake2, targetStake1)
	require.Equal(t, fee1, tm.market.GetLiquidityFee())
}

func TestMarketNotActive(t *testing.T) {
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)

	// this will create a market in Proposed Mode
	tm := getTestMarket2(t, now, nil, nil, false, 0.99)
	defer tm.ctrl.Finish()

	require.Equal(t, types.MarketStateProposed, tm.market.State())

	party1 := "party1"
	tm.WithAccountAndAmount(party1, 1000000)

	hash := vgcrypto.RandomHash()
	order := &types.Order{
		ID:            hash,
		Type:          types.OrderTypeLimit,
		TimeInForce:   types.OrderTimeInForceGTT,
		Status:        types.OrderStatusActive,
		Side:          types.SideBuy,
		Party:         party1,
		MarketID:      tm.market.GetID(),
		Size:          100,
		Price:         num.NewUint(100),
		OriginalPrice: num.NewUint(100),
		Remaining:     100,
		CreatedAt:     now.UnixNano(),
		ExpiresAt:     closingAt.UnixNano(),
		Reference:     "party1-buy-order",
	}

	tm.events = nil
	cpy := *order
	cpy.Status = types.OrderStatusRejected
	cpy.Reason = types.OrderErrorMarketClosed
	expectedEvent := events.NewOrderEvent(context.Background(), &cpy)

	_, err := tm.market.SubmitOrderWithHash(context.Background(), order, hash)
	require.Error(t, err)
	tm.EventHasBeenEmitted(t, expectedEvent)
}

func TestSubmittedOrderIdIsTheDeterministicId(t *testing.T) {
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, nil)
	defer tm.ctrl.Finish()

	party1 := "party1"
	order := &types.Order{
		Type:          types.OrderTypeLimit,
		TimeInForce:   types.OrderTimeInForceGTT,
		Status:        types.OrderStatusActive,
		ID:            "",
		Side:          types.SideBuy,
		Party:         party1,
		MarketID:      tm.market.GetID(),
		Size:          100,
		Price:         num.NewUint(100),
		OriginalPrice: num.NewUint(100),
		Remaining:     100,
		CreatedAt:     now.UnixNano(),
		ExpiresAt:     closingAt.UnixNano(),
		Reference:     "party1-buy-order",
	}
	addAccount(t, tm, party1)

	deterministicID := vgcrypto.RandomHash()
	conf, err := tm.market.Market.SubmitOrder(context.Background(), order.IntoSubmission(), order.Party, deterministicID)
	if err != nil {
		t.Fatalf("failed to submit order:%s", err)
	}

	assert.Equal(t, deterministicID, conf.Order.ID)

	event := tm.orderEvents[0].(*events.Order)
	assert.Equal(t, event.Order().Id, deterministicID)
}

func TestSubmitOrderWithInvalidTickSize(t *testing.T) {
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, nil)
	tm.mktCfg.TickSize = num.NewUint(1000)
	defer tm.ctrl.Finish()

	party1 := "party1"
	order := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(1100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
	}
	addAccount(t, tm, party1)

	deterministicID := vgcrypto.RandomHash()
	_, err := tm.market.Market.SubmitOrder(context.Background(), order.IntoSubmission(), order.Party, deterministicID)
	require.Error(t, types.ErrOrderNotInTickSize, err)

	tm.mktCfg.TickSize = num.NewUint(100)
	_, err = tm.market.Market.SubmitOrder(context.Background(), order.IntoSubmission(), order.Party, deterministicID)
	require.NoError(t, err)
}

func TestPeggingWithTickSize(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	tm.mktCfg.TickSize = num.NewUint(50)
	defer tm.ctrl.Finish()

	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)

	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 50)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)
	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 100),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 100),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}

	party1 := "party1"
	order := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   math.MaxInt64,
		Reference:   "party1-buy-order",
	}
	addAccount(t, tm, party1)
	// submit a pegged order pegged to the mid
	order.PeggedOrder = &types.PeggedOrder{
		Reference: types.PeggedReferenceMid,
		Offset:    num.NewUint(100),
	}
	// mid price is 50025 - 100 = 49,925 => 49950 bid rounded to the nearest tick size up
	conf, err = tm.market.SubmitOrder(context.Background(), order)
	require.NoError(t, err)
	require.Equal(t, "49950", conf.Order.OriginalPrice.String())

	order.Side = types.SideSell
	// mid price is 50025 + 100 = 50125 => 50100 ask rounded to the nearest tick size down
	conf, err = tm.market.SubmitOrder(context.Background(), order)
	require.NoError(t, err)
	require.Equal(t, "50100", conf.Order.OriginalPrice.String())

	mkt := tm.mktCfg.DeepClone()
	// offset is still divisible by ticksize so nothing happens
	mkt.TickSize = num.NewUint(50)
	require.Equal(t, 2, tm.market.GetPeggedOrderCount())
	require.NoError(t, tm.market.Market.Update(context.Background(), mkt, tm.oracleEngine))
	require.Equal(t, 2, tm.market.GetPeggedOrderCount())

	mkt = tm.mktCfg.DeepClone()
	mkt.TickSize = num.NewUint(79)
	// offset is not divisible by ticksize so pegged orders get cancelled
	require.Equal(t, 2, tm.market.GetPeggedOrderCount())
	require.NoError(t, tm.market.Market.Update(context.Background(), mkt, tm.oracleEngine))
	require.Equal(t, 0, tm.market.GetPeggedOrderCount())
}

func TestAmendOrderWithInvalidTickSize(t *testing.T) {
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, nil)
	tm.mktCfg.TickSize = num.NewUint(100)
	defer tm.ctrl.Finish()

	party1 := "party1"
	order := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
	}
	addAccount(t, tm, party1)

	deterministicID := vgcrypto.RandomHash()
	conf, err := tm.market.Market.SubmitOrder(context.Background(), order.IntoSubmission(), order.Party, deterministicID)
	require.NoError(t, err)

	orderAmendment := &types.OrderAmendment{
		OrderID:  conf.Order.ID,
		MarketID: conf.Order.MarketID,
		Price:    num.NewUint(1150),
	}
	_, err = tm.market.Market.AmendOrder(context.Background(), orderAmendment, party1, deterministicID)
	require.Error(t, types.ErrOrderNotInTickSize, err)

	tm.mktCfg.TickSize = num.NewUint(50)
	_, err = tm.market.Market.AmendOrder(context.Background(), orderAmendment, party1, deterministicID)
	require.NoError(t, err)

	// pegged order
	order.Price = nil
	order.OriginalPrice = nil
	// market tick is 50, lets set a peg offset of 75
	order.Side = types.SideBuy
	order.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 75)
	_, err = tm.market.Market.SubmitOrder(context.Background(), order.IntoSubmission(), party1, deterministicID)
	require.Error(t, types.ErrOrderNotInTickSize, err)

	order.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 100)
	_, err = tm.market.Market.SubmitOrder(context.Background(), order.IntoSubmission(), party1, vgcrypto.RandomHash())
	require.NoError(t, err)

	order.Side = types.SideSell
	order.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 75)
	_, err = tm.market.Market.SubmitOrder(context.Background(), order.IntoSubmission(), party1, deterministicID)
	require.Error(t, types.ErrOrderNotInTickSize, err)

	order.ID = crypto.RandomHash()
	order.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 100)
	conf, err = tm.market.Market.SubmitOrder(context.Background(), order.IntoSubmission(), party1, vgcrypto.RandomHash())
	require.NoError(t, err)

	order.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 75)
	orderAmendment = &types.OrderAmendment{
		OrderID:      conf.Order.ID,
		MarketID:     conf.Order.MarketID,
		PeggedOffset: num.NewUint(75),
	}
	_, err = tm.market.Market.AmendOrder(context.Background(), orderAmendment, party1, vgcrypto.RandomHash())
	require.Error(t, types.ErrOrderNotInTickSize, err)
}

func TestMarketWithTradeClosing(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	defer tm.ctrl.Finish()
	// add 2 parties to the party engine
	// this will create 2 parties, credit their account
	// and move some monies to the market
	// this will also output the closed accounts
	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	pubKeys := []*dstypes.Signer{
		dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
	}

	// submit orders
	// party1 buys
	// party2 sells
	orderBuy := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
	}
	orderSell := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order",
	}

	// submit orders
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	// tm.transferResponseStore.EXPECT().Add(gomock.Any()).AnyTimes()

	_, err := tm.market.SubmitOrder(ctx, orderBuy)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	_, err = tm.market.SubmitOrder(ctx, orderSell)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)

	// update collateral time first, normally done by execution engine
	futureTime := closingAt.Add(1 * time.Second)
	properties := map[string]string{}
	properties["trading.terminated"] = "true"
	err = tm.oracleEngine.BroadcastData(ctx, dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)

	properties = map[string]string{}
	properties["prices.ETH.value"] = "100"
	err = tm.oracleEngine.BroadcastData(ctx, dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)

	tm.now = futureTime
	closed := tm.market.OnTick(ctx, futureTime)
	assert.True(t, closed)
}

func TestUpdateMarketWithOracleSpecEarlyTermination(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	defer tm.ctrl.Finish()
	// add 2 parties to the party engine
	// this will create 2 parties, credit their account
	// and move some monies to the market
	// this will also output the closed accounts
	addAccount(t, tm, party1)
	addAccount(t, tm, party2)

	// submit orders
	// party1 buys
	// party2 sells
	orderBuy := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
	}
	orderSell := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order",
	}

	// submit orders
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	// tm.transferResponseStore.EXPECT().Add(gomock.Any()).AnyTimes()

	_, err := tm.market.SubmitOrder(ctx, orderBuy)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	_, err = tm.market.SubmitOrder(ctx, orderSell)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)

	// now update the market
	updatedMkt := tm.mktCfg.DeepClone()

	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecForTradingTermination.Data.UpdateFilters(
		[]*dstypes.SpecFilter{
			{
				Key: &dstypes.SpecPropertyKey{
					Name: spec.BuiltinTimestamp,
					Type: datapb.PropertyKey_TYPE_TIMESTAMP,
				},
				Conditions: []*dstypes.SpecCondition{
					{
						Operator: datapb.Condition_OPERATOR_GREATER_THAN_OR_EQUAL,
						Value:    "0",
					},
				},
			},
		},
	)

	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecBinding.TradingTerminationProperty = spec.BuiltinTimestamp

	err = tm.market.Update(context.Background(), updatedMkt, tm.oracleEngine)
	require.NoError(t, err)
	tm.builtinOracle.OnTick(ctx, tm.now)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateTradingTerminated, tm.market.State())

	pubKeys := []*dstypes.Signer{
		dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
	}
	properties := map[string]string{}
	properties["prices.ETH.value"] = "100"
	err = tm.oracleEngine.BroadcastData(ctx, dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)

	tm.now = tm.now.Add(time.Second)
	closed := tm.market.OnTick(ctx, tm.now)
	assert.True(t, closed)
}

func Test6056(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	defer tm.ctrl.Finish()
	// add 2 parties to the party engine
	// this will create 2 parties, credit their account
	// and move some monies to the market
	// this will also output the closed accounts
	addAccount(t, tm, party1)
	addAccount(t, tm, party2)

	// submit orders
	// party1 buys
	// party2 sells
	orderBuy := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
	}
	orderSell := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order",
	}

	// submit orders
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	// tm.transferResponseStore.EXPECT().Add(gomock.Any()).AnyTimes()

	_, err := tm.market.SubmitOrder(ctx, orderBuy)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	_, err = tm.market.SubmitOrder(ctx, orderSell)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)

	// now update the market
	updatedMkt := tm.mktCfg.DeepClone()

	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecForSettlementData.Data.UpdateFilters(
		[]*dstypes.SpecFilter{
			{
				Key: &dstypes.SpecPropertyKey{
					Name: "prices.ETH.value",
					Type: datapb.PropertyKey_TYPE_INTEGER,
				},
				Conditions: []*dstypes.SpecCondition{
					{
						Operator: datapb.Condition_OPERATOR_GREATER_THAN_OR_EQUAL,
						Value:    "1",
					},
				},
			},
		},
	)

	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecForTradingTermination.Data.UpdateFilters(
		[]*dstypes.SpecFilter{
			{
				Key: &dstypes.SpecPropertyKey{
					Name: "trading.terminated",
					Type: datapb.PropertyKey_TYPE_BOOLEAN,
				},
				Conditions: []*dstypes.SpecCondition{
					{
						Operator: datapb.Condition_OPERATOR_EQUALS,
						Value:    "false",
					},
				},
			},
		},
	)

	pubKeys := []*dstypes.Signer{
		dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
	}
	err = tm.market.Update(context.Background(), updatedMkt, tm.oracleEngine)
	require.NoError(t, err)

	properties := map[string]string{}
	properties["trading.terminated"] = "false"
	err = tm.oracleEngine.BroadcastData(ctx, dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateTradingTerminated, tm.market.State())

	properties = map[string]string{}
	properties["prices.ETH.value"] = "100"
	err = tm.oracleEngine.BroadcastData(ctx, dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)

	tm.now = tm.now.Add(time.Second)
	closed := tm.market.OnTick(ctx, tm.now)
	assert.True(t, closed)
}

func TestOraclesWithMultipleFilterNameFails(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(20, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	defer tm.ctrl.Finish()
	// add 2 parties to the party engine
	// this will create 2 parties, credit their account
	// and move some monies to the market
	// this will also output the closed accounts
	addAccount(t, tm, party1)
	addAccount(t, tm, party2)

	// submit orders
	// party1 buys
	// party2 sells
	orderBuy := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
	}
	orderSell := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order",
	}

	// submit orders
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	_, err := tm.market.SubmitOrder(ctx, orderBuy)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	_, err = tm.market.SubmitOrder(ctx, orderSell)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)

	// now update the market
	updatedMkt := tm.mktCfg.DeepClone()

	f1 := uint64(12)
	f2 := uint64(21)
	err = updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecForSettlementData.Data.UpdateFilters(
		[]*dstypes.SpecFilter{
			{
				Key: &dstypes.SpecPropertyKey{
					Name:                "prices.ETH.value",
					Type:                datapb.PropertyKey_TYPE_INTEGER,
					NumberDecimalPlaces: &f1,
				},
				Conditions: []*dstypes.SpecCondition{
					{
						Operator: datapb.Condition_OPERATOR_GREATER_THAN,
						Value:    "717098987000000000000000000000000000000",
					},
				},
			},
			{
				Key: &dstypes.SpecPropertyKey{
					Name:                "prices.ETH.value",
					Type:                datapb.PropertyKey_TYPE_INTEGER,
					NumberDecimalPlaces: &f2,
				},
				Conditions: []*dstypes.SpecCondition{
					{
						Operator: datapb.Condition_OPERATOR_GREATER_THAN,
						Value:    "957586060000000000000000000000000000000000000000",
					},
				},
			},
		},
	)

	assert.ErrorIs(t, dserrors.ErrDataSourceSpecHasMultipleSameKeyNamesInFilterList, err)

	updatedMkt.TradableInstrument.Instrument.GetFuture().DataSourceSpecForTradingTermination.Data.UpdateFilters(
		[]*dstypes.SpecFilter{
			{
				Key: &dstypes.SpecPropertyKey{
					Name: "trading.terminated",
					Type: datapb.PropertyKey_TYPE_BOOLEAN,
				},
				Conditions: []*dstypes.SpecCondition{
					{
						Operator: datapb.Condition_OPERATOR_EQUALS,
						Value:    "false",
					},
				},
			},
		},
	)

	pubKeys := []*dstypes.Signer{
		dstypes.CreateSignerFromString("0xDEADBEEF", dstypes.SignerTypePubKey),
	}
	err = tm.market.Update(context.Background(), updatedMkt, tm.oracleEngine)
	require.NoError(t, err)

	properties := map[string]string{}
	properties["trading.terminated"] = "false"
	err = tm.oracleEngine.BroadcastData(ctx, dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateTradingTerminated, tm.market.State())

	properties = map[string]string{}
	properties["prices.ETH.value"] = "100"
	err = tm.oracleEngine.BroadcastData(ctx, dstypes.Data{
		Signers: pubKeys,
		Data:    properties,
	})
	require.NoError(t, err)

	tm.now = tm.now.Add(time.Second)
	closed := tm.market.OnTick(ctx, tm.now)

	// The market should be closed, because it was never updated
	assert.True(t, closed)
}

func TestMarketGetMarginOnNewOrderEmptyBook(t *testing.T) {
	party1 := "party1"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	defer tm.ctrl.Finish()
	// add 2 parties to the party engine
	// this will create 2 parties, credit their account
	// and move some monies to the market
	addAccount(t, tm, party1)

	// submit orders
	// party1 buys
	// party2 sells
	orderBuy := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
	}

	// submit orders
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	// tm.transferResponseStore.EXPECT().Add(gomock.Any()).AnyTimes()

	_, err := tm.market.SubmitOrder(context.Background(), orderBuy)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction
}

func TestMarketGetMarginOnFailNoFund(t *testing.T) {
	party1, party2, party3 := "party1", "party2", "party3"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	tm := getTestMarket2(t, now, nil, &types.AuctionDuration{
		Duration: 1,
		// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	}, true, 1)
	defer tm.ctrl.Finish()
	// add 2 parties to the party engine
	// this will create 2 parties, credit their account
	// and move some monies to the market
	addAccountWithAmount(tm, party1, 0)
	addAccountWithAmount(tm, party2, 1000000)
	addAccountWithAmount(tm, party3, 1000000)
	addAccountWithAmount(tm, "lpprov", 100000000)

	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)

	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)
	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 100),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 100),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(500),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(time.Second * 2)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	order1 := &types.Order{
		Status:      types.OrderStatusActive,
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTC,
		ID:          "someid12",
		Side:        types.SideBuy,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-buy-order",
	}
	order2 := &types.Order{
		Status:      types.OrderStatusActive,
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTC,
		ID:          "someid123",
		Side:        types.SideSell,
		Party:       party3,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party3-buy-order",
	}
	_, err = tm.market.SubmitOrder(context.TODO(), order1)
	assert.NoError(t, err)
	confirmation, err := tm.market.SubmitOrder(context.TODO(), order2)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(confirmation.Trades))

	// submit orders
	// party1 buys
	// party2 sells
	orderBuy := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
	}

	// submit orders
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	// tm.transferResponseStore.EXPECT().Add(gomock.Any()).AnyTimes()

	_, err = tm.market.SubmitOrder(context.Background(), orderBuy)
	assert.NotNil(t, err)
	assert.EqualError(t, err, "margin check failed")
}

func TestMarketGetMarginOnAmendOrderCancelReplace(t *testing.T) {
	party1 := "party1"
	now := time.Unix(100000, 0)
	closingAt := time.Unix(1000000, 0)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	tm := getTestMarket(t, now, nil, nil)
	defer tm.ctrl.Finish()

	addAccount(t, tm, party1)

	// submit orders
	// party1 buys
	// party2 sells
	orderBuy := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(100),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order",
		Version:     common.InitialOrderVersion,
	}

	// submit orders
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	// tm.transferResponseStore.EXPECT().Add(gomock.Any()).AnyTimes()

	_, err := tm.market.SubmitOrder(context.Background(), orderBuy)
	assert.Nil(t, err)
	if err != nil {
		t.Fail()
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction
	t.Log("amending order now")

	// now try to amend and make sure monies are updated
	amendedOrder := &types.OrderAmendment{
		OrderID:     orderBuy.ID,
		Price:       num.NewUint(200),
		SizeDelta:   -50,
		TimeInForce: types.OrderTimeInForceGTT,
		ExpiresAt:   &orderBuy.ExpiresAt,
	}

	_, err = tm.market.AmendOrder(context.Background(), amendedOrder, party1, vgcrypto.RandomHash())
	if !assert.Nil(t, err) {
		t.Fatalf("Error: %v", err)
	}
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
}

func TestTriggerByPriceNoTradesInAuction(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	auctionExtensionSeconds := int64(45)
	openEnd := now.Add(time.Duration(auctionExtensionSeconds)*time.Second + time.Second)
	auctionEndTime := openEnd.Add(time.Duration(auctionExtensionSeconds) * time.Second)
	afterAuction := auctionEndTime.Add(time.Nanosecond)
	pMonitorSettings := &types.PriceMonitoringSettings{
		Parameters: &types.PriceMonitoringParameters{
			Triggers: []*types.PriceMonitoringTrigger{
				{
					Horizon:          60,
					HorizonDec:       num.DecimalFromFloat(60),
					Probability:      num.DecimalFromFloat(0.95),
					AuctionExtension: auctionExtensionSeconds,
				},
			},
		},
	}
	initialPrice := uint64(600)
	mmu, _ := num.UintFromDecimal(MAXMOVEUP)
	auctionTriggeringPrice := initialPrice + 1 + mmu.Uint64()
	tm := getTestMarket2(t, now, pMonitorSettings, &types.AuctionDuration{
		Duration: 1,
		// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	}, true, 1)

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 100000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Duration(auctionExtensionSeconds)*time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100*initialPrice)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, initialPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, initialPrice),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction by moving time
	tm.now = openEnd
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), openEnd)
	now = openEnd

	orderBuy1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid1",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-1",
	}
	confirmationBuy, err := tm.market.SubmitOrder(context.Background(), orderBuy1)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	orderSell1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceFOK,
		Status:      types.OrderStatusActive,
		ID:          "someid2",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-sell-order-1",
	}
	confirmationSell, err := tm.market.SubmitOrder(context.Background(), orderSell1)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	require.Equal(t, 1, len(confirmationSell.Trades))

	auctionEnd := tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	orderBuy2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid3",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-2",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy2)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	orderSell2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTC,
		Status:      types.OrderStatusActive,
		ID:          "someid4",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-sell-order-2",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell2)
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)
	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction

	require.Empty(t, confirmationSell.Trades)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // In auction

	tm.now = auctionEndTime
	closed := tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), auctionEndTime)
	assert.False(t, closed)

	tm.now = afterAuction
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), afterAuction)
	require.Equal(t, types.MarketStateActive, tm.market.State())
	assert.False(t, closed)
}

func TestTriggerByPriceAuctionPriceInBounds(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	auctionExtensionSeconds := int64(45)
	openEnd := now.Add(time.Duration(auctionExtensionSeconds)*time.Second + time.Second)
	auctionEndTime := openEnd.Add(time.Duration(auctionExtensionSeconds) * time.Second)
	afterAuction := auctionEndTime.Add(time.Nanosecond)
	pMonitorSettings := &types.PriceMonitoringSettings{
		Parameters: &types.PriceMonitoringParameters{
			Triggers: []*types.PriceMonitoringTrigger{
				{
					Horizon:          60,
					HorizonDec:       num.DecimalFromFloat(60),
					Probability:      num.DecimalFromFloat(0.95),
					AuctionExtension: auctionExtensionSeconds,
				},
			},
		},
	}
	initialPrice := uint64(600)
	deltaD := MAXMOVEUP
	delta, _ := num.UintFromDecimal(deltaD.Add(MINMOVEDOWN).Div(num.DecimalFromFloat(2)))
	mmu, _ := num.UintFromDecimal(MAXMOVEUP)
	validPrice := initialPrice + delta.Uint64()
	auctionTriggeringPrice := initialPrice + mmu.Uint64() + 1
	// let's not start this in opening auction, it complicates the matter
	tm := getTestMarket2(t, now, pMonitorSettings, &types.AuctionDuration{
		Duration: auctionExtensionSeconds,
		// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	}, true, 1)

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 100000)

	// set auction duration
	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Duration(auctionExtensionSeconds)*time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100*initialPrice)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)
	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, initialPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, initialPrice),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave auction
	tm.now = openEnd
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), openEnd)
	now = openEnd

	orderSell1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid2",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-1",
	}
	confirmationSell, err := tm.market.SubmitOrder(context.Background(), orderSell1)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	orderBuy1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceFOK,
		Status:      types.OrderStatusActive,
		ID:          "someid1",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party1-buy-order-1",
	}
	confirmationBuy, err := tm.market.SubmitOrder(context.Background(), orderBuy1)
	require.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	require.Equal(t, 1, len(confirmationBuy.Trades))

	auctionEnd := tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd, "we are in auction?") // Not in auction
	require.Equal(t, types.MarketStateActive, tm.market.State())

	orderSell2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid4",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-2",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell2)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	orderBuy2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTC,
		Status:      types.OrderStatusActive,
		ID:          "someid3",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party1-buy-order-2",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy2)
	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	require.Empty(t, confirmationSell.Trades)

	tm.now = auctionEndTime
	closed := tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), auctionEndTime)
	assert.False(t, closed)

	now = auctionEndTime
	orderSell3 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGFA,
		Status:      types.OrderStatusActive,
		ID:          "someid6",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(validPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-sell-order-3",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell3)
	assert.NotNil(t, confirmationSell)
	assert.NoError(t, err)

	orderBuy3 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGFA,
		Status:      types.OrderStatusActive,
		ID:          "someid5",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(validPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-3",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy3)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)
	require.Empty(t, confirmationBuy.Trades)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd)         // In auction
	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction

	tm.now = afterAuction
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), afterAuction)
	require.Equal(t, tm.market.State(), types.MarketStateActive)
	assert.False(t, closed)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	// TODO: Check that `party2-sell-order-3` & `party1-buy-order-3` get matched in auction and a trade is generated

	// Test that orders get matched as expected upon returning to continuous trading
	now = afterAuction.Add(time.Second)
	orderSell4 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid8",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        1,
		Price:       num.NewUint(validPrice),
		Remaining:   1,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-4",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell4)
	assert.NotNil(t, confirmationSell)
	assert.NoError(t, err)

	orderBuy4 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid7",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        1,
		Price:       num.NewUint(validPrice),
		Remaining:   1,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-4",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy4)
	require.NotNil(t, confirmationBuy)
	require.NoError(t, err)
	require.Equal(t, 1, len(confirmationBuy.Trades))
}

func TestTriggerByPriceAuctionPriceOutsideBounds(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	auxParty, auxParty2 := "auxParty", "auxParty2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	auctionExtensionSeconds := int64(45)
	openingAuctionDuration := &types.AuctionDuration{Duration: auctionExtensionSeconds}
	openEnd := now.Add(time.Duration(openingAuctionDuration.Duration)*time.Second + time.Second)
	auctionEndTime := openEnd.Add(time.Duration(auctionExtensionSeconds) * time.Second)
	initialAuctionEnd := auctionEndTime.Add(time.Second)
	pMonitorSettings := &types.PriceMonitoringSettings{
		Parameters: &types.PriceMonitoringParameters{
			Triggers: []*types.PriceMonitoringTrigger{
				{
					Horizon:          60,
					HorizonDec:       num.DecimalFromFloat(60),
					Probability:      num.DecimalFromFloat(0.95),
					AuctionExtension: auctionExtensionSeconds,
				},
			},
		},
	}
	mmu, _ := num.UintFromDecimal(MAXMOVEUP)
	initialPrice := uint64(600)
	auctionTriggeringPrice := initialPrice + 1 + mmu.Uint64()
	// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	tm := getTestMarket2(t, now, pMonitorSettings, openingAuctionDuration, true, 1)

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 100000)

	// set auction duration
	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Duration(auctionExtensionSeconds)*time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.Equal(t, types.MarketStatePending, tm.market.State()) // enter auction
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100*initialPrice)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)
	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideBuy, auxParty, 1, initialPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideSell, auxParty2, 1, initialPrice),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NotNil(t, conf)
		require.NoError(t, err)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// increase time, so we can leave opening auction
	tm.now = openEnd
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), openEnd)

	md := tm.market.GetMarketData()

	require.Equal(t, types.AuctionTriggerUnspecified, md.Trigger)

	require.Equal(t, types.MarketStateActive, tm.market.State())
	now = openEnd

	orderSell1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid2",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-1",
	}
	confirmationSell, err := tm.market.SubmitOrder(context.Background(), orderSell1)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	orderBuy1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceFOK,
		Status:      types.OrderStatusActive,
		ID:          "someid1",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party1-buy-order-1",
	}
	confirmationBuy, err := tm.market.SubmitOrder(context.Background(), orderBuy1)
	require.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	require.Equal(t, 1, len(confirmationBuy.Trades))

	auctionEnd := tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	orderSell2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid4",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-2",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell2)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	orderBuy2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTC,
		Status:      types.OrderStatusActive,
		ID:          "someid3",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice - 1),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party1-buy-order-2",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy2)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	require.Empty(t, confirmationBuy.Trades)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	amendedOrder := &types.OrderAmendment{
		OrderID:     orderBuy2.ID,
		Price:       num.NewUint(auctionTriggeringPrice),
		SizeDelta:   0,
		TimeInForce: types.OrderTimeInForceGTC,
	}

	conf, err = tm.market.AmendOrder(context.Background(), amendedOrder, party1, vgcrypto.RandomHash())
	require.NoError(t, err)
	require.NotNil(t, conf)
	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // In auction

	tm.now = auctionEndTime
	closed := tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), auctionEndTime)
	assert.False(t, closed)

	now = auctionEndTime
	orderSell3 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGFA,
		Status:      types.OrderStatusActive,
		ID:          "someid6",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-sell-order-3",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell3)
	assert.NotNil(t, confirmationSell)
	assert.NoError(t, err)

	orderBuy3 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGFA,
		Status:      types.OrderStatusActive,
		ID:          "someid5",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-3",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy3)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)
	require.Empty(t, confirmationBuy.Trades)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // In auction

	// Expecting market to still be in auction as auction resulted in invalid price
	tm.now = initialAuctionEnd
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), initialAuctionEnd)
	assert.False(t, closed)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction (trigger can only start auction, but can't stop it from concluding at a higher price)
}

func TestTriggerByMarketOrder(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	var auctionExtensionSeconds int64 = 45
	openingEnd := now.Add(time.Duration(auctionExtensionSeconds+1) * time.Second)
	auctionEndTime := openingEnd.Add(time.Duration(auctionExtensionSeconds) * time.Second)
	pMonitorSettings := &types.PriceMonitoringSettings{
		Parameters: &types.PriceMonitoringParameters{
			Triggers: []*types.PriceMonitoringTrigger{
				{
					Horizon:          60,
					HorizonDec:       num.DecimalFromFloat(60),
					Probability:      num.DecimalFromFloat(0.95),
					AuctionExtension: auctionExtensionSeconds,
				},
			},
		},
	}
	mmu, _ := num.UintFromDecimal(MAXMOVEUP)
	initialPrice := uint64(600)
	auctionTriggeringPriceHigh := initialPrice + 1 + mmu.Uint64()
	tm := getTestMarket2(t, now, pMonitorSettings, &types.AuctionDuration{
		Duration: auctionExtensionSeconds,
		// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	}, true, 1)

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 100000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Duration(auctionExtensionSeconds)*time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100*initialPrice)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, initialPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, initialPrice),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NotNil(t, conf)
		require.NoError(t, err)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// now leave auction
	tm.now = openingEnd
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), openingEnd)
	now = openingEnd

	orderSell1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid2",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-1",
	}
	confirmationSell, err := tm.market.SubmitOrder(context.Background(), orderSell1)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	orderBuy1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceFOK,
		Status:      types.OrderStatusActive,
		ID:          "someid1",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party1-buy-order-1",
	}
	confirmationBuy, err := tm.market.SubmitOrder(context.Background(), orderBuy1)
	require.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	require.Equal(t, 1, len(confirmationBuy.Trades))

	auctionEnd := tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	orderSell2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid3",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        3,
		Price:       num.NewUint(auctionTriggeringPriceHigh - 1),
		Remaining:   3,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-2",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell2)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	require.Empty(t, confirmationSell.Trades)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	orderSell3 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid4",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        1,
		Price:       num.NewUint(auctionTriggeringPriceHigh),
		Remaining:   1,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-3",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell3)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	require.Empty(t, confirmationSell.Trades)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	orderBuy2 := &types.Order{
		Type:      types.OrderTypeMarket,
		Status:    types.OrderStatusActive,
		ID:        "someid5",
		Side:      types.SideBuy,
		Party:     party1,
		MarketID:  tm.market.GetID(),
		Size:      4,
		Price:     num.UintZero(),
		Remaining: 4,
		CreatedAt: now.UnixNano(),
		Reference: "party1-buy-order-2",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy2)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)
	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction

	require.Empty(t, confirmationSell.Trades)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // In auction

	tm.now = auctionEndTime
	closed := tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), auctionEndTime)
	assert.False(t, closed)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // Still in auction
	require.Equal(t, types.MarketStateSuspended, tm.market.State())

	tm.now = auctionEndTime.Add(time.Nanosecond)
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // left auction
	assert.False(t, closed)

	md := tm.market.GetMarketData()
	auctionEnd = md.AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	require.True(t, md.MarkPrice.EQ(num.NewUint(initialPrice)))
}

func TestPriceMonitoringBoundsInGetMarketData(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	extension := int64(45)
	t1 := &types.PriceMonitoringTrigger{
		Horizon:          60,
		HorizonDec:       num.DecimalFromFloat(60),
		Probability:      num.DecimalFromFloat(0.95),
		AuctionExtension: extension,
	}
	t2 := &types.PriceMonitoringTrigger{
		Horizon:          120,
		HorizonDec:       num.DecimalFromFloat(120),
		Probability:      num.DecimalFromFloat(0.99),
		AuctionExtension: extension * 2,
	}
	pMonitorSettings := &types.PriceMonitoringSettings{
		Parameters: &types.PriceMonitoringParameters{
			Triggers: []*types.PriceMonitoringTrigger{
				t1,
				t2,
			},
		},
	}
	openEnd := now.Add(time.Duration(extension)*time.Second + time.Second)
	mmu, _ := num.UintFromDecimal(MAXMOVEUP)
	initialPrice := uint64(600)
	auctionTriggeringPrice := initialPrice + mmu.Uint64() + 1
	tm := getTestMarket2(t, now, pMonitorSettings, &types.AuctionDuration{
		Duration: extension,
		// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	}, true, 1)

	initDec := num.DecimalFromFloat(float64(initialPrice))
	// add 1 for the ceil
	min, _ := num.UintFromDecimal(initDec.Sub(MINMOVEDOWN).Add(num.DecimalFromFloat(1)))
	max, _ := num.UintFromDecimal(initDec.Add(MAXMOVEUP).Floor())
	expectedPmRange1 := types.PriceMonitoringBounds{
		MinValidPrice:  min,
		MaxValidPrice:  max,
		Trigger:        t1,
		ReferencePrice: initDec,
	}
	expectedPmRange2 := types.PriceMonitoringBounds{
		MinValidPrice:  min.Clone(),
		MaxValidPrice:  max.Clone(),
		Trigger:        t2,
		ReferencePrice: initDec,
	}

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 100000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Duration(extension)*time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, initialPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, initialPrice),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave auction
	tm.now = openEnd
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), openEnd)
	now = openEnd

	orderBuy1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid1",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-1",
	}
	confirmationBuy, err := tm.market.SubmitOrder(context.Background(), orderBuy1)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	orderSell1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceFOK,
		Status:      types.OrderStatusActive,
		ID:          "someid2",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-sell-order-1",
	}
	confirmationSell, err := tm.market.SubmitOrder(context.Background(), orderSell1)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)
	require.Equal(t, 1, len(confirmationSell.Trades))

	md := tm.market.GetMarketData()
	require.NotNil(t, md)

	auctionEnd := md.AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	pmBounds := md.PriceMonitoringBounds
	require.Equal(t, 2, len(pmBounds))
	require.True(t, expectedPmRange1.MinValidPrice.EQ(pmBounds[0].MinValidPrice), "%s != %s", expectedPmRange1.MinValidPrice, pmBounds[0].MinValidPrice)
	require.True(t, expectedPmRange1.MaxValidPrice.EQ(pmBounds[0].MaxValidPrice))
	require.True(t, expectedPmRange1.ReferencePrice.Equals(pmBounds[0].ReferencePrice))
	require.Equal(t, *expectedPmRange1.Trigger, *pmBounds[0].Trigger)

	require.True(t, expectedPmRange2.MinValidPrice.EQ(pmBounds[1].MinValidPrice))
	require.True(t, expectedPmRange2.MaxValidPrice.EQ(pmBounds[1].MaxValidPrice))
	require.True(t, expectedPmRange2.ReferencePrice.Equals(pmBounds[1].ReferencePrice))
	require.Equal(t, *expectedPmRange2.Trigger, *pmBounds[1].Trigger)

	orderBuy2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid3",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-2",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy2)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	orderSell2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTC,
		Status:      types.OrderStatusActive,
		ID:          "someid4",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-sell-order-2",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell2)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)
	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction

	require.Empty(t, confirmationSell.Trades)

	md = tm.market.GetMarketData()
	require.NotNil(t, md)
	auctionEnd = md.AuctionEnd
	auctionEndTime := openEnd.Add(time.Duration(t1.AuctionExtension) * time.Second)
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // In auction
	require.Equal(t, types.MarketStateSuspended, tm.market.State())
	// 2 in total
	require.Equal(t, 2, len(md.PriceMonitoringBounds))
	active, triggerd := 0, 0
	for _, v := range md.PriceMonitoringBounds {
		if v.Active {
			active++
			continue
		}
		triggerd++
	}
	// 1 active
	require.Equal(t, 1, active)
	// 1 triggered
	require.Equal(t, 1, triggerd)

	tm.now = auctionEndTime
	closed := tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), auctionEndTime)
	assert.False(t, closed)

	md = tm.market.GetMarketData()
	require.NotNil(t, md)
	auctionEnd = md.AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // In auction
	require.Equal(t, types.MarketStateSuspended, tm.market.State())

	require.Equal(t, 1, len(md.PriceMonitoringBounds))

	tm.now = auctionEndTime.Add(time.Nanosecond)
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), tm.now)
	assert.False(t, closed)

	auctionEndTime = openEnd.Add(time.Duration(t1.AuctionExtension+t2.AuctionExtension) * time.Second)
	md = tm.market.GetMarketData()
	require.NotNil(t, md)
	auctionEnd = md.AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // In auction
	require.Equal(t, types.MarketStateSuspended, tm.market.State())

	tm.now = auctionEndTime
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), auctionEndTime)
	assert.False(t, closed)

	md = tm.market.GetMarketData()
	require.NotNil(t, md)
	auctionEnd = md.AuctionEnd
	require.Equal(t, auctionEndTime.UnixNano(), auctionEnd) // In auction
	require.Equal(t, types.MarketStateSuspended, tm.market.State())

	tm.now = auctionEndTime.Add(time.Nanosecond)
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), tm.now)
	assert.False(t, closed)

	md = tm.market.GetMarketData()
	require.NotNil(t, md)
	auctionEnd = md.AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction
	require.Equal(t, types.MarketStateActive, tm.market.State())

	require.Equal(t, 2, len(md.PriceMonitoringBounds))
	require.True(t, expectedPmRange1.MinValidPrice.EQ(pmBounds[0].MinValidPrice))
	require.True(t, expectedPmRange1.MaxValidPrice.EQ(pmBounds[0].MaxValidPrice))
	require.True(t, expectedPmRange1.ReferencePrice.Equals(pmBounds[0].ReferencePrice))
	require.Equal(t, *expectedPmRange1.Trigger, *pmBounds[0].Trigger)

	require.True(t, expectedPmRange2.MinValidPrice.EQ(pmBounds[1].MinValidPrice))
	require.True(t, expectedPmRange2.MaxValidPrice.EQ(pmBounds[1].MaxValidPrice))
	require.True(t, expectedPmRange2.ReferencePrice.Equals(pmBounds[1].ReferencePrice))
	require.Equal(t, *expectedPmRange2.Trigger, *pmBounds[1].Trigger)
}

func TestTargetStakeReturnedAndCorrect(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	oi := uint64(124)
	matchingPrice := uint64(111)
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})

	rmParams := tm.mktCfg.TradableInstrument.GetSimpleRiskModel().Params
	expectedTargetStake := num.DecimalFromFloat(float64(matchingPrice * oi)).Mul(tm.mktCfg.LiquidityMonitoringParameters.TargetStakeParameters.ScalingFactor)
	if rmParams.FactorLong.GreaterThan(rmParams.FactorShort) {
		expectedTargetStake = expectedTargetStake.Mul(rmParams.FactorLong)
	} else {
		expectedTargetStake = expectedTargetStake.Mul(rmParams.FactorShort)
	}

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 100000000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, matchingPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, matchingPrice),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(50000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	orderSell1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid2",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        oi - 1, // -1 because we trade during opening auction
		Price:       num.NewUint(matchingPrice),
		Remaining:   oi - 1,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-1",
	}
	confirmationSell, err := tm.market.SubmitOrder(context.Background(), orderSell1)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	orderBuy1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceFOK,
		Status:      types.OrderStatusActive,
		ID:          "someid1",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        oi - 1,
		Price:       num.NewUint(matchingPrice),
		Remaining:   oi - 1,
		CreatedAt:   now.UnixNano(),
		Reference:   "party1-buy-order-1",
	}
	confirmationBuy, err := tm.market.SubmitOrder(context.Background(), orderBuy1)
	require.NotNil(t, confirmationBuy)
	assert.NoError(t, err)
	require.NotZero(t, len(confirmationBuy.Trades))

	mktData := tm.market.GetMarketData()
	require.NotNil(t, mktData)
	require.Equal(t, expectedTargetStake.String(), mktData.TargetStake)
}

func TestSuppliedStakeReturnedAndCorrect(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	tm := getTestMarket(t, now, nil, nil)
	var matchingPrice uint64 = 111

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	orderSell1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid2",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        1,
		Price:       num.NewUint(matchingPrice + 1),
		Remaining:   1,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party2-sell-order-1",
	}
	confirmationSell, err := tm.market.SubmitOrder(context.Background(), orderSell1)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	orderBuy1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid1",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        1,
		Price:       num.NewUint(matchingPrice - 1),
		Remaining:   1,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-1",
	}
	confirmationBuy, err := tm.market.SubmitOrder(context.Background(), orderBuy1)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	require.Equal(t, 0, len(confirmationBuy.Trades))

	lp1 := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(200),
		Fee:              num.DecimalFromFloat(0.05),
	}

	err = tm.market.SubmitLiquidityProvision(context.Background(), lp1, party1, vgcrypto.RandomHash())
	require.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)

	lp2 := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(100),
		Fee:              num.DecimalFromFloat(0.06),
	}

	err = tm.market.SubmitLiquidityProvision(context.Background(), lp2, party2, vgcrypto.RandomHash())
	require.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)

	mktData := tm.market.GetMarketData()
	require.NotNil(t, mktData)
	expectedSuppliedStake := num.DecimalFromUint(num.Sum(lp1.CommitmentAmount, lp2.CommitmentAmount))

	require.Equal(t, expectedSuppliedStake.String(), mktData.SuppliedStake)
}

func TestSubmitLiquidityProvisionInOpeningAuction(t *testing.T) {
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	mainParty := "mainParty"
	auxParty := "auxParty"
	p1, p2 := "party1", "party2"
	now := time.Unix(10, 0)
	var auctionDuration int64 = 5
	tm := getTestMarket2(t, now, nil, &types.AuctionDuration{Duration: auctionDuration}, true, 0.99)
	var midPrice uint64 = 100

	addAccount(t, tm, mainParty)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, p1)
	addAccount(t, tm, p2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	lp1 := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(250),
		Fee:              num.DecimalFromFloat(0.05),
	}

	require.Equal(t, types.MarketTradingModeOpeningAuction, tm.market.GetMarketData().MarketTradingMode)

	err := tm.market.SubmitLiquidityProvision(ctx, lp1, mainParty, vgcrypto.RandomHash())
	require.NoError(t, err)

	tradingOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "p1-sell-order", types.SideSell, p1, 1, midPrice),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "p2-buy-order", types.SideBuy, p2, 1, midPrice),
	}
	for _, o := range tradingOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		assert.NoError(t, err)
		assert.NotNil(t, conf)
	}
	orderSell1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "auxParty-sell-order-1", types.SideSell, auxParty, 1, midPrice+2)

	confirmationSell, err := tm.market.SubmitOrder(ctx, orderSell1)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	orderBuy1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "auxParty-buy-order-1", types.SideBuy, auxParty, 1, midPrice-2)

	confirmationBuy, err := tm.market.SubmitOrder(ctx, orderBuy1)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	now = now.Add(time.Duration((auctionDuration + 1) * time.Second.Nanoseconds()))
	tm.now = now
	tm.market.OnTick(ctx, now)

	// Check that liquidity orders appear on the book once reference prices exist
	mktData := tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, mktData.MarketTradingMode)
}

func getMarketOrder(tm *testMarket,
	now time.Time,
	orderType types.OrderType,
	orderTIF types.OrderTimeInForce,
	id string,
	side types.Side,
	partyID string,
	size uint64,
	price uint64,
) *types.Order {
	order := &types.Order{
		Type:        orderType,
		TimeInForce: orderTIF,
		Status:      types.OrderStatusActive,
		ID:          id,
		Side:        side,
		Party:       partyID,
		MarketID:    tm.market.GetID(),
		Size:        size,
		Price:       num.NewUint(price),
		Remaining:   size,
		CreatedAt:   now.UnixNano(),
		Reference:   "marketorder",
	}
	return order
}

func newPeggedOrder(reference types.PeggedReference, offset uint64) *types.PeggedOrder {
	return &types.PeggedOrder{
		Reference: reference,
		Offset:    num.NewUint(offset),
	}
}

func TestOrderBook_Crash2651(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "613f")
	addAccount(t, tm, "f9e7")
	addAccount(t, tm, "98e1")
	addAccount(t, tm, "qqqq")
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	// Switch to auction mode
	tm.mas.StartOpeningAuction(now, &types.AuctionDuration{Duration: 10})
	tm.mas.AuctionStarted(ctx, now)
	tm.market.EnterAuction(ctx)

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFA, "Order01", types.SideBuy, "613f", 5, 9000)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFA, "Order02", types.SideSell, "f9e7", 5, 9000)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFA, "Order03", types.SideBuy, "613f", 4, 8000)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)

	o4 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFA, "Order04", types.SideSell, "f9e7", 4, 8000)
	o4conf, err := tm.market.SubmitOrder(ctx, o4)
	require.NotNil(t, o4conf)
	require.NoError(t, err)

	o5 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFA, "Order05", types.SideBuy, "613f", 4, 3000)
	o5conf, err := tm.market.SubmitOrder(ctx, o5)
	require.NotNil(t, o5conf)
	require.NoError(t, err)

	o6 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFA, "Order06", types.SideSell, "f9e7", 3, 3000)
	o6conf, err := tm.market.SubmitOrder(ctx, o6)
	require.NotNil(t, o6conf)
	require.NoError(t, err)

	o7 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order07", types.SideSell, "f9e7", 20, 0)
	o7.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 1000)
	o7conf, err := tm.market.SubmitOrder(ctx, o7)
	require.NotNil(t, o7conf)
	require.NoError(t, err)

	o8 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFA, "Order08", types.SideSell, "613f", 5, 10001)
	o8conf, err := tm.market.SubmitOrder(ctx, o8)
	require.NotNil(t, o8conf)
	require.NoError(t, err)

	o9 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFA, "Order09", types.SideBuy, "613f", 5, 15001)
	o9conf, err := tm.market.SubmitOrder(ctx, o9)
	require.NotNil(t, o9conf)
	require.NoError(t, err)

	o10 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order10", types.SideBuy, "f9e7", 12, 0)
	o10.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 1000)
	o10conf, err := tm.market.SubmitOrder(ctx, o10)
	require.NotNil(t, o10conf)
	require.NoError(t, err)

	o11 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order11", types.SideBuy, "613f", 21, 0)
	o11.PeggedOrder = newPeggedOrder(types.PeggedReferenceMid, 2000)
	o11conf, err := tm.market.SubmitOrder(ctx, o11)
	require.NotNil(t, o11conf)
	require.NoError(t, err)

	// Leave auction and uncross the book
	tm.market.LeaveAuctionWithIDGen(ctx, now.Add(time.Second*20), newTestIDGenerator())
	require.Equal(t, 3, tm.market.GetPeggedOrderCount())
	require.Equal(t, 3, tm.market.GetParkedOrderCount())
	require.Equal(t, types.MarketStateActive, tm.market.State()) // still in auction

	o12 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order12", types.SideSell, "613f", 22, 9023)
	o12conf, err := tm.market.SubmitOrder(ctx, o12)
	require.NotNil(t, o12conf)
	require.NoError(t, err)

	o13 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order13", types.SideBuy, "98e1", 23, 11119)
	o13conf, err := tm.market.SubmitOrder(ctx, o13)
	require.NotNil(t, o13conf)
	require.NoError(t, err)

	// This order should cause a crash
	o14 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order14", types.SideBuy, "qqqq", 34, 11513)
	o14conf, err := tm.market.SubmitOrder(ctx, o14)
	require.NotNil(t, o14conf)
	require.NoError(t, err)
}

func TestOrderBook_Crash2599(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "A")
	addAccount(t, tm, "B")
	addAccount(t, tm, "C")
	addAccount(t, tm, "D")
	addAccount(t, tm, "E")
	addAccount(t, tm, "F")
	addAccount(t, tm, "G")
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)
	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 11000),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 11000),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(27500),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFN, "Order01", types.SideBuy, "A", 5, 11500)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFN, "Order02", types.SideSell, "B", 25, 11000)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFN, "Order03", types.SideBuy, "A", 10, 10500)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o4 := getMarketOrder(tm, now, types.OrderTypeMarket, types.OrderTimeInForceIOC, "Order04", types.SideSell, "C", 5, 0)
	o4conf, err := tm.market.SubmitOrder(ctx, o4)
	require.NotNil(t, o4conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o5 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order05", types.SideBuy, "C", 35, 0)
	o5.PeggedOrder = newPeggedOrder(types.PeggedReferenceMid, 500)
	o5conf, err := tm.market.SubmitOrder(ctx, o5)
	require.NotNil(t, o5conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o6 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order06", types.SideBuy, "D", 16, 0)
	o6.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 2000)
	o6conf, err := tm.market.SubmitOrder(ctx, o6)
	require.NotNil(t, o6conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o7 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTT, "Order07", types.SideSell, "E", 19, 0)
	o7.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 3000)
	o7.ExpiresAt = now.Add(time.Second * 60).UnixNano()
	o7conf, err := tm.market.SubmitOrder(ctx, o7)
	require.NotNil(t, o7conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o8 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order08", types.SideBuy, "F", 25, 10000)
	o8conf, err := tm.market.SubmitOrder(ctx, o8)
	require.NotNil(t, o8conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	// This one should crash
	o9 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order09", types.SideSell, "F", 25, 10250)
	o9conf, err := tm.market.SubmitOrder(ctx, o9)
	require.NotNil(t, o9conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o10 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order10", types.SideBuy, "G", 45, 14000)
	o10conf, err := tm.market.SubmitOrder(ctx, o10)
	require.NotNil(t, o10conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o11 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order11", types.SideSell, "G", 45, 8500)
	o11conf, err := tm.market.SubmitOrder(ctx, o11)
	require.NotNil(t, o11conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)
}

func TestTriggerAfterOpeningAuction(t *testing.T) {
	party1 := "party1"
	party2 := "party2"
	party3 := "party3"
	party4 := "party4"
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	now := time.Unix(10, 0)
	closingAt := time.Unix(10000000000, 0)
	auctionExtensionSeconds := int64(45)
	openingAuctionDuration := &types.AuctionDuration{Duration: auctionExtensionSeconds}
	openingAuctionEndTime := now.Add(time.Duration(openingAuctionDuration.Duration) * time.Second)
	afterOpeningAuction := openingAuctionEndTime.Add(time.Nanosecond)
	pMonitorAuctionEndTime := afterOpeningAuction.Add(time.Duration(auctionExtensionSeconds) * time.Second)
	afterPMonitorAuction := pMonitorAuctionEndTime.Add(time.Nanosecond)
	pMonitorSettings := &types.PriceMonitoringSettings{
		Parameters: &types.PriceMonitoringParameters{
			Triggers: []*types.PriceMonitoringTrigger{
				{
					Horizon:          60,
					HorizonDec:       num.DecimalFromFloat(60),
					Probability:      num.DecimalFromFloat(0.95),
					AuctionExtension: auctionExtensionSeconds,
				},
			},
		},
	}
	mmu, _ := num.UintFromDecimal(MAXMOVEUP)
	initialPrice := uint64(100)
	auctionTriggeringPrice := initialPrice + 1 + mmu.Uint64()

	tm := getTestMarket(t, now, pMonitorSettings, openingAuctionDuration)

	addAccount(t, tm, party1)
	addAccount(t, tm, party2)
	addAccount(t, tm, party3)
	addAccount(t, tm, party4)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(context.Background(), time.Duration(auctionExtensionSeconds)*time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	gtcOrders := []*types.Order{
		{
			Type:        types.OrderTypeLimit,
			TimeInForce: types.OrderTimeInForceGTC,
			Status:      types.OrderStatusActive,
			ID:          "someid3",
			Side:        types.SideBuy,
			Party:       party3,
			MarketID:    tm.market.GetID(),
			Size:        1,
			Price:       num.NewUint(initialPrice - 5),
			Remaining:   1,
			CreatedAt:   now.UnixNano(),
			ExpiresAt:   closingAt.UnixNano(),
			Reference:   "party3-buy-order-1",
		},
		{
			Type:        types.OrderTypeLimit,
			TimeInForce: types.OrderTimeInForceGTC,
			Status:      types.OrderStatusActive,
			ID:          "someid4",
			Side:        types.SideSell,
			Party:       party4,
			MarketID:    tm.market.GetID(),
			Size:        1,
			Price:       num.NewUint(initialPrice + 10),
			Remaining:   1,
			CreatedAt:   now.UnixNano(),
			Reference:   "party4-sell-order-1",
		},
	}
	for _, o := range gtcOrders {
		conf, err := tm.market.SubmitOrder(context.Background(), o)
		assert.NotNil(t, conf)
		assert.NoError(t, err)
	}
	orderBuy1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid1",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-1",
	}
	confirmationBuy, err := tm.market.SubmitOrder(context.Background(), orderBuy1)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	orderSell1 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTC,
		Status:      types.OrderStatusActive,
		ID:          "someid2",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(initialPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-sell-order-1",
	}
	confirmationSell, err := tm.market.SubmitOrder(context.Background(), orderSell1)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	require.Empty(t, confirmationSell.Trades)

	auctionEnd := tm.market.GetMarketData().AuctionEnd
	require.Equal(t, openingAuctionEndTime.UnixNano(), auctionEnd) // In opening auction

	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(25000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	tm.now = afterOpeningAuction
	closed := tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), afterOpeningAuction)
	assert.False(t, closed)
	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, int64(0), auctionEnd) // Not in auction

	// let's cancel the orders we had to place to end opening auction
	for _, o := range gtcOrders {
		_, err := tm.market.CancelOrder(context.Background(), o.Party, o.ID, vgcrypto.RandomHash())
		assert.NoError(t, err)
	}
	orderBuy2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTT,
		Status:      types.OrderStatusActive,
		ID:          "someid3",
		Side:        types.SideBuy,
		Party:       party1,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		ExpiresAt:   closingAt.UnixNano(),
		Reference:   "party1-buy-order-2",
	}
	confirmationBuy, err = tm.market.SubmitOrder(context.Background(), orderBuy2)
	assert.NotNil(t, confirmationBuy)
	assert.NoError(t, err)

	orderSell2 := &types.Order{
		Type:        types.OrderTypeLimit,
		TimeInForce: types.OrderTimeInForceGTC,
		Status:      types.OrderStatusActive,
		ID:          "someid4",
		Side:        types.SideSell,
		Party:       party2,
		MarketID:    tm.market.GetID(),
		Size:        100,
		Price:       num.NewUint(auctionTriggeringPrice),
		Remaining:   100,
		CreatedAt:   now.UnixNano(),
		Reference:   "party2-sell-order-2",
	}
	confirmationSell, err = tm.market.SubmitOrder(context.Background(), orderSell2)
	require.NotNil(t, confirmationSell)
	require.NoError(t, err)

	require.Empty(t, confirmationSell.Trades)

	auctionEnd = tm.market.GetMarketData().AuctionEnd
	require.Equal(t, pMonitorAuctionEndTime.UnixNano(), auctionEnd) // In auction

	tm.now = pMonitorAuctionEndTime
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), pMonitorAuctionEndTime)
	assert.False(t, closed)

	tm.now = afterPMonitorAuction
	closed = tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), afterPMonitorAuction)
	assert.False(t, closed)
}

func TestOrderBook_Crash2718(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "aaa")
	addAccount(t, tm, "bbb")
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 100),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 100),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	// We start in continuous trading, create order to set best bid
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "aaa", 1, 100)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	// Now the pegged order which will be live
	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideBuy, "bbb", 1, 0)
	o2.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 10)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)
	assert.Equal(t, types.OrderStatusActive, o2.Status, o2.Status.String())
	assert.Equal(t, num.NewUint(90), o2.Price)

	// Force the pegged order to reprice
	o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order03", types.SideBuy, "aaa", 1, 110)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)
	now = now.Add(time.Second * 1)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	o2Update := tm.lastOrderUpdate(o2.ID)
	assert.Equal(t, types.OrderStatusActive, o2Update.Status)
	assert.Equal(t, num.NewUint(100), o2Update.Price)

	// Flip to auction so the pegged order will be parked
	tm.mas.StartOpeningAuction(now, &types.AuctionDuration{Duration: 10})
	tm.mas.AuctionStarted(ctx, now)
	tm.market.EnterAuction(ctx)
	o2Update = tm.lastOrderUpdate(o2.ID)
	assert.Equal(t, types.OrderStatusParked, o2Update.Status)
	assert.True(t, o2Update.Price.IsZero())

	// Flip out of auction to un-park it
	tm.market.LeaveAuctionWithIDGen(ctx, now.Add(time.Second*20), newTestIDGenerator())
	tm.market.LeaveAuctionWithIDGen(ctx, now.Add(time.Second*20), newTestIDGenerator())

	o2Update = tm.lastOrderUpdate(o2.ID)
	assert.Equal(t, types.OrderStatusActive, o2Update.Status)
	assert.Equal(t, num.NewUint(100), o2Update.Price)
}

func TestOrderBook_AmendPriceInParkedOrder(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "aaa")
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	// Create a parked pegged order
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "aaa", 1, 0)
	o1.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 10)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction
	require.NotNil(t, o1conf)
	require.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	assert.Equal(t, types.OrderStatusParked, o1.Status)
	assert.True(t, o1.Price.IsZero())

	// Try to amend the price
	amendment := &types.OrderAmendment{
		OrderID: o1.ID,
		Price:   num.NewUint(200),
	}

	// This should fail as we cannot amend a pegged order price
	amendConf, err := tm.market.AmendOrder(ctx, amendment, "aaa", vgcrypto.RandomHash())
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Nil(t, amendConf)
	require.Error(t, types.OrderErrorUnableToAmendPriceOnPeggedOrder, err)
}

func TestOrderBook_ExpiredOrderTriggersReprice(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "aaa")
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	// Create an expiring order
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTT, "Order01", types.SideBuy, "aaa", 1, 10)
	o1.ExpiresAt = now.Add(5 * time.Second).UnixNano()
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	// Create a pegged order that references its price
	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideBuy, "aaa", 1, 0)
	o2.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 2)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	// Move the clock forward to expire the first order
	now = now.Add(time.Second * 10)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	t.Run("order is parked", func(t *testing.T) {
		// First collect all the orders events
		found := map[string]*proto.Order{}
		for _, e := range tm.events {
			switch evt := e.(type) {
			case *events.Order:
				found[evt.Order().Id] = evt.Order()
			case *events.ExpiredOrders:
				for _, oid := range evt.OrderIDs() {
					found[oid] = &proto.Order{
						Status: types.OrderStatusExpired,
					}
				}
			}
		}

		require.Len(t, found, 2)

		expects := map[string]types.OrderStatus{
			o1.ID: types.OrderStatusExpired,
			o2.ID: types.OrderStatusParked,
		}

		for id, v := range found {
			require.Equal(t, expects[id], v.Status)
		}
	})
}

// This is a scenario to test issue: 2734
// Party A - 100000000
//
//	A - Buy 5@15000 GTC
//
// Party B - 100000000
//
//	B - Sell 10 IOC Market
//
// Party C - Deposit 100000
//
//	C - Buy GTT 6@1001 (60s)
//
// Party D- Fund 578
//
//	D - Pegged 3@BA +1
//
// Party E - Deposit 100000
//
//	E - Sell GTC 3@1002
//
// C amends order price=1002.
func TestOrderBook_CrashWithDistressedPartyPeggedOrderNotRemovedFromPeggedList2734(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 100000000)
	addAccountWithAmount(tm, "party-B", 100000000)
	addAccountWithAmount(tm, "party-C", 100000)
	addAccountWithAmount(tm, "party-D", 578)
	addAccountWithAmount(tm, "party-E", 100000)
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 1000000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)
	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 1000),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 1000),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	// leave opening auction
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "party-A", 5, 15000)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeMarket, types.OrderTimeInForceIOC, "Order02", types.SideSell, "party-B", 10, 0)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTT, "Order03", types.SideBuy, "party-C", 6, 1001)
	o3.ExpiresAt = now.Add(60 * time.Second).UnixNano()
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)

	o4 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order04", types.SideSell, "party-D", 3, 0)
	o4.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 1)
	o4conf, err := tm.market.SubmitOrder(ctx, o4)
	require.Nil(t, o4conf)
	require.Error(t, err)

	o5 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order05", types.SideSell, "party-E", 3, 1002)
	o5conf, err := tm.market.SubmitOrder(ctx, o5)
	require.NotNil(t, o5conf)
	require.NoError(t, err)

	// Try to amend the price
	amendment := &types.OrderAmendment{
		OrderID: o3.ID,
		Price:   num.NewUint(1002),
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "party-C", vgcrypto.RandomHash())
	require.NotNil(t, amendConf)
	require.NoError(t, err)

	// nothing to do we just expect no crash.
}

func TestOrderBook_Crash2733(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{Duration: 30})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 1000000)
	addAccountWithAmount(tm, "party-B", 1000000)
	addAccountWithAmount(tm, "party-C", 100000000)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	for i := 1; i <= 10; i++ {
		o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, fmt.Sprintf("Order1%v", i), types.SideBuy, "party-A", uint64(i), 0)
		o1.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, uint64(i*15))
		o1conf, err := tm.market.SubmitOrder(ctx, o1)
		require.NotNil(t, o1conf)
		require.NoError(t, err)

		o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, fmt.Sprintf("Order2%v", i), types.SideSell, "party-A", uint64(i), 0)
		o2.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, uint64(i*10))
		o2conf, err := tm.market.SubmitOrder(ctx, o2)
		require.NotNil(t, o2conf)
		require.NoError(t, err)

		o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, fmt.Sprintf("Order3%v", i), types.SideBuy, "party-A", uint64(i), 0)
		o3.PeggedOrder = newPeggedOrder(types.PeggedReferenceMid, uint64(i*5))
		o3conf, err := tm.market.SubmitOrder(ctx, o3)
		require.NotNil(t, o3conf)
		require.NoError(t, err)
	}

	// now move time to after auction
	now = now.Add(31 * time.Second)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)

	for i := 1; i <= 10; i++ {
		o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, fmt.Sprintf("Order4%v", i), types.SideSell, "party-B", uint64(i), uint64(i*150))
		o1conf, err := tm.market.SubmitOrder(ctx, o1)
		require.NotNil(t, o1conf)
		require.NoError(t, err)
	}

	for i := 1; i <= 20; i++ {
		o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, fmt.Sprintf("Order5%v", i), types.SideBuy, "party-C", uint64(i), uint64(i*100))
		o1conf, err := tm.market.SubmitOrder(ctx, o1)
		require.NotNil(t, o1conf)
		require.NoError(t, err)
	}
}

func TestOrderBook_Bug2747(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 100000000)
	addAccountWithAmount(tm, "party-B", 100000000)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "party-A", 100, 0)
	o1.PeggedOrder = newPeggedOrder(types.PeggedReferenceMid, 15)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	// Try to amend the price
	amendment := &types.OrderAmendment{
		OrderID:         o1.ID,
		PeggedOffset:    num.NewUint(20),
		PeggedReference: types.PeggedReferenceBestAsk,
	}
	amendConf, err := tm.market.AmendOrder(ctx, amendment, "party-A", vgcrypto.RandomHash())
	assert.Nil(t, amendConf)
	assert.EqualError(t, err, "OrderError: buy cannot reference best ask price")
}

func TestOrderBook_AmendTIME_IN_FORCEForPeggedOrder(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "aaa")
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 1000000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)
	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 100),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 100),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(time.Second * 2)
	tm.now = now
	tm.market.OnTick(ctx, now)
	// Create a normal order to set a BB price
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "aaa", 1, 10)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	// Create a pegged order that references the BB price with an expiry time
	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTT, "Order02", types.SideBuy, "aaa", 1, 0)
	o2.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 2)
	o2.ExpiresAt = now.Add(5 * time.Second).UnixNano()
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	// Amend the pegged order from GTT to GTC
	amendment := &types.OrderAmendment{
		OrderID:     o2.ID,
		TimeInForce: types.OrderTimeInForceGTC,
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "aaa", vgcrypto.RandomHash())
	require.NotNil(t, amendConf)
	require.NoError(t, err)
	assert.Equal(t, types.OrderStatusActive, o2.Status)

	// Move the clock forward to expire any old orders
	now = now.Add(time.Second * 10)
	tm.now = now
	tm.events = nil
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)
	t.Run("no orders expired", func(t *testing.T) {
		// First collect all the orders events
		orders := []*types.Order{}
		for _, e := range tm.events {
			switch evt := e.(type) {
			case *events.Order:
				if evt.Order().Status == types.OrderStatusExpired {
					orders = append(orders, mustOrderFromProto(evt.Order()))
				}
			}
		}
		require.Equal(t, 0, len(orders))
	})

	// The pegged order should not be expired
	assert.Equal(t, types.OrderStatusActive.String(), o2.Status.String())
	assert.Equal(t, 0, tm.market.GetPeggedExpiryOrderCount())
}

func TestOrderBook_AmendTIME_IN_FORCEForPeggedOrder2(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "aaa")
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 1000000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 100),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 100),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(5000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	// Create a normal order to set a BB price
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "aaa", 1, 10)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	// Create a pegged order that references the BB price
	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideBuy, "aaa", 1, 0)
	o2.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestBid, 2)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	exp := now.Add(5 * time.Second).UnixNano()
	// Amend the pegged order so that it has an expiry
	amendment := &types.OrderAmendment{
		OrderID:     o2.ID,
		TimeInForce: types.OrderTimeInForceGTT,
		ExpiresAt:   &exp,
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "aaa", vgcrypto.RandomHash())
	require.NotNil(t, amendConf)
	require.NoError(t, err)
	assert.Equal(t, types.OrderStatusActive, o2.Status)
	assert.Equal(t, 1, tm.market.GetPeggedExpiryOrderCount())

	// Move the clock forward to expire any old orders
	now = now.Add(time.Second * 10)
	tm.now = now
	tm.market.OnTick(vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash()), now)
	t.Run("1 order expired", func(t *testing.T) {
		// First collect all the orders events
		orders := []*types.Order{}
		for _, e := range tm.events {
			switch evt := e.(type) {
			case *events.ExpiredOrders:
				for _, oid := range evt.OrderIDs() {
					orders = append(orders, &types.Order{
						ID: oid,
					})
				}
			}
		}
		require.Equal(t, 1, len(orders))
		assert.Equal(t, orders[0].ID, o2.ID)
	})

	assert.Equal(t, 0, tm.market.GetPeggedExpiryOrderCount())
}

func TestOrderBook_AmendFilledWithActiveStatus2736(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "party-A")
	addAccount(t, tm, "party-B")
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 1000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 5000),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 5000),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(25000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)
	require.Equal(t, types.MarketTradingModeContinuous, tm.market.GetMarketData().MarketTradingMode)

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideSell, "party-A", 5, 5000)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	assert.NotNil(t, o1conf)
	assert.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideBuy, "party-B", 5, 4500)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	assert.NotNil(t, o2conf)
	assert.NoError(t, err)

	// Amend the pegged order so that it has an expiry
	amendment := &types.OrderAmendment{
		OrderID: o2.ID,
		Price:   num.NewUint(5000),
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "party-B", vgcrypto.RandomHash())
	assert.NotNil(t, amendConf)
	assert.NoError(t, err)
	o2Update := tm.lastOrderUpdate(o2.ID)
	assert.Equal(t, types.OrderStatusFilled, o2Update.Status, o2Update.Status.String())
}

func TestOrderBook_PeggedOrderReprice2748(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 100000000)
	addAccountWithAmount(tm, "party-B", 100000000)
	addAccountWithAmount(tm, "party-C", 100000000)
	auxParty, auxParty2 := "aux1", "aux2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideBuy, auxParty, 1, 1),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 10000),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 5000),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 5000),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(12500),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)
	// set the mid-price first to 6.5k
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "party-A", 5, 6000)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideSell, "party-B", 5, 7000)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	// then place pegged order
	o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order03", types.SideBuy, "party-C", 100, 0)
	o3.PeggedOrder = newPeggedOrder(types.PeggedReferenceMid, 15)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)

	assert.Equal(t, o3conf.Order.Status, types.OrderStatusActive)
	assert.Equal(t, 0, tm.market.GetParkedOrderCount())

	// then
	// Amend the pegged order so that it has an expiry
	amendment := &types.OrderAmendment{
		OrderID:      o3.ID,
		PeggedOffset: num.NewUint(6500),
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "party-C", vgcrypto.RandomHash())
	require.NotNil(t, amendConf)
	require.NoError(t, err)

	assert.Equal(t, amendConf.Order.Status, types.OrderStatusParked)
	assert.Equal(t, 1, tm.market.GetParkedOrderCount())
}

func TestOrderBook_AmendGFNToGTCOrGTTNotAllowed2486(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 100000000)
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 1000000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 6000),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 6000),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(25000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	// set the mid-price first to 6.5k
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFN, "Order01", types.SideBuy, "party-A", 5, 6000)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	// then
	// Amend the pegged order so that it has an expiry
	amendment := &types.OrderAmendment{
		OrderID:     o1.ID,
		TimeInForce: types.OrderTimeInForceGTC,
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "party-A", vgcrypto.RandomHash())
	assert.Nil(t, amendConf)
	assert.EqualError(t, err, "OrderError: Cannot amend TIF from GFA or GFN")
}

func TestOrderBook_CancelAll2771(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 100000000)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideSell, "party-A", 1, 0)
	o1.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 10)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction
	assert.Equal(t, o1conf.Order.Status, types.OrderStatusParked)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideSell, "party-A", 1, 0)
	o2.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 10)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)
	assert.Equal(t, o2conf.Order.Status, types.OrderStatusParked)

	confs, err := tm.market.CancelAllOrders(ctx, "party-A")
	assert.NoError(t, err)
	assert.Len(t, confs, 2)
}

func TestOrderBook_RejectAmendPriceOnPeggedOrder2658(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "party-A")
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "party-A", 5, 5000)
	o1.PeggedOrder = newPeggedOrder(types.PeggedReferenceMid, 10)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	assert.NotNil(t, o1conf)
	assert.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	// Try to amend the price
	amendment := &types.OrderAmendment{
		OrderID:   o1.ID,
		Price:     num.NewUint(4000),
		SizeDelta: 10,
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "party-A", vgcrypto.RandomHash())
	assert.Nil(t, amendConf)
	assert.Error(t, types.OrderErrorUnableToAmendPriceOnPeggedOrder, err)
	assert.Equal(t, types.OrderStatusParked, o1.Status)
	assert.Equal(t, uint64(1), o1.Version)
}

func TestOrderBook_AmendToCancelForceReprice(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "party-A")
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideSell, "party-A", 1, 5000)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	assert.NotNil(t, o1conf)
	assert.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideSell, "party-A", 1, 0)
	o2.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 10)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	assert.NotNil(t, o2conf)
	assert.NoError(t, err)

	// Try to amend the price
	amendment := &types.OrderAmendment{
		OrderID:   o1.ID,
		SizeDelta: -1,
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "party-A", vgcrypto.RandomHash())
	assert.NotNil(t, amendConf)
	assert.NoError(t, err)

	assert.Equal(t, types.OrderStatusActive, o2.Status)
	o1Update := tm.lastOrderUpdate(o1.ID)
	assert.Equal(t, types.OrderStatusCancelled, o1Update.Status)
}

func TestOrderBook_AmendExpPersistParkPeggedOrder(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "party-A")
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideSell, "party-A", 10, 4550)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	assert.NotNil(t, o1conf)
	assert.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideSell, "party-A", 105, 0)
	o2.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 100)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	assert.NotNil(t, o2conf)
	assert.NoError(t, err)

	// Try to amend the price
	amendment := &types.OrderAmendment{
		OrderID:   o1.ID,
		SizeDelta: -10,
	}

	amendConf, err := tm.market.AmendOrder(ctx, amendment, "party-A", vgcrypto.RandomHash())
	assert.NotNil(t, amendConf)
	assert.NoError(t, err)
	assert.Equal(t, types.OrderStatusActive, o2.Status)
	assert.False(t, o2.Price.IsZero())
	o1Update := tm.lastOrderUpdate(o1.ID)
	assert.Equal(t, types.OrderStatusCancelled, o1Update.Status)
}

// This test is to make sure when we move into a price monitoring auction that we
// do not allow the parked orders to be repriced.
func TestOrderBook_ParkPeggedOrderWhenMovingToAuction(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket2(t, now, nil, &types.AuctionDuration{
		Duration: 1,
		// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	}, true, 1.01)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "party-A")
	auxParty := "auxParty"
	auxParty2 := "auxParty2"
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)
	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 1000000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 1000),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty2, 1, 1000),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(25000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFN, "Order01", types.SideSell, "party-A", 10, 1010)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGFN, "Order02", types.SideBuy, "party-A", 10, 990)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "PeggyWeggy", types.SideSell, "party-A", 10, 0)
	o3.PeggedOrder = newPeggedOrder(types.PeggedReferenceBestAsk, 100)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)
	assert.Equal(t, int64(5), tm.market.GetOrdersOnBookCount())

	// Move into a price monitoring auction so that the pegged orders are parked and the other orders are cancelled
	tm.market.StartPriceAuction(now)
	tm.market.EnterAuction(ctx)
	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction

	require.Equal(t, 1, tm.market.GetPeggedOrderCount())
	require.Equal(t, 1, tm.market.GetParkedOrderCount())
	assert.Equal(t, int64(2), tm.market.GetOrdersOnBookCount())
}

func TestMarket_LeaveAuctionRepricePeggedOrdersShouldFailIfNoMargin(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	// Create a new party account with very little funding
	addAccountWithAmount(tm, "party-C", 1)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	// Start the opening auction
	tm.mas.StartOpeningAuction(now, &types.AuctionDuration{Duration: 10})
	tm.mas.AuctionStarted(ctx, now)
	tm.market.EnterAuction(ctx)

	lps := &types.LiquidityProvisionSubmission{
		Fee:              num.DecimalFromFloat(0.01),
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(1000000000),
	}

	// Because we do not have enough funds to support our commitment level, we should reject this call
	err := tm.market.SubmitLiquidityProvision(ctx, lps, "party-C", vgcrypto.RandomHash())
	require.Error(t, err)
}

func TestMarket_LeaveAuctionAndRepricePeggedOrders(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, nil)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccount(t, tm, "party-A")
	addAccount(t, tm, "party-B")
	addAccount(t, tm, "party-C")
	auxParty := "auxParty"
	addAccount(t, tm, auxParty)
	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()

	// Start the opening auction
	tm.mas.StartOpeningAuction(now, &types.AuctionDuration{Duration: 10})
	tm.mas.AuctionStarted(ctx, now)
	tm.market.EnterAuction(ctx)

	// Add orders that will outlive the auction to set the reference prices
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideSell, "party-A", 10, 1010)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideBuy, "party-A", 10, 990)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order03", types.SideSell, "party-A", 1, 1000)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)

	o4 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order04", types.SideBuy, "party-A", 1, 1000)
	o4conf, err := tm.market.SubmitOrder(ctx, o4)
	require.NotNil(t, o4conf)
	require.NoError(t, err)

	require.Equal(t, int64(4), tm.market.GetOrdersOnBookCount())

	lps := &types.LiquidityProvisionSubmission{
		Fee:              num.DecimalFromFloat(0.01),
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(1000000000),
	}

	err = tm.market.SubmitLiquidityProvision(ctx, lps, "party-C", vgcrypto.RandomHash())
	require.NoError(t, err)

	// Leave the auction so pegged orders are unparked
	tm.market.LeaveAuctionWithIDGen(ctx, now.Add(time.Second*20), newTestIDGenerator())

	require.Equal(t, int64(2), tm.market.GetOrdersOnBookCount())
	require.Equal(t, 0, tm.market.GetPeggedOrderCount())
	require.Equal(t, 0, tm.market.GetParkedOrderCount())

	// Remove an order to invalidate reference prices and force pegged orders to park
	_, err = tm.market.CancelOrder(ctx, o1.Party, o1.ID, vgcrypto.RandomHash())
	require.NoError(t, err)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction

	// 1 live orders, 1 normal
	// all LP have been removed as cannot be repriced.
	assert.Equal(t, int64(1), tm.market.GetOrdersOnBookCount())
	assert.Equal(t, 0, tm.market.GetPeggedOrderCount())
	assert.Equal(t, 0, tm.market.GetParkedOrderCount())
}

func TestOrderBook_PartiallyFilledMarketOrderThatWouldWashIOC(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1000,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 10000000)
	addAccountWithAmount(tm, "party-B", 10000000)
	auxParty := "auxParty"
	addAccount(t, tm, auxParty)

	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	// Leave auction right away
	tm.market.LeaveAuctionWithIDGen(ctx, now.Add(time.Second*20), newTestIDGenerator())

	// Create 2 buy orders that we will try to match against
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "party-B", 10, 100)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideBuy, "party-A", 10, 90)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	// Send the sell order with enough volume to match both existing trades
	o3 := getMarketOrder(tm, now, types.OrderTypeMarket, types.OrderTimeInForceIOC, "Order03", types.SideSell, "party-A", 20, 0)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)
	assert.Equal(t, types.OrderStatusPartiallyFilled, o3.Status)
	assert.Equal(t, uint64(10), o3.Remaining)
}

func TestOrderBook_PartiallyFilledMarketOrderThatWouldWashFOKSell(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1000,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 10000000)
	addAccountWithAmount(tm, "party-B", 10000000)
	auxParty := "auxParty"
	addAccount(t, tm, auxParty)

	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	// Leave auction right away
	tm.market.LeaveAuctionWithIDGen(ctx, now.Add(time.Second*20), newTestIDGenerator())

	// Create 2 buy orders that we will try to match against
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "party-B", 10, 100)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideBuy, "party-A", 10, 90)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	// Send the sell order with enough volume to match both existing trades
	o3 := getMarketOrder(tm, now, types.OrderTypeMarket, types.OrderTimeInForceFOK, "Order03", types.SideSell, "party-A", 20, 0)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)

	// A wash trade during a FOK order will stop the order fully unfilled
	require.Equal(t, types.OrderStatusStopped, o3.Status)
	assert.Equal(t, uint64(20), o3.Remaining)

	// Send the sell order with only enough volume to match the opposite party
	o4 := getMarketOrder(tm, now, types.OrderTypeMarket, types.OrderTimeInForceFOK, "Order04", types.SideSell, "party-A", 5, 0)
	o4conf, err := tm.market.SubmitOrder(ctx, o4)
	require.NotNil(t, o4conf)
	require.NoError(t, err)

	// Fully matches
	require.Equal(t, types.OrderStatusFilled, o4.Status)
	assert.Equal(t, uint64(0), o4.Remaining)
}

func TestOrderBook_PartiallyFilledMarketOrderThatWouldWashFOKBuy(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket2(t, now, nil, &types.AuctionDuration{
		Duration: 1,
		// increase lpRange so that LP orders don't get pushed too close to MID and test can behave as expected
	}, true, 1)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	auxParty, auxParty2 := "auxParty", "auxParty2"
	addAccountWithAmount(tm, "party-A", 10000000)
	addAccountWithAmount(tm, "party-B", 10000000)
	addAccount(t, tm, auxParty)
	addAccount(t, tm, auxParty2)
	addAccountWithAmount(tm, "lpprov", 10000000)

	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)

	auxOrders := []*types.Order{
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux1", types.SideSell, auxParty, 1, 100000),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux2", types.SideBuy, auxParty, 1, 1),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux3", types.SideSell, auxParty, 1, 100),
		getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "aux4", types.SideBuy, auxParty2, 1, 100),
	}
	for _, o := range auxOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(25000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	// Leave auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	// Create 2 buy orders that we will try to match against
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideSell, "party-B", 10, 100)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideSell, "party-A", 10, 110)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	o5 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order05", types.SideBuy, "party-B", 10, 90)
	o5conf, err := tm.market.SubmitOrder(ctx, o5)
	require.NotNil(t, o5conf)
	require.NoError(t, err)

	// Send the sell order with enough volume to match both existing trades
	o3 := getMarketOrder(tm, now, types.OrderTypeMarket, types.OrderTimeInForceFOK, "Order03", types.SideBuy, "party-A", 15, 0)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)

	// A wash trade during a FOK order will stop the order fully unfilled
	require.Equal(t, types.OrderStatusStopped, o3.Status)
	assert.EqualValues(t, 15, o3.Remaining)

	// Send the sell order with only enough volume to match the opposite party
	o4 := getMarketOrder(tm, now, types.OrderTypeMarket, types.OrderTimeInForceFOK, "Order04", types.SideBuy, "party-A", 5, 0)
	o4conf, err := tm.market.SubmitOrder(ctx, o4)
	require.NotNil(t, o4conf)
	require.NoError(t, err)

	// A wash trade during a FOK order will stop the order fully unfilled
	require.Equal(t, types.OrderStatusFilled, o4.Status)
	assert.Equal(t, uint64(0), o4.Remaining)
}

func TestOrderBook_PartiallyFilledLimitOrderThatWouldWashFOK(t *testing.T) {
	now := time.Unix(10, 0)
	tm := getTestMarket(t, now, nil, &types.AuctionDuration{
		Duration: 1000,
	})
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	addAccountWithAmount(tm, "party-A", 10000000)
	addAccountWithAmount(tm, "party-B", 10000000)
	auxParty := "auxParty"
	addAccount(t, tm, auxParty)

	alwaysOnBid := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnBid", types.SideBuy, auxParty, 1, 1)
	conf, err := tm.market.SubmitOrder(context.Background(), alwaysOnBid)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	alwaysOnAsk := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "alwaysOnAsk", types.SideSell, auxParty, 1, 100000)
	conf, err = tm.market.SubmitOrder(context.Background(), alwaysOnAsk)
	require.NotNil(t, conf)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, conf.Order.Status)

	// Leave auction right away
	tm.market.LeaveAuctionWithIDGen(ctx, now.Add(time.Second*20), newTestIDGenerator())

	md := tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)

	// Create 2 buy orders that we will try to match against
	o1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order01", types.SideBuy, "party-B", 10, 100)
	o1conf, err := tm.market.SubmitOrder(ctx, o1)
	require.NotNil(t, o1conf)
	require.NoError(t, err)

	o2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "Order02", types.SideBuy, "party-A", 10, 90)
	o2conf, err := tm.market.SubmitOrder(ctx, o2)
	require.NotNil(t, o2conf)
	require.NoError(t, err)

	// Send the sell order with enough volume to match both existing trades
	o3 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceFOK, "Order03", types.SideSell, "party-A", 20, 90)
	o3conf, err := tm.market.SubmitOrder(ctx, o3)
	require.NotNil(t, o3conf)
	require.NoError(t, err)

	// A wash trade during FOK will stop the order filly unfilled
	require.Equal(t, types.OrderStatusStopped, o3.Status)
	assert.Equal(t, uint64(20), o3.Remaining)

	// Send the sell order with only enough volume to match the opposite party
	o4 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceFOK, "Order04", types.SideSell, "party-A", 5, 90)
	o4conf, err := tm.market.SubmitOrder(ctx, o4)
	require.NotNil(t, o4conf)
	require.NoError(t, err)

	// A wash trade during FOK will stop the order filly unfilled
	require.Equal(t, types.OrderStatusFilled, o4.Status)
	assert.Equal(t, uint64(0), o4.Remaining)
}

func Test3008And3007CancelLiquidityProvision(t *testing.T) {
	now := time.Unix(10, 0)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	mktCfg := getMarket(defaultPriceMonitorSettings, &types.AuctionDuration{
		Duration: 10000,
	})
	mktCfg.Fees.Factors = &types.FeeFactors{
		LiquidityFee:      num.DecimalFromFloat(0.001),
		InfrastructureFee: num.DecimalFromFloat(0.0005),
		MakerFee:          num.DecimalFromFloat(0.00025),
	}
	mktCfg.TradableInstrument.RiskModel = &types.TradableInstrumentLogNormalRiskModel{
		LogNormalRiskModel: &types.LogNormalRiskModel{
			RiskAversionParameter: num.DecimalFromFloat(0.001),
			Tau:                   num.DecimalFromFloat(0.00011407711613050422),
			Params: &types.LogNormalModelParams{
				Mu:    num.DecimalZero(),
				R:     num.DecimalFromFloat(0.016),
				Sigma: num.DecimalFromFloat(20),
			},
		},
	}

	tm := newTestMarket(t, now).Run(ctx, mktCfg)
	tm.StartOpeningAuction().
		WithAccountAndAmount("party-0", 1000000).
		WithAccountAndAmount("party-1", 1000000).
		WithAccountAndAmount("party-2", 10000000000).
		// provide stake as well but will cancel
		WithAccountAndAmount("party-2-bis", 10000000000).
		WithAccountAndAmount("party-3", 1000000).
		WithAccountAndAmount("party-4", 1000000)

	tm.now = now
	tm.market.OnTick(ctx, now)

	orderParams := []struct {
		id        string
		size      uint64
		side      types.Side
		tif       types.OrderTimeInForce
		pegRef    types.PeggedReference
		pegOffset *num.Uint
	}{
		{"party-4", 1, types.SideBuy, types.OrderTimeInForceGTC, types.PeggedReferenceBestBid, num.NewUint(2000)},
		{"party-3", 1, types.SideSell, types.OrderTimeInForceGTC, types.PeggedReferenceBestAsk, num.NewUint(1000)},
	}
	partyA, partyB := orderParams[0], orderParams[1]

	tpl := OrderTemplate{
		Type: types.OrderTypeLimit,
	}
	orders := []*types.Order{
		// Limit Orders
		tpl.New(types.Order{
			Size:        20,
			Remaining:   20,
			Price:       num.UintZero().Sub(num.NewUint(5500), partyA.pegOffset), // 3500
			Side:        types.SideBuy,
			Party:       "party-0",
			TimeInForce: types.OrderTimeInForceGFA,
		}),
		tpl.New(types.Order{
			Size:        20,
			Remaining:   20,
			Price:       num.UintZero().Sub(num.NewUint(5000), partyB.pegOffset), // 4000
			Side:        types.SideSell,
			Party:       "party-1",
			TimeInForce: types.OrderTimeInForceGFA,
		}),
		tpl.New(types.Order{
			Size:        10,
			Remaining:   10,
			Price:       num.NewUint(5500),
			Side:        types.SideBuy,
			Party:       "party-2",
			TimeInForce: types.OrderTimeInForceGFA,
		}),
		tpl.New(types.Order{
			Size:        100,
			Remaining:   100,
			Price:       num.NewUint(5000),
			Side:        types.SideSell,
			Party:       "party-2",
			TimeInForce: types.OrderTimeInForceGTC,
		}),
		tpl.New(types.Order{
			Size:        100,
			Remaining:   100,
			Price:       num.NewUint(3500),
			Side:        types.SideBuy,
			Party:       "party-0",
			TimeInForce: types.OrderTimeInForceGTC,
		}),
		tpl.New(types.Order{
			Size:        20,
			Remaining:   20,
			Price:       num.NewUint(8500),
			Side:        types.SideBuy,
			Party:       "party-0",
			TimeInForce: types.OrderTimeInForceGTC,
		}),

		// Pegged Orders
		tpl.New(types.Order{
			Party:       partyA.id,
			Side:        partyA.side,
			Size:        partyA.size,
			Remaining:   partyA.size,
			TimeInForce: partyA.tif,
			PeggedOrder: &types.PeggedOrder{Reference: partyA.pegRef, Offset: partyA.pegOffset},
		}),
		tpl.New(types.Order{
			Party:       partyB.id,
			Side:        partyB.side,
			Size:        partyB.size,
			Remaining:   partyB.size,
			TimeInForce: partyB.tif,
			PeggedOrder: &types.PeggedOrder{Reference: partyB.pegRef, Offset: partyB.pegOffset},
		}),
	}

	tm.WithSubmittedOrders(t, orders...)

	// Add a LPSubmission
	// this is a log of stake, enough to cover all
	// the required stake for the market
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(2000000),
		Fee:              num.DecimalFromFloat(0.01),
	}

	require.NoError(t, tm.market.SubmitLiquidityProvision(ctx, lp, "party-2", vgcrypto.RandomHash()))
	// Leave the auction
	tm.now = now.Add(10001 * time.Second)
	tm.market.OnTick(ctx, tm.now)

	assert.Equal(t, 1, tm.market.GetLPSCount())

	// this is our second stake provider
	// small player
	lp2 := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(1000),
		Fee:              num.DecimalFromFloat(0.01),
	}

	// cleanup the events, we want to make sure our orders are created
	tm.events = nil

	require.NoError(t, tm.market.SubmitLiquidityProvision(
		ctx, lp2, "party-2-bis", vgcrypto.RandomHash()))

	tm.market.OnEpochEvent(ctx, types.Epoch{Action: proto.EpochAction_EPOCH_ACTION_START})

	assert.Equal(t, 2, tm.market.GetLPSCount())

	tm.now = now.Add(10011 * time.Second)
	tm.market.OnTick(ctx, tm.now)

	// now we do a cancellation
	lpCancel := &types.LiquidityProvisionCancellation{
		MarketID: tm.market.GetID(),
	}

	// cleanup the events before we continue
	tm.events = nil

	require.NoError(t, tm.market.CancelLiquidityProvision(
		ctx, lpCancel, "party-2-bis"))

	assert.Equal(t, 2, tm.market.GetLPSCount())

	t.Run("LiquidityProvision_CANCELLED", func(t *testing.T) {
		// Filter events until LP is found
		var found *vegapb.LiquidityProvision
		for _, e := range tm.events {
			switch evt := e.(type) {
			case *events.LiquidityProvision:
				if evt.LiquidityProvision().PartyId == "party-2-bis" {
					found = evt.LiquidityProvision()
				}
			}
		}
		require.NotNil(t, found)
		assert.Equal(t, types.LiquidityProvisionStatusPending.String(), found.Status.String())
	})
}

func Test2963EnsureEquityShareAreInMarketData(t *testing.T) {
	now := time.Unix(10, 0)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	mktCfg := getMarket(defaultPriceMonitorSettings, &types.AuctionDuration{
		Duration: 10000,
	})
	mktCfg.Fees.Factors = &types.FeeFactors{
		LiquidityFee:      num.DecimalFromFloat(0.001),
		InfrastructureFee: num.DecimalFromFloat(0.0005),
		MakerFee:          num.DecimalFromFloat(0.00025),
	}
	mktCfg.TradableInstrument.RiskModel = &types.TradableInstrumentLogNormalRiskModel{
		LogNormalRiskModel: &types.LogNormalRiskModel{
			RiskAversionParameter: num.DecimalFromFloat(0.001),
			Tau:                   num.DecimalFromFloat(0.00011407711613050422),
			Params: &types.LogNormalModelParams{
				Mu:    num.DecimalZero(),
				R:     num.DecimalFromFloat(0.016),
				Sigma: num.DecimalFromFloat(20),
			},
		},
	}

	tm := newTestMarket(t, now).Run(ctx, mktCfg)
	tm.StartOpeningAuction().
		WithAccountAndAmount("party-0", 1000000).
		WithAccountAndAmount("party-1", 1000000).
		WithAccountAndAmount("party-2", 10000000000).
		// provide stake as well but will cancel
		WithAccountAndAmount("party-2-bis", 10000000000).
		WithAccountAndAmount("party-3", 1000000).
		WithAccountAndAmount("party-4", 1000000)

	tm.now = now
	tm.market.OnTick(ctx, now)

	orderParams := []struct {
		id        string
		size      uint64
		side      types.Side
		tif       types.OrderTimeInForce
		pegRef    types.PeggedReference
		pegOffset *num.Uint
	}{
		{"party-4", 1, types.SideBuy, types.OrderTimeInForceGTC, types.PeggedReferenceBestBid, num.NewUint(2000)},
		{"party-3", 1, types.SideSell, types.OrderTimeInForceGTC, types.PeggedReferenceBestAsk, num.NewUint(1000)},
	}
	partyA, partyB := orderParams[0], orderParams[1]

	tpl := OrderTemplate{
		Type: types.OrderTypeLimit,
	}
	orders := []*types.Order{
		// Limit Orders
		tpl.New(types.Order{
			Size:        20,
			Remaining:   20,
			Price:       num.Sum(num.NewUint(5500), partyA.pegOffset), // 3500
			Side:        types.SideBuy,
			Party:       "party-0",
			TimeInForce: types.OrderTimeInForceGFA,
		}),
		tpl.New(types.Order{
			Size:        20,
			Remaining:   20,
			Price:       num.Sum(num.NewUint(5000), partyA.pegOffset), // 4000
			Side:        types.SideSell,
			Party:       "party-1",
			TimeInForce: types.OrderTimeInForceGFA,
		}),
		tpl.New(types.Order{
			Size:        10,
			Remaining:   10,
			Price:       num.NewUint(5500),
			Side:        types.SideBuy,
			Party:       "party-2",
			TimeInForce: types.OrderTimeInForceGFA,
		}),
		tpl.New(types.Order{
			Size:        100,
			Remaining:   100,
			Price:       num.NewUint(5000),
			Side:        types.SideSell,
			Party:       "party-2",
			TimeInForce: types.OrderTimeInForceGTC,
		}),
		tpl.New(types.Order{
			Size:        100,
			Remaining:   100,
			Price:       num.NewUint(3500),
			Side:        types.SideBuy,
			Party:       "party-0",
			TimeInForce: types.OrderTimeInForceGTC,
		}),
		tpl.New(types.Order{
			Size:        20,
			Remaining:   20,
			Price:       num.NewUint(8500),
			Side:        types.SideBuy,
			Party:       "party-0",
			TimeInForce: types.OrderTimeInForceGTC,
		}),

		// Pegged Orders
		tpl.New(types.Order{
			Party:       partyA.id,
			Side:        partyA.side,
			Size:        partyA.size,
			Remaining:   partyA.size,
			TimeInForce: partyA.tif,
			PeggedOrder: &types.PeggedOrder{Reference: partyA.pegRef, Offset: partyA.pegOffset},
		}),
		tpl.New(types.Order{
			Party:       partyB.id,
			Side:        partyB.side,
			Size:        partyB.size,
			Remaining:   partyB.size,
			TimeInForce: partyB.tif,
			PeggedOrder: &types.PeggedOrder{Reference: partyB.pegRef, Offset: partyB.pegOffset},
		}),
	}

	tm.WithSubmittedOrders(t, orders...)

	// Add a LPSubmission
	// this is a log of stake, enough to cover all
	// the required stake for the market
	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(2000000),
		Fee:              num.DecimalFromFloat(0.01),
	}

	// Leave the auction
	tm.now = now.Add(10001 * time.Second)
	tm.market.OnTick(ctx, tm.now)

	require.NoError(t, tm.market.SubmitLiquidityProvision(ctx, lp, "party-2", vgcrypto.RandomHash()))
	assert.Equal(t, 0, tm.market.GetLPSCount())

	// this is our second stake provider
	// small player
	lp2 := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(1000),
		Fee:              num.DecimalFromFloat(0.01),
	}

	// cleanup the events, we want to make sure our orders are created
	tm.events = nil

	require.NoError(t, tm.market.SubmitLiquidityProvision(
		ctx, lp2, "party-2-bis", vgcrypto.RandomHash()))
	assert.Equal(t, 0, tm.market.GetLPSCount())

	tm.now = now.Add(10011 * time.Second)
	tm.market.OnTick(ctx, tm.now)
	tm.market.OnEpochEvent(ctx, types.Epoch{Action: proto.EpochAction_EPOCH_ACTION_START})

	mktData := tm.market.GetMarketData()
	assert.Len(t, mktData.LiquidityProviderFeeShare, 2)
}

func TestAverageEntryValuation(t *testing.T) {
	now := time.Unix(10, 0)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	auctionEnd := now.Add(10001 * time.Second)
	mktCfg := getMarket(defaultPriceMonitorSettings, &types.AuctionDuration{
		Duration: 10000,
	})
	mktCfg.Fees.Factors = &types.FeeFactors{
		LiquidityFee:      num.DecimalFromFloat(0.001),
		InfrastructureFee: num.DecimalFromFloat(0.0005),
		MakerFee:          num.DecimalFromFloat(0.00025),
	}
	mktCfg.TradableInstrument.RiskModel = &types.TradableInstrumentLogNormalRiskModel{
		LogNormalRiskModel: &types.LogNormalRiskModel{
			RiskAversionParameter: num.DecimalFromFloat(0.001),
			Tau:                   num.DecimalFromFloat(0.00011407711613050422),
			Params: &types.LogNormalModelParams{
				Mu:    num.DecimalZero(),
				R:     num.DecimalFromFloat(0.016),
				Sigma: num.DecimalFromFloat(20),
			},
		},
	}

	lpparty := "lp-party-1"
	lpparty2 := "lp-party-2"
	lpparty3 := "lp-party-3"

	tm := newTestMarket(t, now).Run(ctx, mktCfg)
	tm.StartOpeningAuction().
		// the liquidity provider
		WithAccountAndAmount(lpparty, 500000000000).
		WithAccountAndAmount(lpparty2, 500000000000).
		WithAccountAndAmount(lpparty3, 500000000000)

	// Add a LPSubmission
	// this is a log of stake, enough to cover all
	// the required stake for the market
	lpSubmission := types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(80000),
		Fee:              num.DecimalFromFloat(0.01),
		Reference:        "ref-lp-submission-1",
	}

	// submit our lp
	require.NoError(t,
		tm.market.SubmitLiquidityProvision(
			ctx, &lpSubmission, lpparty, vgcrypto.RandomHash()),
	)

	lpSubmission2 := lpSubmission
	lpSubmission2.CommitmentAmount = lpSubmission.CommitmentAmount.Clone()
	lpSubmission2.Reference = "lp-submission-2"
	// submit our lp
	require.NoError(t,
		tm.market.SubmitLiquidityProvision(
			ctx, &lpSubmission2, lpparty2, vgcrypto.RandomHash()),
	)

	lpSubmission3 := lpSubmission
	lpSubmission3.CommitmentAmount = lpSubmission.CommitmentAmount.Clone()
	lpSubmission3.Reference = "lp-submission-3"
	// submit our lp
	require.NoError(t,
		tm.market.SubmitLiquidityProvision(
			ctx, &lpSubmission3, lpparty3, vgcrypto.RandomHash()),
	)
	// after LP submission
	tm.EndOpeningAuction(t, auctionEnd, false)

	marketData := tm.market.GetMarketData()
	/*
		expects := map[string]struct {
			found bool
			value string
		}{
			lpparty:  {value: "0.5454545454545455"}, // 6/9
			lpparty2: {value: "0.2727272727272727"}, // 3/9
			lpparty3: {value: "0.1818181818181818"}, // 2/9
		}*/
	// because we now have to submit the LP before leaving auction, all LPs provide the same
	expects := map[string]struct {
		found bool
		value string
	}{
		lpparty:  {value: "0.3333333333333333"},
		lpparty2: {value: "0.3333333333333333"},
		lpparty3: {value: "0.3333333333333333"},
	}

	for _, v := range marketData.LiquidityProviderFeeShare {
		expv, ok := expects[v.Party]
		assert.True(t, ok, "unexpected lp provider in market data", v.Party)
		assert.Equal(t, expv.value, v.EquityLikeShare)
		expv.found = true
		expects[v.Party] = expv
	}

	// now ensure all are found
	for k, v := range expects {
		assert.True(t, v.found, "was not in the list of lp providers", k)
	}
}

func TestBondAccountIsReleasedItMarketRejected(t *testing.T) {
	now := time.Unix(10, 0)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	mktCfg := getMarket(defaultPriceMonitorSettings, &types.AuctionDuration{
		Duration: 10000,
	})
	mktCfg.Fees.Factors = &types.FeeFactors{
		LiquidityFee:      num.DecimalFromFloat(0.001),
		InfrastructureFee: num.DecimalFromFloat(0.0005),
		MakerFee:          num.DecimalFromFloat(0.00025),
	}
	mktCfg.TradableInstrument.RiskModel = &types.TradableInstrumentLogNormalRiskModel{
		LogNormalRiskModel: &types.LogNormalRiskModel{
			RiskAversionParameter: num.DecimalFromFloat(0.001),
			Tau:                   num.DecimalFromFloat(0.00011407711613050422),
			Params: &types.LogNormalModelParams{
				Mu:    num.DecimalZero(),
				R:     num.DecimalFromFloat(0.016),
				Sigma: num.DecimalFromFloat(20),
			},
		},
	}

	lpparty := "lp-party-1"

	tm := newTestMarket(t, now).Run(ctx, mktCfg)
	tm.WithAccountAndAmount(lpparty, 500000)

	tm.now = now
	tm.market.OnTick(ctx, now)

	// Add a LPSubmission
	// this is a log of stake, enough to cover all
	// the required stake for the market
	lpSubmission := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(150000),
		Fee:              num.DecimalFromFloat(0.01),
		Reference:        "ref-lp-submission-1",
	}

	// submit our lp
	require.NoError(t,
		tm.market.SubmitLiquidityProvision(
			ctx, lpSubmission, lpparty, vgcrypto.RandomHash()),
	)

	t.Run("bond account is updated with the new commitment", func(t *testing.T) {
		bacc, err := tm.collateralEngine.GetPartyBondAccount(
			tm.market.GetID(), lpparty, tm.asset)
		assert.NoError(t, err)
		assert.Equal(t, num.NewUint(150000), bacc.Balance)
		gacc, err := tm.collateralEngine.GetPartyGeneralAccount(
			lpparty, tm.asset)
		assert.NoError(t, err)
		assert.Equal(t, num.NewUint(350000), gacc.Balance)
	})

	// now we reject the network and our party bond account should be released to general
	assert.NoError(t,
		tm.market.Reject(context.Background()),
	)

	t.Run("bond is released to general account", func(t *testing.T) {
		// an error as the bond account is being deleted
		_, err := tm.collateralEngine.GetPartyBondAccount(
			tm.market.GetID(), lpparty, tm.asset)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "account does not exist:")
		gacc, err := tm.collateralEngine.GetPartyGeneralAccount(
			lpparty, tm.asset)
		assert.NoError(t, err)
		assert.Equal(t, num.NewUint(500000), gacc.Balance)
	})
}

// TODO karel - write a test with new liquidity
// func TestLiquidityMonitoring_GoIntoAndOutOfAuction(t *testing.T) {
// 	now := time.Unix(10, 0)
// 	openingDuration := &types.AuctionDuration{
// 		Duration: 1,
// 	}
// 	tm := getTestMarket(t, now, nil, openingDuration)
// 	c1 := 0.7
// 	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
// 	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)

// 	md := tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)

// 	lp1 := "lp1"
// 	lp2 := "lp2"
// 	party1 := "party1"
// 	party2 := "party2"
// 	auxParty, auxParty2 := "auxParty", "auxParty2"

// 	addAccount(t, tm, lp1)
// 	addAccount(t, tm, lp2)
// 	addAccount(t, tm, party1)
// 	addAccount(t, tm, party2)
// 	addAccount(t, tm, auxParty)
// 	addAccount(t, tm, auxParty2)

// 	lp1Commitment := num.NewUint(50000)
// 	lp2Commitment := num.NewUint(10000)

// 	matchingPrice := uint64(100)
// 	// Add orders that will stay on the book thus maintaining best_bid and best_ask
// 	buyOrder1 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder1", types.SideBuy, party1, 1, matchingPrice-10)
// 	buyConf1, err := tm.market.SubmitOrder(ctx, buyOrder1)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusActive, buyConf1.Order.Status)

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)

// 	sellOrder1 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder1", types.SideSell, party2, 1, matchingPrice+10)
// 	sellConf1, err := tm.market.SubmitOrder(ctx, sellOrder1)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusActive, sellConf1.Order.Status)

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)

// 	lp1sub := &types.LiquidityProvisionSubmission{
// 		MarketID:         tm.market.GetID(),
// 		CommitmentAmount: lp1Commitment,
// 		Fee:              num.DecimalFromFloat(0.05),
// 	}

// 	lp2sub := &types.LiquidityProvisionSubmission{
// 		MarketID:         tm.market.GetID(),
// 		CommitmentAmount: lp2Commitment,
// 		Fee:              num.DecimalFromFloat(0.1),
// 	}

// 	require.NoError(t,
// 		tm.market.SubmitLiquidityProvision(ctx, lp1sub, lp1, vgcrypto.RandomHash()),
// 	)

// 	require.NoError(t,
// 		tm.market.SubmitLiquidityProvision(ctx, lp2sub, lp2, vgcrypto.RandomHash()),
// 	)

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)

// 	buyOrder2 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder2", types.SideBuy, party1, 1, matchingPrice)
// 	buyConf2, err := tm.market.SubmitOrder(ctx, buyOrder2)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusActive, buyConf2.Order.Status)

// 	sellOrder2 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder2", types.SideSell, party2, 1, matchingPrice)
// 	sellConf2, err := tm.market.SubmitOrder(ctx, sellOrder2)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusActive, sellConf2.Order.Status)
// 	require.Equal(t, 0, len(sellConf2.Trades))

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)
// 	require.Equal(t, num.Sum(lp1Commitment, lp2Commitment).String(), md.SuppliedStake)

// 	// leave opening auction
// 	tm.now = now.Add(2 * time.Second)
// 	closed := tm.market.OnTick(ctx, tm.now)
// 	require.False(t, closed)
// 	tm.stateVar.ReadyForTimeTrigger(tm.asset, tm.market.GetID())
// 	tm.stateVar.OnTick(context.Background(), tm.now.Add(6*time.Minute))

// 	totalCommitment := num.Sum(lp1Commitment, lp2Commitment)
// 	currentStake := num.DecimalFromUint(totalCommitment)
// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode, md.MarketTradingMode.String())
// 	require.Equal(t, totalCommitment.String(), md.SuppliedStake)
// 	require.True(t, md.MarkPrice.EQ(num.NewUint(matchingPrice)))

// 	factor := num.DecimalFromFloat(c1)
// 	supplied, err := num.DecimalFromString(md.SuppliedStake)
// 	require.NoError(t, err)
// 	target, err := num.DecimalFromString(md.TargetStake)
// 	require.NoError(t, err)
// 	require.True(t, supplied.GreaterThan(target.Mul(factor)))

// 	// current = (target * c1) auction not triggered
// 	riskParams := tm.mktCfg.TradableInstrument.GetSimpleRiskModel().Params
// 	require.NotNil(t, riskParams)

// 	matchingPriceDec := num.DecimalFromFloat(float64(matchingPrice))
// 	if riskParams.FactorLong.GreaterThan(riskParams.FactorShort) {
// 		matchingPriceDec = matchingPriceDec.Mul(riskParams.FactorLong)
// 	} else {
// 		matchingPriceDec = matchingPriceDec.Mul(riskParams.FactorShort)
// 	}
// 	maxOrderSizeFp := currentStake.Div(factor.Mul(matchingPriceDec).Mul(tm.mktCfg.LiquidityMonitoringParameters.TargetStakeParameters.ScalingFactor))
// 	maxOrderSizeFp = maxOrderSizeFp.Sub(num.DecimalFromFloat(float64(sellConf2.Order.Size)))
// 	// maxOrderSizeFp := currentStake/(c1*float64(matchingPrice)*math.Max(riskParams.FactorShort, riskParams.FactorLong)*tm.mktCfg.LiquidityMonitoringParameters.TargetStakeParameters.ScalingFactor) - float64(sellConf2.Order.Size)
// 	require.True(t, maxOrderSizeFp.GreaterThan(num.DecimalFromFloat(1)))
// 	maxOSize, _ := num.UintFromDecimal(maxOrderSizeFp.Floor())
// 	maxOrderSize := maxOSize.Uint64()

// 	tm.stateVar.OnTick(context.Background(), tm.now.Add(11*time.Minute))

// 	// Add orders that will trade (no auction triggered yet)
// 	buyOrder3 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder3", types.SideBuy, party1, maxOrderSize, matchingPrice)
// 	buyConf3, err := tm.market.SubmitOrder(ctx, buyOrder3)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusActive, buyConf3.Order.Status)

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)

// 	sellOrder3 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder3", types.SideSell, party2, maxOrderSize, matchingPrice)
// 	sellConf3, err := tm.market.SubmitOrder(ctx, sellOrder3)

// 	tm.now = tm.now.Add(time.Second)
// 	tm.market.OnTick(ctx, tm.now)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusFilled, sellConf3.Order.Status)
// 	require.Equal(t, 1, len(sellConf3.Trades))

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)

// 	supplied, err = num.DecimalFromString(md.SuppliedStake)
// 	require.NoError(t, err)
// 	target, err = num.DecimalFromString(md.TargetStake)
// 	require.NoError(t, err)
// 	require.True(t, supplied.GreaterThan(target.Mul(factor)))

// 	// Add orders that will trade and trigger liquidity auction
// 	buyOrder4 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder4", types.SideBuy, party1, 1, matchingPrice)
// 	buyConf4, err := tm.market.SubmitOrder(ctx, buyOrder4)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusActive, buyConf4.Order.Status)

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)

// 	sellOrder4 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder4", types.SideSell, party2, 1, matchingPrice)
// 	sellConf4, err := tm.market.SubmitOrder(ctx, sellOrder4)

// 	tm.now = tm.now.Add(time.Second)
// 	tm.market.OnTick(ctx, tm.now)
// 	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction
// 	require.NoError(t, err)
// 	// this order will now get filled before triggering auction
// 	require.Equal(t, types.OrderStatusFilled, sellConf4.Order.Status, sellConf4.Order.Status.String())
// 	require.Equal(t, 1, len(sellConf4.Trades))

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeMonitoringAuction, md.MarketTradingMode)
// 	require.Equal(t, types.AuctionTriggerLiquidityTargetNotMet, md.Trigger)

// 	// don't use AddSum, we need to keep the original amount somewhere
// 	lpa2 := &types.LiquidityProvisionAmendment{
// 		CommitmentAmount: num.Sum(lp2sub.CommitmentAmount, num.NewUint(25750)),
// 	}
// 	require.NoError(t,
// 		tm.market.AmendLiquidityProvision(ctx, lpa2, lp2, vgcrypto.RandomHash()),
// 	)

// 	// progress time so liquidity auction ends
// 	tm.now = tm.now.Add(2 * time.Second)
// 	tm.market.OnTick(ctx, tm.now)
// 	require.Equal(t, types.MarketStateActive, tm.market.State()) // left auction

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)

// 	supplied, err = num.DecimalFromString(md.SuppliedStake)
// 	require.NoError(t, err)
// 	target, err = num.DecimalFromString(md.TargetStake)
// 	require.NoError(t, err)
// 	require.True(t, supplied.GreaterThanOrEqual(target))

// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusFilled, sellConf4.Order.Status)

// 	// Bringing commitment back to old level shouldn't be allowed

// 	lpa2.CommitmentAmount = lp2Commitment.Clone()
// 	require.Error(t,
// 		tm.market.AmendLiquidityProvision(ctx, lpa2, lp2, vgcrypto.RandomHash()),
// 	)

// 	md = tm.market.GetMarketData()
// 	var zero uint64
// 	require.Greater(t, md.BestStaticBidVolume, zero)

// 	// Cancelling best_bid should start auction
// 	conf, err := tm.market.CancelOrder(ctx, buyOrder1.Party, buyOrder1.ID, vgcrypto.RandomHash())
// 	require.NoError(t, err)
// 	require.NotNil(t, conf)
// 	tm.now = tm.now.Add(time.Second)
// 	tm.market.OnTick(ctx, tm.now)
// 	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction

// 	// Submitting an order on buy side so that best_bid does exist should stop an auction
// 	md = tm.market.GetMarketData()
// 	require.Equal(t, zero, md.BestStaticBidVolume)
// 	require.Equal(t, types.MarketTradingModeMonitoringAuction, md.MarketTradingMode)
// 	require.Equal(t, types.AuctionTriggerUnableToDeployLPOrders, md.Trigger)

// 	supplied, err = num.DecimalFromString(md.SuppliedStake)
// 	require.NoError(t, err)
// 	target, err = num.DecimalFromString(md.TargetStake)
// 	require.NoError(t, err)
// 	require.True(t, supplied.GreaterThanOrEqual(target))

// 	buyOrder5 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder5", types.SideBuy, party1, 1, matchingPrice-10)
// 	buyConf5, err := tm.market.SubmitOrder(ctx, buyOrder5)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusActive, buyConf5.Order.Status)

// 	// progress time to end auction
// 	tm.now = tm.now.Add(2 * time.Second)
// 	tm.market.OnTick(ctx, tm.now)
// 	require.Equal(t, types.MarketStateActive, tm.market.State()) // left auction

// 	// Submitting an order on buy side so that best_bid does exist should stop an auction
// 	md = tm.market.GetMarketData()
// 	require.Equal(t, buyOrder5.Size, md.BestStaticBidVolume)
// 	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)

// 	supplied, err = num.DecimalFromString(md.SuppliedStake)
// 	require.NoError(t, err)
// 	target, err = num.DecimalFromString(md.TargetStake)
// 	require.NoError(t, err)
// 	require.True(t, supplied.GreaterThanOrEqual(target))

// 	// Trading with best-ask, so it disappears should start an auction
// 	buyOrder6 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder6", types.SideBuy, party1, 1, sellOrder1.Price.Uint64())
// 	buyConf6, err := tm.market.SubmitOrder(ctx, buyOrder6)
// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusFilled, buyConf6.Order.Status)
// 	require.Equal(t, 1, len(buyConf6.Trades))
// 	tm.now = tm.now.Add(time.Second)
// 	tm.market.OnTick(ctx, tm.now)
// 	require.Equal(t, types.MarketStateSuspended, tm.market.State()) // enter auction

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, zero, md.BestStaticOfferVolume)
// 	require.Equal(t, types.MarketTradingModeMonitoringAuction, md.MarketTradingMode)
// 	require.Equal(t, types.AuctionTriggerUnableToDeployLPOrders, md.Trigger)

// 	supplied, err = num.DecimalFromString(md.SuppliedStake)
// 	require.NoError(t, err)
// 	target, err = num.DecimalFromString(md.TargetStake)
// 	require.NoError(t, err)
// 	require.True(t, supplied.LessThan(target))
// 	require.True(t, supplied.GreaterThan(target.Mul(factor)))

// 	// Increasing total stake so that the new target stake is accommodated AND adding a sell so best_ask exists should stop the auction

// 	lpa1 := &types.LiquidityProvisionAmendment{
// 		CommitmentAmount: num.Sum(lp1Commitment, num.NewUint(10000)),
// 	}
// 	err = tm.market.AmendLiquidityProvision(ctx, lpa1, lp1, vgcrypto.RandomHash())
// 	require.NoError(t, err)

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, types.MarketTradingModeMonitoringAuction, md.MarketTradingMode)
// 	require.Equal(t, types.AuctionTriggerUnableToDeployLPOrders, md.Trigger)

// 	supplied, err = num.DecimalFromString(md.SuppliedStake)
// 	require.NoError(t, err)
// 	target, err = num.DecimalFromString(md.TargetStake)
// 	require.NoError(t, err)
// 	require.True(t, supplied.GreaterThanOrEqual(target))

// 	sellOrder5 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder5", types.SideSell, party2, 1, matchingPrice-5)
// 	sellConf5, err := tm.market.SubmitOrder(ctx, sellOrder5)

// 	require.NoError(t, err)
// 	require.Equal(t, types.OrderStatusActive, sellConf5.Order.Status)
// 	require.Equal(t, 0, len(sellConf5.Trades))

// 	tm.now = tm.now.Add(2 * time.Second)
// 	tm.market.OnTick(ctx, tm.now)
// 	require.Equal(t, types.MarketStateActive, tm.market.State()) // left auction

// 	md = tm.market.GetMarketData()
// 	require.Equal(t, sellOrder5.Size, md.BestStaticOfferVolume)
// 	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)

// 	supplied, err = num.DecimalFromString(md.SuppliedStake)
// 	require.NoError(t, err)
// 	target, err = num.DecimalFromString(md.TargetStake)
// 	require.NoError(t, err)
// 	require.True(t, supplied.GreaterThanOrEqual(target))
// }

func TestLiquidityMonitoring_BestBidAskExistAfterAuction(t *testing.T) {
	now := time.Unix(10, 0)
	openingDuration := &types.AuctionDuration{
		Duration: 1,
	}
	tm := getTestMarket(t, now, nil, openingDuration)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	err := tm.market.OnMarketTargetStakeScalingFactorUpdate(num.DecimalFromFloat(0.0))
	require.NoError(t, err)
	md := tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)

	lp1 := "lp1"
	party1 := "party1"
	party2 := "party2"

	addAccount(t, tm, lp1)
	addAccount(t, tm, party1)
	addAccount(t, tm, party2)

	var lp1Commitment uint64 = 50000

	var matchingPrice uint64 = 100
	// Add orders that will stay on the book thus maintaining best_bid and best_ask
	buyOrder1 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder1", types.SideBuy, party1, 1, matchingPrice-10)
	buyConf1, err := tm.market.SubmitOrder(ctx, buyOrder1)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, buyConf1.Order.Status)

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)

	sellOrder1 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder1", types.SideSell, party2, 1, matchingPrice+10)
	sellConf1, err := tm.market.SubmitOrder(ctx, sellOrder1)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, sellConf1.Order.Status)
	tm.market.OnTick(ctx, tm.now)

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)

	lp1sub := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(lp1Commitment),
		Fee:              num.DecimalFromFloat(0.05),
	}

	require.NoError(t,
		tm.market.SubmitLiquidityProvision(ctx, lp1sub, lp1, vgcrypto.RandomHash()),
	)

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeOpeningAuction, md.MarketTradingMode)

	buyOrder2 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder2", types.SideBuy, party1, 1, matchingPrice)
	buyConf2, err := tm.market.SubmitOrder(ctx, buyOrder2)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, buyConf2.Order.Status)

	sellOrder2 := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder2", types.SideSell, party2, 1, matchingPrice)
	sellConf2, err := tm.market.SubmitOrder(ctx, sellOrder2)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, sellConf2.Order.Status)
	require.Equal(t, 0, len(sellConf2.Trades))

	tm.now = tm.now.Add(time.Second * time.Duration(openingDuration.Duration)).Add(time.Millisecond)
	closed := tm.market.OnTick(ctx, tm.now)
	require.False(t, closed)

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)
	require.True(t, md.MarkPrice.EQ(num.NewUint(matchingPrice)))
	require.Equal(t, "0", md.TargetStake)

	sellOrder3 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder3", types.SideSell, party2, 1, buyOrder1.Price.Uint64())
	sellConf3, err := tm.market.SubmitOrder(ctx, sellOrder3)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // enter auction
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusFilled, sellConf3.Order.Status)

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)
	// require.Equal(t, types.AuctionTriggerLiquidityTargetNotMet, md.Trigger)

	buyOrder3 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder3", types.SideBuy, party1, 1, sellOrder1.Price.Uint64())
	buyConf3, err := tm.market.SubmitOrder(ctx, buyOrder3)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusFilled, buyConf3.Order.Status)

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)
	// require.Equal(t, types.AuctionTriggerLiquidityTargetNotMet, md.Trigger)

	sellOrder4 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "sellOrder4", types.SideSell, party2, 11, sellOrder1.Price.Uint64()+1)
	sellConf4, err := tm.market.SubmitOrder(ctx, sellOrder4)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, sellConf4.Order.Status)
	tm.now = tm.now.Add(time.Second)
	tm.market.OnTick(ctx, tm.now)

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)
	// require.Equal(t, types.AuctionTriggerLiquidityTargetNotMet, md.Trigger)

	buyOrder4 := getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "buyOrder4", types.SideBuy, party1, 1, buyOrder1.Price.Uint64()-1)
	buyConf4, err := tm.market.SubmitOrder(ctx, buyOrder4)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActive, buyConf4.Order.Status)

	// we have to wait for the auction to end
	tm.now = tm.now.Add(2 * time.Second)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketStateActive, tm.market.State()) // left auction

	md = tm.market.GetMarketData()
	require.Equal(t, types.MarketTradingModeContinuous, md.MarketTradingMode)
	require.Equal(t, types.AuctionTriggerUnspecified, md.Trigger)
}

func TestAmendTrade(t *testing.T) {
	now := time.Unix(10, 0)
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())

	auctionEnd := now.Add(10001 * time.Second)
	mktCfg := getMarket(defaultPriceMonitorSettings, &types.AuctionDuration{
		Duration: 10000,
	})
	mktCfg.Fees.Factors = &types.FeeFactors{
		LiquidityFee:      num.DecimalFromFloat(0.001),
		InfrastructureFee: num.DecimalFromFloat(0.0005),
		MakerFee:          num.DecimalFromFloat(0.00025),
	}
	mktCfg.TradableInstrument.RiskModel = &types.TradableInstrumentLogNormalRiskModel{
		LogNormalRiskModel: &types.LogNormalRiskModel{
			RiskAversionParameter: num.DecimalFromFloat(0.001),
			Tau:                   num.DecimalFromFloat(0.00011407711613050422),
			Params: &types.LogNormalModelParams{
				Mu:    num.DecimalZero(),
				R:     num.DecimalFromFloat(0.016),
				Sigma: num.DecimalFromFloat(20),
			},
		},
	}

	lpparty := "lp-party-1"
	lpparty2 := "lp-party-2"
	lpparty3 := "lp-party-3"

	p1 := "p1"
	p2 := "p2"

	tm := newTestMarket(t, now).Run(ctx, mktCfg)
	tm.StartOpeningAuction().
		// the liquidity provider
		WithAccountAndAmount(lpparty, 500000000000).
		WithAccountAndAmount(lpparty2, 500000000000).
		WithAccountAndAmount(lpparty3, 500000000000).
		WithAccountAndAmount(p1, 500000000000).
		WithAccountAndAmount(p2, 500000000000)
	addAccountWithAmount(tm, "lpprov", 10000000)

	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(55000),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))
	tm.EndOpeningAuction(t, auctionEnd, false)

	assert.Equal(t, types.MarketTradingModeContinuous, tm.market.GetMarketData().MarketTradingMode)

	tm.events = nil

	p1Order := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "pid1", types.SideBuy, p1, 10, 1010)
	p2Order := getMarketOrder(tm, now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "pid2", types.SideSell, p2, 10, 1050)

	p1conf, err := tm.market.SubmitOrder(ctx, p1Order)
	assert.NoError(t, err)
	assert.Len(t, p1conf.Trades, 0)

	p2conf, err := tm.market.SubmitOrder(ctx, p2Order)
	assert.NoError(t, err)
	assert.Len(t, p2conf.Trades, 0)

	assert.Equal(t, types.MarketTradingModeContinuous, tm.market.GetMarketData().MarketTradingMode)

	// now we
	amend := types.OrderAmendment{
		OrderID:  p1conf.Order.ID,
		MarketID: p1conf.Order.MarketID,
		Price:    num.NewUint(1050),
	}

	tm.events = nil
	amendConf, err := tm.market.AmendOrder(ctx, &amend, p1conf.Order.Party, vgcrypto.RandomHash())
	assert.NoError(t, err)
	assert.Len(t, amendConf.Trades, 1)

	ps := map[string]*events.PositionState{}
	for _, v := range tm.events {
		if e, ok := v.(*events.PositionState); ok {
			ps[e.PartyID()] = e
		}
	}

	assert.Len(t, ps, 2)
	assert.Equal(t, int(ps[p1].Size()), 10)
	assert.Equal(t, int(ps[p2].Size()), -10)
}

func Test_7017_UpdatingMarketDuringOpeningAuction(t *testing.T) {
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	pMonitorSettings := &types.PriceMonitoringSettings{
		Parameters: &types.PriceMonitoringParameters{
			Triggers: []*types.PriceMonitoringTrigger{},
		},
	}
	openingAuctionDuration := 10 * time.Minute
	mktCfg := getMarket(pMonitorSettings, &types.AuctionDuration{
		Duration: int64(openingAuctionDuration.Seconds()),
	})
	lpParty := "party-LP"
	trader1 := "party-trader-1"
	trader2 := "party-trader-2"
	tm := newTestMarket(t, time.Unix(10, 0)).Run(ctx, mktCfg)
	tm.market.OnTick(ctx, tm.now)
	tm.StartOpeningAuction().
		WithAccountAndAmount(lpParty, 1000000).
		WithAccountAndAmount(trader1, 100000).
		WithAccountAndAmount(trader2, 100000)

	// submit limit orders
	midPrice := uint64(1000)
	limitOrders := []*types.Order{
		getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "lo-1", types.SideBuy, trader1, 10, midPrice-uint64(250)),
		getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "lo-2", types.SideBuy, trader1, 10, midPrice),
		getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "lo-3", types.SideSell, trader2, 10, midPrice),
		getMarketOrder(tm, tm.now, types.OrderTypeLimit, types.OrderTimeInForceGTC, "lo-4", types.SideSell, trader2, 10, midPrice+uint64(250)),
	}
	for _, o := range limitOrders {
		conf, err := tm.market.SubmitOrder(ctx, o)
		require.NoError(t, err)
		require.NotNil(t, conf)
	}

	tm.now = tm.now.Add(time.Minute)
	tm.market.OnTick(ctx, tm.now)

	require.NoError(t, tm.market.Update(ctx, &mktCfg, tm.oracleEngine))

	tm.now = tm.now.Add(time.Minute)
	tm.market.OnTick(ctx, tm.now)

	lps := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(70000),
		Fee:              num.DecimalFromFloat(0.05),
		Reference:        "ref-lp-submission-1",
	}

	require.NoError(t,
		tm.market.SubmitLiquidityProvision(
			ctx, lps, lpParty, vgcrypto.RandomHash()),
	)

	// leave opening auction
	tm.now = tm.now.Add(openingAuctionDuration)
	tm.market.OnTick(ctx, tm.now)
	require.Equal(t, types.MarketTradingModeContinuous, tm.market.GetMarketData().MarketTradingMode)
}

func TestLiquidityFeeSettingsWeightedAverage(t *testing.T) {
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	mktCfg := getMarket(defaultPriceMonitorSettings, &types.AuctionDuration{Duration: 1})
	mktCfg.Fees.LiquidityFeeSettings = &types.LiquidityFeeSettings{
		Method: proto.LiquidityFeeSettings_METHOD_WEIGHTED_AVERAGE,
	}

	now := time.Unix(10, 0)
	tm := newTestMarket(t, now).Run(context.Background(), mktCfg)
	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)

	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)
	addAccountWithAmount(tm, "lpprov2", 10000000)
	tm.StartOpeningAuction()

	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(27500),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))

	lp2 := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(27500),
		Fee:              num.DecimalFromFloat(0.02),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp2, "lpprov2", vgcrypto.RandomHash()))

	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	var fee string
	for _, evt := range tm.events {
		if mup, ok := evt.(*events.MarketUpdated); ok {
			fee = mup.Market().Fees.Factors.LiquidityFee
		}
	}
	// two LPs with same comittment, fee should be the average (0.01 + 0.02) / 2 = 0.015
	assert.Equal(t, "0.015", fee)
}

func TestLiquidityFeeSettingsConstantFee(t *testing.T) {
	ctx := vegacontext.WithTraceID(context.Background(), vgcrypto.RandomHash())
	mktCfg := getMarket(defaultPriceMonitorSettings, &types.AuctionDuration{Duration: 1})
	mktCfg.Fees.LiquidityFeeSettings = &types.LiquidityFeeSettings{
		Method:      proto.LiquidityFeeSettings_METHOD_CONSTANT,
		FeeConstant: num.NewDecimalFromFloat(0.8),
	}

	now := time.Unix(10, 0)
	tm := newTestMarket(t, now).Run(context.Background(), mktCfg)
	tm.market.OnMarketAuctionMinimumDurationUpdate(ctx, time.Second)

	tm.broker.EXPECT().Send(gomock.Any()).AnyTimes()
	addAccountWithAmount(tm, "lpprov", 10000000)
	addAccountWithAmount(tm, "lpprov2", 10000000)
	tm.StartOpeningAuction()

	lp := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(27500),
		Fee:              num.DecimalFromFloat(0.01),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp, "lpprov", vgcrypto.RandomHash()))

	lp2 := &types.LiquidityProvisionSubmission{
		MarketID:         tm.market.GetID(),
		CommitmentAmount: num.NewUint(27500),
		Fee:              num.DecimalFromFloat(0.02),
	}
	require.NoError(t, tm.market.SubmitLiquidityProvision(context.Background(), lp2, "lpprov2", vgcrypto.RandomHash()))

	// leave opening auction
	now = now.Add(2 * time.Second)
	tm.now = now
	tm.market.OnTick(ctx, now)

	var fee string
	for _, evt := range tm.events {
		if mup, ok := evt.(*events.MarketUpdated); ok {
			fee = mup.Market().Fees.Factors.LiquidityFee
		}
	}
	// doesn't matter what the LP's set in their nomination, the fee is going to be a constant 0.8
	assert.Equal(t, "0.8", fee)
}

func TestVerifyAMMBounds(t *testing.T) {
	require.Equal(t, "base (8) as factored by market and asset decimals must be greater than lower bound (8)", future.VerifyAMMBounds(num.NewUint(85), num.NewUint(82), num.NewUint(88), num.NewDecimalFromFloat(0.1)).Error())
	require.Equal(t, "upper bound (8) as factored by market and asset decimals must be greater than base (8)", future.VerifyAMMBounds(num.NewUint(85), num.NewUint(78), num.NewUint(88), num.NewDecimalFromFloat(0.1)).Error())
	require.Equal(t, "base (8) as factored by market and asset decimals must be greater than lower bound (8)", future.VerifyAMMBounds(num.NewUint(85), num.NewUint(80), num.NewUint(90), num.NewDecimalFromFloat(0.1)).Error())
	require.NoError(t, future.VerifyAMMBounds(num.NewUint(85), num.NewUint(78), num.NewUint(90), num.NewDecimalFromFloat(0.1)))

	require.NoError(t, future.VerifyAMMBounds(num.NewUint(85), num.NewUint(82), num.NewUint(88), num.NewDecimalFromFloat(1.1)))
	require.NoError(t, future.VerifyAMMBounds(num.NewUint(85), num.NewUint(78), num.NewUint(88), num.NewDecimalFromFloat(1.1)))
	require.NoError(t, future.VerifyAMMBounds(num.NewUint(85), num.NewUint(80), num.NewUint(90), num.NewDecimalFromFloat(1.1)))
	require.NoError(t, future.VerifyAMMBounds(num.NewUint(85), num.NewUint(78), num.NewUint(90), num.NewDecimalFromFloat(1.1)))
}
