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
	"context"
	"fmt"
	"strings"

	"code.vegaprotocol.io/vega/datanode/entities"
	"code.vegaprotocol.io/vega/datanode/metrics"
	"code.vegaprotocol.io/vega/libs/ptr"
	v2 "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	"github.com/georgysavva/scany/pgxscan"
	"github.com/pkg/errors"
)

var ErrProposalNotFound = errors.New("proposal not found")

type Proposals struct {
	*ConnectionSource
}

var proposalsOrdering = TableOrdering{
	ColumnOrdering{ // this HAS to come first
		Name: "state", // this doesn't need to be set.
		Case: &OrderCase{
			When: []WhenCase{
				{
					CursorQueryParameter: CursorQueryParameter{
						ColumnName: "state",
						Cmp:        EQ,
						Value:      entities.ProposalStateOpen.EnumStr(),
					},
					Then: "1",
				},
			},
			Else: ptr.From("2"), // ELSE 2
			// NoReverse: true,          // never reverse this, keep in ascending order.
		},
		Sorting: ASC,
	},
	ColumnOrdering{
		Name:    "vega_time",
		Sorting: ASC,
	},
	ColumnOrdering{
		Name:    "id",
		Sorting: ASC,
	},
}

func NewProposals(connectionSource *ConnectionSource) *Proposals {
	p := &Proposals{
		ConnectionSource: connectionSource,
	}
	return p
}

func (ps *Proposals) Add(ctx context.Context, p entities.Proposal) error {
	defer metrics.StartSQLQuery("Proposals", "Add")()
	_, err := ps.Connection.Exec(ctx,
		`INSERT INTO proposals(
			id,
			reference,
			party_id,
			state,
			terms,
			rationale,
			reason,
			error_details,
			proposal_time,
			vega_time,
			required_majority,
			required_participation,
			required_lp_majority,
			required_lp_participation,
			tx_hash)
		 VALUES ($1,  $2,  $3,  $4,  $5,  $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		 ON CONFLICT (id, vega_time) DO UPDATE SET
			reference = EXCLUDED.reference,
			party_id = EXCLUDED.party_id,
			state = EXCLUDED.state,
			terms = EXCLUDED.terms,
			rationale = EXCLUDED.rationale,
			reason = EXCLUDED.reason,
			error_details = EXCLUDED.error_details,
			proposal_time = EXCLUDED.proposal_time,
			tx_hash = EXCLUDED.tx_hash
			;
		 `,
		p.ID, p.Reference, p.PartyID, p.State, p.Terms, p.Rationale, p.Reason, p.ErrorDetails, p.ProposalTime, p.VegaTime, p.RequiredMajority, p.RequiredParticipation, p.RequiredLPMajority, p.RequiredLPParticipation, p.TxHash)
	return err
}

func (ps *Proposals) GetByID(ctx context.Context, id string) (entities.Proposal, error) {
	defer metrics.StartSQLQuery("Proposals", "GetByID")()
	var p entities.Proposal
	query := `SELECT * FROM proposals_current WHERE id=$1`
	err := pgxscan.Get(ctx, ps.Connection, &p, query, entities.ProposalID(id))
	if pgxscan.NotFound(err) {
		return p, fmt.Errorf("'%v': %w", id, ErrProposalNotFound)
	}

	return p, err
}

func (ps *Proposals) GetByReference(ctx context.Context, ref string) (entities.Proposal, error) {
	defer metrics.StartSQLQuery("Proposals", "GetByReference")()
	var p entities.Proposal
	query := `SELECT * FROM proposals_current WHERE reference=$1 LIMIT 1`
	err := pgxscan.Get(ctx, ps.Connection, &p, query, ref)
	if pgxscan.NotFound(err) {
		return p, fmt.Errorf("'%v': %w", ref, ErrProposalNotFound)
	}

	return p, err
}

func getOpenStateProposalsQuery(inState *entities.ProposalState, conditions []string, pagination entities.CursorPagination,
	pageForward bool, args ...interface{},
) (string, []interface{}, error) {
	query := `select * from proposals_current`

	if len(conditions) > 0 {
		query = fmt.Sprintf("%s WHERE %s", query, strings.Join(conditions, " AND "))
	}
	// we are getting newest first by time, but that does mess up the state ordering, we have to ensure that we're still prioritising open state proposals
	order := proposalsOrdering
	flipped := false
	// reverse time order, either hasForward is true, or both HasForward and HasBackward are false
	if inState != nil {
		order = proposalsOrdering[1:] // ignore the state order by bit, it doesn't apply here
	} else if pagination.NewestFirst {
		// if pagination.NewestFirst && (pagination.HasForward() || !pagination.HasBackward()) {
		// set to descending, we know this will be inverted
		order[0].Sorting = DESC
		flipped = true
	}

	var err error
	query, args, err = PaginateQuery[entities.ProposalCursor](query, args, order, pagination, addOpenCursor)
	if flipped {
		// we need to restore the sorting after using it in this way
		order[0].Sorting = ASC
	}
	if err != nil {
		return "", args, err
	}

	return query, args, nil
}

// set cursor state to open
func addOpenCursor(c entities.ProposalCursor) (entities.ProposalCursor, []string) {
	if c.State == entities.ProposalStateOpen || c.State == entities.ProposalStateUnspecified {
		return c, nil
	}
	c.State = entities.ProposalStateOpen
	// mark state column has been overridden
	return c, []string{"state"}
}

func clonePagination(p entities.CursorPagination) (entities.CursorPagination, error) {
	var first, last int32
	var after, before string

	var pFirst, pLast *int32
	var pAfter, pBefore *string

	if p.HasForward() {
		first = *p.Forward.Limit
		pFirst = &first
		if p.Forward.HasCursor() {
			after = p.Forward.Cursor.Encode()
			pAfter = &after
		}
	}

	if p.HasBackward() {
		last = *p.Backward.Limit
		pLast = &last
		if p.Backward.HasCursor() {
			before = p.Backward.Cursor.Encode()
			pBefore = &before
		}
	}

	return entities.NewCursorPagination(pFirst, pAfter, pLast, pBefore, p.NewestFirst)
}

func (ps *Proposals) Get(ctx context.Context,
	inState *entities.ProposalState,
	partyIDStr *string,
	proposalType *entities.ProposalType,
	pagination entities.CursorPagination,
) ([]entities.Proposal, entities.PageInfo, error) {
	// This one is a bit tricky because we want all the open proposals listed at the top, sorted by date
	// then other proposals in date order regardless of state.

	// In order to do this, we need to construct a union of proposals where state = open, order by vega_time
	// and state != open, order by vega_time
	// If the cursor has been set, and we're traversing forward (newest-oldest), then we need to check if the
	// state of the cursor is = open. If it is then we should append the open state proposals with the non-open state
	// proposals.
	// If the cursor state is != open, we have navigated passed the open state proposals and only need the non-open state proposals.

	// If the cursor has been set and we're traversing backward (newest-oldest), then we need to check if the
	// state of the cursor is = open. If it is then we should only return the proposals where state = open as we've already navigated
	// passed all the non-open proposals.
	// if the state of the cursor is != open, then we need to append all the proposals where the state = open after the proposals where
	// state != open.

	// This combined results of both queries is then wrapped with another select which should return the appropriate number of rows that
	// are required for the pagination to determine whether or not there are any next/previous rows for the pageInfo.
	var (
		pageInfo entities.PageInfo
		query    string
	)
	args := []interface{}{}

	pageForward := pagination.HasForward() || (!pagination.HasForward() && !pagination.HasBackward())
	var conditions []string

	if inState != nil {
		conditions = append(conditions, fmt.Sprintf("state = %s", nextBindVar(&args, *inState)))
	}
	if partyIDStr != nil {
		partyID := entities.PartyID(*partyIDStr)
		conditions = append(conditions, fmt.Sprintf("party_id=%s", nextBindVar(&args, partyID)))
	}

	if proposalType != nil {
		conditions = append(conditions, fmt.Sprintf("terms ? %s", nextBindVar(&args, proposalType.String())))
	}

	var err error
	// we need to clone the pagination objects because we need to alter the pagination data for the different states
	// to support the required ordering of the data

	query, args, err = getOpenStateProposalsQuery(inState, conditions, pagination, pageForward, args...)
	if err != nil {
		return nil, pageInfo, err
	}

	defer metrics.StartSQLQuery("Proposals", "Get")()

	proposals := []entities.Proposal{}
	// get the results:
	rows, err := ps.Connection.Query(ctx, query, args...)
	// ensure rows are closed
	defer rows.Close()
	if err != nil {
		return nil, pageInfo, ErrProposalNotFound
	}
	if err := pgxscan.ScanAll(&proposals, rows); err != nil {
		return nil, pageInfo, fmt.Errorf("scanning proposals: %w", err)
	}

	if limit := calculateLimit(pagination); limit > 0 && limit < len(proposals) {
		proposals = proposals[:limit]
	}

	proposals, pageInfo = entities.PageEntities[*v2.GovernanceDataEdge](proposals, pagination)
	return proposals, pageInfo, nil
}
