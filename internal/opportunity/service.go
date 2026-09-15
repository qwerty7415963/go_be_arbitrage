package opportunity

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/unifiedstate"
)

type Service struct {
	engine       *Engine
	unifiedSvc   *unifiedstate.Service
	config       ScannerConfig
	scanInterval time.Duration
	stopCh       chan struct{}
	mu           sync.Mutex
}

func NewService(unifiedSvc *unifiedstate.Service, config ScannerConfig) *Service {
	return &Service{
		engine:       NewEngine(config),
		unifiedSvc:   unifiedSvc,
		config:       config,
		scanInterval: 1 * time.Second,
		stopCh:       make(chan struct{}),
	}
}

func (s *Service) Start(ctx context.Context) {
	go s.scanLoop(ctx)
	log.Printf("opportunity scanner started (interval=%v)", s.scanInterval)
}

func (s *Service) Stop() {
	close(s.stopCh)
	log.Printf("opportunity scanner stopped")
}

func (s *Service) scanLoop(ctx context.Context) {
	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

func (s *Service) scan() {
	snapshot := s.unifiedSvc.GetSnapshot()
	if snapshot == nil {
		return
	}

	result := s.engine.ScanAll(snapshot)

	if len(result.Opportunities) > 0 {
		log.Printf("opportunity scan: found %d opportunities (instruments=%d, duration=%v)",
			len(result.Opportunities), result.Instruments, result.Duration)
	}
}

func (s *Service) ScanNow() *ScanResult {
	snapshot := s.unifiedSvc.GetSnapshot()
	if snapshot == nil {
		return &ScanResult{
			Opportunities: []*Opportunity{},
			ScannedAt:     time.Now(),
			Instruments:   0,
			Venues:        0,
		}
	}
	return s.engine.ScanAll(snapshot)
}

func (s *Service) GetOpportunity(id uuid.UUID) *Opportunity {
	return s.engine.GetOpportunity(id)
}

func (s *Service) GetAllOpportunities() []*Opportunity {
	return s.engine.GetAllOpportunities()
}

func (s *Service) GetOpportunitiesByType(oppType OpportunityType) []*Opportunity {
	return s.engine.GetOpportunitiesByType(oppType)
}

func (s *Service) GetOpportunitiesByInstrument(instrumentID uuid.UUID) []*Opportunity {
	return s.engine.GetOpportunitiesByInstrument(instrumentID)
}

func (s *Service) RemoveOpportunity(id uuid.UUID) {
	s.engine.RemoveOpportunity(id)
}

func (s *Service) GetConfig() ScannerConfig {
	return s.config
}
