package profiles

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

func testPaths(t *testing.T) paths.AppPaths {
	t.Helper()

	p, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return p
}

func slugs(profiles []Profile) []string {
	result := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, profile.Slug)
	}
	return result
}

func names(profiles []Profile) []string {
	result := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, profile.Name)
	}
	return result
}

func listSlugs(t *testing.T, p paths.AppPaths) []string {
	t.Helper()

	profiles, err := List(p)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return slugs(profiles)
}

func TestDefaultProfileIsTheRootAndOthersGetUniqueSlugs(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	initial, err := List(p)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(initial) != 1 {
		t.Fatalf("initial list = %v, want only the default profile", initial)
	}
	if initial[0].Slug != "" || initial[0].Name != DefaultName || initial[0].Dir != root {
		t.Errorf("default profile = %+v, want slug \"\", name %q, dir %q", initial[0], DefaultName, root)
	}

	business, err := Create(p, "  Business GmbH ")
	if err != nil {
		t.Fatalf("Create Business: %v", err)
	}
	second, err := Create(p, "business gmbh")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	umlaut, err := Create(p, "Müller & Söhne")
	if err != nil {
		t.Fatalf("Create umlaut: %v", err)
	}

	if business.Slug != "business-gmbh" {
		t.Errorf("business slug = %q, want business-gmbh", business.Slug)
	}
	if business.Name != "Business GmbH" {
		t.Errorf("business name = %q, want Business GmbH", business.Name)
	}
	if second.Slug != "business-gmbh-2" {
		t.Errorf("second slug = %q, want business-gmbh-2", second.Slug)
	}
	if umlaut.Slug != "muller-sohne" {
		t.Errorf("umlaut slug = %q, want muller-sohne", umlaut.Slug)
	}
	if !isDir(business.Dir) {
		t.Errorf("business folder %q is not a directory", business.Dir)
	}
	if want := filepath.Join(root, "profiles", "business-gmbh"); business.Dir != want {
		t.Errorf("business folder = %q, want %q", business.Dir, want)
	}

	want := []string{"", "business-gmbh", "business-gmbh-2", "muller-sohne"}
	if got := listSlugs(t, p); !equalStrings(got, want) {
		t.Errorf("list = %v, want %v (default first, then by name)", got, want)
	}
}

func TestNamesAreValidatedAndSlugsCannotEscapeTheProfilesFolder(t *testing.T) {
	p := testPaths(t)

	if _, err := Create(p, "   "); !errors.Is(err, ErrEmptyName) {
		t.Errorf("Create(blank) error = %v, want ErrEmptyName", err)
	}
	if _, err := Create(p, strings.Repeat("x", 41)); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("Create(long) error = %v, want ErrNameTooLong", err)
	}

	escaped, err := Create(p, "../../etc/passwd")
	if err != nil {
		t.Fatalf("Create(../../etc/passwd): %v", err)
	}
	if escaped.Slug != "etc-passwd" {
		t.Errorf("slug = %q, want etc-passwd", escaped.Slug)
	}

	if _, err := ProfileDir(p, "../escape"); err == nil {
		t.Error("ProfileDir(../escape) should fail")
	}
	if _, err := ProfileDir(p, "a/b"); err == nil {
		t.Error("ProfileDir(a/b) should fail")
	}
	if _, err := ProfileDir(p, "default"); !errors.Is(err, ErrDefaultProfile) {
		t.Errorf("ProfileDir(default) error = %v, want ErrDefaultProfile", err)
	}

	var unknown *UnknownError
	if err := SetActive(p, "../escape"); !errors.As(err, &unknown) {
		t.Errorf("SetActive(../escape) error = %v, want *UnknownError", err)
	}
}

func TestTheActiveProfileIsRememberedAndFallsBackWhenItsFolderIsGone(t *testing.T) {
	p := testPaths(t)

	business, err := Create(p, "Business")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	slug := business.Slug
	if err := SetActive(p, slug); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	active, err := Active(p)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if active.Slug != slug {
		t.Errorf("active slug = %q, want %q (persisted across calls)", active.Slug, slug)
	}

	if err := Rename(p, "", "Privat"); err != nil {
		t.Fatalf("Rename default: %v", err)
	}
	if err := Rename(p, slug, "Firma"); err != nil {
		t.Fatalf("Rename business: %v", err)
	}

	profiles, err := List(p)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := names(profiles), []string{"Privat", "Firma"}; !equalStrings(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}

	active, err = Active(p)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if active.Slug != slug {
		t.Errorf("active slug = %q, want %q (rename keeps the choice)", active.Slug, slug)
	}

	if err := os.RemoveAll(business.Dir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	active, err = Active(p)
	if err != nil {
		t.Fatalf("Active after removal: %v", err)
	}
	if active.Slug != "" {
		t.Errorf("active slug = %q, want the default", active.Slug)
	}
}

func TestCreateRenameAndDeleteIncrementTheProcessWideChangeCounter(t *testing.T) {
	p := testPaths(t)
	before := Changes()

	business, err := Create(p, "Business")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := Changes(); got != before+1 {
		t.Errorf("changes = %d, want %d", got, before+1)
	}

	if err := Rename(p, business.Slug, "Firma"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got := Changes(); got != before+2 {
		t.Errorf("changes = %d, want %d (a rename through another call is visible too)", got, before+2)
	}

	if _, err := Delete(p, business.Slug, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := Changes(); got != before+3 {
		t.Errorf("changes = %d, want %d", got, before+3)
	}
}

func TestDeleteMovesTheFolderAsideAndRefusesTheDefaultAndTheOpenProfile(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	business, err := Create(p, "Business")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	slug := business.Slug
	if err := os.WriteFile(filepath.Join(business.Dir, "finglass.db"), []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := SetActive(p, slug); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	if _, err := Delete(p, slug, slug); !errors.Is(err, ErrOpenProfile) {
		t.Errorf("Delete open profile error = %v, want ErrOpenProfile", err)
	}
	if _, err := Delete(p, "default", ""); !errors.Is(err, ErrDefaultProfile) {
		t.Errorf("Delete default error = %v, want ErrDefaultProfile", err)
	}

	moved, err := Delete(p, slug, "")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if pathExists(business.Dir) {
		t.Errorf("business folder %q still exists", business.Dir)
	}
	// #nosec G304 -- test file under t.TempDir().
	content, err := os.ReadFile(filepath.Join(moved, "finglass.db"))
	if err != nil {
		t.Fatalf("ReadFile moved database: %v", err)
	}
	if string(content) != "data" {
		t.Errorf("moved content = %q, want data", content)
	}
	if got, want := filepath.Dir(moved), filepath.Join(root, DeletedDir); got != want {
		t.Errorf("moved parent = %q, want %q", got, want)
	}

	if got, want := listSlugs(t, p), []string{""}; !equalStrings(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}

	active, err := Active(p)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if active.Slug != "" {
		t.Errorf("active slug = %q, want the default (a deleted next-start choice falls back)", active.Slug)
	}
}

func TestProfileSwitchDataDirIsTheRootForTheDefaultProfile(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	business, err := Create(p, "Business")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := DataDir(p, ""); err != nil || got != root {
		t.Errorf("DataDir(default) = %q, %v, want %q", got, err, root)
	}
	if got, err := DataDir(p, business.Slug); err != nil || got != business.Dir {
		t.Errorf("DataDir(business) = %q, %v, want %q", got, err, business.Dir)
	}
}

func TestRegistryRoundTripsNamesWithSeparatorsAndUmlauts(t *testing.T) {
	p := testPaths(t)

	profile, err := Create(p, "Müller = Söhne: #1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	properties, err := load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	key := namePrefix + profile.Slug
	if got := properties[key]; got != "Müller = Söhne: #1" {
		t.Errorf("%s = %q, want %q", key, got, "Müller = Söhne: #1")
	}
}

func TestParsePropertiesAcceptsJavaStyleEscapes(t *testing.T) {
	parsed := parseProperties(
		"# comment\nactive=business\nname.default=Privat\nname.business=M\\u00fcller\\: S\\u00f6hne\n",
	)

	if got := parsed["active"]; got != "business" {
		t.Errorf("active = %q, want business", got)
	}
	if got, want := parsed["name.business"], "Müller: Söhne"; got != want {
		t.Errorf("name.business = %q, want %q", got, want)
	}
}

func TestDefaultNameBecomesDefault2(t *testing.T) {
	p := testPaths(t)

	profile, err := Create(p, "Default")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if profile.Slug != "default-2" {
		t.Errorf("slug = %q, want default-2", profile.Slug)
	}
}

func TestSlugExpansionIsTruncated(t *testing.T) {
	p := testPaths(t)

	profile, err := Create(p, strings.Repeat("ß", 40))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	want := strings.Repeat("s", 40)
	if profile.Slug != want {
		t.Errorf("slug = %q, want %q", profile.Slug, want)
	}
}

func TestSlugSkipsAnExistingFolder(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, ProfilesDir, "business"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	profile, err := Create(p, "Business")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if profile.Slug != "business-2" {
		t.Errorf("slug = %q, want business-2", profile.Slug)
	}
}

func TestSuffixKeepsTheSlugShort(t *testing.T) {
	p := testPaths(t)

	first, err := Create(p, strings.Repeat("a", 40))
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	if first.Slug != strings.Repeat("a", 40) {
		t.Fatalf("first slug = %q, want 40 a", first.Slug)
	}

	second, err := Create(p, strings.Repeat("A", 40))
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	want := strings.Repeat("a", 38) + "-2"
	if second.Slug != want {
		t.Errorf("slug = %q, want %q", second.Slug, want)
	}
	if len(second.Slug) != 40 {
		t.Errorf("slug length = %d, want 40", len(second.Slug))
	}
}

func TestDeleteNeverErases(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	business, err := Create(p, "Business")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	const content = "important data"
	if err := os.WriteFile(filepath.Join(business.Dir, "finglass.db"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	moved, err := Delete(p, business.Slug, "")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// #nosec G304 -- test file under t.TempDir().
	movedContent, err := os.ReadFile(filepath.Join(moved, "finglass.db"))
	if err != nil {
		t.Fatalf("ReadFile moved: %v", err)
	}
	if string(movedContent) != content {
		t.Errorf("moved content = %q, want %q", movedContent, content)
	}

	if got, want := filepath.Dir(moved), filepath.Join(root, DeletedDir); got != want {
		t.Errorf("moved parent = %q, want %q", got, want)
	}

	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(business.Slug) + `-\d{8}-\d{6}$`)
	if base := filepath.Base(moved); !pattern.MatchString(base) {
		t.Errorf("moved name = %q, want %s", base, pattern)
	}
}

func TestForgetCreatedKeepsTheFolder(t *testing.T) {
	p := testPaths(t)

	business, err := Create(p, "Business")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := SetActive(p, business.Slug); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	const content = "kept"
	if err := os.WriteFile(filepath.Join(business.Dir, "finglass.db"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := ForgetCreated(p, business.Slug); err != nil {
		t.Fatalf("ForgetCreated: %v", err)
	}

	if !isDir(business.Dir) {
		t.Errorf("folder %q should still exist", business.Dir)
	}
	data, err := os.ReadFile(filepath.Join(business.Dir, "finglass.db"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", data, content)
	}

	if got, want := listSlugs(t, p), []string{""}; !equalStrings(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}

	active, err := Active(p)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if active.Slug != "" {
		t.Errorf("active slug = %q, want the default", active.Slug)
	}

	if err := ForgetCreated(p, "default"); !errors.Is(err, ErrDefaultProfile) {
		t.Errorf("ForgetCreated(default) error = %v, want ErrDefaultProfile", err)
	}
	var unknown *UnknownError
	if err := ForgetCreated(p, "missing"); !errors.As(err, &unknown) {
		t.Errorf("ForgetCreated(missing) error = %v, want *UnknownError", err)
	}
}

func TestNoTempFilesRemainAfterEveryOperation(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	profile, err := Create(p, "Business")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	assertNoTempFiles(t, root)

	if err := Rename(p, profile.Slug, "Firma"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	assertNoTempFiles(t, root)

	if err := SetActive(p, profile.Slug); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	assertNoTempFiles(t, root)

	if _, err := Delete(p, profile.Slug, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertNoTempFiles(t, root)

	created, err := Create(p, "Second")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if err := ForgetCreated(p, created.Slug); err != nil {
		t.Fatalf("ForgetCreated: %v", err)
	}
	assertNoTempFiles(t, root)
}

func TestSlugFromName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Müller & Söhne", "muller-sohne"},
		{"../../etc/passwd", "etc-passwd"},
		{"  Business GmbH ", "business-gmbh"},
		{"!!!", "profile"},
		{"Ärger über Öl", "arger-uber-ol"},
		{"Straße", "strasse"},
	}

	for _, tc := range cases {
		if got := baseSlug(strings.TrimSpace(tc.name)); got != tc.want {
			t.Errorf("baseSlug(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestUniqueSlugSkipsDefault(t *testing.T) {
	p := testPaths(t)

	slug, err := uniqueSlug(p, "default", map[string]struct{}{})
	if err != nil {
		t.Fatalf("uniqueSlug: %v", err)
	}
	if slug != "default-2" {
		t.Errorf("slug = %q, want default-2", slug)
	}
}

func TestWithSuffix(t *testing.T) {
	if got, want := withSuffix("business", 2), "business-2"; got != want {
		t.Errorf("withSuffix = %q, want %q", got, want)
	}
	if got, want := withSuffix(strings.Repeat("a", 40), 12), strings.Repeat("a", 37)+"-12"; got != want {
		t.Errorf("withSuffix long = %q, want %q", got, want)
	}
	if got, want := withSuffix("business-", 2), "business-2"; got != want {
		t.Errorf("withSuffix trailing hyphen = %q, want %q", got, want)
	}
}

func assertNoTempFiles(t *testing.T, root string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("temporary registry file %q was left behind", entry.Name())
		}
	}
}

func equalStrings(left, right []string) bool {
	return slices.Equal(left, right)
}
