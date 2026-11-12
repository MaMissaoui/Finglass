//go:build !unix

package service

func secureFile(string) error {
	return nil
}
