package profiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

func registryPath(p paths.AppPaths) string {
	return filepath.Join(p.Root(), file)
}

func load(p paths.AppPaths) (map[string]string, error) {
	// #nosec G304 -- the registry path is built from the resolved data root,
	// not from user input.
	text, err := os.ReadFile(registryPath(p))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("read profiles registry: %w", err)
	}

	return parseProperties(string(text)), nil
}

func save(p paths.AppPaths, properties map[string]string) error {
	if err := paths.EnsureDir(p.Root()); err != nil {
		return fmt.Errorf("create data folder: %w", err)
	}

	target := registryPath(p)
	temp := tempPath(p.Root())

	if err := writeRegistry(temp, properties); err != nil {
		_ = os.Remove(temp)
		return err
	}

	if err := os.Rename(temp, target); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace profiles registry: %w", err)
	}

	return nil
}

func writeRegistry(temp string, properties map[string]string) (err error) {
	// #nosec G304 -- the temp path is built from the resolved data root and a
	// process-unique name, not from user input.
	registry, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create profiles registry: %w", err)
	}
	defer func() {
		if closeErr := registry.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close profiles registry: %w", closeErr)
		}
	}()

	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var content strings.Builder
	content.WriteString("# Finglass profiles\n")
	for _, key := range keys {
		content.WriteString(escape(key))
		content.WriteByte('=')
		content.WriteString(escape(properties[key]))
		content.WriteByte('\n')
	}

	if _, err := registry.WriteString(content.String()); err != nil {
		return fmt.Errorf("write profiles registry: %w", err)
	}
	if err := registry.Sync(); err != nil {
		return fmt.Errorf("sync profiles registry: %w", err)
	}

	return nil
}

func tempPath(root string) string {
	nanos := time.Now().UnixNano()
	counter := tempCounter.Add(1)
	name := fmt.Sprintf("profiles.%d-%d-%d.tmp", os.Getpid(), nanos, counter)
	return filepath.Join(root, name)
}
