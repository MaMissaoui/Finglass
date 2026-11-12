package ocr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MaMissaoui/Finglass/internal/core/documents/programs"
)

// DefaultLanguage is the language pair used for recognition.
const DefaultLanguage = "deu+eng"

// CommandTimeout is how long a spawned helper program may run before it is
// killed.
const CommandTimeout = 120 * time.Second

// commandOutput is the captured result of a helper command.
type commandOutput struct {
	// Success says whether the command exited successfully.
	Success bool
	// Stdout is everything the command wrote to stdout.
	Stdout []byte
}

// runWithTimeout runs a program without a shell, with stdin closed and a hard
// timeout.
//
// Arguments are passed separately, never as one shell string. os/exec drains
// stdout and stderr concurrently in its own goroutines. A timeout kills the
// process and waits for it.
func runWithTimeout(program string, args []string, timeout time.Duration) (commandOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// #nosec G204 -- program is an absolute path from programs.Resolve and the
	// arguments are fixed by this package; no shell is involved.
	command := exec.CommandContext(ctx, program, args...)
	// A nil Stdin makes os/exec read from the null device.
	command.Stdin = nil
	// Bounds the wait for the output pipes after the kill, so a grandchild that
	// keeps stdout open cannot hold the call past the timeout.
	command.WaitDelay = time.Second
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard

	err := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return commandOutput{}, errors.New("helper command timed out")
	}
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return commandOutput{}, err
		}
		return commandOutput{Success: false, Stdout: stdout.Bytes()}, nil
	}

	return commandOutput{Success: true, Stdout: stdout.Bytes()}, nil
}

// TesseractCLI is Tesseract through its command-line interface.
type TesseractCLI struct {
	language string
}

// CreateOrNone returns a working engine, or nil when Tesseract or the deu/eng
// traineddata is missing.
//
// This is the equivalent of FinDesk's TesseractOcrEngine.createOrNull: it
// probes `tesseract --list-langs` instead of loading a native library.
func CreateOrNone() *TesseractCLI {
	tesseract, found := programs.Resolve("tesseract")
	if !found {
		return nil
	}
	output, err := runWithTimeout(tesseract, []string{"--list-langs"}, 10*time.Second)
	if err != nil || !output.Success {
		return nil
	}

	listed := string(output.Stdout)
	hasLanguage := func(language string) bool {
		for _, line := range strings.Split(listed, "\n") {
			if strings.TrimSpace(line) == language {
				return true
			}
		}
		return false
	}
	if !hasLanguage("deu") || !hasLanguage("eng") {
		return nil
	}

	return &TesseractCLI{language: DefaultLanguage}
}

// Recognize returns the text lines and their character-weighted confidence.
func (t *TesseractCLI) Recognize(image string) (Recognition, error) {
	args := []string{
		image,
		"stdout",
		"-l",
		t.language,
		"--psm",
		"3",
		"tsv",
	}

	tesseract, found := programs.Resolve("tesseract")
	if !found {
		return Recognition{}, &OcrError{cause: fmt.Errorf("tesseract was not found: %w", fs.ErrNotExist)}
	}
	output, err := runWithTimeout(tesseract, args, CommandTimeout)
	if err != nil {
		return Recognition{}, &OcrError{cause: err}
	}
	if !output.Success {
		return Recognition{}, ErrCommandFailed
	}

	return recognitionFromTSV(output.Stdout), nil
}

type tsvKey struct {
	page      int64
	block     int64
	paragraph int64
	line      int64
}

type tsvWord struct {
	text       string
	confidence float64
}

type tsvLine struct {
	key   tsvKey
	words []tsvWord
}

// recognitionFromTSV groups level-5 rows into Tesseract's text lines and
// weights every word's confidence by its rune count.
func recognitionFromTSV(tsv []byte) Recognition {
	text := string(tsv)
	lines := make([]tsvLine, 0)
	indexes := make(map[tsvKey]int)

	for _, row := range strings.Split(text, "\n") {
		columns := strings.Split(row, "\t")
		if len(columns) < 12 {
			continue
		}
		level, err := strconv.ParseInt(strings.TrimSpace(columns[0]), 10, 64)
		if err != nil || level != 5 {
			continue
		}

		page, err := strconv.ParseInt(strings.TrimSpace(columns[1]), 10, 64)
		if err != nil {
			continue
		}
		block, err := strconv.ParseInt(strings.TrimSpace(columns[2]), 10, 64)
		if err != nil {
			continue
		}
		paragraph, err := strconv.ParseInt(strings.TrimSpace(columns[3]), 10, 64)
		if err != nil {
			continue
		}
		line, err := strconv.ParseInt(strings.TrimSpace(columns[4]), 10, 64)
		if err != nil {
			continue
		}
		confidence, err := strconv.ParseFloat(strings.TrimSpace(columns[10]), 64)
		if err != nil {
			continue
		}
		word := strings.TrimRightFunc(columns[11], unicode.IsSpace)
		if word == "" {
			continue
		}
		key := tsvKey{page: page, block: block, paragraph: paragraph, line: line}

		position, exists := indexes[key]
		if !exists {
			position = len(lines)
			indexes[key] = position
			lines = append(lines, tsvLine{key: key})
		}
		lines[position].words = append(lines[position].words, tsvWord{text: word, confidence: confidence})
	}

	joined := make([]string, 0, len(lines))
	totalChars := 0
	weighted := 0.0

	for _, line := range lines {
		words := make([]string, 0, len(line.words))
		for _, word := range line.words {
			length := utf8.RuneCountInString(word.text)
			totalChars += length
			weighted += word.confidence * float64(length)
			words = append(words, word.text)
		}
		lineText := strings.TrimRightFunc(strings.Join(words, " "), unicode.IsSpace)
		if lineText != "" {
			joined = append(joined, lineText)
		}
	}

	confidence := 0.0
	if totalChars != 0 {
		confidence = weighted / float64(totalChars)
	}

	return Recognition{Text: strings.Join(joined, "\n"), Confidence: confidence}
}
