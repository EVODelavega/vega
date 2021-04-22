package steps

import (
	"fmt"
	"strconv"
	"strings"

	types "code.vegaprotocol.io/vega/proto"
)

func formatDiff(msg string, expected, got map[string]string) error {
	var expectedStr strings.Builder
	var gotStr strings.Builder
	formatStr := "\n\t%s\t(%s)"
	for name, value := range expected {
		_, _ = fmt.Fprintf(&expectedStr, formatStr, name, value)
		_, _ = fmt.Fprintf(&gotStr, formatStr, name, got[name])
	}

	return fmt.Errorf("\n%s\nexpected:%s\ngot:%s",
		msg,
		expectedStr.String(),
		gotStr.String(),
	)
}

func u64ToS(n uint64) string {
	return strconv.FormatUint(n, 10)
}

func u64SToS(ns []uint64) string {
	ss := []string{}
	for _, n := range ns {
		ss = append(ss, u64ToS(n))
	}
	return strings.Join(ss, " ")
}

func i64ToS(n int64) string {
	return strconv.FormatInt(n, 10)
}

func errOrderNotFound(reference string, trader string, err error) error {
	return fmt.Errorf("order not found for trader(%s) with reference(%s): %v", trader, reference, err)
}

type CancelOrderError struct {
	reference string
	request   types.OrderCancellation
	Err       error
}

func (c CancelOrderError) Error() string {
	return fmt.Sprintf("failed to cancel order [%v] with reference %s: %v", c.request, c.reference, c.Err)
}

func (c *CancelOrderError) Unwrap() error { return c.Err }

type SubmitOrderError struct {
	reference string
	request   types.Order
	Err       error
}

func (s SubmitOrderError) Error() string {
	return fmt.Sprintf("failed to submit order [%v] with reference %s: %v", s.request, s.reference, s.Err)
}

func (s *SubmitOrderError) Unwrap() error { return s.Err }
