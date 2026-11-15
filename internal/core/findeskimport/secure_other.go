//go:build !unix

package findeskimport

import "github.com/MaMissaoui/Finglass/internal/core/privatefs"

func secureFile(path string) error {
	_ = privatefs.SecureFile(path)
	return nil
}
