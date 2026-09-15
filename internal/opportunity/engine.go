package opportunity

import (
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
)

type Engine struct {
	calculator *Calculator
	config     ScannerConfig
	results    map[uuid.UUID]*Opportunity
	mu         sync.RWMutex
}

func NewEngine(config ScannerConfig) *Engine {
	return &Engine{
		calculator: NewCalculator(config),
		config:     config,
		results:    make(map[uuid.UUID]*Opportunity),
	}
}

func (e *Engine) ScanAll(state *unifiedstate.UnifiedStateSnapshot) *ScanResult {
	start := time.Now()
	var opportunities []*Opportunity

	for _, inst := range state.Instruments {
		if inst.IsStale || inst.HealthyVenues == 0 {
			continue
		}

		venuePairs := e.getVenuePairs(inst)
		for _, pair := range venuePairs {
			opp, err := e.calculator.CalculatePriceArb(pair)
			if err == nil && opp != nil {
				opportunities = append(opportunities, opp)
			}
		}

		fundingPairs := e.getFundingPairs(inst)
		for _, pair := range fundingPairs {
			opp, err := e.calculator.CalculateFundingArb(pair)
			if err == nil && opp != nil {
				opportunities = append(opportunities, opp)
			}
		}

		for venueID, vs := range inst.VenueStates {
			if vs.Health != unifiedstate.VenueHealthHealthy {
				continue
			}
			if vs.MarkPrice == nil || vs.IndexPrice == nil {
				continue
			}
			venueCode := ""
			if inst.BestBidVenue != nil && *inst.BestBidVenue == venueID {
				venueCode = vs.VenueCode
			}
			if venueCode == "" {
				venueCode = vs.VenueCode
			}

			input := &BasisArbInput{
				InstrumentID:    inst.InstrumentID,
				CanonicalSymbol: inst.CanonicalSymbol,
				Venue:           vs,
				VenueID:         venueID,
				VenueCode:       venueCode,
			}
			opp, err := e.calculator.CalculateBasisArb(input)
			if err == nil && opp != nil {
				opportunities = append(opportunities, opp)
			}
		}
	}

	e.expireOldOpportunities()

	e.mu.Lock()
	for _, opp := range opportunities {
		e.results[opp.ID] = opp
	}
	e.mu.Unlock()

	return &ScanResult{
		Opportunities: opportunities,
		ScannedAt:     time.Now(),
		Duration:      time.Since(start),
		Instruments:   state.TotalInstruments,
		Venues:        len(e.countVenues(state)),
	}
}

func (e *Engine) getVenuePairs(inst *unifiedstate.InstrumentState) []*PriceArbInput {
	var pairs []*PriceArbInput

	venues := make([]venuePair, 0, len(inst.VenueStates))
	for id, vs := range inst.VenueStates {
		if vs.Health == unifiedstate.VenueHealthHealthy && vs.BestBid != nil && vs.BestAsk != nil {
			venues = append(venues, venuePair{id: id, state: vs})
		}
	}

	for i := 0; i < len(venues); i++ {
		for j := i + 1; j < len(venues); j++ {
			a := venues[i]
			b := venues[j]

			if a.state.BestAsk != nil && b.state.BestBid != nil {
				pairs = append(pairs, &PriceArbInput{
					InstrumentID:    inst.InstrumentID,
					CanonicalSymbol: inst.CanonicalSymbol,
					BuyVenue:        a.state,
					SellVenue:       b.state,
					BuyVenueID:      a.id,
					BuyVenueCode:    a.state.VenueCode,
					SellVenueID:     b.id,
					SellVenueCode:   b.state.VenueCode,
				})
			}

			if b.state.BestAsk != nil && a.state.BestBid != nil {
				pairs = append(pairs, &PriceArbInput{
					InstrumentID:    inst.InstrumentID,
					CanonicalSymbol: inst.CanonicalSymbol,
					BuyVenue:        b.state,
					SellVenue:       a.state,
					BuyVenueID:      b.id,
					BuyVenueCode:    b.state.VenueCode,
					SellVenueID:     a.id,
					SellVenueCode:   a.state.VenueCode,
				})
			}
		}
	}

	return pairs
}

func (e *Engine) getFundingPairs(inst *unifiedstate.InstrumentState) []*FundingArbInput {
	var pairs []*FundingArbInput

	venues := make([]venuePair, 0, len(inst.VenueStates))
	for id, vs := range inst.VenueStates {
		if vs.Health == unifiedstate.VenueHealthHealthy && vs.Funding != nil {
			venues = append(venues, venuePair{id: id, state: vs})
		}
	}

	for i := 0; i < len(venues); i++ {
		for j := i + 1; j < len(venues); j++ {
			a := venues[i]
			b := venues[j]

			pairs = append(pairs, &FundingArbInput{
				InstrumentID:    inst.InstrumentID,
				CanonicalSymbol: inst.CanonicalSymbol,
				VenueA:          a.state,
				VenueB:          b.state,
				VenueAID:        a.id,
				VenueACode:      a.state.VenueCode,
				VenueBID:        b.id,
				VenueBCode:      b.state.VenueCode,
			})
		}
	}

	return pairs
}

func (e *Engine) expireOldOpportunities() {
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()

	for id, opp := range e.results {
		if now.After(opp.ExpiresAt) {
			opp.Status = OpportunityStatusExpired
			opp.UpdatedAt = now
			delete(e.results, id)
		}
	}
}

func (e *Engine) GetOpportunity(id uuid.UUID) *Opportunity {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.results[id]
}

func (e *Engine) GetAllOpportunities() []*Opportunity {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]*Opportunity, 0, len(e.results))
	for _, opp := range e.results {
		result = append(result, opp)
	}
	return result
}

func (e *Engine) GetOpportunitiesByType(oppType OpportunityType) []*Opportunity {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*Opportunity
	for _, opp := range e.results {
		if opp.OpportunityType == oppType {
			result = append(result, opp)
		}
	}
	return result
}

func (e *Engine) GetOpportunitiesByInstrument(instrumentID uuid.UUID) []*Opportunity {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*Opportunity
	for _, opp := range e.results {
		if opp.InstrumentID == instrumentID {
			result = append(result, opp)
		}
	}
	return result
}

func (e *Engine) RemoveOpportunity(id uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.results, id)
}

func (e *Engine) countVenues(state *unifiedstate.UnifiedStateSnapshot) map[uuid.UUID]bool {
	venues := make(map[uuid.UUID]bool)
	for _, inst := range state.Instruments {
		for id := range inst.VenueStates {
			venues[id] = true
		}
	}
	return venues
}

type venuePair struct {
	id    uuid.UUID
	state *unifiedstate.VenueMarketState
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
