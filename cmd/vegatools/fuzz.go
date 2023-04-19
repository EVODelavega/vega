package tools

import (
	"code.vegaprotocol.io/vega/core/config"
	"code.vegaprotocol.io/vega/vegatools/protofuzz"
)

type protofuzzCmd struct {
	config.OutputFlag

	In   string   `short:"i" long:"input" required:"true" description:"proto file to use when generating messages"`
	Out  string   `short:"o" long:"out" description:"output file to write to [default is STDOUT]"`
	Type []string `short:"t" long:"type" default:"" description:"Message types to populate with random values [default ALL]"`
}

func (opts *protofuzzCmd) Execute(_ []string) error {
	protofuzz.Run(
		opts.In,
		opts.Out,
		opts.Type,
	)
	return nil
}
