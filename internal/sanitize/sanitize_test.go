package sanitize

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:     "script tags are stripped",
			input:    `<p>Hello</p><script>alert('xss')</script>`,
			contains: []string{"Hello"},
			excludes: []string{"<script>", "alert("},
		},
		{
			name:     "p tag is preserved",
			input:    `<p>Hello world</p>`,
			contains: []string{"<p>", "Hello world", "</p>"},
			excludes: []string{},
		},
		{
			name:     "strong tag is preserved",
			input:    `<p><strong>Bold text</strong></p>`,
			contains: []string{"<strong>", "Bold text", "</strong>"},
			excludes: []string{},
		},
		{
			name:     "a tag is preserved",
			input:    `<p><a href="https://example.com">link</a></p>`,
			contains: []string{`<a href="https://example.com"`, "link", "</a>"},
			excludes: []string{},
		},
		{
			name:     "img tag is preserved",
			input:    `<p><img src="https://example.com/img.png" alt="image"></p>`,
			contains: []string{"<img", `src="https://example.com/img.png"`, `alt="image"`},
			excludes: []string{},
		},
		{
			name:     "javascript URLs are stripped from href",
			input:    `<a href="javascript:alert('xss')">click me</a>`,
			excludes: []string{"javascript:"},
		},
		{
			name:     "javascript URLs are stripped from src",
			input:    `<img src="javascript:alert('xss')">`,
			excludes: []string{"javascript:"},
		},
		{
			name:     "inline styles are stripped",
			input:    `<p style="color:red;font-size:9999px">styled</p>`,
			contains: []string{"styled"},
			excludes: []string{"style=", "color:red"},
		},
		{
			name:     "plain text gets wrapped in p tags",
			input:    `just some plain text`,
			contains: []string{"<p>", "just some plain text", "</p>"},
			excludes: []string{},
		},
		{
			name:     "text with only inline elements gets wrapped in p tags",
			input:    `<strong>bold</strong> and <em>italic</em>`,
			contains: []string{"<p>", "<strong>bold</strong>", "</p>"},
			excludes: []string{},
		},
		{
			name:     "content with block elements is not double-wrapped",
			input:    `<p>already a paragraph</p>`,
			contains: []string{"<p>already a paragraph</p>"},
			// Ensure it is not wrapped in a second <p>
			excludes: []string{"<p><p>"},
		},
		{
			name:     "empty string returns empty",
			input:    "",
			contains: []string{},
			// bluemonday returns "" for empty input; wrapping gives "<p></p>"
			// The function wraps even empty input since no block tags are present.
			// Just verify no panic and no harmful content.
			excludes: []string{"<script>"},
		},
		{
			name:     "div block element is not double-wrapped",
			input:    `<div><p>content</p></div>`,
			contains: []string{"content"},
			excludes: []string{"<script>"},
		},
		{
			name:     "standalone figure is not wrapped in p",
			input:    `<figure><img src="https://example.com/i.png" alt="x"></figure>`,
			contains: []string{"<figure"},
			excludes: []string{"<p><figure"},
		},
		{
			name:     "event handler attributes are stripped",
			input:    `<p onclick="alert('xss')">click</p>`,
			contains: []string{"click"},
			excludes: []string{"onclick", "alert("},
		},
		{
			name:     "video element is preserved and not wrapped in p",
			input:    `<video controls src="https://example.com/v.mp4"></video>`,
			contains: []string{"<video", "controls", `src="https://example.com/v.mp4"`},
			excludes: []string{"<p><video"},
		},
		{
			name:     "audio element is preserved and not wrapped in p",
			input:    `<audio controls src="https://example.com/a.mp3"></audio>`,
			contains: []string{"<audio", "controls", `src="https://example.com/a.mp3"`},
			excludes: []string{"<p><audio"},
		},
		{
			name:     "video autoplay is stripped",
			input:    `<video controls autoplay src="https://example.com/v.mp4"></video>`,
			contains: []string{"<video", "controls"},
			excludes: []string{"autoplay"},
		},
		{
			name:     "video loop is stripped",
			input:    `<video controls loop src="https://example.com/v.mp4"></video>`,
			contains: []string{"<video", "controls"},
			excludes: []string{"loop"},
		},
		{
			name:     "video onerror event handler is stripped",
			input:    `<video controls src="https://example.com/v.mp4" onerror="alert(1)"></video>`,
			contains: []string{"<video", "controls"},
			excludes: []string{"onerror", "alert(1)"},
		},
		{
			name:     "javascript URL in video src is stripped",
			input:    `<video controls src="javascript:alert(1)"></video>`,
			excludes: []string{"javascript:"},
		},
		{
			name:     "iframe is not permitted",
			input:    `<iframe src="https://example.com"></iframe>`,
			excludes: []string{"<iframe"},
		},
		{
			name:     "source element type and src attrs are preserved",
			input:    `<video controls><source src="https://example.com/v.mp4" type="video/mp4"></video>`,
			contains: []string{`src="https://example.com/v.mp4"`, `type="video/mp4"`},
			excludes: []string{"<script>"},
		},
		{
			name:     "audio onerror event handler is stripped",
			input:    `<audio controls src="https://example.com/a.mp3" onerror="alert(1)"></audio>`,
			contains: []string{"<audio", "controls"},
			excludes: []string{"onerror", "alert(1)"},
		},
		{
			name:     "audio onclick event handler is stripped",
			input:    `<audio controls src="https://example.com/a.mp3" onclick="evil()"></audio>`,
			contains: []string{"<audio", "controls"},
			excludes: []string{"onclick", "evil()"},
		},
		{
			name:     "source onerror event handler is stripped",
			input:    `<video controls><source src="https://example.com/v.mp4" onerror="alert(1)"></video>`,
			contains: []string{`src="https://example.com/v.mp4"`},
			excludes: []string{"onerror", "alert(1)"},
		},
		{
			name:     "source onload event handler is stripped",
			input:    `<video controls><source src="https://example.com/v.mp4" onload="evil()"></video>`,
			contains: []string{`src="https://example.com/v.mp4"`},
			excludes: []string{"onload", "evil()"},
		},
		{
			name:     "video poster with javascript URL is stripped",
			input:    `<video controls poster="javascript:alert(1)" src="https://example.com/v.mp4"></video>`,
			contains: []string{"<video", "controls"},
			excludes: []string{"javascript:", "poster="},
		},
		{
			name:     "video poster with https URL is preserved",
			input:    `<video controls poster="https://example.com/thumb.jpg" src="https://example.com/v.mp4"></video>`,
			contains: []string{"<video", `poster="https://example.com/thumb.jpg"`},
			excludes: []string{"javascript:"},
		},
		{
			name:     "source src with javascript URL is stripped",
			input:    `<video controls><source src="javascript:alert(1)" type="video/mp4"></video>`,
			contains: []string{"<video"},
			excludes: []string{"javascript:", "alert(1)"},
		},
		{
			name:     "source srcset is stripped entirely",
			input:    `<picture><source srcset="https://example.com/a.jpg 1x, https://attacker.example/beacon 2x" type="image/webp"></picture>`,
			excludes: []string{"srcset", "attacker.example", "beacon"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(HTML(tt.input))
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf(
						"HTML(%q): expected result to contain %q, got %q",
						tt.input, want, result,
					)
				}
			}
			for _, unwanted := range tt.excludes {
				if strings.Contains(result, unwanted) {
					t.Errorf(
						"HTML(%q): expected result NOT to contain %q, got %q",
						tt.input, unwanted, result,
					)
				}
			}
		})
	}
}

func TestSnippet(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:     "HIGHLIGHT sentinel becomes mark open tag",
			input:    `word [[HIGHLIGHT]]term[[/HIGHLIGHT]] word`,
			contains: []string{"<mark>", "term", "</mark>"},
			excludes: []string{"[[HIGHLIGHT]]", "[[/HIGHLIGHT]]"},
		},
		{
			name:     "HIGHLIGHT replaced and surrounding text preserved",
			input:    `before [[HIGHLIGHT]]match[[/HIGHLIGHT]] after`,
			contains: []string{"before", "<mark>match</mark>", "after"},
			excludes: []string{},
		},
		{
			name:     "multiple highlights in one snippet",
			input:    `[[HIGHLIGHT]]a[[/HIGHLIGHT]] and [[HIGHLIGHT]]b[[/HIGHLIGHT]]`,
			contains: []string{"<mark>a</mark>", "<mark>b</mark>"},
			excludes: []string{"[[HIGHLIGHT]]", "[[/HIGHLIGHT]]"},
		},
		{
			name:     "HTML in snippet is stripped",
			input:    `<b>bold</b> [[HIGHLIGHT]]term[[/HIGHLIGHT]]`,
			contains: []string{"<mark>term</mark>"},
			excludes: []string{"<b>", "</b>"},
		},
		{
			name:     "script tag in snippet is stripped",
			input:    `[[HIGHLIGHT]]word[[/HIGHLIGHT]] <script>evil()</script>`,
			contains: []string{"<mark>word</mark>"},
			excludes: []string{"<script>", "evil()"},
		},
		{
			name:     "script tag with sentinel cannot leak through",
			input:    `<script>[[HIGHLIGHT]]code[[/HIGHLIGHT]]</script>`,
			excludes: []string{"<script>", "</script>"},
		},
		{
			name:     "inline event handler in snippet is stripped",
			input:    `<span onclick="alert(1)">[[HIGHLIGHT]]x[[/HIGHLIGHT]]</span>`,
			contains: []string{"<mark>x</mark>"},
			excludes: []string{"onclick", "alert(1)"},
		},
		{
			name:     "no sentinels returns plain text without mark tags",
			input:    `just a plain snippet`,
			contains: []string{"just a plain snippet"},
			excludes: []string{"<mark>", "</mark>"},
		},
		{
			name:     "empty string returns empty",
			input:    "",
			contains: []string{""},
			excludes: []string{"<mark>", "<script>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(Snippet(tt.input))
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf(
						"Snippet(%q): expected result to contain %q, got %q",
						tt.input, want, result,
					)
				}
			}
			for _, unwanted := range tt.excludes {
				if strings.Contains(result, unwanted) {
					t.Errorf(
						"Snippet(%q): expected result NOT to contain %q, got %q",
						tt.input, unwanted, result,
					)
				}
			}
		})
	}
}

func TestResolveRelativeURLs(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		baseURL  string
		contains []string
		excludes []string
	}{
		{
			name:     "relative src resolved against base directory",
			content:  `<img src="images/foo.png">`,
			baseURL:  "https://example.com/blog/post",
			contains: []string{`src="https://example.com/blog/images/foo.png"`},
			excludes: []string{`src="images/foo.png"`},
		},
		{
			name:     "absolute path src resolved to origin",
			content:  `<img src="/images/foo.png">`,
			baseURL:  "https://example.com/blog/post",
			contains: []string{`src="https://example.com/images/foo.png"`},
			excludes: []string{`src="/images/foo.png"`},
		},
		{
			name:     "already-absolute href is unchanged",
			content:  `<a href="https://other.com/page">link</a>`,
			baseURL:  "https://example.com/blog/post",
			contains: []string{`href="https://other.com/page"`},
			excludes: []string{},
		},
		{
			name:     "fragment-only href is unchanged",
			content:  `<a href="#section">anchor</a>`,
			baseURL:  "https://example.com/blog/post",
			contains: []string{`href="#section"`},
			excludes: []string{},
		},
		{
			name:     "empty href is unchanged",
			content:  `<a href="">empty</a>`,
			baseURL:  "https://example.com/blog/post",
			contains: []string{`href=""`},
			excludes: []string{},
		},
		{
			name:     "empty base URL returns content unchanged",
			content:  `<img src="images/foo.png">`,
			baseURL:  "",
			contains: []string{`src="images/foo.png"`},
			excludes: []string{},
		},
		{
			name:     "invalid base URL returns content unchanged",
			content:  `<img src="images/foo.png">`,
			baseURL:  "://not a valid url",
			contains: []string{`src="images/foo.png"`},
			excludes: []string{},
		},
		{
			name: "multiple attributes in one string are all resolved",
			content: `<img src="a.png"><a href="b.html">link</a>` +
				`<img src="c.png">`,
			baseURL: "https://example.com/dir/page",
			contains: []string{
				`src="https://example.com/dir/a.png"`,
				`href="https://example.com/dir/b.html"`,
				`src="https://example.com/dir/c.png"`,
			},
			excludes: []string{
				`src="a.png"`,
				`href="b.html"`,
				`src="c.png"`,
			},
		},
		{
			name:     "base URL with trailing slash works correctly",
			content:  `<img src="images/foo.png">`,
			baseURL:  "https://example.com/blog/",
			contains: []string{`src="https://example.com/blog/images/foo.png"`},
			excludes: []string{`src="images/foo.png"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveRelativeURLs(tt.content, tt.baseURL)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf(
						"ResolveRelativeURLs(%q, %q): expected result to"+
							" contain %q, got %q",
						tt.content, tt.baseURL, want, result,
					)
				}
			}
			for _, unwanted := range tt.excludes {
				if strings.Contains(result, unwanted) {
					t.Errorf(
						"ResolveRelativeURLs(%q, %q): expected result NOT"+
							" to contain %q, got %q",
						tt.content, tt.baseURL, unwanted, result,
					)
				}
			}
		})
	}
}

// TestResolveRelativeURLs_UnsafeBaseURL exercises ResolveRelativeURLs
// with a base URL that itself carries a dangerous scheme. This case
// can occur in production when items.url contains a javascript:,
// data:, or file: URL — the ingest path (resolveLink in
// internal/poller/feed.go) does NOT validate URL schemes, so any
// absolute URL is stored verbatim and later passed as the base when
// rendering item content.
//
// The function does not crash, and its output IS NOT directly safe —
// it produces strings like `src="javascript:///images/foo.png"`
// inside the HTML it returns. Defense-in-depth comes from the
// downstream sanitize.HTML call (bluemonday UGCPolicy), which strips
// hrefs/srcs with non-allow-listed schemes.
//
// The cross-pipeline assertion in TestResolveRelativeURLs_UnsafeBaseURL_FullPipeline
// confirms the full chain — ResolveRelativeURLs → HTML — produces
// output free of executable URL schemes. The standalone tests here
// pin the intermediate (unsanitized) output so a future change that
// alters the pipeline order is forced to re-prove safety.
func TestResolveRelativeURLs_UnsafeBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		baseURL  string
		contains []string
	}{
		{
			name:    "javascript base + relative src produces javascript-scheme URL",
			content: `<img src="images/foo.png">`,
			baseURL: "javascript:alert(1)",
			// url.URL.ResolveReference with an opaque base produces
			// "javascript:///images/foo.png". Pinning this so any
			// change to the resolver is visible.
			contains: []string{`javascript:///images/foo.png`},
		},
		{
			name:     "javascript base + absolute-path href",
			content:  `<a href="/page.html">x</a>`,
			baseURL:  "javascript:foo",
			contains: []string{`javascript:///page.html`},
		},
		{
			name:     "data base + relative src",
			content:  `<img src="x.png">`,
			baseURL:  "data:text/html,<b>x</b>",
			contains: []string{`data:///x.png`},
		},
		{
			name:     "file base + relative src",
			content:  `<img src="x.png">`,
			baseURL:  "file:///etc/passwd",
			contains: []string{`file:///etc/x.png`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRelativeURLs(tt.content, tt.baseURL)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf(
						"ResolveRelativeURLs(%q, %q) = %q, want substring %q",
						tt.content, tt.baseURL, got, want,
					)
				}
			}
		})
	}
}

// TestResolveRelativeURLs_UnsafeBaseURL_FullPipeline exercises the
// production sequence (handler/item_detail.go:105-106):
//
//	content := sanitize.ResolveRelativeURLs(item.Content, item.URL)
//	content = string(sanitize.HTML(content))
//
// when item.URL carries a dangerous scheme. The contract: the final
// HTML produced by this pipeline MUST NOT contain executable href or
// src attribute values pointing at javascript:, data:, file:, or
// vbscript: schemes.
//
// This is a defense-in-depth assertion — even if resolveLink (the
// ingest-time validation) never gains a scheme allow-list, the
// downstream HTML sanitizer is the safety net.
func TestResolveRelativeURLs_UnsafeBaseURL_FullPipeline(t *testing.T) {
	cases := []struct {
		name, content, base string
	}{
		{
			name:    "javascript base, relative img",
			content: `<p>see <img src="img.png"> here</p>`,
			base:    "javascript:alert(1)",
		},
		{
			name:    "javascript base, relative anchor",
			content: `<p><a href="page.html">click</a></p>`,
			base:    "javascript:alert(1)",
		},
		{
			name:    "data base, relative img",
			content: `<p><img src="img.png"></p>`,
			base:    "data:text/html,<b>x</b>",
		},
		{
			name:    "file base, relative anchor",
			content: `<p><a href="page.html">x</a></p>`,
			base:    "file:///etc/passwd",
		},
		{
			name:    "vbscript-looking base (parses as scheme), relative img",
			content: `<p><img src="img.png"></p>`,
			base:    "vbscript:msgbox(1)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := ResolveRelativeURLs(tc.content, tc.base)
			final := string(HTML(resolved))
			lower := strings.ToLower(final)
			// None of the dangerous schemes may appear in a finished
			// href or src attribute value.
			for _, scheme := range []string{"javascript", "data", "file", "vbscript"} {
				bad := []string{
					`href="` + scheme + `:`,
					`href='` + scheme + `:`,
					`src="` + scheme + `:`,
					`src='` + scheme + `:`,
				}
				for _, b := range bad {
					if strings.Contains(lower, b) {
						t.Errorf(
							"pipeline produced unsafe attribute %q\n  base=%q\n  content=%q\n  final=%q",
							b, tc.base, tc.content, final,
						)
					}
				}
			}
		})
	}
}

// TestResolveRelativeURLs_AttributeQuoting probes the regex used by
// ResolveRelativeURLs (`(src|href)="([^"]*)"`) against attribute-quoting
// styles that real-world HTML emits but the regex does not match.
//
// The intent of these tests is to PIN the limitation: feeds that emit
// any quoting style other than double-quoted attributes are silently
// skipped by ResolveRelativeURLs, leaving relative URLs unresolved in
// the rendered article. This is a UX bug (broken images / dead links),
// not a security bug — the downstream bluemonday sanitizer enforces
// scheme allow-lists.
//
// Fix guidance: replace the regex with a proper HTML parser
// (golang.org/x/net/html). The parser normalises attribute syntax and
// would correctly handle every case below.
func TestResolveRelativeURLs_AttributeQuoting(t *testing.T) {
	const base = "https://example.com/blog/post"
	const wantResolved = "https://example.com/blog/images/foo.png"

	tests := []struct {
		name        string
		content     string
		wantResolve bool // true if a properly-quoted attribute resolves
		note        string
	}{
		{
			name:        "double-quoted src (regex matches)",
			content:     `<img src="images/foo.png">`,
			wantResolve: true,
			note:        "control: baseline behaviour",
		},
		{
			name:        "single-quoted src",
			content:     `<img src='images/foo.png'>`,
			wantResolve: true,
			note:        "feeds using single quotes are silently skipped",
		},
		{
			name:        "unquoted src",
			content:     `<img src=images/foo.png>`,
			wantResolve: true,
			note:        "HTML5 allows unquoted attribute values",
		},
		{
			name:        "single-quoted href",
			content:     `<a href='images/foo.png'>x</a>`,
			wantResolve: true,
			note:        "same problem for href",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRelativeURLs(tt.content, base)
			resolved := strings.Contains(got, wantResolved)
			if tt.wantResolve && !resolved {
				t.Errorf(
					"ResolveRelativeURLs(%q) = %q; expected to contain %q (note: %s)",
					tt.content, got, wantResolved, tt.note,
				)
			}
		})
	}
}

// TestResolveRelativeURLs_AttributeNameBoundaries probes the regex's
// lack of word-boundary anchoring on the attribute name. The regex
// `(src|href)="..."` matches any attribute whose NAME ends in "src"
// or "href" — e.g. `data-src`, `data-href`, `srcset` partially.
//
// The most important false-positive is `data-src="..."` (lazy-loading
// images): the regex sees `src="..."` inside the longer attribute and
// rewrites the value. Lazy-loading content using `data-src` paired with
// a placeholder `src` would have its real URL rewritten to an absolute
// form — possibly correct, possibly not, but in either case it is the
// wrong attribute to act on.
func TestResolveRelativeURLs_AttributeNameBoundaries(t *testing.T) {
	const base = "https://example.com/blog/post"

	tests := []struct {
		name              string
		content           string
		wantUnchangedAttr string // substring that must remain in output
		note              string
	}{
		{
			name:              "data-src is not src",
			content:           `<img data-src="images/foo.png">`,
			wantUnchangedAttr: `data-src="images/foo.png"`,
			note:              "regex has no word boundary; data-src is matched",
		},
		{
			name:              "data-href is not href",
			content:           `<a data-href="page.html">x</a>`,
			wantUnchangedAttr: `data-href="page.html"`,
			note:              "same problem for href",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRelativeURLs(tt.content, base)
			if !strings.Contains(got, tt.wantUnchangedAttr) {
				t.Errorf(
					"ResolveRelativeURLs(%q) = %q; expected attribute %q to be untouched (note: %s)",
					tt.content, got, tt.wantUnchangedAttr, tt.note,
				)
			}
		})
	}
}

// TestResolveRelativeURLs_OtherURLAttributes pins the set of URL-bearing
// HTML attributes that the regex does NOT handle. Each of these can
// legitimately appear in feed content (especially Media RSS / podcasts)
// with a relative value; the regex skips them and the relative form
// survives into the rendered page.
//
// srcset is interesting because it is stripped by bluemonday (the
// source srcset test in TestHTML confirms this), so in the current
// pipeline the relative URL is moot — the whole attr is removed. But
// poster on <video> is allow-listed by the sanitiser (so long as the
// value matches ^https?://) and would survive to the user.
//
// For each attribute below, the unresolved relative value is asserted
// to survive the rewriter. This is a UX bug, not a security bug:
// poster fails the sanitiser's https? match and gets dropped; form
// is dropped by UGCPolicy; srcset is dropped on source. Net effect:
// these attributes vanish entirely instead of being resolved + kept.
func TestResolveRelativeURLs_OtherURLAttributes(t *testing.T) {
	const base = "https://example.com/blog/post"

	tests := []struct {
		name string
		// content fed to ResolveRelativeURLs.
		content string
		// attribute substring that must still appear in the output,
		// demonstrating that the rewriter did not resolve it.
		wantUnresolved string
		note           string
	}{
		{
			name:           "srcset is not resolved",
			content:        `<img srcset="small.jpg 1x, large.jpg 2x">`,
			wantUnresolved: `srcset="small.jpg 1x, large.jpg 2x"`,
			note:           "srcset URLs stay relative",
		},
		{
			name:           "video poster is not resolved",
			content:        `<video poster="trailer-thumb.jpg"></video>`,
			wantUnresolved: `poster="trailer-thumb.jpg"`,
			note:           "poster on <video> stays relative",
		},
		{
			name:           "form action is not resolved",
			content:        `<form action="submit"><input></form>`,
			wantUnresolved: `action="submit"`,
			note:           "form action stays relative (bluemonday strips forms anyway)",
		},
		{
			name:           "object data is not resolved",
			content:        `<object data="movie.swf"></object>`,
			wantUnresolved: `data="movie.swf"`,
			note:           "<object data> is the canonical URL attribute on <object>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRelativeURLs(tt.content, base)
			if !strings.Contains(got, tt.wantUnresolved) {
				t.Errorf(
					"expected unresolved attribute %q in output %q (note: %s)",
					tt.wantUnresolved, got, tt.note,
				)
			}
		})
	}
}

// TestResolveRelativeURLs_ProtocolRelative pins behaviour for
// protocol-relative URLs ("//cdn.example.com/x.jpg"). RFC 3986 calls
// these "network-path references"; they inherit the scheme of the
// base. net/url.ResolveReference handles them correctly.
func TestResolveRelativeURLs_ProtocolRelative(t *testing.T) {
	got := ResolveRelativeURLs(
		`<img src="//cdn.example.com/x.jpg">`,
		"https://example.com/blog/post",
	)
	want := `src="https://cdn.example.com/x.jpg"`
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want substring %q", got, want)
	}
}

// TestResolveRelativeURLs_MailtoAndTel pins behaviour for non-http
// schemes commonly used in feeds (mailto:, tel:). These parse as
// absolute URLs (url.IsAbs() returns true) and so the rewriter leaves
// them alone.
func TestResolveRelativeURLs_MailtoAndTel(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "mailto href is untouched",
			content: `<a href="mailto:x@y.z">mail</a>`,
		},
		{
			name:    "tel href is untouched",
			content: `<a href="tel:+15551234">call</a>`,
		},
	}
	const base = "https://example.com/blog/post"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRelativeURLs(tt.content, base)
			if got != tt.content {
				t.Errorf("got %q, want %q (unchanged)", got, tt.content)
			}
		})
	}
}

func TestHighlightCodeBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name: "go code block produces chroma output",
			input: `<pre><code class="language-go">` +
				`fmt.Println("hello")</code></pre>`,
			contains: []string{`class="chroma"`},
			excludes: []string{},
		},
		{
			name:     "code block without language class still produces output",
			input:    `<pre><code>plain code here</code></pre>`,
			contains: []string{`class="chroma"`},
			excludes: []string{},
		},
		{
			name:     "content with no code blocks is returned unchanged",
			input:    `<p>No code here, just a paragraph.</p>`,
			contains: []string{"<p>No code here, just a paragraph.</p>"},
			excludes: []string{`class="chroma"`},
		},
		{
			name: "HTML entities in code are unescaped before highlighting",
			input: `<pre><code class="language-go">` +
				`if x &lt; 10 &amp;&amp; y &gt; 0 { }</code></pre>`,
			// Chroma produces highlighted HTML output.
			contains: []string{`class="chroma"`},
			// Double-encoding (&amp;lt; etc.) must not appear — the source
			// entities were decoded before tokenisation, not passed raw.
			excludes: []string{"&amp;lt;", "&amp;amp;"},
		},
		{
			name:     "empty input returns empty string",
			input:    "",
			contains: []string{""},
			excludes: []string{`class="chroma"`},
		},
		{
			name:     "content with only regular HTML passes through unchanged",
			input:    `<h1>Title</h1><p>Body text.</p>`,
			contains: []string{"<h1>Title</h1>", "<p>Body text.</p>"},
			excludes: []string{`class="chroma"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HighlightCodeBlocks(tt.input)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf(
						"HighlightCodeBlocks(%q): expected result to"+
							" contain %q, got %q",
						tt.input, want, result,
					)
				}
			}
			for _, unwanted := range tt.excludes {
				if strings.Contains(result, unwanted) {
					t.Errorf(
						"HighlightCodeBlocks(%q): expected result NOT"+
							" to contain %q, got %q",
						tt.input, unwanted, result,
					)
				}
			}
		})
	}
}

func FuzzHTML(f *testing.F) {
	f.Add("<p>Hello</p>")
	f.Add("<script>alert('xss')</script>")
	f.Add("")
	f.Add("<img src=x onerror=alert(1)>")
	f.Fuzz(func(t *testing.T, input string) {
		result := string(HTML(input))
		if strings.Contains(result, "<script") {
			t.Errorf("output contains <script: %q", result)
		}
		if strings.Contains(strings.ToLower(result), "javascript:") {
			t.Errorf("output contains javascript: %q", result)
		}
	})
}

func FuzzResolveRelativeURLs(f *testing.F) {
	f.Add(`<img src="images/foo.png">`, "https://example.com/blog/post")
	f.Add(`<a href="/page">link</a>`, "https://example.com/")
	f.Add(`<img src="https://other.com/img.png">`, "https://example.com/")
	f.Add(`<a href="#section">anchor</a>`, "https://example.com/post")
	f.Add("", "https://example.com/")
	f.Add(`<img src="x.png">`, "")
	f.Add(`<a href="page.html">x</a>`, "not-a-valid-url")
	f.Fuzz(func(t *testing.T, content, baseURL string) {
		result := ResolveRelativeURLs(content, baseURL)
		// Must never panic (guaranteed by reaching this line).
		// javascript: scheme must never appear in a resolved attribute value.
		if strings.Contains(strings.ToLower(result), `href="javascript:`) ||
			strings.Contains(strings.ToLower(result), `src="javascript:`) {
			t.Errorf(
				"ResolveRelativeURLs produced javascript: URL: %q", result,
			)
		}
	})
}

func FuzzHighlightCodeBlocks(f *testing.F) {
	f.Add(`<pre><code class="language-go">fmt.Println("hello")</code></pre>`)
	f.Add(`<pre><code>plain text</code></pre>`)
	f.Add(`<p>no code blocks here</p>`)
	f.Add("")
	f.Add(`<pre><code class="language-go">` +
		`if x &lt; 10 &amp;&amp; y &gt; 0 { }</code></pre>`)
	f.Fuzz(func(t *testing.T, html string) {
		// Must never panic.
		_ = HighlightCodeBlocks(html)
	})
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short text returned verbatim",
			input:  "hello world",
			maxLen: 100,
			want:   "hello world",
		},
		{
			name:   "block boundaries insert spaces",
			input:  "<p>foo</p><p>bar</p>",
			maxLen: 100,
			want:   "foo bar",
		},
		{
			name:   "HTML entities are decoded",
			input:  "Tom &amp; Jerry",
			maxLen: 100,
			want:   "Tom & Jerry",
		},
		{
			name:   "tags are stripped",
			input:  "<p><strong>Bold</strong> text</p>",
			maxLen: 100,
			want:   "Bold text",
		},
		{
			name: "truncation at last space before maxLen appends ellipsis",
			// Input length 43, maxLen 20. text[:20] = "the quick brown fox "
			// (trailing space at index 19). LastIndex keeps "the quick brown fox",
			// appends "..."
			input:  "the quick brown fox jumps over the lazy dog",
			maxLen: 20,
			want:   "the quick brown fox...",
		},
		{
			name:   "multiple whitespace collapses to single space",
			input:  "foo   bar\n\nbaz",
			maxLen: 100,
			want:   "foo bar baz",
		},
		{
			name:   "exactly maxLen returned unchanged",
			input:  "12345",
			maxLen: 5,
			want:   "12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Summarize(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("Summarize(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func FuzzSummarize(f *testing.F) {
	f.Add("<p>hello world</p>", 100)
	f.Add("plain text", 10)
	f.Add("", 50)
	f.Add("<p>foo</p><p>bar</p>", 15)
	f.Add("a&#x1F600;b", 10) // multi-byte content
	f.Add("text with no spaces for truncation edge case", 5)

	f.Fuzz(func(t *testing.T, raw string, maxLen int) {
		if maxLen <= 0 {
			return
		}
		out := Summarize(raw, maxLen)
		// Output bytes must be valid UTF-8: strictPolicy + entity-decode are
		// UTF-8-safe, but byte-level truncation at text[:maxLen] could cut a
		// rune. The current implementation may leave dangling bytes — this
		// test pins the contract, and if it flags we have a real bug.
		if !utf8.ValidString(out) {
			t.Errorf("Summarize(%q, %d) = %q: result is not valid UTF-8", raw, maxLen, out)
		}
		// Bounded output length.
		if len(out) > maxLen+3 {
			t.Errorf("Summarize(%q, %d) len = %d, want <= %d", raw, maxLen, len(out), maxLen+3)
		}
	})
}

func FuzzSnippet(f *testing.F) {
	f.Add("plain text")
	f.Add("[[HIGHLIGHT]]match[[/HIGHLIGHT]]")
	f.Add("<script>alert(1)</script>")
	f.Add("[[HIGHLIGHT]]<img src=x>[[/HIGHLIGHT]]")
	f.Add("")

	f.Fuzz(func(t *testing.T, s string) {
		out := string(Snippet(s))
		// The only permitted tags in Snippet output are <mark> and </mark>.
		// strictPolicy sanitizes input first; any other tag in output would be
		// a bluemonday-policy escape.
		lower := strings.ToLower(out)
		if i := strings.Index(lower, "<"); i >= 0 {
			// Walk each '<' and verify it begins a mark tag.
			for j := i; j < len(lower); {
				k := strings.Index(lower[j:], "<")
				if k < 0 {
					break
				}
				k += j
				rest := lower[k:]
				if !strings.HasPrefix(rest, "<mark>") && !strings.HasPrefix(rest, "</mark>") {
					t.Errorf("Snippet(%q) = %q: contains unexpected tag at byte %d", s, out, k)
					return
				}
				j = k + 1
			}
		}
	})
}
