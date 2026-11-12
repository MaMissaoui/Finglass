// Package profileswitch ports crates/finglass-core/src/app/switch.rs: the
// UI-free part of the live profile switch.
//
// A switch always goes through the target profile's unlock window. The order
// matters: prepare the target first, then record the switch in the profile
// that is still open, then remember the target. Nothing is torn down here; the
// app shell does that after [BeginSwitch] has returned a [startup.Startup].
package profileswitch

import (
	"context"
	"fmt"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
)

// SwitchErrorKind says why a profile switch was refused.
type SwitchErrorKind uint8

const (
	// SwitchSameProfile means the target is the profile that is already open.
	SwitchSameProfile SwitchErrorKind = iota
	// SwitchUnknownProfile means no profile is registered under that slug.
	SwitchUnknownProfile
	// SwitchBusy means another background job or a switch is already running.
	SwitchBusy
	// SwitchProfile means a profile registry or folder failure.
	SwitchProfile
	// SwitchStartup means preparing the target profile failed.
	SwitchStartup
	// SwitchDatabase means writing the audit entry failed.
	SwitchDatabase
)

// SwitchError is everything that can go wrong while switching profiles.
//
// No value carries a passphrase or any other secret.
type SwitchError struct {
	// Kind says which failure this is.
	Kind SwitchErrorKind
	// Err is the underlying error, when there is one.
	Err error
}

// Error implements error. It never contains a passphrase.
func (e *SwitchError) Error() string {
	switch e.Kind {
	case SwitchSameProfile:
		return "the target profile is already open"
	case SwitchUnknownProfile:
		return "unknown profile"
	case SwitchBusy:
		return "another task is still running"
	default:
		if e.Err != nil {
			return e.Err.Error()
		}
	}
	return i18n.Text(i18n.En, i18n.ErrorUnexpected)
}

// Unwrap returns the underlying error.
func (e *SwitchError) Unwrap() error {
	return e.Err
}

// Message returns the localized text the Settings page shows for a refused
// switch.
func (e *SwitchError) Message(language i18n.Language) string {
	switch e.Kind {
	case SwitchBusy:
		return i18n.Text(language, i18n.ProfileSwitchBusy)
	default:
		return i18n.Text(language, i18n.ErrorUnexpected)
	}
}

// BeginSwitch prepares to, records the switch in the old database and makes to
// the active profile.
//
// from and to are profile slugs (nil is the default profile). Nothing is torn
// down here: the caller closes the old profile only after this returns
// successfully.
func BeginSwitch(
	database *db.Database,
	appPaths paths.AppPaths,
	from *string,
	to *string,
	now time.Time,
) (startup.Startup, error) {
	if sameSlug(to, from) {
		return startup.Startup{}, &SwitchError{Kind: SwitchSameProfile}
	}

	if to != nil {
		listed, err := profiles.List(appPaths)
		if err != nil {
			return startup.Startup{}, &SwitchError{Kind: SwitchProfile, Err: err}
		}
		known := false
		for _, profile := range listed {
			if profile.Slug == *to {
				known = true
				break
			}
		}
		if !known {
			return startup.Startup{}, &SwitchError{Kind: SwitchUnknownProfile}
		}
	}

	// The staged restore only moves the target folder's database files and
	// documents, so the old database may stay open while this runs. If it
	// fails, the old profile stays open and nothing has been recorded.
	prepared, err := startup.PrepareProfile(appPaths, to, now)
	if err != nil {
		return startup.Startup{}, &SwitchError{Kind: SwitchStartup, Err: err}
	}

	// The entry belongs to the database being left, while it is still the open
	// one. Slugs only: names and paths are private and do not belong in the
	// audit log.
	details := fmt.Sprintf(
		"Switched from profile %s to %s",
		startup.SlugKey(from),
		startup.SlugKey(to),
	)
	if err := audit.Log(
		context.Background(),
		database,
		"PROFILE_SWITCH",
		audit.ActorUser,
		details,
	); err != nil {
		return startup.Startup{}, &SwitchError{Kind: SwitchDatabase, Err: err}
	}

	value := ""
	if to != nil {
		value = *to
	}
	if err := profiles.SetActive(appPaths, value); err != nil {
		return startup.Startup{}, &SwitchError{Kind: SwitchProfile, Err: err}
	}

	return prepared, nil
}

func sameSlug(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
