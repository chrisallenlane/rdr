package poller

import (
	"html"
	"regexp"
	"strings"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
)

// paragraphRe splits text on runs of two or more consecutive newlines.
var paragraphRe = regexp.MustCompile(`\n{2,}`)

// synthesizeMediaContent inspects a feed item's Media RSS extension data and,
// when both content and description are empty, synthesizes HTML content from
// the best available media element plus the media:description text.
//
// Priority order for the media element:
//  1. media:group > media:content with video/ or audio/ MIME type
//  2. enclosures with video/ or audio/ MIME type
//  3. media:group > media:thumbnail (fallback for YouTube-style feeds)
//
// Returns ("", "") when no synthesis is warranted.
func synthesizeMediaContent(item *gofeed.Item) (content, description string) {
	if item.Content != "" || item.Description != "" {
		return "", ""
	}

	mediaGroup := mediaGroupChildren(item.Extensions)
	mediaDesc := mediaDescriptionText(mediaGroup)

	// 1. media:group > media:content with a real MIME type.
	if url, mimeType := findPlayableMediaContent(mediaGroup); url != "" {
		tag := mediaElementTag(mimeType, url)
		return tag + formatDescription(mediaDesc), mediaDesc
	}

	// 2. Enclosures with a real MIME type.
	for _, e := range item.Enclosures {
		if e.URL != "" && isPlayableMIME(e.Type) {
			tag := mediaElementTag(e.Type, e.URL)
			return tag + formatDescription(mediaDesc), mediaDesc
		}
	}

	// 3. Thumbnail fallback (e.g. YouTube where media:content is Flash).
	if thumb := thumbnailURL(mediaGroup); thumb != "" {
		escapedURL := html.EscapeString(thumb)
		escapedLink := html.EscapeString(item.Link)
		escapedTitle := html.EscapeString(item.Title)
		tag := `<a href="` + escapedLink + `"><img src="` + escapedURL +
			`" alt="` + escapedTitle + `" loading="lazy"></a>`
		return tag + formatDescription(mediaDesc), mediaDesc
	}

	return "", ""
}

func mediaGroupChildren(
	extensions map[string]map[string][]ext.Extension,
) map[string][]ext.Extension {
	if extensions == nil {
		return nil
	}
	media, ok := extensions["media"]
	if !ok {
		return nil
	}
	groups, ok := media["group"]
	if !ok || len(groups) == 0 {
		return nil
	}
	return groups[0].Children
}

func findPlayableMediaContent(
	children map[string][]ext.Extension,
) (url, mimeType string) {
	if children == nil {
		return "", ""
	}
	contents, ok := children["content"]
	if !ok {
		return "", ""
	}
	for _, c := range contents {
		t := c.Attrs["type"]
		u := c.Attrs["url"]
		if u != "" && isPlayableMIME(t) {
			return u, t
		}
	}
	return "", ""
}

func thumbnailURL(children map[string][]ext.Extension) string {
	if children == nil {
		return ""
	}
	thumbs, ok := children["thumbnail"]
	if !ok || len(thumbs) == 0 {
		return ""
	}
	return thumbs[0].Attrs["url"]
}

func mediaDescriptionText(children map[string][]ext.Extension) string {
	if children == nil {
		return ""
	}
	descs, ok := children["description"]
	if !ok || len(descs) == 0 {
		return ""
	}
	return descs[0].Value
}

func isPlayableMIME(mimeType string) bool {
	return strings.HasPrefix(mimeType, "video/") ||
		strings.HasPrefix(mimeType, "audio/")
}

func mediaElementTag(mimeType, url string) string {
	escapedURL := html.EscapeString(url)
	if strings.HasPrefix(mimeType, "video/") {
		return `<video controls src="` + escapedURL + `"></video>`
	}
	return `<audio controls src="` + escapedURL + `"></audio>`
}

// formatDescription converts a plain-text media description into HTML. It
// splits on two-or-more consecutive newlines into <p> blocks and converts
// single newlines within each block to <br>. Input is HTML-escaped before
// wrapping so that any markup-looking characters in feed text are treated
// as literal text.
//
// Returns an empty string when text is empty.
func formatDescription(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	var sb strings.Builder
	for _, para := range paragraphRe.Split(text, -1) {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		escaped := html.EscapeString(para)
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
		sb.WriteString("<p>")
		sb.WriteString(escaped)
		sb.WriteString("</p>")
	}
	return sb.String()
}
