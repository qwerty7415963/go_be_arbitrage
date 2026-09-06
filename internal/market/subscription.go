package market

import (
	"sync"

	"github.com/google/uuid"
)

type SubscriptionStatus string

const (
	SubscriptionStatusPending    SubscriptionStatus = "PENDING"
	SubscriptionStatusActive     SubscriptionStatus = "ACTIVE"
	SubscriptionStatusFailed     SubscriptionStatus = "FAILED"
	SubscriptionStatusUnsubscribed SubscriptionStatus = "UNSUBSCRIBED"
)

type Subscription struct {
	ID            uuid.UUID
	VenueID       uuid.UUID
	InstrumentID  uuid.UUID
	Channel       string
	Status        SubscriptionStatus
	ConnectionID  uuid.UUID
	Error         error
	mu            sync.RWMutex
}

type SubscriptionManager struct {
	subscriptions map[uuid.UUID]*Subscription
	venueSubs     map[uuid.UUID]map[uuid.UUID]bool
	instrumentSubs map[uuid.UUID]map[uuid.UUID]bool
	channelSubs   map[string]map[uuid.UUID]bool
	mu            sync.RWMutex
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		subscriptions:  make(map[uuid.UUID]*Subscription),
		venueSubs:      make(map[uuid.UUID]map[uuid.UUID]bool),
		instrumentSubs: make(map[uuid.UUID]map[uuid.UUID]bool),
		channelSubs:    make(map[string]map[uuid.UUID]bool),
	}
}

func (sm *SubscriptionManager) Subscribe(venueID, instrumentID uuid.UUID, channel string, connectionID uuid.UUID) *Subscription {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sub := &Subscription{
		ID:           uuid.New(),
		VenueID:      venueID,
		InstrumentID: instrumentID,
		Channel:      channel,
		Status:       SubscriptionStatusPending,
		ConnectionID: connectionID,
	}

	sm.subscriptions[sub.ID] = sub

	if sm.venueSubs[venueID] == nil {
		sm.venueSubs[venueID] = make(map[uuid.UUID]bool)
	}
	sm.venueSubs[venueID][sub.ID] = true

	if sm.instrumentSubs[instrumentID] == nil {
		sm.instrumentSubs[instrumentID] = make(map[uuid.UUID]bool)
	}
	sm.instrumentSubs[instrumentID][sub.ID] = true

	if sm.channelSubs[channel] == nil {
		sm.channelSubs[channel] = make(map[uuid.UUID]bool)
	}
	sm.channelSubs[channel][sub.ID] = true

	return sub
}

func (sm *SubscriptionManager) GetSubscription(id uuid.UUID) (*Subscription, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sub, ok := sm.subscriptions[id]
	return sub, ok
}

func (sm *SubscriptionManager) UpdateStatus(id uuid.UUID, status SubscriptionStatus) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sub, ok := sm.subscriptions[id]; ok {
		sub.mu.Lock()
		defer sub.mu.Unlock()
		sub.Status = status
	}
}

func (sm *SubscriptionManager) UpdateError(id uuid.UUID, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sub, ok := sm.subscriptions[id]; ok {
		sub.mu.Lock()
		defer sub.mu.Unlock()
		sub.Error = err
		sub.Status = SubscriptionStatusFailed
	}
}

func (sm *SubscriptionManager) Unsubscribe(id uuid.UUID) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sub, ok := sm.subscriptions[id]; ok {
		sub.mu.Lock()
		defer sub.mu.Unlock()
		sub.Status = SubscriptionStatusUnsubscribed

		if ids, ok := sm.venueSubs[sub.VenueID]; ok {
			delete(ids, id)
		}
		if ids, ok := sm.instrumentSubs[sub.InstrumentID]; ok {
			delete(ids, id)
		}
		if ids, ok := sm.channelSubs[sub.Channel]; ok {
			delete(ids, id)
		}
	}
}

func (sm *SubscriptionManager) GetVenueSubscriptions(venueID uuid.UUID) []*Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var subs []*Subscription
	if ids, ok := sm.venueSubs[venueID]; ok {
		for id := range ids {
			if sub, ok := sm.subscriptions[id]; ok {
				subs = append(subs, sub)
			}
		}
	}
	return subs
}

func (sm *SubscriptionManager) GetInstrumentSubscriptions(instrumentID uuid.UUID) []*Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var subs []*Subscription
	if ids, ok := sm.instrumentSubs[instrumentID]; ok {
		for id := range ids {
			if sub, ok := sm.subscriptions[id]; ok {
				subs = append(subs, sub)
			}
		}
	}
	return subs
}

func (sm *SubscriptionManager) GetChannelSubscriptions(channel string) []*Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var subs []*Subscription
	if ids, ok := sm.channelSubs[channel]; ok {
		for id := range ids {
			if sub, ok := sm.subscriptions[id]; ok {
				subs = append(subs, sub)
			}
		}
	}
	return subs
}

func (sm *SubscriptionManager) GetActiveSubscriptions() []*Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var subs []*Subscription
	for _, sub := range sm.subscriptions {
		sub.mu.RLock()
		if sub.Status == SubscriptionStatusActive {
			subs = append(subs, sub)
		}
		sub.mu.RUnlock()
	}
	return subs
}

func (sm *SubscriptionManager) GetPendingSubscriptions() []*Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var subs []*Subscription
	for _, sub := range sm.subscriptions {
		sub.mu.RLock()
		if sub.Status == SubscriptionStatusPending {
			subs = append(subs, sub)
		}
		sub.mu.RUnlock()
	}
	return subs
}

func (sm *SubscriptionManager) RemoveSubscription(id uuid.UUID) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sub, ok := sm.subscriptions[id]; ok {
		if ids, ok := sm.venueSubs[sub.VenueID]; ok {
			delete(ids, id)
		}
		if ids, ok := sm.instrumentSubs[sub.InstrumentID]; ok {
			delete(ids, id)
		}
		if ids, ok := sm.channelSubs[sub.Channel]; ok {
			delete(ids, id)
		}
		delete(sm.subscriptions, id)
	}
}
