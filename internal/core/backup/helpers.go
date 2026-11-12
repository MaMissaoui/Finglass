// Filesystem, ZIP and manifest helpers for backups.
package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/db"
)

// safeTarget resolves a ZIP entry inside root, rejecting path traversal ("zip
// slip") and unexpected locations.
func safeTarget(root, name string) (string, error) {
	normalized := strings.ReplaceAll(name, "\\", "/")

	if strings.HasPrefix(normalized, "/") || hasParentComponent(normalized) {
		return "", &UnsafePathError{Name: name}
	}

	allowed := normalized == DBName || isContentName(normalized)
	if !allowed {
		return "", &UnsafePathError{Name: name}
	}

	target := filepath.Join(root, normalized)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relative) {
		return "", &UnsafePathError{Name: name}
	}

	return target, nil
}

// hasParentComponent reports whether path has a ".." component.
func hasParentComponent(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// isContentName reports whether normalized names one file directly inside a
// content folder.
func isContentName(normalized string) bool {
	for _, dirName := range contentDirs {
		rest, ok := strings.CutPrefix(normalized, dirName)
		if !ok {
			continue
		}
		rest, ok = strings.CutPrefix(rest, "/")
		if !ok {
			continue
		}
		if rest != "" && !strings.Contains(rest, "/") && rest != "." && rest != ".." {
			return true
		}
	}
	return false
}

// copyAtMost copies up to limit bytes and returns more than limit when the
// input had more, so the caller can reject it.
// readSome reads like Rust's Read::read: zero bytes means the end of the input.
// A Go reader may return (0, nil) without being at the end, so that is retried.
func readSome(input io.Reader, buffer []byte) (int, error) {
	for {
		read, err := input.Read(buffer)
		if read > 0 || err != nil {
			return read, err
		}
	}
}

func copyAtMost(input io.Reader, target string, limit uint64) (uint64, error) {
	output, err := createPrivateFile(target)
	if err != nil {
		return 0, err
	}
	defer func() { _ = output.Close() }()

	buffer := make([]byte, bufferSize)
	var total uint64

	read, err := readSome(input, buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return total, err
	}
	for read > 0 && total+uint64(read) <= limit {
		if _, err := output.Write(buffer[:read]); err != nil {
			return total, err
		}
		total += uint64(read)
		read, err = readSome(input, buffer)
		if err != nil && !errors.Is(err, io.EOF) {
			return total, err
		}
	}

	if read > 0 {
		return total + uint64(read), nil
	}
	return total, nil
}

// writeZip writes the manifest, the database copy and the content files into an
// already-created ZIP file.
func writeZip(tmpZip string, info Info, tmpDB string, content []contentFile) error {
	// #nosec G304 -- tmpZip is the private temporary ZIP this service created.
	file, err := os.OpenFile(tmpZip, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	writer := zip.NewWriter(file)

	manifest := fmt.Sprintf(
		"# Finglass backup\ncreated=%s\nappVersion=%s\ntransactions=%d\ndocuments=%d\n",
		info.Created, info.AppVersion, info.Transactions, info.Documents,
	)

	manifestWriter, err := createZipEntry(writer, Manifest)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(manifestWriter, manifest); err != nil {
		return err
	}

	databaseWriter, err := createZipEntry(writer, DBName)
	if err != nil {
		return err
	}
	// #nosec G304 -- tmpDB is the temporary database copy this service created.
	databaseFile, err := os.Open(tmpDB)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(databaseWriter, databaseFile)
	closeErr := databaseFile.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}

	for _, item := range content {
		entryWriter, err := createZipEntry(writer, item.name)
		if err != nil {
			return err
		}
		// #nosec G304 -- item.path is a content file this service listed.
		contentFile, err := os.Open(item.path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(entryWriter, contentFile)
		closeErr := contentFile.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}

	return writer.Close()
}

// createZipEntry starts a Deflate entry with Unix mode 0600.
func createZipEntry(writer *zip.Writer, name string) (io.Writer, error) {
	// Rust's zip writer stamps each entry with the current time.
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()}
	header.SetMode(0o600)
	return writer.CreateHeader(header)
}

// attachedTransactionCount counts transactions in an attached copy of the
// database using the live connection's key.
func attachedTransactionCount(d *db.Database, file string) (int64, error) {
	var count int64

	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ?1 AS backup_copy", file); err != nil {
			return err
		}

		countErr := conn.QueryRowContext(ctx, "SELECT count(*) FROM backup_copy.transactions").Scan(&count)
		_, detachErr := conn.ExecContext(ctx, "DETACH DATABASE backup_copy")

		if countErr != nil {
			// The count error wins over a detach error.
			return countErr
		}
		return detachErr
	})

	return count, err
}

// parseProperties reads `key=value` lines the way `java.util.Properties` does
// leniently enough for our own manifest.
func parseProperties(text string) map[string]string {
	properties := make(map[string]string)

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		var key, value string
		if index := strings.IndexAny(line, "=:"); index >= 0 {
			key = line[:index]
			value = line[index+1:]
		} else if index := unicodeWhitespaceIndex(line); index >= 0 {
			key = line[:index]
			value = line[index:]
		} else {
			key = line
		}

		properties[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	return properties
}

// createPrivateTempFile creates an empty temporary file that is owner-only from
// the first byte.
func createPrivateTempFile(dir, prefix, suffix string) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		path := uniqueTempPath(dir, prefix, suffix)

		// #nosec G304 -- path is a private temporary name this service builds.
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_ = file.Close()
			return path, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return "", err
	}

	return "", errors.New("could not create a temporary file")
}

// createPrivateFile creates a file that is owner-only from the first byte.
func createPrivateFile(path string) (*os.File, error) {
	// #nosec G304 -- path is a validated target inside the staging folder.
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}

// canonicalOrAbsolute canonicalizes a path that may not exist yet by
// canonicalizing its parent and keeping the file name.
func canonicalOrAbsolute(path string) (string, error) {
	if canonical, err := canonicalPath(path); err == nil {
		return canonical, nil
	}

	parent := filepath.Dir(path)
	if parent == "" {
		parent = "."
	}
	name := filepath.Base(path)
	if path == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "", errors.New("path has no file name")
	}

	canonicalParent, err := canonicalPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(canonicalParent, name), nil
}

// canonicalPath resolves symlinks and then makes the result absolute.
func canonicalPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

// moveIntoPlace moves the finished ZIP into place, replacing an earlier backup.
func moveIntoPlace(tmpZip, target string) error {
	err := os.Rename(tmpZip, target)
	if err == nil {
		return nil
	}

	// Windows cannot rename over an existing file; mirror the replace fallback.
	if _, statErr := os.Stat(target); statErr == nil {
		if removeErr := os.Remove(target); removeErr != nil {
			return removeErr
		}
		return os.Rename(tmpZip, target)
	}
	return err
}

// removeDirAllIfExists removes a folder when it exists.
func removeDirAllIfExists(path string) error {
	err := os.RemoveAll(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
