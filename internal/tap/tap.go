// Package tap parses and writes the Test Anything Protocol (TAP versions
// 12–14 as commonly produced by prove, node-tap, bats, and cargo test
// adapters). A TAP stream has no suite concept, so the caller labels each
// stream — usually with the shard's file name.
package tap

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/JaydenCJ/muxunit/internal/model"
)

// testPointRe matches "ok"/"not ok" lines: optional number, optional
// description, optional "# DIRECTIVE reason" trailer.
var testPointRe = regexp.MustCompile(`^(not )?ok\b\s*(\d+)?\s*(.*)$`)

// planRe matches "1..N" plans with an optional skip-all reason.
var planRe = regexp.MustCompile(`^1\.\.(\d+)(?:\s*#\s*(.*))?$`)

// Parse reads one TAP stream into a single-suite report named suiteName.
//
// Supported: version lines, leading or trailing plans, numbered and
// unnumbered test points, SKIP/TODO directives, inline YAML diagnostic
// blocks (attached verbatim to the preceding test point), comments, and
// "Bail out!". Indented subtest blocks (TAP 14) are treated as diagnostics
// of their parent point. Test points the plan promised but the stream never
// delivered become synthetic Error cases — a truncated shard must never
// masquerade as a green one.
func Parse(r io.Reader, suiteName string) (*model.Report, error) {
	suite := model.Suite{Name: suiteName}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		planned    = -1
		sawAny     bool
		bailed     bool
		bailReason string
		yamlLines  []string
		inYAML     bool
		lineNo     int
	)
	flushYAML := func() {
		if len(yamlLines) > 0 && len(suite.Cases) > 0 {
			last := &suite.Cases[len(suite.Cases)-1]
			block := strings.Join(yamlLines, "\n")
			if last.Detail == "" {
				last.Detail = block
			} else {
				last.Detail += "\n" + block
			}
			// Promote the conventional "message:" key so converted JUnit
			// failures and diff lines carry a one-line reason.
			if last.Message == "" {
				last.Message = yamlMessage(yamlLines)
			}
		}
		yamlLines = nil
		inYAML = false
	}

	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		// Indented content: YAML diagnostics or TAP 14 subtests.
		if line != trimmed && strings.TrimLeft(line, " \t") == trimmed && len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			switch {
			case inYAML && trimmed == "...":
				flushYAML()
			case inYAML:
				yamlLines = append(yamlLines, trimmed)
			case trimmed == "---":
				inYAML = true
			default:
				// Subtest noise; the parent's summary line carries the verdict.
			}
			continue
		}
		if inYAML {
			// Unterminated YAML block ends implicitly at the next top-level line.
			flushYAML()
		}

		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "TAP version"):
			continue
		case strings.HasPrefix(trimmed, "#"):
			continue
		case strings.HasPrefix(trimmed, "Bail out!"):
			bailed = true
			bailReason = strings.TrimSpace(strings.TrimPrefix(trimmed, "Bail out!"))
			flushYAML()
		case planRe.MatchString(trimmed):
			m := planRe.FindStringSubmatch(trimmed)
			n, _ := strconv.Atoi(m[1])
			planned = n
			if reason := strings.TrimSpace(m[2]); planned == 0 && reason != "" {
				// "1..0 # Skipped: no tests" — an intentionally empty stream.
				suite.Properties = append(suite.Properties,
					model.Property{Name: "tap.skip_all", Value: reason})
			}
		case testPointRe.MatchString(trimmed):
			if bailed {
				continue // everything after a bail-out is unreliable
			}
			m := testPointRe.FindStringSubmatch(trimmed)
			c := parsePoint(m, len(suite.Cases)+1)
			suite.Cases = append(suite.Cases, c)
			sawAny = true
		default:
			return nil, fmt.Errorf("tap: line %d: not valid TAP: %q", lineNo, trimmed)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read tap input: %w", err)
	}
	flushYAML()

	if planned < 0 && !sawAny && !bailed {
		return nil, fmt.Errorf("tap: stream has neither a plan nor any test points")
	}
	if bailed {
		msg := "Bail out!"
		if bailReason != "" {
			msg = "Bail out! " + bailReason
		}
		suite.Cases = append(suite.Cases, model.Case{
			Name:    "bail out",
			Status:  model.Error,
			Message: msg,
		})
	}
	// A plan that promised more points than arrived means the runner died
	// mid-stream; surface each missing point as an error, not silence.
	if planned >= 0 && !bailed {
		for n := len(suite.Cases) + 1; n <= planned; n++ {
			suite.Cases = append(suite.Cases, model.Case{
				Name:    fmt.Sprintf("test %d", n),
				Status:  model.Error,
				Message: "planned but never ran (truncated TAP stream)",
			})
		}
	}
	return &model.Report{Suites: []model.Suite{suite}}, nil
}

// parsePoint converts one matched "ok"/"not ok" line into a Case.
func parsePoint(m []string, ordinal int) model.Case {
	failed := m[1] != ""
	rest := m[3]

	// Split an unescaped "#" directive trailer off the description.
	desc, directive := splitDirective(rest)
	desc = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(desc), "- "))
	desc = unescape(desc)
	if desc == "" {
		num := m[2]
		if num == "" {
			num = strconv.Itoa(ordinal)
		}
		desc = "test " + num
	}

	c := model.Case{Name: desc}
	dir := strings.ToUpper(directive)
	switch {
	case strings.HasPrefix(dir, "SKIP"):
		c.Status = model.Skip
		c.Message = strings.TrimSpace(directive[min(4, len(directive)):])
	case strings.HasPrefix(dir, "TODO"):
		// TODO points never fail a run; an unexpectedly passing TODO is
		// still a pass so it shows up as progress.
		if failed {
			c.Status = model.Skip
			c.Message = strings.TrimSpace("TODO " + strings.TrimSpace(directive[min(4, len(directive)):]))
		} else {
			c.Status = model.Pass
		}
	case failed:
		c.Status = model.Fail
	default:
		c.Status = model.Pass
	}
	return c
}

// yamlMessage pulls the value of a top-level "message:" key out of a YAML
// diagnostic block, unquoting the scalar when needed.
func yamlMessage(lines []string) string {
	for _, l := range lines {
		val, ok := strings.CutPrefix(l, "message:")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && val[0] == '"' {
			if unq, err := strconv.Unquote(val); err == nil {
				return unq
			}
		}
		return val
	}
	return ""
}

// splitDirective finds the first unescaped "#" and returns (description,
// directive-after-hash). TAP escapes a literal hash as "\#".
func splitDirective(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && (i == 0 || s[i-1] != '\\') {
			return s[:i], strings.TrimSpace(s[i+1:])
		}
	}
	return s, ""
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, `\#`, `#`)
	return strings.ReplaceAll(s, `\\`, `\`)
}

func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `#`, `\#`)
}

// Write serializes the report as a TAP version 13 stream. TAP is flat, so
// with more than one suite each description is prefixed "suite > name";
// a single-suite report keeps bare names and round-trips cleanly.
// Fail/Error details are emitted as YAML diagnostic blocks.
func Write(w io.Writer, rep *model.Report) error {
	bw := bufio.NewWriter(w)
	total := rep.TotalCases()
	fmt.Fprintln(bw, "TAP version 13")
	fmt.Fprintf(bw, "1..%d\n", total)
	prefixSuite := len(rep.Suites) > 1
	n := 0
	for _, s := range rep.Suites {
		for _, c := range s.Cases {
			n++
			name := c.Name
			if prefixSuite && s.Name != "" {
				name = s.Name + " > " + name
			}
			name = escape(name)
			switch c.Status {
			case model.Pass:
				fmt.Fprintf(bw, "ok %d - %s\n", n, name)
			case model.Skip:
				reason := c.Message
				if reason == "" {
					reason = "skipped"
				}
				fmt.Fprintf(bw, "ok %d - %s # SKIP %s\n", n, name, reason)
			default: // Fail and Error both map to "not ok"
				fmt.Fprintf(bw, "not ok %d - %s\n", n, name)
				writeYAML(bw, c)
			}
		}
	}
	return bw.Flush()
}

// writeYAML emits the diagnostic block for a failed test point.
func writeYAML(bw *bufio.Writer, c model.Case) {
	if c.Message == "" && c.Detail == "" {
		return
	}
	fmt.Fprintln(bw, "  ---")
	// Skip the message line when the detail block (e.g. a round-tripped
	// TAP diagnostic) already carries the same message: key.
	if c.Message != "" && yamlMessage(strings.Split(c.Detail, "\n")) != c.Message {
		fmt.Fprintf(bw, "  message: %s\n", yamlScalar(c.Message))
	}
	fmt.Fprintf(bw, "  severity: %s\n", c.Status)
	if c.Detail != "" {
		fmt.Fprintln(bw, "  detail: |")
		for _, line := range strings.Split(c.Detail, "\n") {
			fmt.Fprintf(bw, "    %s\n", line)
		}
	}
	fmt.Fprintln(bw, "  ...")
}

// yamlScalar quotes a message when it could be misread as YAML structure.
func yamlScalar(s string) string {
	if strings.ContainsAny(s, ":#{}[]\"'\n") || strings.TrimSpace(s) != s {
		return strconv.Quote(s)
	}
	return s
}
