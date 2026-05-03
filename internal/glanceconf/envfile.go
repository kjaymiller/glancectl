package glanceconf

import (
	"bufio"
	"os"
	"strings"
)

// applyEnvFile parses a dotenv-lite file (KEY=VALUE per line, '#' comments,
// optional surrounding quotes) and sets each entry in os.Environ if the
// key is not already set. Mirrors how docker compose / glance read .env.
func applyEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		if _, ok := os.LookupEnv(key); !ok {
			_ = os.Setenv(key, val)
		}
	}
	return scan.Err()
}
