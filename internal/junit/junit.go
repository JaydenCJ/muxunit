// Package junit parses and writes JUnit-style XML reports. The dialect is
// deliberately liberal on input (Jest, pytest, Gradle, go-junit-report,
// Surefire all disagree on details) and strict, sorted, and deterministic
// on output.
package junit

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/JaydenCJ/muxunit/internal/model"
)

// xmlSuites is the <testsuites> document root.
type xmlSuites struct {
	XMLName xml.Name   `xml:"testsuites"`
	Suites  []xmlSuite `xml:"testsuite"`
}

// xmlSuiteRoot is a bare <testsuite> document root (pytest, phpunit).
type xmlSuiteRoot struct {
	XMLName xml.Name `xml:"testsuite"`
	xmlSuiteBody
}

type xmlSuite struct {
	xmlSuiteBody
}

// xmlSuiteBody carries the fields shared by both root shapes and nesting.
type xmlSuiteBody struct {
	Name       string     `xml:"name,attr"`
	Timestamp  string     `xml:"timestamp,attr"`
	Hostname   string     `xml:"hostname,attr"`
	Time       string     `xml:"time,attr"`
	Properties []xmlProp  `xml:"properties>property"`
	Cases      []xmlCase  `xml:"testcase"`
	Children   []xmlSuite `xml:"testsuite"` // Gradle/Ant nest suites
}

type xmlProp struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type xmlCase struct {
	Name      string     `xml:"name,attr"`
	ClassName string     `xml:"classname,attr"`
	Time      string     `xml:"time,attr"`
	Failure   *xmlResult `xml:"failure"`
	Error     *xmlResult `xml:"error"`
	Skipped   *xmlResult `xml:"skipped"`
	SystemOut string     `xml:"system-out"`
	SystemErr string     `xml:"system-err"`
}

type xmlResult struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

// Parse reads one JUnit XML document. Both <testsuites> and bare <testsuite>
// roots are accepted; nested suites are flattened with "/"-joined names so the
// canonical model stays a flat list.
func Parse(r io.Reader) (*model.Report, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read junit input: %w", err)
	}
	var multi xmlSuites
	if err := xml.Unmarshal(data, &multi); err == nil {
		rep := &model.Report{}
		for _, s := range multi.Suites {
			flatten(rep, s.xmlSuiteBody, "")
		}
		return rep, nil
	}
	var single xmlSuiteRoot
	if err := xml.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("parse junit xml: %w", err)
	}
	rep := &model.Report{}
	flatten(rep, single.xmlSuiteBody, "")
	return rep, nil
}

// flatten appends s (and its nested children, names joined by "/") to rep.
func flatten(rep *model.Report, s xmlSuiteBody, prefix string) {
	name := s.Name
	if prefix != "" {
		if name == "" {
			name = prefix
		} else {
			name = prefix + "/" + name
		}
	}
	if len(s.Cases) > 0 || len(s.Children) == 0 {
		suite := model.Suite{
			Name:      name,
			Timestamp: s.Timestamp,
			Hostname:  s.Hostname,
		}
		for _, p := range s.Properties {
			suite.Properties = append(suite.Properties, model.Property{Name: p.Name, Value: p.Value})
		}
		for _, c := range s.Cases {
			suite.Cases = append(suite.Cases, convertCase(c))
		}
		rep.Suites = append(rep.Suites, suite)
	}
	for _, child := range s.Children {
		flatten(rep, child.xmlSuiteBody, name)
	}
}

func convertCase(c xmlCase) model.Case {
	out := model.Case{
		Name:      c.Name,
		ClassName: c.ClassName,
		Time:      parseSeconds(c.Time),
		Status:    model.Pass,
		SystemOut: strings.TrimSpace(c.SystemOut),
		SystemErr: strings.TrimSpace(c.SystemErr),
	}
	// Precedence when a producer emits several children: error is the most
	// severe signal, then failure, then skipped.
	switch {
	case c.Error != nil:
		out.Status = model.Error
		out.Message = c.Error.Message
		out.Detail = strings.TrimSpace(c.Error.Body)
	case c.Failure != nil:
		out.Status = model.Fail
		out.Message = c.Failure.Message
		out.Detail = strings.TrimSpace(c.Failure.Body)
	case c.Skipped != nil:
		out.Status = model.Skip
		out.Message = c.Skipped.Message
		out.Detail = strings.TrimSpace(c.Skipped.Body)
	}
	return out
}

// parseSeconds tolerates the time formats seen in the wild: "1.5", "1,500"
// (Ant with a grouping locale), and absent attributes.
func parseSeconds(s string) float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// --- writer ---

type outSuites struct {
	XMLName  xml.Name   `xml:"testsuites"`
	Tests    int        `xml:"tests,attr"`
	Failures int        `xml:"failures,attr"`
	Errors   int        `xml:"errors,attr"`
	Skipped  int        `xml:"skipped,attr"`
	Time     string     `xml:"time,attr"`
	Suites   []outSuite `xml:"testsuite"`
}

type outSuite struct {
	Name      string    `xml:"name,attr"`
	Tests     int       `xml:"tests,attr"`
	Failures  int       `xml:"failures,attr"`
	Errors    int       `xml:"errors,attr"`
	Skipped   int       `xml:"skipped,attr"`
	Time      string    `xml:"time,attr"`
	Timestamp string    `xml:"timestamp,attr,omitempty"`
	Hostname  string    `xml:"hostname,attr,omitempty"`
	Props     *outProps `xml:"properties,omitempty"`
	Cases     []outCase `xml:"testcase"`
}

type outProps struct {
	Property []xmlProp `xml:"property"`
}

type outCase struct {
	Name      string     `xml:"name,attr"`
	ClassName string     `xml:"classname,attr,omitempty"`
	Time      string     `xml:"time,attr"`
	Failure   *outResult `xml:"failure,omitempty"`
	Error     *outResult `xml:"error,omitempty"`
	Skipped   *outResult `xml:"skipped,omitempty"`
	SystemOut string     `xml:"system-out,omitempty"`
	SystemErr string     `xml:"system-err,omitempty"`
}

type outResult struct {
	Message string `xml:"message,attr,omitempty"`
	Body    string `xml:",chardata"`
}

// Write serializes the report as a <testsuites> document with recomputed
// per-suite and total counters. Output is deterministic for a given report.
func Write(w io.Writer, rep *model.Report) error {
	doc := outSuites{}
	total := rep.Counts()
	doc.Tests = total.Total
	doc.Failures = total.Failed
	doc.Errors = total.Errored
	doc.Skipped = total.Skipped
	doc.Time = formatSeconds(total.Time)
	for _, s := range rep.Suites {
		counts := model.CountsOf(s)
		os := outSuite{
			Name:      s.Name,
			Tests:     counts.Total,
			Failures:  counts.Failed,
			Errors:    counts.Errored,
			Skipped:   counts.Skipped,
			Time:      formatSeconds(counts.Time),
			Timestamp: s.Timestamp,
			Hostname:  s.Hostname,
		}
		if len(s.Properties) > 0 {
			props := &outProps{}
			for _, p := range s.Properties {
				props.Property = append(props.Property, xmlProp{Name: p.Name, Value: p.Value})
			}
			os.Props = props
		}
		for _, c := range s.Cases {
			oc := outCase{
				Name:      c.Name,
				ClassName: c.ClassName,
				Time:      formatSeconds(c.Time),
				SystemOut: c.SystemOut,
				SystemErr: c.SystemErr,
			}
			res := &outResult{Message: c.Message, Body: c.Detail}
			switch c.Status {
			case model.Fail:
				oc.Failure = res
			case model.Error:
				oc.Error = res
			case model.Skip:
				oc.Skipped = res
			}
			os.Cases = append(os.Cases, oc)
		}
		doc.Suites = append(doc.Suites, os)
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encode junit xml: %w", err)
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// formatSeconds prints durations the way JUnit consumers expect: plain
// decimal seconds with millisecond precision.
func formatSeconds(t float64) string {
	return strconv.FormatFloat(t, 'f', 3, 64)
}
