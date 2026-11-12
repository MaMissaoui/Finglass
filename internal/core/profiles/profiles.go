// Package profiles manages separate data sets, as FinDesk's ProfileStore and
// ProfileSwitch path logic do.
//
// The default profile is the data root itself, so data from before profiles
// existed stays where it is. Other profiles live in profiles/<slug>/. The
// registry is profiles.properties in the root and is written atomically.
//
// The on-disk format matches `crates/finglass-core/src/profiles.rs` byte for
// byte, so the Go build and the Rust app share the same registry.
package profiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

// DefaultName is the name shown for the default profile until the user renames
// it.
const DefaultName = "Personal"

// MaxNameLength is the longest allowed profile name, in runes.
const MaxNameLength = 40

// ProfilesDir is the folder that holds the non-default profiles.
const ProfilesDir = "profiles"

// DeletedDir is where a deleted profile is moved instead of erased, so it can
// be recovered by hand.
const DeletedDir = "profiles-deleted"

const (
	file       = "profiles.properties"
	keyActive  = "active"
	namePrefix = "name."
	defaultKey = "default"
)

var changes atomic.Uint64

var tempCounter atomic.Uint64

// Profile is one profile: a name and its data folder.
type Profile struct {
	// Slug is the slug, or "" for the default profile.
	Slug string
	// Name is the display name.
	Name string
	// Dir is the profile's data folder.
	Dir string
}

// IsDefault reports whether this is the default profile.
func (p Profile) IsDefault() bool {
	return p.Slug == ""
}

var (
	// ErrEmptyName is returned when the name is empty after trimming.
	ErrEmptyName = errors.New("name is required")
	// ErrNameTooLong is returned when the name is longer than MaxNameLength.
	ErrNameTooLong = errors.New("name is too long (max. 40 characters)")
	// ErrOpenProfile is returned when the profile is currently open.
	ErrOpenProfile = errors.New("the open profile can't be deleted")
	// ErrDefaultProfile is returned when the default profile is not allowed.
	ErrDefaultProfile = errors.New("the default profile can't be deleted")
)

// UnknownError is returned when no profile with this slug is registered.
type UnknownError struct {
	// Slug is the slug that was not found.
	Slug string
}

// Error implements error.
func (e *UnknownError) Error() string {
	return "unknown profile: " + e.Slug
}

// Changes returns the number of successful profile changes.
//
// It increments after every successful [Create], [Rename], [Delete] and
// [ForgetCreated], so views with their own call notice the change.
func Changes() uint64 {
	return changes.Load()
}

func bumpChanges() {
	changes.Add(1)
}

// ProfileDir returns the folder of a non-default profile.
//
// "default" is rejected here, as in FinDesk's ProfileStore.profileDir; use
// paths.AppPaths.ProfileDir when the default profile's root is wanted.
func ProfileDir(p paths.AppPaths, slug string) (string, error) {
	if slug == defaultKey {
		return "", ErrDefaultProfile
	}

	dir, err := p.ProfileDir(slug)
	if err != nil {
		return "", fmt.Errorf("profile folder: %w", err)
	}
	return dir, nil
}

// DataDir returns the data folder for a profile slug; "" is the default
// profile.
func DataDir(p paths.AppPaths, slug string) (string, error) {
	if slug == "" {
		return p.Root(), nil
	}
	return ProfileDir(p, slug)
}

// List returns the default profile first, then the others ordered by name and
// slug.
func List(p paths.AppPaths) ([]Profile, error) {
	properties, err := load(p)
	if err != nil {
		return nil, err
	}
	return buildList(properties, p), nil
}

// Active returns the profile to open, or the default one when the stored
// profile's folder is gone.
func Active(p paths.AppPaths) (Profile, error) {
	properties, err := load(p)
	if err != nil {
		return Profile{}, err
	}

	slug := properties[keyActive]
	fallback := defaultProfile(properties, p)
	if slug == "" {
		return fallback, nil
	}

	for _, profile := range buildList(properties, p) {
		if profile.Slug == slug {
			return profile, nil
		}
	}
	return fallback, nil
}

// Create creates a profile and its data folder, returning it.
func Create(p paths.AppPaths, name string) (Profile, error) {
	clean, err := validName(name)
	if err != nil {
		return Profile{}, err
	}

	properties, err := load(p)
	if err != nil {
		return Profile{}, err
	}

	taken := make(map[string]struct{})
	for key := range properties {
		if slug, ok := strings.CutPrefix(key, namePrefix); ok {
			taken[slug] = struct{}{}
		}
	}

	slug, err := uniqueSlug(p, baseSlug(clean), taken)
	if err != nil {
		return Profile{}, err
	}

	dir, err := p.ProfileDir(slug)
	if err != nil {
		return Profile{}, fmt.Errorf("profile folder: %w", err)
	}
	if err := paths.EnsureDir(dir); err != nil {
		return Profile{}, fmt.Errorf("create profile folder: %w", err)
	}

	properties[namePrefix+slug] = clean
	if err := save(p, properties); err != nil {
		return Profile{}, err
	}
	bumpChanges()

	return Profile{Slug: slug, Name: clean, Dir: dir}, nil
}

// Rename renames the default profile ("") or a stored profile.
func Rename(p paths.AppPaths, slug, name string) error {
	clean, err := validName(name)
	if err != nil {
		return err
	}

	properties, err := load(p)
	if err != nil {
		return err
	}

	keySlug := slug
	if keySlug == "" {
		keySlug = defaultKey
	}
	key := namePrefix + keySlug

	if slug != "" {
		if _, ok := properties[key]; !ok {
			return &UnknownError{Slug: slug}
		}
	}

	properties[key] = clean
	if err := save(p, properties); err != nil {
		return err
	}
	bumpChanges()
	return nil
}

// SetActive remembers the profile to open. It takes effect at the next start.
//
// An empty slug selects the default profile.
func SetActive(p paths.AppPaths, slug string) error {
	properties, err := load(p)
	if err != nil {
		return err
	}

	if slug != "" {
		known := false
		for _, profile := range buildList(properties, p) {
			if profile.Slug == slug {
				known = true
				break
			}
		}
		if !known {
			return &UnknownError{Slug: slug}
		}
	}

	properties[keyActive] = slug
	return save(p, properties)
}

// Delete moves a profile's folder to profiles-deleted/ (nothing is erased) and
// returns where it went.
//
// The default profile and the one currently open cannot be deleted. openSlug
// is "" for the default profile.
func Delete(p paths.AppPaths, slug, openSlug string) (string, error) {
	properties, err := load(p)
	if err != nil {
		return "", err
	}

	key := namePrefix + slug
	if openSlug == slug {
		return "", ErrOpenProfile
	}
	if slug == defaultKey {
		return "", ErrDefaultProfile
	}
	if _, ok := properties[key]; !ok {
		return "", &UnknownError{Slug: slug}
	}

	source, err := ProfileDir(p, slug)
	if err != nil {
		return "", err
	}

	deletedRoot := filepath.Join(p.Root(), DeletedDir)
	target := filepath.Join(deletedRoot, slug+"-"+timestamp())

	if pathExists(source) {
		if err := paths.EnsureDir(deletedRoot); err != nil {
			return "", fmt.Errorf("create deleted profiles folder: %w", err)
		}
		if err := os.Rename(source, target); err != nil {
			return "", fmt.Errorf("move deleted profile: %w", err)
		}
	}

	delete(properties, key)
	if properties[keyActive] == slug {
		properties[keyActive] = ""
	}
	if err := save(p, properties); err != nil {
		return "", err
	}
	bumpChanges()

	return target, nil
}

// ForgetCreated removes a profile's registry entry without touching its folder.
//
// It is for rolling back a profile that was just created and never became
// usable. Unlike [Delete], it leaves the folder to the caller, which is what a
// failed migration needs after it has already emptied and removed it.
func ForgetCreated(p paths.AppPaths, slug string) error {
	properties, err := load(p)
	if err != nil {
		return err
	}

	key := namePrefix + slug
	if slug == defaultKey {
		return ErrDefaultProfile
	}
	if _, ok := properties[key]; !ok {
		return &UnknownError{Slug: slug}
	}

	delete(properties, key)
	if properties[keyActive] == slug {
		properties[keyActive] = ""
	}
	if err := save(p, properties); err != nil {
		return err
	}
	bumpChanges()
	return nil
}

func defaultProfile(properties map[string]string, p paths.AppPaths) Profile {
	name, ok := properties[namePrefix+defaultKey]
	if !ok {
		name = DefaultName
	}

	return Profile{Slug: "", Name: name, Dir: p.Root()}
}

func buildList(properties map[string]string, p paths.AppPaths) []Profile {
	others := make([]Profile, 0, len(properties))
	for key, name := range properties {
		slug, ok := strings.CutPrefix(key, namePrefix)
		if !ok {
			continue
		}
		if slug == "" || slug == defaultKey {
			continue
		}

		dir, err := p.ProfileDir(slug)
		if err != nil {
			continue
		}
		if !isDir(dir) {
			continue
		}

		others = append(others, Profile{Slug: slug, Name: name, Dir: dir})
	}

	sort.Slice(others, func(left, right int) bool {
		leftName := strings.ToLower(others[left].Name)
		rightName := strings.ToLower(others[right].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return others[left].Slug < others[right].Slug
	})

	profiles := make([]Profile, 0, len(others)+1)
	profiles = append(profiles, defaultProfile(properties, p))
	profiles = append(profiles, others...)
	return profiles
}

func validName(name string) (string, error) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "", ErrEmptyName
	}
	if utf8.RuneCountInString(clean) > MaxNameLength {
		return "", ErrNameTooLong
	}
	return clean, nil
}

func uniqueSlug(p paths.AppPaths, base string, taken map[string]struct{}) (string, error) {
	for suffix := uint64(1); ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = withSuffix(base, suffix)
		}

		if _, ok := taken[candidate]; ok || candidate == defaultKey {
			continue
		}

		dir, err := p.ProfileDir(candidate)
		if err != nil {
			continue
		}
		if pathExists(dir) {
			continue
		}
		return candidate, nil
	}
}

func withSuffix(base string, suffix uint64) string {
	suffixText := fmt.Sprintf("-%d", suffix)
	room := MaxNameLength - len(suffixText)
	if room < 0 {
		room = 0
	}

	stem := []rune(base)
	if len(stem) > room {
		stem = stem[:room]
	}

	return strings.TrimRight(string(stem), "-") + suffixText
}

func timestamp() string {
	return time.Now().UTC().Format("20060102-150405")
}

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
