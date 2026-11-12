package migration

import (
	"crypto/sha256"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const (
	fixturePassphrase = "fixture passphrase 1"
	targetPassphrase  = "target passphrase 1"
)

func fixtureCopy(t *testing.T) string {
	t.Helper()
	fixture := filepath.Join("..", "..", "core", "findeskimport", "testdata", "findesk")
	root := filepath.Join(t.TempDir(), "findesk")
	copyTree(t, fixture, root)
	return root
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o700); err != nil {
		t.Fatalf("create %s: %v", to, err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatalf("read %s: %v", from, err)
	}
	for _, entry := range entries {
		source := filepath.Join(from, entry.Name())
		target := filepath.Join(to, entry.Name())
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("stat %s: %v", source, err)
		}
		if info.IsDir() {
			copyTree(t, source, target)
			continue
		}
		data, err := os.ReadFile(source) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil { // #nosec G703 -- target is inside the test's temporary copy.
			t.Fatalf("write %s: %v", target, err)
		}
	}
}

func testPaths(t *testing.T) paths.AppPaths {
	t.Helper()
	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	return resolved
}

func findeskPassphrase(t *testing.T) *secret.Passphrase {
	t.Helper()
	created, err := secret.NewPassphrase([]byte(fixturePassphrase), secret.Unlock)
	if err != nil {
		t.Fatalf("new FinDesk passphrase: %v", err)
	}
	t.Cleanup(created.Wipe)
	return created
}

func targetNewPassphrase(t *testing.T) *secret.Passphrase {
	t.Helper()
	created, err := secret.NewPassphrase([]byte(targetPassphrase), secret.New)
	if err != nil {
		t.Fatalf("new target passphrase: %v", err)
	}
	t.Cleanup(created.Wipe)
	return created
}

func offset() *time.Location {
	return time.FixedZone("", 2*60*60)
}

func hashes(t *testing.T, root string) map[string][sha256.Size]byte {
	t.Helper()
	result := make(map[string][sha256.Size]byte)
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			if info.IsDir() {
				walk(path)
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			data, err := os.ReadFile(path) // #nosec G304 -- path is inside the test's FinDesk copy.
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatalf("relative %s: %v", path, err)
			}
			result[relative] = sha256.Sum256(append([]byte(relative), data...))
		}
	}
	walk(root)
	return result
}

func writeNamedProfile(t *testing.T, root, slug string) {
	t.Helper()
	dir := filepath.Join(root, "profiles", slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create named profile: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "findesk.db")) // #nosec G304 -- inside the test's FinDesk copy.
	if err != nil {
		t.Fatalf("read database: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "findesk.db"), data, 0o600); err != nil { // #nosec G703 -- target is inside the test's temporary copy.
		t.Fatalf("write database: %v", err)
	}
	copyTree(t, filepath.Join(root, "documents"), filepath.Join(dir, "documents"))
}

func assertFixtureCounts(t *testing.T, report findeskimport.FinDeskImportReport) {
	t.Helper()
	if report.AccountGroups.Imported != 1 {
		t.Errorf("account groups = %d", report.AccountGroups.Imported)
	}
	if report.BankConnections.Imported != 2 {
		t.Errorf("bank connections = %d", report.BankConnections.Imported)
	}
	if report.Accounts.Imported != 3 {
		t.Errorf("accounts = %d", report.Accounts.Imported)
	}
	if report.Categories.Imported != 3 {
		t.Errorf("categories = %d", report.Categories.Imported)
	}
	if report.Rules.Imported != 2 {
		t.Errorf("rules = %d", report.Rules.Imported)
	}
	if report.Transactions.Imported != 8 {
		t.Errorf("transactions = %d", report.Transactions.Imported)
	}
	if report.Documents.Copied != 1 {
		t.Errorf("documents copied = %d", report.Documents.Copied)
	}
	if report.Documents.Missing != 2 {
		t.Errorf("documents missing = %d", report.Documents.Missing)
	}
	if report.Settings.Imported != 8 {
		t.Errorf("settings = %d", report.Settings.Imported)
	}
	if report.PaymentOrdersLeftBehind != 1 {
		t.Errorf("payment orders = %d", report.PaymentOrdersLeftBehind)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestARootWithOnlyADatabaseIsTheDefaultProfile(t *testing.T) {
	root := fixtureCopy(t)

	install := Discover(root)
	if install == nil {
		t.Fatal("no install found")
	}

	if install.Root != root {
		t.Errorf("root = %q, want %q", install.Root, root)
	}
	if len(install.Profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(install.Profiles))
	}
	want := FinDeskProfile{Slug: nil, Name: "Personal", DataDir: root}
	if !reflect.DeepEqual(install.Profiles[0], want) {
		t.Errorf("profile = %+v, want %+v", install.Profiles[0], want)
	}
	if install.ActiveSlug != nil {
		t.Errorf("active = %v, want nil", *install.ActiveSlug)
	}
}

func TestAPropertiesFileNamesTheProfilesAndTheirActiveOne(t *testing.T) {
	root := fixtureCopy(t)
	writeNamedProfile(t, root, "work")
	if err := os.WriteFile(
		filepath.Join(root, "profiles.properties"),
		[]byte("name.default=Personal\nname.work=M\\u00fcller\nactive=work\n"),
		0o600,
	); err != nil {
		t.Fatalf("write properties: %v", err)
	}

	install := Discover(root)
	if install == nil {
		t.Fatal("no install found")
	}

	if len(install.Profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(install.Profiles))
	}
	if install.Profiles[0].Name != "Personal" {
		t.Errorf("default name = %q", install.Profiles[0].Name)
	}
	if install.Profiles[1].Slug == nil || *install.Profiles[1].Slug != "work" {
		t.Errorf("named slug = %v", install.Profiles[1].Slug)
	}
	if install.Profiles[1].Name != "Müller" {
		t.Errorf("named name = %q", install.Profiles[1].Name)
	}
	if install.ActiveSlug == nil || *install.ActiveSlug != "work" {
		t.Errorf("active = %v", install.ActiveSlug)
	}
}

func TestANamedProfileWithoutADatabaseIsLeftOut(t *testing.T) {
	root := fixtureCopy(t)
	if err := os.MkdirAll(filepath.Join(root, "profiles", "empty"), 0o700); err != nil {
		t.Fatalf("create empty profile: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "profiles.properties"),
		[]byte("name.default=Personal\nname.empty=Empty\n"),
		0o600,
	); err != nil {
		t.Fatalf("write properties: %v", err)
	}

	install := Discover(root)
	if install == nil {
		t.Fatal("no install found")
	}
	if len(install.Profiles) != 1 || install.Profiles[0].Slug != nil {
		t.Errorf("profiles = %+v", install.Profiles)
	}
}

func TestASymlinkedDatabaseIsLeftOut(t *testing.T) {
	root := fixtureCopy(t)
	linked := filepath.Join(t.TempDir(), "linked-findesk")
	if err := os.MkdirAll(linked, 0o700); err != nil {
		t.Fatalf("create linked: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "findesk.db"), filepath.Join(linked, "findesk.db")); err != nil {
		t.Fatalf("symlink database: %v", err)
	}

	if install := Discover(linked); install != nil {
		t.Errorf("install = %+v, want nil", install)
	}
}

func TestAnEmptyFolderHasNoInstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "findesk")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create root: %v", err)
	}

	if install := Discover(root); install != nil {
		t.Errorf("install = %+v, want nil", install)
	}
}

func TestDiscoveryLeavesTheFinDeskFolderByteIdentical(t *testing.T) {
	root := fixtureCopy(t)
	writeNamedProfile(t, root, "work")
	if err := os.WriteFile(
		filepath.Join(root, "profiles.properties"),
		[]byte("name.default=Personal\nname.work=Work\nactive=work\n"),
		0o600,
	); err != nil {
		t.Fatalf("write properties: %v", err)
	}

	before := hashes(t, root)
	install := Discover(root)
	after := hashes(t, root)

	if install == nil || len(install.Profiles) != 2 {
		t.Fatalf("install = %+v", install)
	}
	if !maps.Equal(before, after) {
		t.Error("the FinDesk folder changed")
	}
}

func TestTheOfferNeedsAFreshFinglassRootAndAnInstall(t *testing.T) {
	root := fixtureCopy(t)
	install := Discover(root)

	target := testPaths(t)
	if !ShouldOffer(target, install) {
		t.Error("a fresh root should offer the migration")
	}
	if ShouldOffer(target, nil) {
		t.Error("no install should not offer the migration")
	}
}

func TestTheMarkerStopsTheOffer(t *testing.T) {
	root := fixtureCopy(t)
	install := Discover(root)
	target := testPaths(t)

	if err := WriteMarker(target, true, time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC)); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if ShouldOffer(target, install) {
		t.Error("the marker should stop the offer")
	}
}

func TestAnExistingDatabaseStopsTheOffer(t *testing.T) {
	root := fixtureCopy(t)
	install := Discover(root)
	target := testPaths(t)
	file, err := target.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	if err := os.WriteFile(file, []byte("db"), 0o600); err != nil {
		t.Fatalf("write database: %v", err)
	}

	if ShouldOffer(target, install) {
		t.Error("an existing database should stop the offer")
	}
}

func TestASecondFinglassProfileStopsTheOffer(t *testing.T) {
	root := fixtureCopy(t)
	install := Discover(root)
	target := testPaths(t)
	if _, err := profiles.Create(target, "Business"); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	if ShouldOffer(target, install) {
		t.Error("a second profile should stop the offer")
	}
}

func TestTheMarkerRecordsTheChoiceAsRFC3339(t *testing.T) {
	target := testPaths(t)
	now := time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC)

	if err := WriteMarker(target, true, now); err != nil {
		t.Fatalf("write migrate marker: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target.Root(), MarkerFile))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if string(data) != "migrate 2026-10-15T12:34:56Z\n" {
		t.Errorf("marker = %q", data)
	}

	if err := WriteMarker(target, false, now); err != nil {
		t.Fatalf("write skip marker: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(target.Root(), MarkerFile))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if string(data) != "skip 2026-10-15T12:34:56Z\n" {
		t.Errorf("marker = %q", data)
	}
}

func TestTheMarkerWritesRustsRFC3339Fractions(t *testing.T) {
	target := testPaths(t)
	base := time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC)

	cases := []struct {
		name string
		now  time.Time
		want string
	}{
		{"120 milliseconds", base.Add(120 * time.Millisecond), "migrate 2026-10-15T12:34:56.12Z\n"},
		{"one nanosecond", base.Add(time.Nanosecond), "migrate 2026-10-15T12:34:56.000000001Z\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := WriteMarker(target, true, test.now); err != nil {
				t.Fatalf("write marker: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(target.Root(), MarkerFile))
			if err != nil {
				t.Fatalf("read marker: %v", err)
			}
			if string(data) != test.want {
				t.Errorf("marker = %q, want %q", data, test.want)
			}
		})
	}
}

func TestTheDefaultProfileIsMigratedIntoTheRoot(t *testing.T) {
	root := fixtureCopy(t)
	if err := os.WriteFile(filepath.Join(root, "profiles.properties"), []byte("name.default=Privat\n"), 0o600); err != nil {
		t.Fatalf("write properties: %v", err)
	}
	install := Discover(root)
	profile := install.Profiles[0]
	before := hashes(t, root)

	target := testPaths(t)
	migrated, err := MigrateProfile(target, &profile, findeskPassphrase(t), targetNewPassphrase(t), offset())
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if migrated.FinglassSlug != nil {
		t.Errorf("slug = %v, want nil", *migrated.FinglassSlug)
	}
	assertFixtureCounts(t, migrated.Report)

	file, err := target.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	opened, err := db.Open(file, targetNewPassphrase(t))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}
	listed, err := profiles.List(target)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(listed) == 0 || listed[0].Name != "Privat" {
		t.Errorf("profiles = %+v, want FinDesk's default name", listed)
	}

	documents, err := os.ReadDir(filepath.Join(target.Root(), "documents"))
	if err != nil {
		t.Fatalf("read documents: %v", err)
	}
	if len(documents) != 1 {
		t.Errorf("documents = %d, want 1", len(documents))
	}
	if _, err := os.Stat(filepath.Join(target.Root(), StagingDir)); !os.IsNotExist(err) {
		t.Errorf("staging still exists: %v", err)
	}
	if !maps.Equal(hashes(t, root), before) {
		t.Error("FinDesk's folder was only read")
	}
}

func TestANamedProfileGetsItsOwnFinglassSlug(t *testing.T) {
	root := fixtureCopy(t)
	writeNamedProfile(t, root, "work")
	if err := os.WriteFile(
		filepath.Join(root, "profiles.properties"),
		[]byte("name.default=Personal\nname.work=M\\u00fcller\nactive=work\n"),
		0o600,
	); err != nil {
		t.Fatalf("write properties: %v", err)
	}
	install := Discover(root)
	var profile *FinDeskProfile
	for index := range install.Profiles {
		if install.Profiles[index].Slug != nil && *install.Profiles[index].Slug == "work" {
			profile = &install.Profiles[index]
			break
		}
	}
	if profile == nil {
		t.Fatal("work profile not found")
	}
	before := hashes(t, root)

	target := testPaths(t)
	migrated, err := MigrateProfile(target, profile, findeskPassphrase(t), targetNewPassphrase(t), offset())
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if migrated.FinglassSlug == nil || *migrated.FinglassSlug != "muller" {
		t.Errorf("slug = %v, want muller", migrated.FinglassSlug)
	}
	assertFixtureCounts(t, migrated.Report)

	slug := *migrated.FinglassSlug
	file, err := target.DatabaseFile(slug)
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	opened, err := db.Open(file, targetNewPassphrase(t))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}
	listed, err := profiles.List(target)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("profiles = %d, want 2", len(listed))
	}
	if listed[1].Name != "Müller" || listed[1].Slug != slug {
		t.Errorf("listed = %+v", listed[1])
	}
	profileDir, err := target.ProfileDir(slug)
	if err != nil {
		t.Fatalf("profile dir: %v", err)
	}
	documents, err := os.ReadDir(filepath.Join(profileDir, "documents"))
	if err != nil {
		t.Fatalf("read documents: %v", err)
	}
	if len(documents) != 1 {
		t.Errorf("documents = %d, want 1", len(documents))
	}
	if _, err := os.Stat(filepath.Join(target.Root(), StagingDir)); !os.IsNotExist(err) {
		t.Errorf("staging still exists: %v", err)
	}
	if !maps.Equal(hashes(t, root), before) {
		t.Error("FinDesk's folder was only read")
	}
}

func TestAWrongPassphraseCreatesNothing(t *testing.T) {
	root := fixtureCopy(t)
	install := Discover(root)
	profile := install.Profiles[0]

	target := testPaths(t)
	wrong, err := secret.NewPassphrase([]byte("not the fixture passphrase"), secret.Unlock)
	if err != nil {
		t.Fatalf("new wrong passphrase: %v", err)
	}
	defer wrong.Wipe()

	_, err = MigrateProfile(target, &profile, wrong, targetNewPassphrase(t), offset())
	if !errors.Is(err, findeskimport.ErrWrongPassphrase) {
		t.Fatalf("error = %v, want a wrong passphrase", err)
	}
	file, err := target.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("database exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target.Root(), StagingDir)); !os.IsNotExist(err) {
		t.Errorf("staging still exists: %v", err)
	}
	listed, err := profiles.List(target)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("profiles = %d, want 1", len(listed))
	}
}

func TestANonEmptyDefaultTargetMovesNothing(t *testing.T) {
	root := fixtureCopy(t)
	install := Discover(root)
	profile := install.Profiles[0]

	target := testPaths(t)
	documents := filepath.Join(target.Root(), "documents")
	if err := os.MkdirAll(documents, 0o700); err != nil {
		t.Fatalf("create documents: %v", err)
	}
	existing := filepath.Join(documents, "existing.pdf")
	if err := os.WriteFile(existing, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write existing document: %v", err)
	}

	_, err := MigrateProfile(target, &profile, findeskPassphrase(t), targetNewPassphrase(t), offset())
	var migrationError *MigrationError
	if !errors.As(err, &migrationError) || migrationError.Kind != MigrationTargetNotEmpty {
		t.Fatalf("error = %v, want target not empty", err)
	}
	file, err := target.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("database exists: %v", err)
	}
	data, err := os.ReadFile(existing) // #nosec G304 -- path is inside the test's temporary target.
	if err != nil || string(data) != "keep me" {
		t.Errorf("existing document = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(target.Root(), StagingDir)); !os.IsNotExist(err) {
		t.Errorf("staging still exists: %v", err)
	}
}

func TestActiveAfterPrefersTheMigratedActiveProfile(t *testing.T) {
	install := &FinDeskInstall{Root: "/findesk", Profiles: nil, ActiveSlug: stringPointer("work")}

	got := ActiveAfter(install, []SlugPair{{FinDesk: stringPointer("work"), Finglass: stringPointer("muller")}})
	if got == nil || *got != "muller" {
		t.Errorf("active = %v, want muller", got)
	}
	if got := ActiveAfter(install, nil); got != nil {
		t.Errorf("active = %v, want the default fallback", *got)
	}
}

func TestActiveAfterFallsBackToTheDefaultProfile(t *testing.T) {
	install := &FinDeskInstall{Root: "/findesk", Profiles: nil, ActiveSlug: nil}

	if got := ActiveAfter(install, []SlugPair{{FinDesk: nil, Finglass: nil}}); got != nil {
		t.Errorf("active = %v, want nil", *got)
	}
	if got := ActiveAfter(install, []SlugPair{{
		FinDesk:  stringPointer("work"),
		Finglass: stringPointer("muller"),
	}}); got != nil {
		t.Errorf("another profile being migrated does not make it active: %v", *got)
	}
}

func TestAStagingFolderWithoutADatabaseMovesNothing(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(filepath.Join(staging, "documents"), 0o700); err != nil {
		t.Fatalf("create staging: %v", err)
	}
	receipt := filepath.Join(staging, "documents", "receipt.pdf")
	if err := os.WriteFile(receipt, []byte("pdf"), 0o600); err != nil {
		t.Fatalf("write receipt: %v", err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("create target: %v", err)
	}

	if err := moveIntoPlace(staging, target); err == nil {
		t.Fatal("move succeeded without a database")
	}
	if info, err := os.Stat(receipt); err != nil || !info.Mode().IsRegular() {
		t.Errorf("receipt moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "documents")); !os.IsNotExist(err) {
		t.Errorf("target documents exists: %v", err)
	}
}

func TestAFailedNamedMoveRollsBackTheCreatedProfile(t *testing.T) {
	root := fixtureCopy(t)
	writeNamedProfile(t, root, "work")
	if err := os.WriteFile(
		filepath.Join(root, "profiles.properties"),
		[]byte("name.default=Personal\nname.work=Work\nactive=work\n"),
		0o600,
	); err != nil {
		t.Fatalf("write properties: %v", err)
	}
	install := Discover(root)
	var profile *FinDeskProfile
	for index := range install.Profiles {
		if install.Profiles[index].Slug != nil && *install.Profiles[index].Slug == "work" {
			profile = &install.Profiles[index]
			break
		}
	}
	if profile == nil {
		t.Fatal("work profile not found")
	}
	before := hashes(t, root)

	target := testPaths(t)
	failNextMoveBeforeDatabase()
	_, err := MigrateProfile(target, profile, findeskPassphrase(t), targetNewPassphrase(t), offset())
	var migrationError *MigrationError
	if !errors.As(err, &migrationError) || migrationError.Kind != MigrationIO {
		t.Fatalf("error = %v, want an I/O error", err)
	}

	listed, err := profiles.List(target)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	for _, current := range listed {
		if current.Name == "Work" {
			t.Errorf("the failed profile is still listed: %+v", current)
		}
	}
	if _, err := os.Stat(filepath.Join(target.Root(), "profiles", "work")); !os.IsNotExist(err) {
		t.Errorf("the half-filled profile folder is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target.Root(), StagingDir)); !os.IsNotExist(err) {
		t.Errorf("staging is removed: %v", err)
	}
	if !maps.Equal(hashes(t, root), before) {
		t.Error("FinDesk's folder was only read")
	}
}
