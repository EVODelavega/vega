package governance

import (
	"context"
	"sync"
	"time"

	"code.vegaprotocol.io/vega/assets"
	"code.vegaprotocol.io/vega/events"
	"code.vegaprotocol.io/vega/logging"
	types "code.vegaprotocol.io/vega/proto"
	"code.vegaprotocol.io/vega/validators"

	"github.com/pkg/errors"
)

var (
	ErrProposalNotFound                        = errors.New("proposal not found")
	ErrProposalIsDuplicate                     = errors.New("proposal with given ID already exists")
	ErrProposalCloseTimeInvalid                = errors.New("proposal closes too soon or too late")
	ErrProposalEnactTimeInvalid                = errors.New("proposal enactment times too soon or late")
	ErrVoterInsufficientTokens                 = errors.New("vote requires more tokens than party has")
	ErrVotePeriodExpired                       = errors.New("proposal voting has been closed")
	ErrAssetProposalReferenceDuplicate         = errors.New("duplicate asset proposal for reference")
	ErrProposalInvalidState                    = errors.New("proposal state not valid, only open can be submitted")
	ErrProposalCloseTimeTooSoon                = errors.New("proposal closes too soon")
	ErrProposalCloseTimeTooLate                = errors.New("proposal closes too late")
	ErrProposalEnactTimeTooSoon                = errors.New("proposal enactment time is too soon")
	ErrProposalEnactTimeTooLate                = errors.New("proposal enactment time is too late")
	ErrProposalInsufficientTokens              = errors.New("party requires more tokens to submit a proposal")
	ErrProposalMinPaticipationStakeTooLow      = errors.New("proposal minimum participation stake is too low")
	ErrProposalMinPaticipationStakeInvalid     = errors.New("proposal minimum participation stake is out of bounds [0..1]")
	ErrProposalMinRequiredMajorityStakeTooLow  = errors.New("proposal minimum required majority stake is too low")
	ErrProposalMinRequiredMajorityStakeInvalid = errors.New("proposal minimum required majority stake is out of bounds [0.5..1]")
	ErrProposalPassed                          = errors.New("proposal has passed and can no longer be voted on")
	ErrNoNetworkParams                         = errors.New("network parameters were not configured for this proposal type")
)

// Broker - event bus
//go:generate go run github.com/golang/mock/mockgen -destination mocks/broker_mock.go -package mocks code.vegaprotocol.io/vega/governance Broker
type Broker interface {
	Send(e events.Event)
}

// Accounts ...
//go:generate go run github.com/golang/mock/mockgen -destination mocks/accounts_mock.go -package mocks code.vegaprotocol.io/vega/governance Accounts
type Accounts interface {
	GetPartyTokenAccount(id string) (*types.Account, error)
	GetTotalTokens() uint64
}

//go:generate go run github.com/golang/mock/mockgen -destination mocks/assets_mock.go -package mocks code.vegaprotocol.io/vega/governance Assets
type Assets interface {
	NewAsset(ref string, assetSrc *types.AssetSource) (string, error)
	Get(assetID string) (*assets.Asset, error)
}

// TimeService ...
//go:generate go run github.com/golang/mock/mockgen -destination mocks/time_service_mock.go -package mocks code.vegaprotocol.io/vega/governance TimeService
type TimeService interface {
	GetTimeNow() (time.Time, error)
}

// ExtResChecker ...
//go:generate go run github.com/golang/mock/mockgen -destination mocks/ext_res_checker_mock.go -package mocks code.vegaprotocol.io/vega/governance ExtResChecker
type ExtResChecker interface {
	StartCheck(validators.Resource, func(interface{}, bool), time.Time) error
}

// Engine is the governance engine that handles proposal and vote lifecycle.
type Engine struct {
	Config
	mu                     sync.Mutex
	log                    *logging.Logger
	accs                   Accounts
	currentTime            time.Time
	activeProposals        map[string]*proposalData
	networkParams          NetworkParameters
	nodeProposalValidation *NodeValidation
	broker                 Broker
	assets                 Assets
}

type proposalData struct {
	*types.Proposal
	yes map[string]*types.Vote
	no  map[string]*types.Vote
}

func NewEngine(log *logging.Logger, cfg Config, params *NetworkParameters, accs Accounts, broker Broker, assets Assets, erc ExtResChecker, now time.Time) (*Engine, error) {
	log = log.Named(namedLogger)
	// ensure params are set
	nodeValidation, err := NewNodeValidation(log, assets, now, erc)
	if err != nil {
		return nil, err
	}

	return &Engine{
		Config:                 cfg,
		accs:                   accs,
		log:                    log,
		currentTime:            now,
		activeProposals:        map[string]*proposalData{},
		networkParams:          *params,
		nodeProposalValidation: nodeValidation,
		broker:                 broker,
		assets:                 assets,
	}, nil
}

// ReloadConf updates the internal configuration of the governance engine
func (e *Engine) ReloadConf(cfg Config) {
	e.log.Info("reloading configuration")
	if e.log.GetLevel() != cfg.Level.Get() {
		e.log.Info("updating log level",
			logging.String("old", e.log.GetLevel().String()),
			logging.String("new", cfg.Level.String()),
		)
		e.log.SetLevel(cfg.Level.Get())
	}

	e.mu.Lock()
	e.Config = cfg
	e.mu.Unlock()
}

func (e *Engine) preEnactProposal(p *types.Proposal) (te *ToEnact, err error) {
	te = &ToEnact{
		p: p,
	}
	defer func() {
		if err != nil {
			p.State = types.Proposal_STATE_FAILED
		}
	}()
	switch change := p.Terms.Change.(type) {
	case *types.ProposalTerms_NewMarket:
		mkt, err := createMarket(p.ID, change.NewMarket.Changes, &e.networkParams, e.currentTime)
		if err != nil {
			return nil, err
		}
		te.m = mkt
	case *types.ProposalTerms_NewAsset:
		asset, err := e.assets.Get(p.GetID())
		if err != nil {
			return nil, err
		}
		te.a = asset.ProtoAsset()
	}
	return
}

// OnChainTimeUpdate triggers time bound state changes.
func (e *Engine) OnChainTimeUpdate(ctx context.Context, t time.Time) []*ToEnact {
	e.currentTime = t
	var toBeEnacted []*ToEnact
	if len(e.activeProposals) > 0 {
		now := t.Unix()

		totalStake := e.accs.GetTotalTokens()
		counter := newStakeCounter(e.log, e.accs)

		for id, proposal := range e.activeProposals {
			if proposal.Terms.ClosingTimestamp < now {
				e.closeProposal(ctx, proposal, counter, totalStake)
			}

			if proposal.State != types.Proposal_STATE_OPEN && proposal.State != types.Proposal_STATE_PASSED {
				delete(e.activeProposals, id)
			} else if proposal.State == types.Proposal_STATE_PASSED && proposal.Terms.EnactmentTimestamp < now {
				enact, err := e.preEnactProposal(proposal.Proposal)
				if err != nil {
					e.broker.Send(events.NewProposalEvent(ctx, *proposal.Proposal))
					e.log.Error("proposal enactment has failed",
						logging.String("proposal-id", proposal.ID),
						logging.Error(err))
				} else {
					toBeEnacted = append(toBeEnacted, enact)
				}
				delete(e.activeProposals, id)
			}
		}
	}

	// then get all proposal accepted through node validation, and start their vote time.
	accepted, rejected := e.nodeProposalValidation.OnChainTimeUpdate(t)
	for _, p := range accepted {
		e.log.Info("proposal has been validated by nodes, starting now",
			logging.String("proposal-id", p.ID))
		p.State = types.Proposal_STATE_OPEN
		e.broker.Send(events.NewProposalEvent(ctx, *p))
		e.startProposal(p) // can't fail, and proposal has been validated at an ulterior time
	}
	for _, p := range rejected {
		e.log.Info("proposal has not been validated by nodes",
			logging.String("proposal-id", p.ID))
		p.State = types.Proposal_STATE_REJECTED
		e.broker.Send(events.NewProposalEvent(ctx, *p))
	}

	// flush here for now
	return toBeEnacted
}

// SubmitProposal submits new proposal to the governance engine so it can be voted on, passed and enacted.
// Only open can be submitted and validated at this point. No further validation happens.
func (e *Engine) SubmitProposal(ctx context.Context, p types.Proposal) error {
	if _, exists := e.activeProposals[p.ID]; exists {
		return ErrProposalIsDuplicate // state is not allowed to change externally
	}
	if p.State == types.Proposal_STATE_OPEN {
		err := e.validateOpenProposal(p)
		if err != nil {
			p.State = types.Proposal_STATE_REJECTED
			if e.log.GetLevel() == logging.DebugLevel {
				e.log.Debug("Proposal rejected", logging.String("proposal-id", p.ID))
			}
		} else {
			// now if it's a 2 steps proposal, start the node votes
			if e.isTwoStepsProposal(&p) {
				p.State = types.Proposal_STATE_WAITING_FOR_NODE_VOTE
				err = e.startTwoStepsProposal(&p)
			} else {
				e.startProposal(&p)
			}
		}
		e.broker.Send(events.NewProposalEvent(ctx, p))
		return err
	}
	return ErrProposalInvalidState
}

func (e *Engine) startProposal(p *types.Proposal) {
	e.activeProposals[p.ID] = &proposalData{
		Proposal: p,
		yes:      map[string]*types.Vote{},
		no:       map[string]*types.Vote{},
	}
}

func (e *Engine) startTwoStepsProposal(p *types.Proposal) error {
	return e.nodeProposalValidation.Start(p)
}

func (e *Engine) isTwoStepsProposal(p *types.Proposal) bool {
	return e.nodeProposalValidation.IsNodeValidationRequired(p)
}

func (e *Engine) getProposalParams(terms *types.ProposalTerms) (*ProposalParameters, error) {
	if terms.GetNewMarket() != nil {
		return &e.networkParams.NewMarkets, nil
	}
	return nil, ErrNoNetworkParams
}

// validates proposals read from the chain
func (e *Engine) validateOpenProposal(proposal types.Proposal) error {
	params, err := e.getProposalParams(proposal.Terms)
	if err != nil {
		return err
	}
	if proposal.Terms.ClosingTimestamp < e.currentTime.Add(params.MinClose).Unix() {
		return ErrProposalCloseTimeTooSoon
	}
	if proposal.Terms.ClosingTimestamp > e.currentTime.Add(params.MaxClose).Unix() {
		return ErrProposalCloseTimeTooLate
	}
	if proposal.Terms.EnactmentTimestamp < e.currentTime.Add(params.MinEnact).Unix() {
		return ErrProposalEnactTimeTooSoon
	}
	if proposal.Terms.EnactmentTimestamp > e.currentTime.Add(params.MaxEnact).Unix() {
		return ErrProposalEnactTimeTooLate
	}
	proposerTokens, err := getGovernanceTokens(e.accs, proposal.PartyID)
	if err != nil {
		return err
	}
	totalTokens := e.accs.GetTotalTokens()
	if float32(proposerTokens) < float32(totalTokens)*params.MinProposerBalance {
		return ErrProposalInsufficientTokens
	}
	return e.validateChange(proposal.Terms)
}

// validates proposed change
func (e *Engine) validateChange(terms *types.ProposalTerms) error {
	switch change := terms.Change.(type) {
	case *types.ProposalTerms_NewMarket:
		return validateNewMarket(e.currentTime, change.NewMarket.Changes)
	}
	return nil
}

// AddVote adds vote onto an existing active proposal (if found) so the proposal could pass and be enacted
func (e *Engine) AddVote(ctx context.Context, vote types.Vote) error {
	proposal, err := e.validateVote(vote)
	if err != nil {
		return err
	}
	// we only want to count the last vote, so add to yes/no map, delete from the other
	// if the party hasn't cast a vote yet, the delete is just a noop
	if vote.Value == types.Vote_VALUE_YES {
		delete(proposal.no, vote.PartyID)
		proposal.yes[vote.PartyID] = &vote
	} else {
		delete(proposal.yes, vote.PartyID)
		proposal.no[vote.PartyID] = &vote
	}
	e.broker.Send(events.NewVoteEvent(ctx, vote))
	return nil
}

func (e *Engine) validateVote(vote types.Vote) (*proposalData, error) {
	proposal, found := e.activeProposals[vote.ProposalID]
	if !found {
		return nil, ErrProposalNotFound
	} else if proposal.State == types.Proposal_STATE_PASSED {
		return nil, ErrProposalPassed
	}

	params, err := e.getProposalParams(proposal.Terms)
	if err != nil {
		return nil, err
	}

	voterTokens, err := getGovernanceTokens(e.accs, vote.PartyID)
	if err != nil {
		return nil, err
	}
	totalTokens := e.accs.GetTotalTokens()
	if float32(voterTokens) < float32(totalTokens)*params.MinVoterBalance {
		return nil, ErrVoterInsufficientTokens
	}

	return proposal, nil
}

// sets proposal in either declined or passed state
func (e *Engine) closeProposal(ctx context.Context, proposal *proposalData, counter *stakeCounter, totalStake uint64) error {
	if proposal.State == types.Proposal_STATE_OPEN {
		proposal.State = types.Proposal_STATE_DECLINED // declined unless passed

		params, err := e.getProposalParams(proposal.Terms)
		if err != nil {
			return err
		}

		yes := counter.countVotes(proposal.yes)
		no := counter.countVotes(proposal.no)
		totalVotes := float32(yes + no)

		// yes          > (yes + no)* required majority ratio
		if float32(yes) > totalVotes*params.RequiredMajority &&
			//(yes+no) >= (yes + no + novote)* required participation ratio
			totalVotes >= float32(totalStake)*params.RequiredParticipation {
			proposal.State = types.Proposal_STATE_PASSED
			e.log.Debug("Proposal passed", logging.String("proposal-id", proposal.ID))
		} else if totalVotes == 0 {
			e.log.Info("Proposal declined - no votes", logging.String("proposal-id", proposal.ID))
		} else {
			e.log.Info(
				"Proposal declined",
				logging.String("proposal-id", proposal.ID),
				logging.Uint64("yes-votes", yes),
				logging.Float32("min-yes-required", totalVotes*params.RequiredMajority),
				logging.Float32("total-votes", totalVotes),
				logging.Float32("min-total-votes-required", float32(totalStake)*params.RequiredParticipation),
				logging.Float32("tokens", float32(totalStake)),
			)
		}
		e.broker.Send(events.NewProposalEvent(ctx, *proposal.Proposal))
	}
	return nil
}

// stakeCounter caches token balance per party and counts votes
// reads from accounts on every miss and does not have expiration policy
type stakeCounter struct {
	log      *logging.Logger
	accounts Accounts
	balances map[string]uint64
}

func newStakeCounter(log *logging.Logger, accounts Accounts) *stakeCounter {
	return &stakeCounter{
		log:      log,
		accounts: accounts,
		balances: map[string]uint64{},
	}
}
func (s *stakeCounter) countVotes(votes map[string]*types.Vote) uint64 {
	var tally uint64
	for _, v := range votes {
		tally += s.getTokens(v.PartyID)
	}
	return tally
}

func (s *stakeCounter) getTokens(partyID string) uint64 {
	if balance, found := s.balances[partyID]; found {
		return balance
	}
	balance, err := getGovernanceTokens(s.accounts, partyID)
	if err != nil {
		s.log.Error(
			"Failed to get governance tokens balance for party",
			logging.String("party-id", partyID),
			logging.Error(err),
		)
		// not much we can do with the error as there is nowhere to buble up the error on tick
		return 0
	}
	s.balances[partyID] = balance
	return balance
}

func getGovernanceTokens(accounts Accounts, partyID string) (uint64, error) {
	account, err := accounts.GetPartyTokenAccount(partyID)
	if err != nil {
		return 0, err
	}
	return account.Balance, nil
}
