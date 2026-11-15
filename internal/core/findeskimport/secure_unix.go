//go:build unix

package findeskimport

import "github.com/MaMissaoui/Finglass/internal/core/privatefs"

func secureFile(path string) error {
	if err := privatefs.SecureFile(path); err != nil && !privatefs.Unsupported(err) {
		return err
	}
	return nil
}
