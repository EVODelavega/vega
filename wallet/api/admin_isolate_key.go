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

package api

import (
	"context"
	"fmt"

	"code.vegaprotocol.io/vega/libs/jsonrpc"
	"github.com/mitchellh/mapstructure"
)

type AdminIsolateKeyParams struct {
	Wallet                   string `json:"wallet"`
	PublicKey                string `json:"publicKey"`
	IsolatedWalletPassphrase string `json:"isolatedWalletPassphrase"`
}

type AdminIsolateKeyResult struct {
	Wallet string `json:"wallet"`
}

type AdminIsolateKey struct {
	walletStore WalletStore
}

// Handle isolates a key in a specific wallet.
func (h *AdminIsolateKey) Handle(ctx context.Context, rawParams jsonrpc.Params) (jsonrpc.Result, *jsonrpc.ErrorDetails) {
	params, err := validateAdminIsolateKeyParams(rawParams)
	if err != nil {
		return nil, InvalidParams(err)
	}

	if exist, err := h.walletStore.WalletExists(ctx, params.Wallet); err != nil {
		return nil, InternalError(fmt.Errorf("could not verify the wallet exists: %w", err))
	} else if !exist {
		return nil, InvalidParams(ErrWalletDoesNotExist)
	}

	alreadyUnlocked, err := h.walletStore.IsWalletAlreadyUnlocked(ctx, params.Wallet)
	if err != nil {
		return nil, InternalError(fmt.Errorf("could not verify whether the wallet is already unlock or not: %w", err))
	}
	if !alreadyUnlocked {
		return nil, RequestNotPermittedError(ErrWalletIsLocked)
	}

	w, err := h.walletStore.GetWallet(ctx, params.Wallet)
	if err != nil {
		return nil, InternalError(fmt.Errorf("could not retrieve the wallet: %w", err))
	}

	if !w.HasPublicKey(params.PublicKey) {
		return nil, InvalidParams(ErrPublicKeyDoesNotExist)
	}

	isolatedWallet, err := w.IsolateWithKey(params.PublicKey)
	if err != nil {
		return nil, InternalError(fmt.Errorf("could not isolate the key: %w", err))
	}

	if err := h.walletStore.CreateWallet(ctx, isolatedWallet, params.IsolatedWalletPassphrase); err != nil {
		return nil, InternalError(fmt.Errorf("could not save the wallet with isolated key: %w", err))
	}

	return AdminIsolateKeyResult{
		Wallet: isolatedWallet.Name(),
	}, nil
}

func validateAdminIsolateKeyParams(rawParams jsonrpc.Params) (AdminIsolateKeyParams, error) {
	if rawParams == nil {
		return AdminIsolateKeyParams{}, ErrParamsRequired
	}

	params := AdminIsolateKeyParams{}
	if err := mapstructure.Decode(rawParams, &params); err != nil {
		return AdminIsolateKeyParams{}, ErrParamsDoNotMatch
	}

	if params.Wallet == "" {
		return AdminIsolateKeyParams{}, ErrWalletIsRequired
	}

	if params.PublicKey == "" {
		return AdminIsolateKeyParams{}, ErrPublicKeyIsRequired
	}

	if params.IsolatedWalletPassphrase == "" {
		return AdminIsolateKeyParams{}, ErrIsolatedWalletPassphraseIsRequired
	}

	return params, nil
}

func NewAdminIsolateKey(
	walletStore WalletStore,
) *AdminIsolateKey {
	return &AdminIsolateKey{
		walletStore: walletStore,
	}
}
