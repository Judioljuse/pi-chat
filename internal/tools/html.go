package tools

import (
	"io"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// skipped elements hold no readable text (or only page chrome).
var skipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true, atom.Svg: true,
	atom.Nav: true, atom.Footer: true, atom.Aside: true,
	atom.Iframe: true, atom.Object: true, atom.Canvas: true, atom.Select: true, atom.Button: true,
}

// block elements end a line of text.
var block = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Br: true, atom.Li: true, atom.Tr: true, atom.H1: true, atom.H2: true,
	atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true, atom.Section: true, atom.Article: true,
	atom.Blockquote: true, atom.Pre: true, atom.Table: true, atom.Ul: true, atom.Ol: true, atom.Dd: true,
	atom.Dt: true, atom.Hr: true, atom.Main: true, atom.Figcaption: true, atom.Td: true, atom.Th: true,
}

// ExtractHTML returns the page title and its readable text with whitespace collapsed.
// It stops reading after maxBody bytes of input, whatever the caller passed.
func ExtractHTML(r io.Reader) (title, text string) {
	z := html.NewTokenizer(io.LimitReader(r, maxBody))
	z.SetMaxBuf(maxBody)
	var b strings.Builder
	var line strings.Builder
	var tb strings.Builder
	skip := 0 // depth inside skipped elements
	inTitle := false
	flush := func() {
		s := strings.Join(strings.Fields(line.String()), " ")
		line.Reset()
		if s != "" {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	for b.Len() < maxPageChars*4 {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			flush()
			return strings.Join(strings.Fields(tb.String()), " "), strings.TrimSpace(b.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			a := atom.Lookup(name)
			if a == atom.Title {
				inTitle = tt == html.StartTagToken
				continue
			}
			if skipped[a] && tt == html.StartTagToken && !voidElement(a) {
				skip++
				continue
			}
			if block[a] && skip == 0 {
				flush()
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			a := atom.Lookup(name)
			if a == atom.Title {
				inTitle = false
				continue
			}
			if skipped[a] && skip > 0 {
				skip--
				continue
			}
			if block[a] && skip == 0 {
				flush()
			}
		case html.TextToken:
			if inTitle {
				tb.Write(z.Text())
				continue
			}
			if skip == 0 {
				line.Write(z.Text())
				line.WriteByte(' ')
			}
		}
	}
	flush()
	return strings.Join(strings.Fields(tb.String()), " "), strings.TrimSpace(b.String())
}

func voidElement(a atom.Atom) bool {
	switch a {
	case atom.Br, atom.Hr, atom.Img, atom.Input, atom.Meta, atom.Link, atom.Area, atom.Base, atom.Col, atom.Embed, atom.Source, atom.Track, atom.Wbr:
		return true
	}
	return false
}
