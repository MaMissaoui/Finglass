//go:build windows

package privatefs

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// SecureDir makes path a private folder by replacing its DACL with a protected
// one that allows only the current user, SYSTEM and Administrators.
func SecureDir(path string) error {
	return secure(path, true)
}

// SecureFile makes path a private file by replacing its DACL with a protected
// one that allows only the current user, SYSTEM and Administrators.
func SecureFile(path string) error {
	return secure(path, false)
}

func secure(path string, folder bool) error {
	userSID, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	sddl := "D:P(A;;FA;;;" + userSID + ")(A;;FA;;;SY)(A;;FA;;;BA)"
	if folder {
		sddl = "D:P(A;OICI;FA;;;" + userSID + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	}

	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if dacl == nil {
		return fmt.Errorf("%s: %w", path, errDACLMissing)
	}

	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Check reports whether path has a DACL whose every entry allows only the current
// user, SYSTEM and Administrators.
func Check(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if sd == nil {
		return fmt.Errorf("%s: %w", path, errDACLMissing)
	}

	// The ACEs decide. A path that SecureDir/SecureFile set is protected; a
	// child that only inherits from a secured folder is not protected but
	// carries the same owner/SYSTEM/Administrators ACEs, which checkSDDL
	// requires of every entry.
	userSID, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := checkSDDL(sd.String(), userSID); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// currentUserSID reads the SID from the process token, never from the
// environment or the folder's owner.
func currentUserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	if user == nil || user.User.Sid == nil {
		return "", errDACLMissing
	}

	sid := user.User.Sid.String()
	if sid == "" {
		return "", errDACLMissing
	}
	return sid, nil
}
