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

package cmd

import (
	"context"
	"fmt"
	"io"

	"code.vegaprotocol.io/vega/cmd/vegawallet/commands/cli"
	"code.vegaprotocol.io/vega/cmd/vegawallet/commands/flags"
	"code.vegaprotocol.io/vega/cmd/vegawallet/commands/printer"
	"code.vegaprotocol.io/vega/paths"
	"code.vegaprotocol.io/vega/wallet/service/v2/connections"
	sessionStoreV1 "code.vegaprotocol.io/vega/wallet/service/v2/connections/store/session/v1"
	"github.com/spf13/cobra"
)

var (
	listSessionsLong = cli.LongDesc(`
		List all the tracked sessions
	`)

	listSessionsExample = cli.Examples(`
		# List the tracked sessions
		{{.Software}} session list
	`)
)

type ListSessionsHandler func() ([]connections.Session, error)

func NewCmdListSessions(w io.Writer, rf *RootFlags) *cobra.Command {
	h := func() ([]connections.Session, error) {
		vegaPaths := paths.New(rf.Home)

		sessionStore, err := sessionStoreV1.InitialiseStore(vegaPaths)
		if err != nil {
			return nil, fmt.Errorf("couldn't load the session store: %w", err)
		}

		return sessionStore.ListSessions(context.Background())
	}

	return BuildCmdListSessions(w, h, rf)
}

func BuildCmdListSessions(w io.Writer, handler ListSessionsHandler, rf *RootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List all the tracked sessions",
		Long:    listSessionsLong,
		Example: listSessionsExample,
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := handler()
			if err != nil {
				return err
			}

			switch rf.Output {
			case flags.InteractiveOutput:
				printListSessions(w, res)
			case flags.JSONOutput:
				return printer.FprintJSON(w, res)
			}
			return nil
		},
	}

	return cmd
}

func printListSessions(w io.Writer, sessions []connections.Session) {
	p := printer.NewInteractivePrinter(w)

	str := p.String()
	defer p.Print(str)

	if len(sessions) == 0 {
		str.InfoText("No session found.").NextLine()
		return
	}

	for i, session := range sessions {
		str.Text("- ").WarningText(session.Token.String()).NextLine()
		str.Pad().Text("Hostname: ").WarningText(session.Hostname).NextLine()
		str.Pad().Text("Wallet: ").WarningText(session.Wallet)

		if i == len(sessions)-1 {
			str.NextLine()
		} else {
			str.NextSection()
		}
	}
}
