// Copyright (c) 2022 Gobalsky Labs Limited
//
// Use of this software is governed by the Business Source License included
// in the LICENSE.VEGA file and at https://www.mariadb.com/bsl11.
//
// Change Date: 18 months from the later of the date of the first publicly
// available Distribution of this version of the repository, and 25 June 2022.
//
// On the date above, in accordance with the Business Source License, use
// of this software will be governed by version 3 or later of the GNU General
// Public License.

package steps

import (
	"fmt"

	"github.com/cucumber/godog"

	"code.vegaprotocol.io/vega/core/integration/stubs"
	"code.vegaprotocol.io/vega/libs/num"
	types "code.vegaprotocol.io/vega/protos/vega"
)

func ThePartiesShouldHaveTheFollowingMarginLevels(
	broker *stubs.BrokerStub,
	table *godog.Table,
) error {
	for _, row := range parseExpectedMarginsTable(table) {
		partyID := row.MustStr("party")
		marketID := row.MustStr("market id")
		maintenance := row.Uint("maintenance")
		search, hasSearch := row.UIB("search")
		initial, hasInitial := row.UIB("initial")
		release, hasRelease := row.UIB("release")

		levels, err := broker.GetMarginByPartyAndMarket(partyID, marketID)
		if err != nil {
			return errCannotGetMarginLevelsForPartyAndMarket(partyID, marketID, err)
		}

		var hasError bool
		if levels.MaintenanceMargin != maintenance.String() {
			hasError = true
		}
		if hasSearch && levels.SearchLevel != search.String() {
			hasError = true
		}
		if hasInitial && levels.InitialMargin != initial.String() {
			hasError = true
		}
		if hasRelease && levels.CollateralReleaseLevel != release.String() {
			hasError = true
		}
		if hasError {
			return errInvalidMargins(maintenance, search, initial, release, levels, partyID)
		}
	}
	return nil
}

func errCannotGetMarginLevelsForPartyAndMarket(partyID, market string, err error) error {
	return fmt.Errorf("couldn't get margin levels for party(%s) and market(%s): %s", partyID, market, err.Error())
}

func errInvalidMargins(
	maintenance, search, initial, release *num.Uint,
	levels types.MarginLevels,
	partyID string,
) error {
	return formatDiff(fmt.Sprintf("invalid margins for party \"%s\"", partyID),
		map[string]string{
			"maintenance": uiToS(maintenance),
			"search":      uiToS(search),
			"initial":     uiToS(initial),
			"release":     uiToS(release),
		},
		map[string]string{
			"maintenance": levels.MaintenanceMargin,
			"search":      levels.SearchLevel,
			"initial":     levels.InitialMargin,
			"release":     levels.CollateralReleaseLevel,
		},
	)
}

func parseExpectedMarginsTable(table *godog.Table) []RowWrapper {
	return StrictParseTable(table, []string{
		"party",
		"market id",
		"maintenance",
	}, []string{
		"search",
		"initial",
		"release",
	},
	)
}

func uiToS(v *num.Uint) string {
	if v == nil {
		return ""
	}
	return v.String()
}
