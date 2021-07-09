package steps

import (
	"fmt"

	"code.vegaprotocol.io/data-node/integration/stubs"
	types "code.vegaprotocol.io/data-node/proto"

	"github.com/cucumber/godog/gherkin"
)

func TheOrderBookOfMarketShouldHaveTheFollowingVolumes(broker *stubs.BrokerStub, marketID string, table *gherkin.DataTable, ) error {
	for _, row := range parseOrderBookTable(table) {
		volume := row.MustU64("volume")
		price := row.MustU64("price")
		side := row.MustSide("side")

		sell, buy := broker.GetBookDepth(marketID)
		if side == types.Side_SIDE_SELL {
			vol := sell[price]
			if vol != volume {
				return fmt.Errorf("invalid volume(%d) at price(%d) and side(%s) for market(%v), expected(%v)", vol, price, side.String(), marketID, volume)
			}
			continue
		}
		vol := buy[price]
		if vol != volume {
			return fmt.Errorf("invalid volume(%d) at price(%d) and side(%s) for market(%v), expected(%v)", vol, price, side.String(), marketID, volume)
		}
	}
	return nil
}

func parseOrderBookTable(table *gherkin.DataTable) []RowWrapper {
	return StrictParseTable(table, []string{
		"volume",
		"price",
		"side",
	}, []string{})
}
