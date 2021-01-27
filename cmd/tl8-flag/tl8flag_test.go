package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSegment(t *testing.T) {
	source := []byte(`# document

A paragraph,
which spans multiple lines.

## first heading {#first translated="4_18"}
`)
	doc, err := segment(source)
	if err != nil {
		t.Fatal(err)
	}

	headingDocument := heading{
		Line:       1,
		ID:         "document",
		Translated: "",
	}
	headingFirst := heading{
		Line:       6,
		ID:         "first",
		Translated: "4_18",
	}
	wantHeadings := []heading{
		headingDocument,
		headingFirst,
	}
	if diff := cmp.Diff(wantHeadings, doc.headings); diff != "" {
		t.Errorf("unexpected headings: diff (-want +got):\n%s", diff)
	}

	wantSections := []section{
		{
			Heading: headingDocument,
			Lines: []string{
				"",
				"A paragraph,",
				"which spans multiple lines.",
				"",
			},
		},
		{
			Heading: headingFirst,
			Lines:   []string{},
		},
	}
	if diff := cmp.Diff(wantSections, doc.sections); diff != "" {
		t.Errorf("unexpected sections: diff (-want +got):\n%s", diff)
	}
}

func TestFlag(t *testing.T) {
	oldSource := []byte(`# document {version="4_18"}

Introduction.

## first heading {#first}

Old explanation.

## second heading {#second}

Unchanged explanation.
`)
	frenchOldSource := []byte(`# document {version="4_18"}

Introduction.

## premier titre {#first translated="4_18"}

Ancienne explication.

## deuxième rubrique {#second translated="4_18"}

Explication inchangée.
`)

	tmp := t.TempDir()
	newSource := bytes.ReplaceAll(oldSource, []byte("Old"), []byte("New"))
	newSource = bytes.ReplaceAll(newSource, []byte(`version="4_18"`), []byte(`version="4_19"`))
	fn := filepath.Join(tmp, "userguide.markdown")
	if err := ioutil.WriteFile(fn, newSource, 0644); err != nil {
		t.Fatal(err)
	}
	frenchFn := filepath.Join(tmp, "fr", "userguide.markdown")
	if err := os.MkdirAll(filepath.Dir(frenchFn), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(frenchFn, frenchOldSource, 0644); err != nil {
		t.Fatal(err)
	}

	oldTmp := t.TempDir()
	oldFn := filepath.Join(oldTmp, "userguide.markdown")
	if err := ioutil.WriteFile(oldFn, oldSource, 0644); err != nil {
		t.Fatal(err)
	}

	if err := flag1(fn, oldFn); err != nil {
		t.Fatal(err)
	}

	updatedFrenchSource, err := ioutil.ReadFile(frenchFn)
	if err != nil {
		t.Fatal(err)
	}
	// document and second heading should be updated,
	// first heading should not be updated (→ out of date)
	wantFrenchSource := []byte(`# document {version="4_19"}

Introduction.

## premier titre {#first translated="4_18"}

Ancienne explication.

## deuxième rubrique {#second translated="4_19"}

Explication inchangée.
`)

	if diff := cmp.Diff(wantFrenchSource, updatedFrenchSource); diff != "" {
		t.Errorf("unexpected french translation update: diff (-want +got):\n%s", diff)
	}
}
