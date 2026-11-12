package accountgroups

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func equal(left, right []string) bool {
	return slices.Equal(left, right)
}

func database(t *testing.T) *db.Database {
	t.Helper()

	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	opened, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func group(t *testing.T, d *db.Database, name string, sortOrder int64) model.AccountGroupID {
	t.Helper()

	id, err := Create(context.Background(), d, model.NewAccountGroup{Name: name, SortOrder: sortOrder})
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	return id
}

func names(t *testing.T, d *db.Database) []string {
	t.Helper()

	groups, err := List(context.Background(), d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	result := make([]string, 0, len(groups))
	for _, group := range groups {
		result = append(result, group.Name)
	}
	return result
}

func entries(t *testing.T, d *db.Database) []audit.Entry {
	t.Helper()

	recent, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return recent
}

func TestMoveSwapsWithTheNeighbourAndIsANoOpAtTheEnds(t *testing.T) {
	d := database(t)
	first := group(t, d, "A", 1)
	second := group(t, d, "B", 2)
	third := group(t, d, "C", 3)

	if moved, err := MoveGroup(context.Background(), d, third, true); err != nil || !moved {
		t.Fatalf("MoveGroup(third, up) = %v, %v", moved, err)
	}
	if got := names(t, d); !equal(got, []string{"A", "C", "B"}) {
		t.Errorf("names = %v", got)
	}

	if moved, err := MoveGroup(context.Background(), d, first, true); err != nil || moved {
		t.Fatalf("MoveGroup(first, up) = %v, %v; the first group cannot move up", moved, err)
	}
	if got := names(t, d); !equal(got, []string{"A", "C", "B"}) {
		t.Errorf("names = %v", got)
	}

	if moved, err := MoveGroup(context.Background(), d, first, false); err != nil || !moved {
		t.Fatalf("MoveGroup(first, down) = %v, %v", moved, err)
	}
	if got := names(t, d); !equal(got, []string{"C", "A", "B"}) {
		t.Errorf("names = %v", got)
	}

	if moved, err := MoveGroup(context.Background(), d, second, false); err != nil || moved {
		t.Fatalf("MoveGroup(second, down) = %v, %v; the last group cannot move down", moved, err)
	}
	if got := names(t, d); !equal(got, []string{"C", "A", "B"}) {
		t.Errorf("names = %v", got)
	}
}

func TestEveryMoveWritesOneAuditEntryEvenWhenItDoesNothing(t *testing.T) {
	d := database(t)
	first := group(t, d, "A", 1)
	before := len(entries(t, d))

	if _, err := MoveGroup(context.Background(), d, first, true); err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}
	after := entries(t, d)
	if len(after) != before+1 {
		t.Fatalf("audit entries = %d, want %d", len(after), before+1)
	}
	if after[0].Action != "REORDER_ACCOUNT_GROUP" {
		t.Errorf("action = %q", after[0].Action)
	}
	if after[0].Actor != audit.ActorUser {
		t.Errorf("actor = %q", after[0].Actor)
	}
	want := "Moved account group " + itoa(int64(first)) + " up"
	if after[0].Details != want {
		t.Errorf("details = %q, want %q", after[0].Details, want)
	}
}

func TestCreateListRenameAndDeleteRoundTrip(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	created := group(t, d, "Group", 5)
	groups, err := List(ctx, d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("List length = %d", len(groups))
	}
	if groups[0].ID != created || groups[0].Name != "Group" || groups[0].SortOrder != 5 {
		t.Errorf("List = %+v", groups[0])
	}

	createEntries := entries(t, d)
	if len(createEntries) != 1 {
		t.Fatalf("audit entries after create = %d", len(createEntries))
	}
	if createEntries[0].Action != "account_group.create" || createEntries[0].Actor != audit.ActorUser {
		t.Errorf("create audit = %+v", createEntries[0])
	}
	if createEntries[0].Details != "id="+itoa(int64(created))+" name=Group" {
		t.Errorf("create details = %q", createEntries[0].Details)
	}

	if err := Rename(ctx, d, created, "Renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got := names(t, d); !equal(got, []string{"Renamed"}) {
		t.Errorf("names after rename = %v", got)
	}
	renameEntries := entries(t, d)
	if renameEntries[0].Action != "account_group.rename" ||
		renameEntries[0].Actor != audit.ActorUser ||
		renameEntries[0].Details != "id="+itoa(int64(created))+" name=Renamed" {
		t.Errorf("rename audit = %+v", renameEntries[0])
	}

	if err := Rename(ctx, d, model.AccountGroupID(999999), "Missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("Rename of an unknown group = %v", err)
	}

	if err := Delete(ctx, d, created); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := names(t, d); len(got) != 0 {
		t.Errorf("names after delete = %v", got)
	}
	deleteEntries := entries(t, d)
	if deleteEntries[0].Action != "account_group.delete" ||
		deleteEntries[0].Actor != audit.ActorUser ||
		deleteEntries[0].Details != "id="+itoa(int64(created)) {
		t.Errorf("delete audit = %+v", deleteEntries[0])
	}

	if err := Delete(ctx, d, created); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestUnknownGroupsDoNotMoveButAreAudited(t *testing.T) {
	d := database(t)
	before := len(entries(t, d))

	moved, err := MoveGroup(context.Background(), d, model.AccountGroupID(999999), true)
	if err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}
	if moved {
		t.Error("an unknown group moved")
	}
	if got := len(entries(t, d)); got != before+1 {
		t.Errorf("audit entries = %d, want %d", got, before+1)
	}
}
