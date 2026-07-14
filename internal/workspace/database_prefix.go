package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var (
	dbLoginPrefixPattern   = regexp.MustCompile(`^[a-z]{2}$`)
	dbLoginPropertyPattern = regexp.MustCompile(`\bdbLoginPrefix\s*:`)
	dbLoginStringPattern   = regexp.MustCompile(`(\bdbLoginPrefix\s*:\s*)'([^'\r\n]*)'|(\bdbLoginPrefix\s*:\s*)"([^"\r\n]*)"`)
)

// ValidateDBLoginPrefix requires the raw, two-letter database login prefix.
// The trailing separator is added when the starter configuration is updated.
func ValidateDBLoginPrefix(prefix string) error {
	if !dbLoginPrefixPattern.MatchString(prefix) {
		return errors.New("database login prefix must contain exactly two lowercase letters")
	}
	return nil
}

func updateDBLoginPrefix(targetDir, prefix string) error {
	if err := ValidateDBLoginPrefix(prefix); err != nil {
		return err
	}

	path := filepath.Join(targetDir, "nuxt.config.ts")
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Nuxt configuration: %w", err)
	}

	properties := dbLoginPropertyPattern.FindAllIndex(contents, -1)
	if len(properties) != 1 {
		return fmt.Errorf("expected exactly one dbLoginPrefix property in nuxt.config.ts, found %d", len(properties))
	}

	match := dbLoginStringPattern.FindSubmatchIndex(contents)
	if match == nil {
		return errors.New("dbLoginPrefix in nuxt.config.ts must be a quoted string")
	}
	valueStart, valueEnd := match[4], match[5]
	if valueStart < 0 {
		valueStart, valueEnd = match[8], match[9]
	}
	if valueStart < 0 {
		return errors.New("dbLoginPrefix in nuxt.config.ts must be a quoted string")
	}

	replacement := prefix + "-"
	updated := make([]byte, 0, len(contents)-valueEnd+valueStart+len(replacement))
	updated = append(updated, contents[:valueStart]...)
	updated = append(updated, replacement...)
	updated = append(updated, contents[valueEnd:]...)

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect Nuxt configuration: %w", err)
	}
	if err := writeFileAtomically(path, updated, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write Nuxt configuration: %w", err)
	}
	return nil
}

func writeFileAtomically(path string, contents []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err = temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err = temporary.Write(contents); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}
