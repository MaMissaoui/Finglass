package storetest_test

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
	"github.com/MaMissaoui/Finglass/internal/core/credentials/storetest"
)

// TestMemoryStoreConforms runs always: the in-memory store must obey the same
// contract as the system stores.
func TestMemoryStoreConforms(t *testing.T) {
	storetest.Run(t, credentials.NewMemory())
}
