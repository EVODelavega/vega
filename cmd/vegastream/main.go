/*
Command vegastream connects to a gRPC server and subscribes to various streams (accounts, orders, trades etc).

For the accounts subscription, specify account type, and optionally market and/or party.

For the orders and trades subscriptions, specify market and party.

For the positions subscription, specify party.

For the candles and (market) depth subscriptions, specify market.

Syntax:

    vegastream -addr somenode.somenet.vega.xyz:3002 [plus other options...]
*/
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"code.vegaprotocol.io/vega/proto"
	"code.vegaprotocol.io/vega/proto/api"
	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/grpc"
)

var (
	party      string
	market     string
	serverAddr string
)

func init() {
	flag.StringVar(&party, "party", "", "name of the party to listen for updates")
	flag.StringVar(&market, "market", "", "id of the market to listen for updates")
	flag.StringVar(&serverAddr, "addr", "127.0.0.1:3002", "address of the grpc server")
}

func run(ctx context.Context, wg *sync.WaitGroup) error {
	wg.Add(1)
	conn, err := grpc.Dial(serverAddr, grpc.WithInsecure())
	if err != nil {
		return err
	}

	var batchSize int64 = 50

	client := api.NewTradingDataClient(conn)
	stream, err := client.ObserveEventBus(ctx)
	if err != nil {
		wg.Done()
		conn.Close()
		return err
	}

	req := &api.ObserveEventsRequest{
		MarketID:  market,
		PartyID:   party,
		BatchSize: batchSize,
		Type:      []proto.BusEventType{proto.BusEventType_BUS_EVENT_TYPE_ALL},
	}

	if err := stream.Send(req); err != nil {
		wg.Done()
		return fmt.Errorf("error when sending initial message in stream: %w", err)
	}

	poll := &api.ObserveEventsRequest{
		BatchSize: batchSize,
	}

	go func() {
		defer wg.Done()
		defer conn.Close()
		defer stream.CloseSend()

		m := jsonpb.Marshaler{}
		for {
			o, err := stream.Recv()
			if err == io.EOF {
				log.Printf("stream closed by server err=%v", err)
				break
			}
			if err != nil {
				log.Printf("stream closed err=%v", err)
				break
			}
			for _, e := range o.Events {
				estr, err := m.MarshalToString(e)
				if err != nil {
					log.Printf("unable to marshal event err=%v", err)
				}

				fmt.Printf("%v\n", estr)
			}
			if err := stream.SendMsg(poll); err != nil {
				log.Printf("failed to poll next event batch err=%v", err)
				return
			}
		}

	}()

	return nil
}

func main() {
	flag.Parse()

	if len(serverAddr) <= 0 {
		log.Printf("error: missing grpc server address")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := sync.WaitGroup{}
	if err := run(ctx, &wg); err != nil {
		log.Printf("error when starting the stream: %v", err)
	}

	waitSig(cancel)
	wg.Wait()
}

func waitSig(cancel func()) {
	var gracefulStop = make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)

	sig := <-gracefulStop
	log.Printf("Caught signal name=%v", sig)
	log.Printf("closing client connections")
	cancel()
}
