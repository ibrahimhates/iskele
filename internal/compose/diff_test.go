package compose

import (
	"context"
	"slices"
	"testing"
)

// changeFor finds one service's change in a diff.
func changeFor(t *testing.T, diff Diff, service string) ServiceChange {
	t.Helper()

	for _, change := range diff.Services {
		if change.Service == service {
			return change
		}
	}
	t.Fatalf("no change reported for %q; diff = %+v", service, diff.Services)
	return ServiceChange{}
}

func compare(t *testing.T, before, after string) Diff {
	t.Helper()

	dir := t.TempDir()
	diff, err := Compare(context.Background(),
		Input{Name: "s", Compose: before, WorkingDir: dir},
		Input{Name: "s", Compose: after, WorkingDir: dir})
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	return diff
}

func TestCompareReportsNoChangeForAnIdenticalFile(t *testing.T) {
	file := "services:\n  app:\n    image: alpine:3.20\n"

	if diff := compare(t, file, file); !diff.Empty() {
		t.Errorf("diff = %+v, want nothing", diff)
	}
}

func TestCompareNamesTheFieldThatChanged(t *testing.T) {
	diff := compare(t,
		"services:\n  app:\n    image: alpine:3.20\n",
		"services:\n  app:\n    image: alpine:3.21\n")

	change := changeFor(t, diff, "app")
	if change.Kind != ChangeModified {
		t.Errorf("kind = %q, want modified", change.Kind)
	}
	if !slices.Contains(change.Fields, "image") {
		t.Errorf("fields = %v, want image", change.Fields)
	}
	if !change.Recreates {
		t.Error("a changed image replaces the container")
	}
}

func TestCompareReportsAddedAndRemovedServices(t *testing.T) {
	diff := compare(t,
		"services:\n  old:\n    image: alpine\n",
		"services:\n  new:\n    image: alpine\n")

	if changeFor(t, diff, "new").Kind != ChangeAdded {
		t.Error("the new service should be reported as added")
	}
	if changeFor(t, diff, "old").Kind != ChangeRemoved {
		t.Error("the removed service should be reported as removed")
	}
}

func TestCompareReportsResourceChanges(t *testing.T) {
	diff := compare(t,
		"services:\n  app:\n    image: alpine\nvolumes:\n  old: {}\n",
		"services:\n  app:\n    image: alpine\nvolumes:\n  new: {}\nnetworks:\n  edge: {}\n")

	if len(diff.Volumes) != 2 {
		t.Errorf("volume changes = %+v, want one added and one removed", diff.Volumes)
	}
	if len(diff.Networks) != 1 || diff.Networks[0].Kind != ChangeAdded {
		t.Errorf("network changes = %+v, want one added", diff.Networks)
	}
}

// Reordering environment entries changes the YAML but not the container.
// Reporting it would train operators to ignore the diff.
func TestCompareIgnoresReordering(t *testing.T) {
	diff := compare(t,
		"services:\n  app:\n    image: alpine\n    environment:\n      - A=1\n      - B=2\n",
		"services:\n  app:\n    image: alpine\n    environment:\n      - B=2\n      - A=1\n")

	if !diff.Empty() {
		t.Errorf("diff = %+v, want nothing: only the order changed", diff.Services)
	}
}

func TestCompareSeesPortAndVolumeChanges(t *testing.T) {
	diff := compare(t,
		"services:\n  app:\n    image: alpine\n    ports:\n      - \"80:80\"\n",
		"services:\n  app:\n    image: alpine\n    ports:\n      - \"8080:80\"\n")

	if !slices.Contains(changeFor(t, diff, "app").Fields, "ports") {
		t.Errorf("fields = %v, want ports", changeFor(t, diff, "app").Fields)
	}
}

// A stack can be edited out of a broken state, and that edit is exactly when a
// diff is most useful.
func TestCompareTreatsAnUnparseableCurrentVersionAsEmpty(t *testing.T) {
	diff := compare(t,
		"services:\n  app:\n  image: [",
		"services:\n  app:\n    image: alpine\n")

	if changeFor(t, diff, "app").Kind != ChangeAdded {
		t.Errorf("diff = %+v, want everything to read as new", diff.Services)
	}
}

func TestCompareFailsOnAnUnparseableNewVersion(t *testing.T) {
	dir := t.TempDir()
	_, err := Compare(context.Background(),
		Input{Name: "s", Compose: "services:\n  app:\n    image: alpine\n", WorkingDir: dir},
		Input{Name: "s", Compose: "services:\n  app:\n  image: [", WorkingDir: dir})
	if err == nil {
		t.Fatal("Compare() error = nil, want the new version's parse failure")
	}
}
