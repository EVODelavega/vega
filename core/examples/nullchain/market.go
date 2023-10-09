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

package nullchain

import (
	"errors"
	"strings"

	"code.vegaprotocol.io/vega/core/examples/nullchain/config"
	"code.vegaprotocol.io/vega/core/types"

	"code.vegaprotocol.io/vega/protos/vega"
)

var ErrMarketCreationFailed = errors.New("market creation failed")

func CreateMarketAny(w *Wallet, conn *Connection, proposer *Party, voters ...*Party) (*vega.Market, error) {
	now, _ := conn.VegaTime()
	txn, reference := MarketProposalTxn(now, proposer.pubkey)
	err := w.SubmitTransaction(conn, proposer, txn)
	if err != nil {
		return nil, err
	}

	// Step foward until proposal is validated
	if err := MoveByDuration(4 * config.BlockDuration); err != nil {
		return nil, err
	}

	// Vote for the proposal
	proposal, err := conn.GetProposalByReference(reference)
	if err != nil {
		return nil, err
	}

	txn = VoteTxn(proposal.Id, types.VoteValueYes)
	for _, voter := range voters {
		w.SubmitTransaction(conn, voter, txn)
	}

	// Move forward until enacted
	if err := MoveByDuration(20 * config.BlockDuration); err != nil {
		return nil, err
	}

	// Get the market
	markets, err := conn.GetMarkets()
	if err != nil {
		return nil, err
	}

	if len(markets) == 0 {
		return nil, ErrMarketCreationFailed
	}

	// Return the last market as that *should* be the newest one
	return markets[len(markets)-1], nil
}

func SettleMarket(w *Wallet, conn *Connection, oracle *Party) error {
	terminationTxn := OracleTxn("trading.termination", "true")
	err := w.SubmitTransaction(conn, oracle, terminationTxn)
	if err != nil {
		return err
	}

	settlementTxn := OracleTxn(strings.Join([]string{"prices", config.NormalAsset, "value"}, "."), "1000")
	err = w.SubmitTransaction(conn, oracle, settlementTxn)
	if err != nil {
		return err
	}
	// Move time so that it is processed
	return MoveByDuration(10 * config.BlockDuration)
}
