package credentials

import (
	"fmt"
	"os"
	"strings"
)

// AccountFor is the credential-store account string of a profile-scoped bank
// access.
//
// It uses an absolute path (without resolving symlinks or `..`), then
// "#bank-<id>". The profile folder keeps two profiles with the same bank id
// apart.
func AccountFor(profileDir string, bankID int64) string {
	return fmt.Sprintf("%s#bank-%d", absolutePath(profileDir), bankID)
}

// absolutePOSIXPath makes a POSIX path absolute: collapse separators, drop `.`
// components, keep `..`, keep a trailing separator, and start relative paths at
// the current directory. It is shared by every system so an account key stored
// on one system keeps its exact form on another.
func absolutePOSIXPath(path string) string {
	if path == "" {
		return path
	}

	trailingSeparator := strings.HasSuffix(path, "/")
	absolute := strings.HasPrefix(path, "/")

	root, components := splitPOSIX(path, absolute)
	if !absolute {
		current, err := os.Getwd()
		if err != nil {
			return path
		}
		currentRoot, currentComponents := splitPOSIX(current, strings.HasPrefix(current, "/"))
		root = currentRoot
		components = append(currentComponents, components...)
	}

	result := root + strings.Join(components, "/")
	if trailingSeparator && !strings.HasSuffix(result, "/") {
		result += "/"
	}
	return result
}

// splitPOSIX separates an absolute root from its path components. Repeated
// separators and `.` components disappear; `..` stays because resolving it
// could change the meaning of a symlinked path.
func splitPOSIX(value string, absolute bool) (root string, components []string) {
	if absolute {
		if strings.HasPrefix(value, "//") && !strings.HasPrefix(value, "///") {
			root = "//"
		} else {
			root = "/"
		}
	}

	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." {
			continue
		}
		components = append(components, component)
	}
	return root, components
}
