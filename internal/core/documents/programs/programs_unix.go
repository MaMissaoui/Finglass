//go:build unix

package programs

import "os"

func isExecutable(metadata os.FileInfo) bool {
	return metadata.Mode().Perm()&0o111 != 0
}
