// Copyright (c) 2022 Gobalsky Labs Limited
//
// Use of this software is governed by the Business Source License included
// in the LICENSE.DATANODE file and at https://www.mariadb.com/bsl11.
//
// Change Date: 18 months from the later of the date of the first publicly
// available Distribution of this version of the repository, and 25 June 2022.
//
// On the date above, in accordance with the Business Source License, use
// of this software will be governed by version 3 or later of the GNU General
// Public License.

package sqlstore

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"strings"

	"code.vegaprotocol.io/vega/datanode/entities"
)

type (
	Sorting = string
	Compare = string
)

const (
	ASC  Sorting = "ASC"
	DESC Sorting = "DESC"

	EQ Compare = "="
	NE Compare = "!="
	GT Compare = ">"
	LT Compare = "<"
	GE Compare = ">="
	LE Compare = "<="
)

type ColumnOrdering struct {
	// Name of the column in the database table to match to the struct field
	Name string
	// support enum ordering
	Case *OrderCase
	// Sorting is the sorting order to use for the column
	Sorting Sorting
}

type WhenCase struct {
	CursorQueryParameter        // field name, comparison operator, value
	Then                 string // value to evauluate to if WHEN clause is met
}

type OrderCase struct {
	When      []WhenCase // WHEN as slice. Using a map wouldn't work as order of cases can be important.
	Else      *string    // ELSE value
	NoReverse bool       // set to false if the case should not be reversed (e.g. proposals prioritising open proposals)
}

// NewColumnOrdering calls NewColumnOrderingCase. Returns Ordering object for query builder.
func NewColumnOrdering(name string, sorting Sorting) ColumnOrdering {
	return NewColumnOrderingCase(name, sorting, nil)
}

// NewColumnOrderingCase does the same as the old column ordering, but adds support for ordering by case.
func NewColumnOrderingCase(n string, sort Sorting, cases *OrderCase) ColumnOrdering {
	return ColumnOrdering{
		Name:    n,
		Case:    cases,
		Sorting: sort,
	}
}

func (o OrderCase) String() string {
	parts := make([]string, 0, len(o.When)+1)
	for _, c := range o.When {
		parts = append(parts, fmt.Sprintf("WHEN (%s) THEN %s", c.When(), c.Then))
	}
	if o.Else != nil {
		parts = append(parts, fmt.Sprintf("ELSE %s", *o.Else))
	}
	return fmt.Sprintf("CASE %s END", strings.Join(parts, " "))
}

type TableOrdering []ColumnOrdering

func (t *TableOrdering) OrderByClause() string {
	if len(*t) == 0 {
		return ""
	}

	fragments := make([]string, len(*t))
	for i, column := range *t {
		if column.Case == nil {
			fragments[i] = fmt.Sprintf("%s %s", column.Name, column.Sorting)
		} else {
			fragments[i] = fmt.Sprintf("%s %s", column.Case.String(), column.Sorting)
		}
	}
	return fmt.Sprintf("ORDER BY %s", strings.Join(fragments, ","))
}

func (t *TableOrdering) Reversed() TableOrdering {
	reversed := make([]ColumnOrdering, len(*t))
	for i, column := range *t {
		if column.Case != nil && column.Case.NoReverse {
			reversed[i] = column
			continue
		}
		if column.Sorting == DESC {
			reversed[i] = ColumnOrdering{Name: column.Name, Case: column.Case, Sorting: ASC}
		}
		if column.Sorting == ASC {
			reversed[i] = ColumnOrdering{Name: column.Name, Case: column.Case, Sorting: DESC}
		}
	}
	return reversed
}

// CursorPredicate generates an SQL predicate which excludes all rows before the supplied cursor,
// with regards to the supplied table ordering. The values used for comparison are added to
// the args list and bind variables used in the query fragment.
//
// For example, with if you had a query with columns sorted foo ASCENDING, bar DESCENDING and a
// cursor with {foo=1, bar=2}, it would yield a string predicate like this:
//
// (foo > $1) OR (foo = $1 AND bar <= $2)
//
// And 'args' would have 1 and 2 appended to it.
//
// Notes:
//   - The predicate *includes* the value at the cursor
//   - Only fields that are present in both the cursor and the ordering are considered
//   - The union of those fields must have enough information to uniquely identify a row
//   - The table ordering must be sufficient to ensure that a row identified by a cursor cannot
//     change position in relation to the other rows
//   - overrides are provided by the PaginateQuery callbacks. They indicate which field was overidden to
//     include specific values, and as such should not be used in conjunction with > and < operators
func CursorPredicate(args []interface{}, cursor interface{}, ordering TableOrdering, overrides ...string) (string, []interface{}, error) {
	cursorPredicates := []string{}
	equalPredicates := []string{}
	eqFields := map[string]struct{}{}
	for _, f := range overrides {
		eqFields[f] = struct{}{}
	}

	for i, column := range ordering {
		// For the non-last columns, use LT/GT, so we don't include stuff before the cursor
		var operator string
		_, isOverride := eqFields[column.Name]
		if column.Sorting == ASC {
			operator = ">"
		} else if column.Sorting == DESC {
			operator = "<"
		} else {
			return "", nil, fmt.Errorf("unknown sort direction %s", column.Sorting)
		}

		// For the last column, we want to use GTE/LTE so we include the value at the cursor
		isLast := i == (len(ordering) - 1)
		if !isOverride && isLast {
			operator = operator + "="
		}

		value, err := StructValueForColumn(cursor, column.Name)
		if err != nil {
			return "", nil, err
		}

		bindVar := nextBindVar(&args, value)
		// if our main predicate is X > 10, but we want to include x = 5, then we must skip the inequality part
		// and skip the predicate where x = 5 by itself. We only really need the last part and a single callback
		// but when multiple callbacks are provided, having all of them may be useful
		if !isOverride {
			inequalityPredicate := fmt.Sprintf("%s %s %s", column.Name, operator, bindVar)

			colPredicates := append(equalPredicates, inequalityPredicate)
			colPredicateString := strings.Join(colPredicates, " AND ")
			colPredicateString = fmt.Sprintf("(%s)", colPredicateString)
			cursorPredicates = append(cursorPredicates, colPredicateString)
		}
		equalityPredicate := fmt.Sprintf("%s = %s", column.Name, bindVar)
		equalPredicates = append(equalPredicates, equalityPredicate)
		if isOverride && isLast {
			cpStr := fmt.Sprintf("(%s)", strings.Join(equalPredicates, " AND "))
			cursorPredicates = append(cursorPredicates, cpStr)
		}
	}

	// We could keep track of all overrides (in case multiple were applied by 1 callback)
	// and only use the overrides as predicates to return
	predicateString := strings.Join(cursorPredicates, " OR ")

	return predicateString, args, nil
}

type parser interface {
	Parse(string) error
}

// This is a bit magical, it allows us to use the real cursor type for instantiation and the pointer
// type for calling methods with pointer receivers (e.g. Parse) for details see
// https://go.googlesource.com/proposal/+/refs/heads/master/design/43651-type-parameters.md#pointer-method-example
type parserPtr[T any] interface {
	parser
	*T
}

// We have to roll our own equals function here for comparing the cursors because some cursor parameters use
// types that do not implement `comparable`.
func equals[T any](actual, other T) (bool, error) {
	var a, b bytes.Buffer
	enc := gob.NewEncoder(&a)
	err := enc.Encode(actual)
	if err != nil {
		return false, err
	}

	enc = gob.NewEncoder(&b)
	err = enc.Encode(other)
	if err != nil {
		return false, err
	}

	return bytes.Equal(a.Bytes(), b.Bytes()), nil
}

// PaginateQuery takes a query string & bind arg list and returns the same with additional SQL to
//   - exclude rows before the cursor (or after it if the cursor is a backwards looking one)
//   - limit the number of rows to the pagination limit +1 (no cursor) or +2 (cursor)
//     [for purposes of later figuring out whether there are next or previous pages]
//   - order the query according to the TableOrdering supplied
//     the order is reversed if pagination request is backwards
//
// Additional callbacks can be passed which alter the cursor. After each call, the cursor predicate will be re-built
// and added to the overal predicate as WHERE ... ((predicate 1) OR (predicate 2) OR (predicate3)).
// The callback should return the updated cursor and an optional slice of field names that should not be combined with the > and < operators.
// an empty slice of field names is taken to mean the cursor was not changed, and can be skipped.
//
// For example with cursor to a row where foo=42, and a pagination saying get the next 3 then:
// PaginateQuery[MyCursor]("SELECT foo FROM my_table", args, ordering, pagination)
//
// Would append `42` to the arg list and return
// SELECT foo FROM my_table WHERE foo>=$1 ORDER BY foo ASC LIMIT 5
//
// See CursorPredicate() for more details about how the cursor filtering is done.
func PaginateQuery[T any, PT parserPtr[T]](
	query string,
	args []interface{},
	ordering TableOrdering,
	pagination entities.CursorPagination,
	cursorUpdates ...func(T) (T, []string),
) (string, []interface{}, error) {
	// Extract a cursor struct from the pagination struct
	cursor, err := parseCursor[T, PT](pagination)
	if err != nil {
		return "", nil, fmt.Errorf("parsing cursor: %w", err)
	}

	// If we're fetching rows before the cursor, reverse the ordering
	// this probably is too much. NewestFirst == true -> hasforward doesn't matter
	//                            HasBackward == true -> inverse of NewestFirst
	if (pagination.HasBackward() && !pagination.NewestFirst) || // Navigating backwards in time order
		(pagination.HasForward() && pagination.NewestFirst) || // Navigating forward in reverse time order
		(!pagination.HasBackward() && !pagination.HasForward() && pagination.NewestFirst) { // No pagination provided, but in reverse time order
		ordering = ordering.Reversed()
		fmt.Println("select > REVERSED")
	}

	// If the cursor wasn't empty, exclude rows preceding the cursor's row
	var emptyCursor T
	isEmpty, err := equals[T](cursor, emptyCursor)
	if err != nil {
		return "", nil, fmt.Errorf("checking empty cursor: %w", err)
	}
	if !isEmpty {
		whereOrAnd := "WHERE"
		if strings.Contains(strings.ToUpper(query), "WHERE") {
			whereOrAnd = "AND"
		}

		var predicate, prevPred string
		predicates := make([]string, 0, len(cursorUpdates)+1)
		predicate, args, err = CursorPredicate(args, cursor, ordering)
		if err != nil {
			return "", nil, fmt.Errorf("building cursor predicate: %w", err)
		}
		predicates = append(predicates, predicate)
		prevPred = predicate
		for _, cb := range cursorUpdates {
			newC, fields := cb(cursor)
			if len(fields) == 0 {
				continue
			}
			// deeper look, actually diff the cursors
			cursor = newC
			// if the cursor was cleared, skip it
			predicate, args, err = CursorPredicate(args, cursor, ordering, fields...)
			if err != nil {
				return "", nil, fmt.Errorf("building cursor predicate: %w", err)
			}
			if prevPred == predicate {
				continue // skip duplicates in query
			}
			predicates = append(predicates, predicate)
		}
		if len(predicates) > 1 {
			// combine and format correctly
			// ((x = 1) OR (x > 1 AND y = 0)) OR ((x = 0) or (x > 0 AND y = 1))
			predicate = fmt.Sprintf("(%s)", strings.Join(predicates, ") OR ("))
		} else {
			predicate = predicates[0]
		}
		// now combine the multiple predicates accordingly
		query = fmt.Sprintf("%s %s (%s)", query, whereOrAnd, predicate)
	}

	// Add an ORDER BY clause
	query = fmt.Sprintf("%s %s", query, ordering.OrderByClause())

	// And a LIMIT clause
	limit := calculateLimit(pagination)
	if limit != 0 {
		query = fmt.Sprintf("%s LIMIT %d", query, limit)
	}

	if len(query) > 0 {
		fmt.Println(query)
	}
	return query, args, nil
}

func parseCursor[T any, PT parserPtr[T]](pagination entities.CursorPagination) (T, error) {
	cursor := PT(new(T))

	cursorStr := ""
	if pagination.HasForward() && pagination.Forward.HasCursor() {
		cursorStr = pagination.Forward.Cursor.Value()
	} else if pagination.HasBackward() && pagination.Backward.HasCursor() {
		cursorStr = pagination.Backward.Cursor.Value()
	}

	if cursorStr != "" {
		err := cursor.Parse(cursorStr)
		if err != nil {
			return *cursor, fmt.Errorf("parsing cursor: %w", err)
		}
	}
	return *cursor, nil
}

type CursorQueryParameter struct {
	ColumnName string
	Sort       Sorting
	Cmp        Compare
	Value      any
}

func NewCursorQueryParameter(columnName string, sort Sorting, cmp Compare, value any) CursorQueryParameter {
	return CursorQueryParameter{
		ColumnName: columnName,
		Sort:       sort,
		Cmp:        cmp,
		Value:      value,
	}
}

func (c CursorQueryParameter) When() string {
	return fmt.Sprintf("%s %s %v", c.ColumnName, c.Cmp, c.Value) // e.g. state = 'foo', or price > 1000
}

func (c CursorQueryParameter) Where(args ...interface{}) (string, []interface{}) {
	if c.Cmp == "" || c.Value == nil {
		return "", args
	}

	where := fmt.Sprintf("%s %s %v", c.ColumnName, c.Cmp, nextBindVar(&args, c.Value))
	return where, args
}

func (c CursorQueryParameter) OrderBy() string {
	return fmt.Sprintf("%s %s", c.ColumnName, c.Sort)
}

type CursorQueryParameters []CursorQueryParameter

func (c CursorQueryParameters) Where(args ...interface{}) (string, []interface{}) {
	var where string

	for i, cursor := range c {
		var cursorCondition string
		cursorCondition, args = cursor.Where(args...)
		if i > 0 && strings.TrimSpace(where) != "" && strings.TrimSpace(cursorCondition) != "" {
			where = fmt.Sprintf("%s AND", where)
		}
		where = fmt.Sprintf("%s %s", where, cursorCondition)
	}

	return strings.TrimSpace(where), args
}

func (c CursorQueryParameters) OrderBy() string {
	var orderBy string

	for i, cursor := range c {
		if i > 0 {
			orderBy = fmt.Sprintf("%s,", orderBy)
		}
		orderBy = fmt.Sprintf("%s %s", orderBy, cursor.OrderBy())
	}

	return strings.TrimSpace(orderBy)
}
