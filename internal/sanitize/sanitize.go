// Package sanitize provides HTML sanitisation and content-processing helpers.
package sanitize

import (
	"bytes"
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
	"golang.org/x/net/html"
)

// urlAttrs maps element names to the set of attributes that carry URLs and
// should be resolved against the base URL. Only elements that legitimately
// appear in feed content are listed.
var urlAttrs = map[string]map[string]bool{
	"a":      {"href": true},
	"img":    {"src": true, "srcset": true},
	"source": {"src": true, "srcset": true},
	"video":  {"src": true, "poster": true},
	"audio":  {"src": true},
}

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
	`(?i)<(p|div|h[1-6]|blockquote|ul|ol|table|pre|figure|video|audio)[\s>]`,
)

func init() {
	sanitizePolicy = bluemonday.UGCPolicy()
	sanitizePolicy.AllowElements("figure", "figcaption", "picture", "source")
	sanitizePolicy.AllowAttrs("sizes", "media").OnElements("source")
	sanitizePolicy.AllowAttrs("loading").OnElements("img")
	// Allow language class on <code> for syntax highlighting detection.
	sanitizePolicy.AllowAttrs("class").
		Matching(regexp.MustCompile(`^language-[\w+-]+$`)).
		OnElements("code")
	// UGCPolicy() strips inline styles by default — do NOT call AllowStyles()

	// Allow HTML5 media elements synthesized from Media RSS feeds.
	// autoplay, loop, and event handlers are intentionally NOT allow-listed.
	sanitizePolicy.AllowElements("video", "audio")
	sanitizePolicy.AllowAttrs("controls", "src", "preload").OnElements("video", "audio")
	sanitizePolicy.AllowAttrs("poster").
		Matching(regexp.MustCompile(`(?i)^https?://`)).
		OnElements("video")
	sanitizePolicy.AllowAttrs("type").OnElements("source")
	sanitizePolicy.AllowAttrs("src").
		Matching(regexp.MustCompile(`(?i)^https?://`)).
		OnElements("source")

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

// ResolveRelativeURLs rewrites relative URL attributes in HTML content to
// absolute URLs using the given base URL. This allows images and links with
// relative paths to load correctly when displayed outside their original
// context.
//
// It uses golang.org/x/net/html tokenization so that all attribute quoting
// styles (double-quoted, single-quoted, unquoted) are handled correctly, and
// only exact attribute names are matched (e.g. data-src is never mistaken for
// src).
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

	var buf strings.Builder
	tok := html.NewTokenizer(strings.NewReader(content))
	for {
		tt := tok.Next()
		if tt == html.ErrorToken {
			break
		}

		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			buf.WriteString(tok.Token().String())
			continue
		}

		t := tok.Token()
		attrMap, inMatrix := urlAttrs[t.Data]
		if !inMatrix {
			buf.WriteString(t.String())
			continue
		}

		// Rewrite URL attributes that are in the matrix for this element.
		for i, a := range t.Attr {
			if !attrMap[a.Key] {
				continue
			}
			if a.Key == "srcset" {
				t.Attr[i].Val = resolveSrcset(a.Val, base)
			} else {
				t.Attr[i].Val = resolveURL(a.Val, base)
			}
		}
		buf.WriteString(t.String())
	}
	return buf.String()
}

// resolveURL resolves a single URL value against base. Values that are empty,
// fragment-only, or already absolute are returned unchanged.
func resolveURL(val string, base *url.URL) string {
	if val == "" || strings.HasPrefix(val, "#") {
		return val
	}
	u, err := url.Parse(val)
	if err != nil || u.IsAbs() {
		return val
	}
	return base.ResolveReference(u).String()
}

// resolveSrcset resolves each URL in a srcset attribute value against base.
// srcset is a comma-separated list of "URL [descriptor]" pairs.
func resolveSrcset(srcset string, base *url.URL) string {
	if srcset == "" {
		return srcset
	}
	parts := strings.Split(srcset, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		resolved := resolveURL(fields[0], base)
		fields[0] = resolved
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
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

// whitespaceRe collapses runs of whitespace (including newlines) to a single space.
var whitespaceRe = regexp.MustCompile(`\s+`)

// blockCloseRe matches closing block-level HTML tags.
var blockCloseRe = regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|blockquote|tr|dt|dd|figcaption|section|article|header|footer|pre)>`)

// Summarize strips HTML tags, collapses whitespace, and truncates to maxLen
// characters at a word boundary, appending "..." if truncated.
func Summarize(raw string, maxLen int) string {
	// Insert spaces at block boundaries before stripping tags so that
	// adjacent text nodes don't get concatenated (e.g. "</p><p>" -> " ").
	text := blockCloseRe.ReplaceAllString(raw, " ")
	text = strictPolicy.Sanitize(text)
	text = htmlpkg.UnescapeString(text)
	text = whitespaceRe.ReplaceAllString(strings.TrimSpace(text), " ")

	if len(text) <= maxLen {
		return text
	}

	// Truncate at the last space before maxLen.
	truncated := text[:maxLen]
	if i := strings.LastIndex(truncated, " "); i > 0 {
		truncated = truncated[:i]
	}
	return truncated + "..."
}

// Snippet strips HTML from an FTS5 snippet, then replaces the plain-text
// highlight sentinels with <mark> tags for safe rendering.
func Snippet(s string) template.HTML {
	clean := strictPolicy.Sanitize(s)
	clean = strings.ReplaceAll(clean, "[[HIGHLIGHT]]", "<mark>")
	clean = strings.ReplaceAll(clean, "[[/HIGHLIGHT]]", "</mark>")
	return template.HTML(clean)
}
