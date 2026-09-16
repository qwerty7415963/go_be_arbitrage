package strategy

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/opportunity"
)

type Engine struct {
	instances map[uuid.UUID]*runningInstance
	mu        sync.RWMutex
}

type runningInstance struct {
	instance *StrategyInstance
	cancel   context.CancelFunc
}

func NewEngine() *Engine {
	return &Engine{
		instances: make(map[uuid.UUID]*runningInstance),
	}
}

func (e *Engine) StartInstance(ctx context.Context, instance *StrategyInstance, oppSvc *opportunity.Service) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.instances[instance.ID]; exists {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	e.instances[instance.ID] = &runningInstance{
		instance: instance,
		cancel:   cancel,
	}

	go e.runInstance(ctx, instance, oppSvc)
}

func (e *Engine) StopInstance(id uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if ri, exists := e.instances[id]; exists {
		ri.cancel()
		delete(e.instances, id)
	}
}

func (e *Engine) GetRunningInstances() []uuid.UUID {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var ids []uuid.UUID
	for id := range e.instances {
		ids = append(ids, id)
	}
	return ids
}

func (e *Engine) runInstance(ctx context.Context, instance *StrategyInstance, oppSvc *opportunity.Service) {
	log.Printf("strategy engine: starting instance %s (mode=%s)", instance.ID, instance.Mode)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("strategy engine: stopping instance %s", instance.ID)
			return
		case <-ticker.C:
			e.evaluateOpportunities(ctx, instance, oppSvc)
		}
	}
}

func (e *Engine) evaluateOpportunities(ctx context.Context, instance *StrategyInstance, oppSvc *opportunity.Service) {
	opps := oppSvc.GetAllOpportunities()
	if len(opps) == 0 {
		return
	}

	for _, opp := range opps {
		if !e.matchesStrategy(instance, opp) {
			continue
		}

		decision := e.makeDecision(instance, opp)
		if decision != nil {
			log.Printf("strategy engine: instance %s decided %s for opportunity %s",
				instance.ID, decision.Action, opp.ID)
		}
	}
}

func (e *Engine) matchesStrategy(instance *StrategyInstance, opp *opportunity.Opportunity) bool {
	if instance.Config == nil {
		return true
	}

	if len(instance.Config.Instruments) > 0 {
		found := false
		for _, id := range instance.Config.Instruments {
			if id == opp.InstrumentID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (e *Engine) makeDecision(instance *StrategyInstance, opp *opportunity.Opportunity) *StrategyDecision {
	if instance.Config != nil {
		if opp.ExpectedNetEdgeBPS < instance.Config.MinNetEdgeBPS {
			return nil
		}
		if opp.Confidence < instance.Config.MinConfidence {
			return nil
		}
	}

	if opp.Status != opportunity.OpportunityStatusDetected {
		return nil
	}

	return &StrategyDecision{
		ID:            uuid.New(),
		StrategyID:    instance.ID,
		OpportunityID: opp.ID,
		Action:        "ACCEPT",
		Status:        string(instance.Mode),
		ExecutedAt:    time.Now(),
		CreatedAt:     time.Now(),
	}
}
