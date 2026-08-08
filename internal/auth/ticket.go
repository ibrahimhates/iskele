package auth

import (
	"errors"
	"sync"
	"time"

	"github.com/ibrahimhates/iskele/internal/store"
)

// TicketTTL is how long a WebSocket/SSE ticket stays valid. Long enough for a
// browser to open the connection, short enough that a ticket captured from a
// proxy log is useless.
const TicketTTL = 60 * time.Second

// ErrTicketInvalid is returned for an unknown, expired or already-used ticket.
var ErrTicketInvalid = errors.New("invalid or expired ticket")

// Ticket is a single-use credential for a streaming connection.
//
// Browsers cannot set an Authorization header on a WebSocket handshake or an
// EventSource request, so the token has to travel in the URL. Putting the
// access token there would leak it into proxy logs and browser history; a
// ticket that dies on first use and expires in a minute does not.
type Ticket struct {
	UserID    string
	Username  string
	Role      store.Role
	TokenID   string
	Scopes    []string
	ExpiresAt time.Time
}

// TicketStore issues and redeems tickets.
//
// It is in-memory on purpose: a ticket outlives at most one minute and one
// connection, so surviving a restart would buy nothing.
type TicketStore struct {
	mu      sync.Mutex
	tickets map[string]Ticket
	ttl     time.Duration
	now     func() time.Time
}

// NewTicketStore builds a ticket store. A zero ttl uses TicketTTL.
func NewTicketStore(ttl time.Duration) *TicketStore {
	if ttl <= 0 {
		ttl = TicketTTL
	}
	return &TicketStore{
		tickets: make(map[string]Ticket),
		ttl:     ttl,
		now:     time.Now,
	}
}

// SetClock replaces the time source, for tests.
func (s *TicketStore) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Issue mints a ticket for an authenticated caller.
func (s *TicketStore) Issue(t Ticket) (string, error) {
	value, err := randomToken(32)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t.ExpiresAt = s.now().Add(s.ttl)
	s.tickets[value] = t

	// Opportunistic sweep: without it, tickets that are issued but never used
	// would accumulate for as long as the process runs.
	s.pruneLocked()

	return value, nil
}

// Redeem consumes a ticket, returning the identity it carries.
func (s *TicketStore) Redeem(value string) (Ticket, error) {
	if value == "" {
		return Ticket{}, ErrTicketInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tickets[value]
	if !ok {
		return Ticket{}, ErrTicketInvalid
	}
	// Single use: delete whether or not it turns out to be expired.
	delete(s.tickets, value)

	if s.now().After(t.ExpiresAt) {
		return Ticket{}, ErrTicketInvalid
	}
	return t, nil
}

// Len reports how many tickets are outstanding, for tests and diagnostics.
func (s *TicketStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tickets)
}

// pruneLocked drops expired tickets. The caller must hold the mutex.
func (s *TicketStore) pruneLocked() {
	now := s.now()
	for value, t := range s.tickets {
		if now.After(t.ExpiresAt) {
			delete(s.tickets, value)
		}
	}
}
