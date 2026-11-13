package fintsproduct

// SetForTest sets productID for this package's tests. Production code sets it
// only with -ldflags -X.
func SetForTest(id string) {
	productID = id
}
