package model

import (
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// redactedField is one field of a redacted debug representation.
type redactedField struct {
	name   string
	value  any
	secret bool
}

// String returns a representation that never contains the bank user id.
func (item BankAccessItem) String() string {
	return formatRedacted("BankAccessItem", item.redactedFields(), false)
}

// GoString returns a representation that never contains the bank user id.
func (item BankAccessItem) GoString() string {
	return formatRedacted("model.BankAccessItem", item.redactedFields(), true)
}

// Format writes a representation that never contains the bank user id, for
// every fmt verb.
func (item BankAccessItem) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, formatRedacted(
		"model.BankAccessItem",
		item.redactedFields(),
		state.Flag('#') || state.Flag('+'),
	))
}

func (item BankAccessItem) redactedFields() []redactedField {
	return []redactedField{
		{name: "ID", value: item.ID},
		{name: "BankName", value: item.BankName},
		{name: "BLZ", value: item.BLZ},
		{name: "BIC", value: item.BIC},
		{name: "UserID", secret: true},
		{name: "TanMethod", value: item.TanMethod},
		{name: "ServerURL", value: item.ServerURL},
		{name: "LastSync", value: item.LastSync},
		{name: "Capabilities", value: item.Capabilities},
		{name: "SyncFrom", value: item.SyncFrom},
		{name: "IncludeInSyncAll", value: item.IncludeInSyncAll},
		{name: "Offline", value: item.Offline},
	}
}

// String returns a representation that never contains the bank user id.
func (connection BankConnection) String() string {
	return formatRedacted("BankConnection", connection.redactedFields(), false)
}

// GoString returns a representation that never contains the bank user id.
func (connection BankConnection) GoString() string {
	return formatRedacted("model.BankConnection", connection.redactedFields(), true)
}

// Format writes a representation that never contains the bank user id, for
// every fmt verb.
func (connection BankConnection) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, formatRedacted(
		"model.BankConnection",
		connection.redactedFields(),
		state.Flag('#') || state.Flag('+'),
	))
}

func (connection BankConnection) redactedFields() []redactedField {
	return []redactedField{
		{name: "ID", value: connection.ID},
		{name: "BankName", value: connection.BankName},
		{name: "BLZ", value: connection.BLZ},
		{name: "BIC", value: connection.BIC},
		{name: "UserID", secret: true},
		{name: "ServerURL", value: connection.ServerURL},
		{name: "TanMethod", value: connection.TanMethod},
		{name: "SyncFrom", value: connection.SyncFrom},
		{name: "LastSync", value: connection.LastSync},
		{name: "Capabilities", value: connection.Capabilities},
		{name: "IncludeInSyncAll", value: connection.IncludeInSyncAll},
		{name: "Offline", value: connection.Offline},
	}
}

func formatRedacted(typeName string, fields []redactedField, withNames bool) string {
	var formatted strings.Builder
	formatted.WriteString(typeName)
	formatted.WriteString("{")
	for index, field := range fields {
		if index > 0 {
			formatted.WriteString(", ")
		}
		if withNames {
			formatted.WriteString(field.name)
			formatted.WriteString(": ")
		}
		if field.secret {
			formatted.WriteString(`"***"`)
			continue
		}
		formatted.WriteString(debugValue(field.value))
	}
	formatted.WriteString("}")
	return formatted.String()
}

// debugValue formats a value the way Rust's Debug would show it, without ever
// dereferencing a bank user id.
func debugValue(value any) string {
	if value == nil {
		return "None"
	}

	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return "None"
		}
		return "Some(" + debugValue(reflected.Elem().Interface()) + ")"
	}
	if reflected.Kind() == reflect.String {
		return strconv.Quote(reflected.String())
	}

	return fmt.Sprintf("%v", value)
}
