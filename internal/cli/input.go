package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JaydenCJ/muxunit/internal/junit"
	"github.com/JaydenCJ/muxunit/internal/model"
	"github.com/JaydenCJ/muxunit/internal/render"
	"github.com/JaydenCJ/muxunit/internal/tap"
)

// Format is a report syntax muxunit can read or write.
type Format string

const (
	FormatAuto  Format = "auto"
	FormatJUnit Format = "junit"
	FormatTAP   Format = "tap"
	FormatJSON  Format = "json" // output only
)

// parseFormat validates a --from/--to token.
func parseFormat(tok string, allowJSON bool) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(tok)) {
	case "auto", "":
		return FormatAuto, nil
	case "junit", "xml":
		return FormatJUnit, nil
	case "tap":
		return FormatTAP, nil
	case "json":
		if allowJSON {
			return FormatJSON, nil
		}
	}
	return FormatAuto, fmt.Errorf("unknown format %q", tok)
}

// detectFormat picks a parser from the file extension, falling back to
// content sniffing so extension-less shards and stdin still work.
func detectFormat(path string, data []byte) Format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xml", ".junit":
		return FormatJUnit
	case ".tap", ".t":
		return FormatTAP
	}
	head := strings.TrimLeft(string(data[:min(len(data), 512)]), " \t\r\n\ufeff")
	if strings.HasPrefix(head, "<") {
		return FormatJUnit
	}
	return FormatTAP
}

// suiteLabel names a TAP stream after its file: "shard-3.tap" → "shard-3".
func suiteLabel(path string) string {
	if path == "-" {
		return "stdin"
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// readReport loads and parses one report file ("-" = stdin).
func readReport(path string, from Format, stdin io.Reader) (*model.Report, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	format := from
	if format == FormatAuto {
		format = detectFormat(path, data)
	}
	switch format {
	case FormatJUnit:
		rep, err := junit.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return rep, nil
	case FormatTAP:
		rep, err := tap.Parse(bytes.NewReader(data), suiteLabel(path))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return rep, nil
	}
	return nil, fmt.Errorf("%s: cannot read format %q", path, format)
}

// readReports loads every path in order.
func readReports(paths []string, from Format, stdin io.Reader) ([]*model.Report, error) {
	reps := make([]*model.Report, 0, len(paths))
	for _, p := range paths {
		rep, err := readReport(p, from, stdin)
		if err != nil {
			return nil, err
		}
		reps = append(reps, rep)
	}
	return reps, nil
}

// writeReport serializes rep in the requested format to outPath ("" or "-"
// = stdout). Close errors are reported, not swallowed: a gate must not
// exit 0 when the artifact never fully reached disk.
func writeReport(rep *model.Report, to Format, outPath string, stdout io.Writer) error {
	var w io.Writer = stdout
	var f *os.File
	if outPath != "" && outPath != "-" {
		var err error
		f, err = os.Create(outPath)
		if err != nil {
			return err
		}
		w = f
	}
	var err error
	switch to {
	case FormatJUnit, FormatAuto:
		err = junit.Write(w, rep)
	case FormatTAP:
		err = tap.Write(w, rep)
	case FormatJSON:
		err = render.WriteReportJSON(w, rep)
	default:
		err = fmt.Errorf("cannot write format %q", to)
	}
	if f != nil {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("%s: %w", outPath, cerr)
		}
	}
	return err
}
