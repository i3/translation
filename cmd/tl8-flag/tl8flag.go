package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

type heading struct {
	Line       int
	ID         string
	Translated string
}

type section struct {
	Heading heading
	Lines   []string
}

type document struct {
	Version      string
	sections     []section
	sectionsByID map[string]section
	headings     []heading
	headingsByID map[string]heading
}

func segment(source []byte) (*document, error) {
	// TODO: de-duplicate these goldmark.New() calls into an internal/ package
	md := goldmark.New(
		// GFM is GitHub Flavored Markdown, which we need for tables, for
		// example.
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			// The Attribute option allows us to id, classes, and arbitrary
			// options on headings (for translation status).
			parser.WithAttribute(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)
	parser := md.Parser()
	rd := text.NewReader(source)
	root := parser.Parse(rd)

	// modeled after (go/token).File:
	var lineoffsets []int // lines contains the offset of the first character for each line (the first entry is always 0)

	processed := 0
	for {
		lineoffsets = append(lineoffsets, processed)
		idx := bytes.IndexByte(source[processed:], '\n')
		if idx == -1 {
			break
		}
		processed += idx + 1
	}

	doc := &document{}

	var headings []heading
	headingsByID := make(map[string]heading)
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Kind() == ast.KindHeading {
			var h heading
			for _, attr := range n.Attributes() {
				b, ok := attr.Value.([]byte)
				if !ok {
					continue
				}
				val := string(b)
				switch string(attr.Name) {
				case "id":
					h.ID = val
				case "translated":
					h.Translated = val
				case "version":
					doc.Version = val
				}
			}
			if h.ID == "" {
				//return ast.WalkStop, fmt.Errorf("heading does not have id")
			}
			segments := n.Lines()
			first := segments.At(0)
			line := sort.Search(len(lineoffsets), func(i int) bool {
				return lineoffsets[i] > first.Start
			}) - 1
			if line < 0 {
				return ast.WalkStop, fmt.Errorf("BUG: could not find line offset for position %d", first.Start)
			}
			h.Line = line + 1
			headings = append(headings, h)
			headingsByID[h.ID] = h
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, err
	}

	var sections []section
	sectionsByID := make(map[string]section)
	// Split the document into lines, then segment the lines into sections based
	// on the headers.
	lines := strings.Split(string(source), "\n")
	for idx, h := range headings {
		end := len(lines) - 1
		if idx < len(headings)-1 {
			end = headings[idx+1].Line - 1
		}
		s := section{
			Heading: h,
			Lines:   lines[h.Line:end],
		}
		sectionsByID[h.ID] = s
		sections = append(sections, s)
	}

	doc.sections = sections
	doc.headings = headings
	doc.headingsByID = headingsByID
	doc.sectionsByID = sectionsByID
	return doc, nil
}

// fn is e.g. userguide.markdown
func flag1(fn, oldPath string) error {
	path, err := filepath.Abs(fn)
	if err != nil {
		return err
	}
	currentSource, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	current, err := segment(currentSource)
	if err != nil {
		return err
	}

	oldSource, err := ioutil.ReadFile(oldPath)
	if err != nil {
		return err
	}
	old, err := segment(oldSource)
	if err != nil {
		return err
	}

	unchanged := make(map[string]bool)
	for _, current := range current.sections {
		old, ok := old.sectionsByID[current.Heading.ID]
		if !ok {
			log.Printf("BUG: section %q not found in -old_path=%s", current.Heading.ID, oldPath)
			continue
		}
		diff := cmp.Diff(old.Lines, current.Lines)
		changed := diff != ""
		unchanged[current.Heading.ID] = !changed
		if changed {
			log.Printf("changed (-old +current):\n%s", diff)
		}
	}

	dir := filepath.Dir(path)
	fis, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, fi := range fis {
		if !fi.Mode().IsDir() || fi.Name() == "." || fi.Name() == ".." {
			continue
		}
		translationPath := filepath.Join(dir, fi.Name(), filepath.Base(fn))
		b, err := ioutil.ReadFile(translationPath)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Print(err)
			}
			continue
		}
		lines := strings.Split(string(b), "\n")
		log.Printf("processing translation %s", translationPath)
		translation, err := segment(b)
		if err != nil {
			return err
		}
		for _, heading := range translation.headings {
			if unchanged[heading.ID] && heading.Translated != "" {
				log.Printf("  updating heading %q (up-to-date)", heading.ID)
				lines[heading.Line-1] = translatedRe.ReplaceAllString(lines[heading.Line-1], `translated="`+current.Version+`"`)
			}
		}
		documentHeading := translation.headings[0]
		lines[documentHeading.Line-1] = versionRe.ReplaceAllString(lines[documentHeading.Line-1], `version="`+current.Version+`"`)
		if err := ioutil.WriteFile(translationPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return err
		}
	}

	return nil
}

var (
	translatedRe = regexp.MustCompile(`translated="([^"]+)"`)
	versionRe    = regexp.MustCompile(`version="([^"]+)"`)
)

func tl8flag() error {
	var (
		oldPath = flag.String("old_path",
			"",
			"old version of the document")
	)
	flag.Parse()
	if flag.NArg() != 1 {
		return fmt.Errorf("syntax: %s <markdown-file>", filepath.Base(os.Args[0]))
	}
	if *oldPath == "" {
		return fmt.Errorf("-old_path is required")
	}
	fn := flag.Arg(0)
	if err := flag1(fn, *oldPath); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := tl8flag(); err != nil {
		log.Fatal(err)
	}
}
