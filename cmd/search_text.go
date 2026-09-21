package cmd

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/1broseidon/cymbal/index"
)

const rgSearchTimeout = 10 * time.Second

func searchText(dbPath, query, language string, limit int, jsonOut bool, includes, excludes []string) error {
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return searchTextRg(rgPath, dbPath, query, language, limit, jsonOut, includes, excludes)
	}
	return searchTextGo(dbPath, query, language, limit, jsonOut, includes, excludes)
}

// Ripgrep accelerates literal queries. Regex syntax uses the native scanner,
// avoiding dialect differences and JSON serialization of unfiltered file contents.
func searchTextRg(rgPath, dbPath, query, language string, limit int, jsonOut bool, includes, excludes []string) error {
	if query == "" || strings.ContainsAny(query, "\\.^$*+?()[]{}|\r\n") {
		return searchTextGo(dbPath, query, language, limit, jsonOut, includes, excludes)
	}
	matcher, err := regexp.Compile(query)
	if err != nil {
		return fmt.Errorf("invalid search pattern: %w", err)
	}
	paths := index.PathFilter{Include: includes, Exclude: excludes}
	files, err := index.TextFiles(dbPath, language, paths)
	if err != nil {
		return err
	}
	if limit <= 0 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(context.Background(), rgSearchTimeout)
	defer cancel()
	var results []index.TextResult
	for len(files) > 0 {
		n := rgFileBatchSize(files)
		matches, err := rgTextBatch(ctx, rgPath, files[:n], query, matcher, limit-len(results))
		if err != nil {
			return err
		}
		results = append(results, matches...)
		if len(results) >= limit {
			break
		}
		files = files[n:]
	}
	return finishTextSearch(query, results, jsonOut)
}

// Keep process arguments small on Windows as well as Unix.
func rgFileBatchSize(files []index.FileInfo) int {
	size, n := 0, 0
	for n < len(files) && n < 128 {
		next := len(files[n].Path)*2 + 4
		if n > 0 && size+next > 16000 {
			break
		}
		size += next
		n++
	}
	return n
}

type rgValue struct {
	Text  string `json:"text"`
	Bytes string `json:"bytes"`
}

func (v rgValue) value() (string, error) {
	if v.Bytes == "" {
		return v.Text, nil
	}
	data, err := base64.StdEncoding.DecodeString(v.Bytes)
	return string(data), err
}

type rgMatch struct {
	Type string `json:"type"`
	Data struct {
		Path  rgValue `json:"path"`
		Lines rgValue `json:"lines"`
		Line  int     `json:"line_number"`
	} `json:"data"`
}

func rgTextBatch(parent context.Context, rgPath string, files []index.FileInfo, prefix string, matcher *regexp.Regexp, limit int) ([]index.TextResult, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	args := []string{"--no-config", "--json", "--fixed-strings", "--text", "--encoding", "none", "--no-ignore", "--hidden", "--sort", "path", "-e", prefix, "--"}
	inventory := make(map[string]index.FileInfo, len(files))
	for _, file := range files {
		args = append(args, file.Path)
		inventory[file.Path] = file
	}
	command := exec.CommandContext(ctx, rgPath, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("starting text search: %w", err)
	}
	results, scanErr := readRGMatches(stdout, inventory, matcher, limit)
	limited := len(results) >= limit
	if scanErr != nil || limited {
		cancel()
	}
	waitErr := command.Wait()
	if scanErr != nil {
		return nil, scanErr
	}
	if limited {
		return results, nil
	}
	if parent.Err() != nil {
		return nil, fmt.Errorf("text search: %w", parent.Err())
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("text search failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
		}
	}
	return results, nil
}

func readRGMatches(stdout io.Reader, inventory map[string]index.FileInfo, matcher *regexp.Regexp, limit int) ([]index.TextResult, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var results []index.TextResult
	for scanner.Scan() {
		var event rgMatch
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("reading text search output: %w", err)
		}
		if event.Type != "match" {
			continue
		}
		path, pathErr := event.Data.Path.value()
		line, lineErr := event.Data.Lines.value()
		if err := errors.Join(pathErr, lineErr); err != nil {
			return nil, err
		}
		// Match bufio.ScanLines, including CRLF handling, before applying the regex.
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		file, ok := inventory[path]
		if !ok {
			return nil, fmt.Errorf("text search returned an unrequested file: %s", path)
		}
		if !matcher.MatchString(line) {
			continue
		}
		results = append(results, index.NewTextResult(file, event.Data.Line, line))
		if len(results) >= limit {
			break
		}
	}
	return results, scanner.Err()
}

func searchTextGo(dbPath, query, language string, limit int, jsonOut bool, includes, excludes []string) error {
	results, err := index.TextSearchWithOptions(dbPath, query, language, limit, index.TextSearchOptions{
		Regexp: true, Paths: index.PathFilter{Include: includes, Exclude: excludes},
	})
	if err != nil {
		return err
	}
	return finishTextSearch(query, results, jsonOut)
}

func finishTextSearch(query string, results []index.TextResult, jsonOut bool) error {
	if len(results) == 0 {
		return fmt.Errorf("no results found for '%s'", query)
	}
	return renderTextResults(query, results, jsonOut)
}
