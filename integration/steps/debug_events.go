package steps

import (
	"fmt"

	"code.vegaprotocol.io/vega/events"
	"code.vegaprotocol.io/vega/integration/stubs"
	"code.vegaprotocol.io/vega/logging"
)

type marketInfoEvt interface {
	MarketEvent() string
}

func DebugAllEvents(broker *stubs.BrokerStub, log *logging.Logger) {
	log.Info("Dumping ALL events")
	all := broker.GetAllEventsMap()
	header := "\n| Type: %s | count: %d |\n"
	full := ""
	for t, e := range all {
		if t == events.AccountEvent {
			log.Infof(header, t.String, len(e))
			DebugAccounts(broker, log)
			continue
		}
		if t == events.LiquidityProvisionEvent {
			log.Infof(header, t.String, len(e))
			DebugLPDetail(log, broker)
			continue
		}
		full += fmt.Sprintf(header, t.String(), len(e))
		full += eventList(e)
	}
	log.Info(full)
}

func eventList(evts []events.Event) string {
	ret := ""
	row := "%d | %s | %#v | %#v | %s |\n"
	for i, e := range evts {
		mem := " - "
		if me, ok := e.(marketInfoEvt); ok {
			mem = me.MarketEvent()
		}
		sm := e.StreamMessage()
		ret += fmt.Sprintf(row, i+1, sm.String(), e, sm, mem)
	}
	return ret
}
