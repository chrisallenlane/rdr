// Package sanitize provides HTML sanitisation and content-processing helpers.
package sanitize

import (
	"bytes"
	"fmt"
	htmlpkg "html"
	"html/template"
	"net/url"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
)

// relativeURLRe matches src="..." and href="..." attributes.
var relativeURLRe = regexp.MustCompile(`(src|href)="([^"]*)"`)

// codeBlockRe matches <pre><code class="language-xxx">...</code></pre> blocks,
// with or without a language class.
var codeBlockRe = regexp.MustCompile(
	`(?s)<pre><code(?:\s+class="language-([^"]*)")?>(.+?)</code></pre>`,
)

// chromaFormatter is the shared Chroma HTML formatter (CSS classes mode).
var chromaFormatter = chromahtml.New(chromahtml.WithClasses(true))

var sanitizePolicy *bluemonday.Policy

// strictPolicy is a shared StrictPolicy instance used by Snippet.
var strictPolicy *bluemonday.Policy

// blockTagRe matches block-level HTML elements.
var blockTagRe = regexp.MustCompile(
	`(?i)<(p|div|h[1-6]|blockquote|ul|ol|table|pre)[\s>]`,
)

func init() {
	sanitizePolicy = bluemonday.UGCPolicy()
	sanitizePolicy.AllowElements("figure", "figcaption", "picture", "source")
	sanitizePolicy.AllowAttrs("srcset", "sizes", "media").OnElements("source")
	sanitizePolicy.AllowAttrs("loading").OnElements("img")
	// Allow language class on <code> for syntax highlighting detection.
	sanitizePolicy.AllowAttrs("class").
		Matching(regexp.MustCompile(`^language-[\w+-]+$`)).
		OnElements("code")
	// UGCPolicy() strips inline styles by default — do NOT call AllowStyles()

	strictPolicy = bluemonday.StrictPolicy()
}

// HTML sanitises untrusted HTML using the shared bluemonday policy.
// If the result contains no block-level elements it is wrapped in <p> tags
// so that plain-text feeds are rendered properly.
func HTML(raw string) template.HTML {
	clean := sanitizePolicy.Sanitize(raw)

	// Wrap in <p> when no block-level tags are present (plain-text feeds).
	if !blockTagRe.MatchString(clean) {
		clean = "<p>" + clean + "</p>"
	}

	return template.HTML(clean)
}

// ResolveRelativeURLs rewrites relative src and href attributes in HTML
// content to absolute URLs using the given base URL. This allows images
// and links with relative paths to load correctly when displayed outside
// their original context.
func ResolveRelativeURLs(content, baseURL string) string {
	if baseURL == "" {
		return content
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return content
	}
	// Use the directory of the URL as the base, not the URL itself.
	if !strings.HasSuffix(base.Path, "/") {
		if i := strings.LastIndex(base.Path, "/"); i >= 0 {
			base.Path = base.Path[:i+1]
		}
	}

	return relativeURLRe.ReplaceAllStringFunc(content, func(match string) string {
		parts := relativeURLRe.FindStringSubmatch(match)
		attr, val := parts[1], parts[2]
		if val == "" || strings.HasPrefix(val, "#") {
			return match
		}
		u, err := url.Parse(val)
		if err != nil || u.IsAbs() {
			return match
		}
		// For absolute paths (/foo/bar), prepend the origin.
		// For relative paths (foo/bar), resolve against the base directory.
		resolved := base.ResolveReference(u)
		return fmt.Sprintf(`%s="%s"`, attr, resolved.String())
	})
}

// HighlightCodeBlocks finds <pre><code> blocks in sanitized HTML and
// applies syntax highlighting via Chroma. If a language class is present
// (e.g. class="language-go"), it is used directly. Otherwise Chroma's
// auto-detection is attempted, falling back to plain text.
func HighlightCodeBlocks(html string) string {
	return codeBlockRe.ReplaceAllStringFunc(html, func(match string) string {
		parts := codeBlockRe.FindStringSubmatch(match)
		lang := parts[1] // may be empty
		code := parts[2]

		// Decode HTML entities back to plain text for the lexer.
		code = htmlpkg.UnescapeString(code)

		// Pick a lexer: explicit language, auto-detect, or fallback.
		var lexer chroma.Lexer
		if lang != "" {
			lexer = lexers.Get(lang)
		}
		if lexer == nil {
			lexer = lexers.Analyse(code)
		}
		if lexer == nil {
			lexer = lexers.Fallback
		}
		lexer = chroma.Coalesce(lexer)

		tokens, err := lexer.Tokenise(nil, code)
		if err != nil {
			return match // leave original on error
		}

		var buf bytes.Buffer
		// Style is irrelevant in CSS-class mode, but the API requires one.
		if err := chromaFormatter.Format(&buf, styles.Fallback, tokens); err != nil {
			return match
		}
		return buf.String()
	})
}

// Snippet strips HTML from an FTS5 snippet, then replaces the plain-text
// highlight sentinels with <mark> tags for safe rendering.
func Snippet(s string) template.HTML {
	clean := strictPolicy.Sanitize(s)
	clean = strings.ReplaceAll(clean, "[[HIGHLIGHT]]", "<mark>")
	clean = strings.ReplaceAll(clean, "[[/HIGHLIGHT]]", "</mark>")
	return template.HTML(clean)
}
