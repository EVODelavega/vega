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

package nodewallets

import (
	"fmt"
	"path/filepath"

	"code.vegaprotocol.io/vega/core/nodewallets/eth"
	"code.vegaprotocol.io/vega/core/nodewallets/eth/clef"
	"code.vegaprotocol.io/vega/core/nodewallets/eth/keystore"
	"code.vegaprotocol.io/vega/core/nodewallets/registry"
	"code.vegaprotocol.io/vega/paths"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

func GetEthereumWallet(vegaPaths paths.Paths, registryPassphrase string) (*eth.Wallet, error) {
	registryLoader, err := registry.NewLoader(vegaPaths, registryPassphrase)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialise node wallet registry: %v", err)
	}

	registry, err := registryLoader.Get(registryPassphrase)
	if err != nil {
		return nil, fmt.Errorf("couldn't load node wallet registry: %v", err)
	}

	if registry.Ethereum == nil {
		return nil, ErrEthereumWalletIsMissing
	}

	return GetEthereumWalletWithRegistry(vegaPaths, registry)
}

func GetEthereumWalletWithRegistry(vegaPaths paths.Paths, reg *registry.Registry) (*eth.Wallet, error) {
	switch walletRegistry := reg.Ethereum.Details.(type) {
	case registry.EthereumClefWallet:
		ethAddress := ethcommon.HexToAddress(walletRegistry.AccountAddress)

		client, err := rpc.Dial(walletRegistry.ClefAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to dial Clef daemon: %w", err)
		}

		w, err := clef.NewWallet(client, walletRegistry.ClefAddress, ethAddress)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialise Ethereum Clef node wallet: %w", err)
		}

		return eth.NewWallet(w), nil
	case registry.EthereumKeyStoreWallet:
		walletLoader, err := keystore.InitialiseWalletLoader(vegaPaths)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialise Ethereum key store node wallet loader: %w", err)
		}

		w, err := walletLoader.Load(walletRegistry.Name, walletRegistry.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("couldn't load Ethereum key store node wallet: %w", err)
		}

		return eth.NewWallet(w), nil
	default:
		return nil, fmt.Errorf("could not create unknown Ethereum wallet type %q", reg.Ethereum.Type)
	}
}

func GenerateEthereumWallet(
	vegaPaths paths.Paths,
	registryPassphrase,
	walletPassphrase string,
	clefAddress string,
	overwrite bool,
) (map[string]string, error) {
	registryLoader, err := registry.NewLoader(vegaPaths, registryPassphrase)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialise node wallet registry: %v", err)
	}

	reg, err := registryLoader.Get(registryPassphrase)
	if err != nil {
		return nil, fmt.Errorf("couldn't load node wallet registry: %v", err)
	}

	if !overwrite && reg.Ethereum != nil {
		return nil, ErrEthereumWalletAlreadyExists
	}

	var data map[string]string

	if clefAddress != "" {
		client, err := rpc.Dial(clefAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to dial Clef daemon: %w", err)
		}

		w, err := clef.GenerateNewWallet(client, clefAddress)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate Ethereum clef node wallet: %w", err)
		}

		data = map[string]string{
			"clefAddress":    clefAddress,
			"accountAddress": w.PubKey().Hex(),
		}

		reg.Ethereum = &registry.RegisteredEthereumWallet{
			Type: registry.EthereumWalletTypeClef,
			Details: registry.EthereumClefWallet{
				Name:           w.Name(),
				AccountAddress: w.PubKey().Hex(),
				ClefAddress:    clefAddress,
			},
		}
	} else {
		keyStoreLoader, err := keystore.InitialiseWalletLoader(vegaPaths)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialise Ethereum key store node wallet loader: %w", err)
		}

		w, d, err := keyStoreLoader.Generate(walletPassphrase)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate Ethereum key store node wallet: %w", err)
		}

		data = d

		reg.Ethereum = &registry.RegisteredEthereumWallet{
			Type: registry.EthereumWalletTypeKeyStore,
			Details: registry.EthereumKeyStoreWallet{
				Name:       w.Name(),
				Passphrase: walletPassphrase,
			},
		}
	}

	if err := registryLoader.Save(reg, registryPassphrase); err != nil {
		return nil, fmt.Errorf("couldn't save registry: %w", err)
	}

	data["registryFilePath"] = registryLoader.RegistryFilePath()
	return data, nil
}

func ImportEthereumWallet(
	vegaPaths paths.Paths,
	registryPassphrase,
	walletPassphrase,
	clefAccount,
	clefAddress,
	sourceFilePath string,
	overwrite bool,
) (map[string]string, error) {
	registryLoader, err := registry.NewLoader(vegaPaths, registryPassphrase)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialise node wallet registry: %v", err)
	}

	reg, err := registryLoader.Get(registryPassphrase)
	if err != nil {
		return nil, fmt.Errorf("couldn't load node wallet registry: %v", err)
	}

	if !overwrite && reg.Ethereum != nil {
		return nil, ErrEthereumWalletAlreadyExists
	}

	var data map[string]string

	if clefAddress != "" {
		if !ethcommon.IsHexAddress(clefAccount) {
			return nil, fmt.Errorf("invalid Ethereum hex address %q", clefAccount)
		}

		ethAddress := ethcommon.HexToAddress(clefAccount)

		client, err := rpc.Dial(clefAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to dial Clef daemon: %w", err)
		}

		w, err := clef.NewWallet(client, clefAddress, ethAddress)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialise Ethereum Clef node wallet: %w", err)
		}

		data = map[string]string{
			"clefAddress":    clefAddress,
			"accountAddress": w.PubKey().Hex(),
		}

		reg.Ethereum = &registry.RegisteredEthereumWallet{
			Type: registry.EthereumWalletTypeClef,
			Details: registry.EthereumClefWallet{
				Name:           w.Name(),
				AccountAddress: w.PubKey().Hex(),
				ClefAddress:    clefAddress,
			},
		}
	} else {
		if !filepath.IsAbs(sourceFilePath) {
			return nil, fmt.Errorf("path to the wallet file need to be absolute")
		}

		ethWalletLoader, err := keystore.InitialiseWalletLoader(vegaPaths)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialise Ethereum node wallet loader: %w", err)
		}

		w, d, err := ethWalletLoader.Import(sourceFilePath, walletPassphrase)
		if err != nil {
			return nil, fmt.Errorf("couldn't import Ethereum node wallet: %w", err)
		}

		data = d

		reg.Ethereum = &registry.RegisteredEthereumWallet{
			Type: registry.EthereumWalletTypeKeyStore,
			Details: registry.EthereumKeyStoreWallet{
				Name:       w.Name(),
				Passphrase: walletPassphrase,
			},
		}
	}

	if err := registryLoader.Save(reg, registryPassphrase); err != nil {
		return nil, fmt.Errorf("couldn't save registry: %w", err)
	}

	data["registryFilePath"] = registryLoader.RegistryFilePath()
	return data, nil
}
