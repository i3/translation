package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/renameio"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const preamble = `<!DOCTYPE html>
<html lang="en">
<head>
<link rel="icon" type="image/x-icon" href="/favicon.ico">
<meta http-equiv="Content-Type" content="application/xhtml+xml; charset=UTF-8" />
<meta name="generator" content="AsciiDoc 9.0.3" />
<title>i3: i3 User’s Guide</title>
<link rel="stylesheet" href="https://i3wm.org/css/style.css" type="text/css" />
<link rel="stylesheet" href="markdownxhtml11.css" type="text/css" />
<script type="text/javascript">
/*<![CDATA[*/
document.addEventListener("DOMContentLoaded", function(){asciidoc.footnotes(); asciidoc.toc(2);}, false);
/*]]>*/
</script>
<script type="text/javascript" src="/js/asciidoc-xhtml11.js"></script>
</head>
<body>
    <header>
        <a class="logo" href="/">
            <img src="https://i3wm.org/img/logo.svg" alt="i3 WM logo" />
        </a>
        <nav>
            <ul>
                <li><a style="border-bottom: 2px solid #fff" href="/docs">Docs</a></li>
                <li><a href="/screenshots">Screens</a></li>
                <li><a href="https://www.reddit.com/r/i3wm/">FAQ</a></li>
                <li><a href="/contact">Contact</a></li>
                <li><a href="https://github.com/i3/i3/issues">Bugs</a></li>
            </ul>
        </nav>
    </header>
    <main>
`

const footer = `</main>
</body>
</html>
`

type translationStatusNode struct {
	ast.BaseBlock

	translatedVersion string
}

// Kind implements Node.Kind
func (n *translationStatusNode) Kind() ast.NodeKind {
	return kindTranslationStatus
}

// Dump implements Node.Dump
func (n *translationStatusNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

var kindTranslationStatus = ast.NewNodeKind("TranslationStatus")

type versionNode struct {
	ast.BaseBlock

	since string
}

// Kind implements Node.Kind
func (n *versionNode) Kind() ast.NodeKind {
	return kindVersionNode
}

// Dump implements Node.Dump
func (n *versionNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

var kindVersionNode = ast.NewNodeKind("VersionNode")

type tl8transformer struct{}

func modifyASTFromHeading(heading ast.Node) {
	tsn := &translationStatusNode{}
	vsn := &versionNode{}
	for _, attr := range heading.Attributes() {
		b, ok := attr.Value.([]byte)
		if !ok {
			continue
		}
		val := string(b)
		if string(attr.Name) == "class" && strings.HasPrefix(val, "since_") {
			vsn.since = strings.TrimPrefix(val, "since_")
		}
		if string(attr.Name) == "translated" {
			tsn.translatedVersion = val
		}
		//log.Printf("heading attr, name=%s, value=%s", attr.Name, val)
	}

	if vsn.since != "" {
		// Adding a child will make it part of the heading HTML element
		// (e.g. <h1>)
		heading.AppendChild(heading, vsn)
	}

	if tsn.translatedVersion != "" {
		// Insert the TranslationStatus node after the heading:
		tsn.SetNextSibling(heading.NextSibling())
		heading.SetNextSibling(tsn)
	}
}

// Transform is called once per document.
func (t *tl8transformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		//log.Printf("ast.Walk(type=%v, kind=%v)", n.Type(), n.Kind())
		if n.Type() == ast.TypeDocument {
			return ast.WalkContinue, nil
		}
		if n.Kind() == ast.KindHeading && entering {
			modifyASTFromHeading(n)
		}
		return ast.WalkSkipChildren, nil
	})
}

func numericVersionToHuman(v string) string {
	return strings.ReplaceAll(v, "_", ".")
}

type tl8renderer struct{}

func (r *tl8renderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindTranslationStatus, r.renderTranslationStatus)
	reg.Register(kindVersionNode, r.renderVersion)
}

func (r *tl8renderer) renderVersion(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	vsn := node.(*versionNode)
	fmt.Fprintf(w, `<span class="sinceversion">since i3 v%s</span>`, numericVersionToHuman(vsn.since))
	return ast.WalkContinue, nil
}

func (r *tl8renderer) renderTranslationStatus(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	tsn := node.(*translationStatusNode)

	// NOTE: Ideally we would link to a list of commits for the relevant file
	// between the last-translated version and the current version.
	// Unfortunately, GitHub does not seem to provide such a view.

	translatedVersion := numericVersionToHuman(tsn.translatedVersion)
	fmt.Fprintf(w, `<i>
Out-of-date! This section’s translation was last updated for i3 v%s
(<a href="https://github.com/i3/i3/commits/next/docs/%s">what changed?</a>) 
(<a href="https://github.com/i3/i3/edit/next/docs/%s">contribute</a>)
</i>`,
		translatedVersion,
		"userguide", /* TODO */
		"userguide" /* TODO */)
	return ast.WalkContinue, nil
}

func render1(fn string) error {
	outfn := strings.TrimSuffix(fn, filepath.Ext(fn)) + ".html"
	source, err := ioutil.ReadFile(fn)
	if err != nil {
		return err
	}

	md := goldmark.New(
		// GFM is GitHub Flavored Markdown, which we need for tables, for
		// example.
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			// The Attribute option allows us to id, classes, and arbitrary
			// options on headings (for translation status).
			parser.WithAttribute(),
			parser.WithASTTransformers(util.Prioritized(&tl8transformer{}, 1)),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
			renderer.WithNodeRenderers(
				util.Prioritized(&tl8renderer{}, 500)),
		),
	)

	out, err := renameio.TempFile("", outfn)
	if err != nil {
		return err
	}

	out.Write([]byte(preamble))

	if err := md.Convert(source, out); err != nil {
		return err
	}

	out.Write([]byte(footer))

	if err := out.CloseAtomicallyReplace(); err != nil {
		return err
	}
	return nil
}

func tl8render() error {
	flag.Parse()
	if flag.NArg() < 1 {
		return fmt.Errorf("syntax: %s <markdown-file> [<markdown-file>...]", filepath.Base(os.Args[0]))
	}

	for _, fn := range flag.Args() {
		if err := render1(fn); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	if err := tl8render(); err != nil {
		log.Fatal(err)
	}
}
