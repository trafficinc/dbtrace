package initconfig

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Options struct {
	Path  string
	DSN   string
	Force bool
	Env   map[string]string
}

func Run(opts Options) error {
	path := opts.Path
	if path == "" {
		path = "dbtrace.yaml"
	}

	if !opts.Force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; use --force to overwrite", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check %s: %w", path, err)
		}
	}

	dsn := strings.TrimSpace(opts.DSN)
	var err error
	if dsn == "" {
		dsn, err = detectDSN(opts.Env)
		if err != nil {
			return err
		}
	}

	content := renderConfig(dsn)
	if err := os.WriteFile(filepath.Clean(path), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func detectDSN(env map[string]string) (string, error) {
	values := mergeEnv(os.Environ())
	for key, value := range env {
		values[key] = value
	}

	if dsn, ok := dsnFromValues(values); ok {
		return dsn, nil
	}

	dotenv, err := readDotEnv(".env")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	for key, value := range dotenv {
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}

	if conn := strings.ToLower(values["DB_CONNECTION"]); conn != "" && conn != "mysql" {
		return "", fmt.Errorf("DB_CONNECTION is %q, expected mysql", values["DB_CONNECTION"])
	}
	if dsn, ok := dsnFromValues(values); ok {
		return dsn, nil
	}

	return "", errors.New("could not detect database settings; set DB_* variables, create a .env file, or pass --dsn")
}

func dsnFromValues(values map[string]string) (string, bool) {
	host := strings.TrimSpace(values["DB_HOST"])
	database := strings.TrimSpace(values["DB_DATABASE"])
	user := strings.TrimSpace(values["DB_USERNAME"])
	if host == "" || database == "" || user == "" {
		return "", false
	}

	port := strings.TrimSpace(values["DB_PORT"])
	if port == "" {
		port = "3306"
	}
	password := values["DB_PASSWORD"]
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", user, escapePassword(password), host, port, database), true
}

func escapePassword(password string) string {
	return strings.ReplaceAll(url.QueryEscape(password), "+", "%20")
}

func mergeEnv(pairs []string) map[string]string {
	out := map[string]string{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func readDotEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		out[key] = value
	}
	return out, nil
}

func renderConfig(dsn string) string {
	return "database:\n" +
		"  dsn: " + strconv.Quote(dsn) + "\n\n" +
		"snapshot:\n" +
		"  workers: 4\n" +
		"  chunk_size: 10000\n" +
		"  output_dir: \".dbtrace/snapshots\"\n\n" +
		"report:\n" +
		"  max_lines_per_operation: 50\n" +
		"  max_value_length: 200\n\n" +
		"keys:\n" +
		"  # table_name:\n" +
		"  #   - column_a\n" +
		"  #   - column_b\n\n" +
		"ignore:\n" +
		"  tables:\n" +
		"    - sessions\n" +
		"    - cache\n" +
		"    - jobs\n" +
		"  columns:\n" +
		"    - created_at\n" +
		"    - updated_at\n" +
		"    - last_seen\n"
}
