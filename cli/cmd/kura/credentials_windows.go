//go:build windows

package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func restrictFile(path string) error {
	return restrictToCurrentUser(path, false)
}

func restrictDir(path string) error {
	return restrictToCurrentUser(path, true)
}

func restrictToCurrentUser(path string, directory bool) error {
	sid, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("restrict credential file: %w", err)
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	dacl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}, nil)
	if err != nil {
		return fmt.Errorf("restrict credential file: %w", err)
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
		return fmt.Errorf("restrict credential file: %w", err)
	}
	return nil
}

func currentUserSID() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid.Copy()
}

func assertUserPrivate(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("account session file must not be a symlink")
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || sd == nil {
		return fmt.Errorf("credential file permissions could not be verified")
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("account session file is accessible to other users")
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount < 1 {
		return fmt.Errorf("account session file is accessible to other users")
	}
	userSID, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("credential file permissions could not be verified")
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil || ace == nil {
			return fmt.Errorf("credential file permissions could not be verified")
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("account session file is accessible to other users")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() || !sid.Equals(userSID) {
			return fmt.Errorf("account session file is accessible to other users")
		}
	}
	return nil
}
