//go:build !unix

package findeskimport

func secureFile(string) error {
	return nil
}
