package findeskproc

import (
	"bytes"
	"encoding/csv"
	"strings"
)

// parsePSOutput reads `ps -A -o comm=` output, one command per line. macOS
// prints the full executable path, Linux the process's command name (possibly
// truncated, but "FinDesk" is short enough to survive).
func parsePSOutput(output []byte) State {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return Unknown
	}

	for _, line := range strings.Split(text, "\n") {
		if isFinDeskBaseName(strings.TrimSpace(line), false) {
			return Running
		}
	}
	return NotRunning
}

// parseTasklistOutput reads `tasklist /FO CSV /NH` output and checks the image
// name in the first CSV field.
func parseTasklistOutput(output []byte) State {
	if len(bytes.TrimSpace(output)) == 0 {
		return Unknown
	}

	reader := csv.NewReader(bytes.NewReader(output))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) == 0 {
		return Unknown
	}

	for _, record := range records {
		if len(record) == 0 {
			continue
		}
		if isFinDeskBaseName(strings.TrimSpace(record[0]), true) {
			return Running
		}
	}
	return NotRunning
}

// isFinDeskBaseName reports whether value's executable base name is FinDesk.
// withExe also accepts the Windows launcher's FinDesk.exe. The comparison is
// case-insensitive; FinDeskHelper, findesk-backup.sh and MyFinDesk do not
// match.
func isFinDeskBaseName(value string, withExe bool) bool {
	if value == "" {
		return false
	}
	base := value
	if index := strings.LastIndexAny(base, `/\`); index >= 0 {
		base = base[index+1:]
	}
	if strings.EqualFold(base, "FinDesk") {
		return true
	}
	return withExe && strings.EqualFold(base, "FinDesk.exe")
}
