//go:build !unix

package programs

import "os"

func isExecutable(_ os.FileInfo) bool {
	return true
}
