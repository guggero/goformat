package foo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// signIn seeds a stored session, standing in for a completed emailed-code
// login.
func (h *refreshHarness) signIn(t *testing.T, access, refresh string) {
	t.Helper()

	require.NoError(t, h.svc.store.Save(
		context.Background(), brokers.Session{
			Token:   sessionWith(t, access, refresh, "front-1"),
			Subject: "sat@example.com",
		},
	))
}
