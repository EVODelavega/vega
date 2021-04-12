package core_test

import (
	"context"
	"flag"
	"os"
	"testing"
	"time"

	"code.vegaprotocol.io/vega/integration/steps"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/cucumber/godog/gherkin"
)

var (
	gdOpts = godog.Options{
		Output: colors.Colored(os.Stdout),
		Format: "progress",
	}
)

func init() {
	godog.BindFlags("godog.", flag.CommandLine, &gdOpts)
}

func TestMain(m *testing.M) {
	flag.Parse()
	gdOpts.Paths = flag.Args()

	status := godog.RunWithOptions("godogs", func(s *godog.Suite) {
		FeatureContext(s)
	}, gdOpts)

	if st := m.Run(); st > status {
		status = st
	}
	os.Exit(status)
}

func FeatureContext(s *godog.Suite) {
	// each step changes the output from the reporter
	// so we know where a mock failed
	s.BeforeStep(func(step *gherkin.Step) {
		// rm any errors from previous step (if applies)
		reporter.err = nil
		reporter.step = step.Text
	})
	// if a mock assert failed, we're just setting an error here and crash out of the test here
	s.AfterStep(func(step *gherkin.Step, err error) {
		if err != nil && reporter.err == nil {
			reporter.err = err
		}
		if reporter.err != nil {
			reporter.Fatalf("some mock assertion failed: %v", reporter.err)
		}
	})

	// Market steps
	s.Step(`^the markets start on "([^"]*)" and expire on "([^"]*)"$`, func(startDate, expiryDate string) error {
		start, expiry, err := steps.MarketsStartOnAndExpireOn(startDate, expiryDate)
		if err == nil {
			marketExpiry = expiry
			marketStart = start
		}
		return err
	})
	s.Step(`the simple risk model named "([^"]*)":$`, func(name string, table *gherkin.DataTable) error {
		return steps.TheSimpleRiskModel(marketConfig, name, table)
	})
	s.Step(`the log normal risk model named "([^"]*)":$`, func(name string, table *gherkin.DataTable) error {
		return steps.TheLogNormalRiskModel(marketConfig, name, table)
	})
	s.Step(`the fees configuration named "([^"]*)":$`, func(name string, table *gherkin.DataTable) error {
		return steps.TheFeesConfiguration(marketConfig, name, table)
	})
	s.Step(`the oracle spec filtering data from "([^"]*)" named "([^"]*)":$`, func(pubKeys, name string, table *gherkin.DataTable) error {
		return steps.TheOracleSpec(marketConfig, name, pubKeys, table)
	})
	s.Step(`the price monitoring updated every "([^"]*)" seconds named "([^"]*)":$`, func(updateFrequency, name string, table *gherkin.DataTable) error {
		return steps.ThePriceMonitoring(marketConfig, name, updateFrequency, table)
	})
	s.Step(`the margin calculator named "([^"]*)":$`, func(name string, table *gherkin.DataTable) error {
		return steps.TheMarginCalculator(marketConfig, name, table)
	})
	s.Step(`^the markets:$`, func(table *gherkin.DataTable) error {
		markets := steps.TheMarkets(marketConfig, marketExpiry, table)

		t, _ := time.Parse("2006-01-02T15:04:05Z", marketStart)
		execsetup = getExecutionTestSetup(t, markets)

		// reset market start time and expiry for next run
		marketExpiry = defaultMarketExpiry
		marketStart = defaultMarketStart

		return nil
	})

	// Other steps
	s.Step(`^the following network parameters are set:$`, func(table *gherkin.DataTable) error {
		params := steps.TheFollowingNetworkParametersAreSet(table)
		if v, ok := params["market.auction.minimumDuration"]; ok {
			d := v.(time.Duration)
			if err := execsetup.engine.OnMarketAuctionMinimumDurationUpdate(context.Background(), d); err != nil {
				return err
			}
		}
		return nil
	})
	s.Step(`^"([^"]*)" withdraws "([^"]*)" from the account "([^"]*)"$`, func(owner, rawAmount, asset string) error {
		return steps.TraderWithdrawsFromAccount(execsetup.collateral, owner, rawAmount, asset)
	})
	s.Step(`^the initial insurance pool balance is "([^"]*)" for the markets:$`, theInsurancePoolInitialBalanceForTheMarketsIs)
	s.Step(`^time is updated to "([^"]*)"$`, func(rawTime string) error {
		return steps.TimeIsUpdatedTo(execsetup.timesvc, rawTime)
	})
	s.Step(`^the traders cancel the following orders:$`, func(table *gherkin.DataTable) error {
		return steps.TradersCancelTheFollowingOrders(execsetup.broker, execsetup.engine, execsetup.errorHandler, table)
	})
	s.Step(`^the traders cancel all their orders for the markets:$`, func(table *gherkin.DataTable) error {
		return steps.TradersCancelTheFollowingOrders(execsetup.broker, execsetup.engine, execsetup.errorHandler, table)
	})
	s.Step(`^the traders amend the following orders:$`, func(table *gherkin.DataTable) error {
		return steps.TradersAmendTheFollowingOrders(execsetup.errorHandler, execsetup.broker, execsetup.engine, table)
	})
	s.Step(`^the traders place the following pegged orders:$`, func(table *gherkin.DataTable) error {
		return steps.TradersPlacePeggedOrders(execsetup.engine, table)
	})
	s.Step(`^the traders deposit on asset's general account the following amount:$`, func(table *gherkin.DataTable) error {
		return steps.TradersDepositAssets(execsetup.collateral, execsetup.broker, table)
	})
	s.Step(`^the traders place the following orders:$`, func(table *gherkin.DataTable) error {
		return steps.TradersPlaceTheFollowingOrders(execsetup.engine, execsetup.errorHandler, table)
	})
	s.Step(`^the traders submit the following liquidity provision:$`, func(table *gherkin.DataTable) error {
		return steps.TradersSubmitLiquidityProvision(execsetup.engine, table)
	})
	s.Step(`^the opening auction period ends for market "([^"]+)"$`, func(marketID string) error {
		return steps.MarketOpeningAuctionPeriodEnds(execsetup.timesvc, execsetup.mkts, marketID)
	})
	s.Step(`^the oracles broadcast data signed with "([^"]*)":$`, func(pubKeys string, properties *gherkin.DataTable) error {
		return steps.OraclesBroadcastDataSignedWithKeys(execsetup.oracleEngine, pubKeys, properties)
	})

	// Assertion steps
	s.Step(`^the following amendments should be rejected:$`, func(table *gherkin.DataTable) error {
		return steps.TheFollowingAmendmentsShouldBeRejected(execsetup.errorHandler, table)
	})
	s.Step(`^the following amendments should be accepted:$`, func(table *gherkin.DataTable) error {
		return steps.TheFollowingAmendmentsShouldBeAccepted(table)
	})
	s.Step(`^the traders should have the following account balances:$`, func(table *gherkin.DataTable) error {
		return steps.TradersShouldHaveTheFollowingAccountBalances(execsetup.broker, table)
	})
	s.Step(`^the traders should have the following margin levels:$`, func(table *gherkin.DataTable) error {
		return steps.TheTradersShouldHaveTheFollowingMarginLevels(execsetup.broker, table)
	})
	s.Step(`^the traders should have the following profit and loss:$`, func(table *gherkin.DataTable) error {
		return steps.TradersHaveTheFollowingProfitAndLoss(execsetup.positionPlugin, table)
	})
	s.Step(`^the order book should have the following volumes for market "([^"]*)":$`, func(marketID string, table *gherkin.DataTable) error {
		return steps.TheOrderBookOfMarketShouldHaveTheFollowingVolumes(execsetup.broker, marketID, table)
	})
	s.Step(`^the orders should have the following status:$`, func(table *gherkin.DataTable) error {
		return steps.TheOrdersShouldHaveTheFollowingStatus(execsetup.broker, table)
	})
	s.Step(`^the following orders should be rejected:$`, func(table *gherkin.DataTable) error {
		return steps.TheFollowingOrdersShouldBeRejected(execsetup.broker, table)
	})
	s.Step(`^"([^"]*)" should have general account balance of "([^"]*)" for asset "([^"]*)"$`, func(trader, balance, asset string) error {
		return steps.TraderShouldHaveGeneralAccountBalanceForAsset(execsetup.broker, trader, asset, balance)
	})
	s.Step(`^"([^"]*)" should have one account per asset$`, func(owner string) error {
		return steps.TraderShouldHaveOneAccountPerAsset(execsetup.broker, owner)
	})
	s.Step(`^"([^"]*)" should have one margin account per market$`, func(owner string) error {
		return steps.TraderShouldHaveOneMarginAccountPerMarket(execsetup.broker, owner)
	})
	s.Step(`^the cumulated balance for all accounts should be worth "([^"]*)"$`, func(rawAmount string) error {
		return steps.TheCumulatedBalanceForAllAccountsShouldBeWorth(execsetup.broker, rawAmount)
	})
	s.Step(`^the settlement account should have a balance of "([^"]*)" for the market "([^"]*)"$`, func(rawAmount, marketID string) error {
		return steps.TheSettlementAccountShouldHaveBalanceForMarket(execsetup.broker, rawAmount, marketID)
	})
	s.Step(`^the following network trades should be executed:$`, func(table *gherkin.DataTable) error {
		return steps.TheFollowingNetworkTradesShouldBeExecuted(execsetup.broker, table)
	})
	s.Step(`^the following trades should be executed:$`, func(table *gherkin.DataTable) error {
		return steps.TheFollowingTradesShouldBeExecuted(execsetup.broker, table)
	})
	s.Step(`^the trading mode should be "([^"]*)" for the market "([^"]*)"$`, func(tradingMode, marketID string) error {
		return steps.TheTradingModeShouldBeForMarket(execsetup.engine, marketID, tradingMode)
	})
	s.Step(`^the insurance pool balance should be "([^"]*)" for the market "([^"]*)"$`, func(rawAmount, marketID string) error {
		return steps.TheInsurancePoolBalanceShouldBeForTheMarket(execsetup.broker, rawAmount, marketID)
	})
	s.Step(`^the following transfers should happen:$`, func(table *gherkin.DataTable) error {
		return steps.TheFollowingTransfersShouldHappen(execsetup.broker, table)
	})
	s.Step(`^the mark price should be "([^"]*)" for the market "([^"]*)"$`, func(rawMarkPrice, marketID string) error {
		return steps.TheMarkPriceForTheMarketIs(execsetup.engine, marketID, rawMarkPrice)
	})
	s.Step(`^I see the following order events:$`, func(table *gherkin.DataTable) error {
		return steps.OrderEventsSent(execsetup.broker, table)
	})
	s.Step(`^I see the LP events:$`, func(table *gherkin.DataTable) error {
		return steps.LiquidityProvisionEventsSent(execsetup.broker, table)
	})

	// Debug steps
	s.Step(`^debug transfers$`, func() error {
		return steps.DebugTransfers(execsetup.broker, execsetup.log)
	})
	s.Step(`^debug trades$`, func() error {
		return steps.DebugTrades(execsetup.broker, execsetup.log)
	})
	s.Step(`^debug orders$`, func() error {
		return steps.DebugOrders(execsetup.broker, execsetup.log)
	})
	s.Step(`^debug market data for "([^"]*)"$`, func(mkt string) error {
		return steps.DebugMarketData(execsetup.engine, execsetup.log, mkt)
	})
	s.Step(`^debug auction events$`, func() error {
		return steps.DebugAuctionEvents(execsetup.broker, execsetup.log)
	})

	// Event steps
	s.Step(`^clear order events by reference:$`, func(table *gherkin.DataTable) error {
		return steps.ClearOrdersByReference(execsetup.broker, table)
	})
	s.Step(`^clear transfer events$`, func() error {
		steps.ClearTransferEvents(execsetup.broker)
		return nil
	})
	s.Step(`^clear order events$`, func() error {
		steps.ClearOrderEvents(execsetup.broker)
		return nil
	})

	// Experimental error assertion
	s.Step(`^the system should return error "([^"]*)"$`, func(msg string) error {
		return steps.TheSystemShouldReturnError(execsetup.errorHandler, msg)
	})
}
