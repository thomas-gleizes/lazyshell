package session

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// parseDotEnvFile reads a .env-style file: KEY=VALUE lines, optionally
// prefixed with "export ", blank lines and "#" comments ignored, values
// optionally wrapped in matching single or double quotes. It does not
// interpolate other variables or support multiline values — lazyshell
// hand-rolls this rather than pull in a dependency for a format this simple.
func parseDotEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vars := make(map[string]string)

	lineNo := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNo++

		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = strings.TrimPrefix(line, "export ")

		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("%s:%d: ligne invalide, attendu CLE=valeur : %q", path, lineNo, line)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%s:%d: nom de variable vide", path, lineNo)
		}

		vars[key] = unquoteDotEnvValue(strings.TrimSpace(value))
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return vars, nil
}

// loadDotEnvFileIfExists is parseDotEnvFile for the automatic "<cwd>/.env"
// lookup: a missing file is not an error there, since most sessions simply
// have none.
func loadDotEnvFileIfExists(path string) (map[string]string, error) {
	vars, err := parseDotEnvFile(path)
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}

	return vars, err
}

// unquoteDotEnvValue strips a single matching pair of leading/trailing quotes
// (' or "), the two forms a .env value is conventionally wrapped in. A value
// with no quotes, or mismatched ones, is left exactly as written.
func unquoteDotEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}

	first, last := value[0], value[len(value)-1]
	if (first == '"' || first == '\'') && first == last {
		return value[1 : len(value)-1]
	}

	return value
}
