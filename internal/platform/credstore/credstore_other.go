//go:build !darwin && !windows && !linux

package credstore

import "github.com/MaMissaoui/Finglass/internal/core/credentials"

// NewSystem returns an unavailable store on a system without a protected
// credential store Finglass supports.
func NewSystem(_ string) credentials.Store {
	return credentials.NewUnavailable()
}
