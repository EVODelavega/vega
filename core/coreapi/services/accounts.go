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

package services

import (
	"context"
	"sync"

	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/subscribers"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	coreapipb "code.vegaprotocol.io/vega/protos/vega/api/v1"
)

type accountE interface {
	events.Event
	Account() vegapb.Account
}

type Accounts struct {
	*subscribers.Base

	mu sync.RWMutex
	// parties -> accounts id -> accounts
	parties map[string]map[string]vegapb.Account
	// markets id -> accounts id -> account
	markets map[string]map[string]vegapb.Account
	// global accounts id -> account
	globals map[string]vegapb.Account
}

func NewAccounts(ctx context.Context) *Accounts {
	return &Accounts{
		Base:    subscribers.NewBase(ctx, 1000, true),
		parties: map[string]map[string]vegapb.Account{},
		markets: map[string]map[string]vegapb.Account{},
		globals: map[string]vegapb.Account{},
	}
}

func (a *Accounts) Push(evts ...events.Event) {
	if len(evts) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range evts {
		switch acc := e.(type) {
		case accountE:
			a.addAccount(acc.Account())
		}
	}
}

func (a *Accounts) List(party, market string) []*coreapipb.Account {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(party) > 0 {
		return a.getPartyAccounts(party, market)
	}
	if len(market) > 0 {
		return a.getMarketAccounts(market)
	}
	return a.getGlobalAccounts()
}

func (a *Accounts) Types() []events.Type {
	return []events.Type{
		events.AccountEvent,
	}
}

func (a *Accounts) getPartyAccounts(party, market string) []*coreapipb.Account {
	accs, ok := a.parties[party]
	if !ok {
		return nil
	}

	// at least one
	out := make([]*coreapipb.Account, 0, 1)
	for _, v := range accs {
		if len(market) > 0 && v.MarketId != market {
			continue
		}
		out = append(out, toAccount(v))
	}

	return out
}

func (a *Accounts) getMarketAccounts(market string) []*coreapipb.Account {
	accs, ok := a.markets[market]
	if !ok {
		return nil
	}

	out := make([]*coreapipb.Account, 0, len(accs))
	for _, v := range accs {
		out = append(out, toAccount(v))
	}

	return out
}

func (a *Accounts) getGlobalAccounts() []*coreapipb.Account {
	out := make([]*coreapipb.Account, 0, len(a.globals))
	for _, v := range a.globals {
		out = append(out, toAccount(v))
	}

	return out
}

func (a *Accounts) addAccount(acc vegapb.Account) {
	if acc.MarketId == "!" && acc.Owner == "*" {
		a.globals[acc.Id] = acc
	}

	if acc.Owner != "*" {
		a.addPartyAccount(acc)
	}

	a.addMarketAccount(acc)
}

func (a *Accounts) addPartyAccount(acc vegapb.Account) {
	accs, ok := a.parties[acc.Owner]
	if !ok {
		accs = map[string]vegapb.Account{}
		a.parties[acc.Owner] = accs
	}
	accs[acc.Id] = acc
}

func (a *Accounts) addMarketAccount(acc vegapb.Account) {
	accs, ok := a.parties[acc.MarketId]
	if !ok {
		accs = map[string]vegapb.Account{}
		a.parties[acc.MarketId] = accs
	}
	accs[acc.Id] = acc
}

func toAccount(acc vegapb.Account) *coreapipb.Account {
	market := ""
	if acc.MarketId != "!" {
		market = acc.MarketId
	}
	owner := "0000000000000000000000000000000000000000000000000000000000000000"
	if acc.Owner != "*" {
		owner = acc.Owner
	}

	return &coreapipb.Account{
		Party:   owner,
		Market:  market,
		Balance: acc.Balance,
		Asset:   acc.Asset,
		Type:    acc.Type.String(),
	}
}
