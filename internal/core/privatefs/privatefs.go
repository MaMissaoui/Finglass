// Package privatefs restricts private folders and files to their owner.
//
// On Unix a private folder is 0700 and a private file 0600. Windows has no
// POSIX permission bits, so there a protected DACL that allows only the
// current user, SYSTEM and Administrators stands in for them.
package privatefs
