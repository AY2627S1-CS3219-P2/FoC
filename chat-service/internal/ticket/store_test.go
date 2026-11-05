package ticket

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedeemTicket_EmptyTicket(t *testing.T) {
	s := NewStore(nil)
	_, err := s.RedeemTicket(context.Background(), "")
	assert.ErrorIs(t, err, ErrInvalidTicket)
}
