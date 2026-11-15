//go:build windows

package privatefs

import (
	"errors"
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

// Unsupported reports whether err means the volume cannot keep private
// permissions.
func Unsupported(err error) bool {
	return errors.Is(err, ErrNoACLs)
}

func secure(path string, folder bool) error {
	user, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	userSID := user.String()
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
		if !volumeKeepsACLs(path) {
			return fmt.Errorf("%s: %w: %w", path, ErrNoACLs, err)
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// volumeKeepsACLs reports whether the volume holding path keeps ACLs. A
// filesystem such as FAT does not, so failing to set a DACL there is not a
// failure of the private-folder promise but a property of the volume.
func volumeKeepsACLs(path string) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	volume := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumePathName(name, &volume[0], uint32(len(volume))); err != nil {
		return false
	}

	var flags uint32
	if err := windows.GetVolumeInformation(&volume[0], nil, 0, nil, nil, &flags, nil, 0); err != nil {
		return false
	}
	return flags&windows.FILE_PERSISTENT_ACLS != 0
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
	allowed, err := privateDACLMatcher()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := checkSDDL(sd.String(), allowed); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// privateDACLMatcher reports whether an ACE's SID string names the current
// user, SYSTEM or Administrators.
//
// SDDL prints a SID as an alias (LA, SY, BA) or in its S-1-… form, and the
// current user can print as an alias too (the built-in Administrator prints
// LA), so the text is never compared. Every SID string is converted to a real
// SID and compared as one; a string that does not convert is not allowed.
func privateDACLMatcher() (func(sid string) bool, error) {
	user, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}

	return func(sid string) bool {
		value, err := windows.StringToSid(sid)
		if err != nil {
			// A SID string that does not convert names no allowed principal.
			return false
		}
		return windows.EqualSid(value, user) ||
			windows.EqualSid(value, system) ||
			windows.EqualSid(value, administrators)
	}, nil
}

// currentUserSID reads the SID from the process token, never from the
// environment or the folder's owner.
func currentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	if user == nil || user.User.Sid == nil {
		return nil, errDACLMissing
	}

	sid := user.User.Sid
	if sid.String() == "" {
		return nil, errDACLMissing
	}
	return sid, nil
}
