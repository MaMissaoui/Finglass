// Package fintsproduct holds the FinTS product registration id.
//
// The id is a release secret. It is injected at link time with
//
//	-ldflags -X github.com/MaMissaoui/Finglass/internal/core/fintsproduct.productID=<id>
//
// and is empty in the source tree. Never log or display the id; report only
// [Present].
package fintsproduct

// productID is the FinTS product registration id. It is empty in the tree and
// set only by -ldflags -X. Never log or display it.
var productID string

// ID returns the configured product id and true only when it is valid. It
// returns "", false otherwise. Never log or display the returned value.
func ID() (string, bool) {
	if !valid(productID) {
		return "", false
	}
	return productID, true
}

// Present reports whether a valid product id was linked into this build. It is
// safe to report: it never reveals the id.
func Present() bool {
	_, ok := ID()
	return ok
}

// valid accepts 1-30 ASCII letters or digits. The restricted set also keeps
// FinTS syntax separators (? ' + : @) out.
func valid(id string) bool {
	if len(id) < 1 || len(id) > 30 {
		return false
	}
	for index := 0; index < len(id); index++ {
		character := id[index]
		if (character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}
