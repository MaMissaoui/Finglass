package accountsview

import "testing"

func TestClonesShareTheCountAndEndNeverGoesBelowZero(t *testing.T) {
	busy := NewBusy()
	other := busy
	if busy != other {
		t.Fatal("copied Busy handles are not identical")
	}
	if busy.Active() {
		t.Fatal("new Busy is active")
	}
	other.Begin()
	if !busy.Active() {
		t.Fatal("copy does not share the count")
	}
	busy.End()
	busy.End()
	if other.Active() {
		t.Fatal("End went below zero or did not share the count")
	}
	busy.Run(func() {
		if !other.Active() {
			t.Fatal("Run did not mark the section busy")
		}
	})
	if busy.Active() {
		t.Fatal("Run left the section busy")
	}
}

func TestRunEndsTheSectionWhenWorkPanics(t *testing.T) {
	busy := NewBusy()

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("Run swallowed the panic")
			}
		}()
		busy.Run(func() {
			panic("boom")
		})
	}()

	if busy.Active() {
		t.Fatal("Run left the section busy after a panic")
	}
}
