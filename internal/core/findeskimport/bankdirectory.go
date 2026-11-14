// Reading FinDesk's own bank list.
//
// FinDesk looks a bank's FinTS server URL up in the blz.properties file inside
// the hbci4j-core jar bundled with the installed FinDesk.app. The import reads
// that file read-only from the user's own installation; nothing is copied into
// Finglass and no list content is ever logged or returned in an error.
package findeskimport

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxBankDirectoryJarBytes   = 64 << 20
	maxBankDirectoryEntries    = 20_000
	maxBankDirectoryDataBytes  = 4 << 20
	maxBankDirectoryLineBytes  = 4 << 10
	maxFinTServerURLLength     = 512
	bankDirectoryEntryName     = "blz.properties"
	bankDirectoryPropertiesDir = "Contents/app"
	bankDirectoryJarPattern    = "hbci4j-core-*.jar"
)

var errBankDirectoryTooLarge = errors.New("bank directory: the bank list is too large")

// FinDeskBankDirectory builds a server-URL lookup from the bank list bundled
// with the first existing FinDesk.app in appPaths.
//
// A missing app, a folder without exactly one hbci4j-core jar, or a jar without
// the list all yield no lookup. The returned closure holds an in-memory map and
// no file handle.
func FinDeskBankDirectory(appPaths []string) (ServerURLLookup, error) {
	for _, appPath := range appPaths {
		info, err := os.Stat(appPath)
		if err != nil || !info.IsDir() {
			continue
		}

		matches, err := filepath.Glob(filepath.Join(appPath, bankDirectoryPropertiesDir, bankDirectoryJarPattern))
		if err != nil {
			return nil, fmt.Errorf("bank directory: %w", err)
		}
		if len(matches) != 1 {
			return nil, nil
		}

		entries, err := readBankDirectoryJar(matches[0])
		if err != nil {
			return nil, err
		}
		if entries == nil {
			return nil, nil
		}
		return func(blz string) (string, bool) {
			value, ok := entries[blz]
			return value, ok
		}, nil
	}
	return nil, nil
}

// readBankDirectoryJar finds and parses blz.properties. A jar without the entry
// returns nil, nil.
func readBankDirectoryJar(path string) (map[string]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("bank directory: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("bank directory: the hbci4j jar is not a regular file")
	}
	if info.Size() > maxBankDirectoryJarBytes {
		return nil, fmt.Errorf("bank directory: the hbci4j jar is too large")
	}

	file, err := os.Open(path) // #nosec G304 -- path is the user's own FinDesk installation.
	if err != nil {
		return nil, fmt.Errorf("bank directory: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		return nil, fmt.Errorf("bank directory: %w", err)
	}
	if len(reader.File) > maxBankDirectoryEntries {
		return nil, fmt.Errorf("bank directory: the hbci4j jar has too many entries")
	}

	var properties *zip.File
	for _, entry := range reader.File {
		if entry.Name == bankDirectoryEntryName {
			properties = entry
			break
		}
	}
	if properties == nil {
		return nil, nil
	}

	content, err := readZipEntry(properties)
	if err != nil {
		return nil, err
	}
	return parseBankDirectory(content), nil
}

func readZipEntry(entry *zip.File) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("bank directory: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	content, err := io.ReadAll(io.LimitReader(reader, maxBankDirectoryDataBytes+1))
	if err != nil {
		return nil, fmt.Errorf("bank directory: %w", err)
	}
	if len(content) > maxBankDirectoryDataBytes {
		return nil, errBankDirectoryTooLarge
	}
	return content, nil
}

// parseBankDirectory reads FinDesk's line format: UTF-8, optional CRLF, full
// lines starting with # are comments, and fields are separated by '|'. The
// server URL is field 5 (0-based), so a line needs at least six fields.
func parseBankDirectory(content []byte) map[string]string {
	entries := make(map[string]string)
	start := 0
	for start <= len(content) {
		end := bytes.IndexByte(content[start:], '\n')
		var line []byte
		if end < 0 {
			line = content[start:]
			start = len(content) + 1
		} else {
			line = content[start : start+end]
			start += end + 1
		}

		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) > maxBankDirectoryLineBytes {
			continue
		}
		if len(line) == 0 || line[0] == '#' {
			continue
		}

		separator := bytes.IndexByte(line, '=')
		if separator <= 0 {
			continue
		}
		blz := strings.TrimSpace(string(line[:separator]))
		if !isEightDigitBLZ(blz) {
			continue
		}

		fields := strings.Split(string(line[separator+1:]), "|")
		if len(fields) < 3 || len(fields) <= 5 {
			continue
		}
		serverURL := strings.TrimSpace(fields[5])
		if !validFinTServerURL(serverURL) {
			continue
		}
		entries[blz] = serverURL
	}
	return entries
}

func isEightDigitBLZ(value string) bool {
	if len(value) != 8 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func validFinTServerURL(value string) bool {
	if value == "" || len(value) > maxFinTServerURLLength {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || strings.ContainsRune(value, '#') {
		return false
	}
	return true
}
