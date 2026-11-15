// Package credstore builds the operating system's protected credential store.
//
// It lives outside internal/core so banksync and credentials never reach a
// package that brings in net (x/sys/windows and the D-Bus client do):
//
//   - macOS: the login Keychain through the Security framework, via
//     github.com/keybase/go-keychain;
//   - Windows: Credential Manager (generic credential, local machine);
//   - Linux: the Secret Service (default collection);
//   - every other system: an unavailable store.
//
// No store uses zalando/go-keyring or the security(1) command: both would put
// a secret in a child process's argv, where any local process can read it.
// Secrets are bytes; they are never logged and never appear in an error.
package credstore
