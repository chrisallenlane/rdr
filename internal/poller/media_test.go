package poller

import (
	"strings"
	"testing"

	"github.com/chrisallenlane/rdr/internal/sanitize"
	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
	"golang.org/x/net/html"
)

// --- Unit tests for pure helper functions ---

func TestIsPlayableMIME(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     bool
	}{
		{"video/mp4", "video/mp4", true},
		{"video/webm", "video/webm", true},
		{"audio/mpeg", "audio/mpeg", true},
		{"audio/ogg", "audio/ogg", true},
		{"application/x-shockwave-flash", "application/x-shockwave-flash", false},
		{"image/jpeg", "image/jpeg", false},
		{"text/html", "text/html", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlayableMIME(tt.mimeType); got != tt.want {
				t.Errorf("isPlayableMIME(%q) = %v, want %v", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestMediaElementTag(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		url      string
		contains []string
		excludes []string
	}{
		{
			name:     "video mime produces video element",
			mimeType: "video/mp4",
			url:      "https://example.com/v.mp4",
			contains: []string{"<video", "controls", `src="https://example.com/v.mp4"`},
		},
		{
			name:     "audio mime produces audio element",
			mimeType: "audio/mpeg",
			url:      "https://example.com/a.mp3",
			contains: []string{"<audio", "controls", `src="https://example.com/a.mp3"`},
		},
		{
			name:     "URL is HTML-escaped",
			mimeType: "video/mp4",
			url:      `https://example.com/v.mp4?a=1&b=2`,
			contains: []string{`src="https://example.com/v.mp4?a=1&amp;b=2"`},
			excludes: []string{`a=1&b=2"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mediaElementTag(tt.mimeType, tt.url)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("mediaElementTag(%q, %q) = %q, want substring %q", tt.mimeType, tt.url, got, want)
				}
			}
			for _, unwanted := range tt.excludes {
				if strings.Contains(got, unwanted) {
					t.Errorf("mediaElementTag(%q, %q) = %q, must not contain %q", tt.mimeType, tt.url, got, unwanted)
				}
			}
		})
	}
}

func TestFormatDescription(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:     "empty returns empty",
			input:    "",
			contains: []string{},
			excludes: []string{"<p>"},
		},
		{
			name:     "simple text wrapped in p",
			input:    "Hello world",
			contains: []string{"<p>Hello world</p>"},
			excludes: []string{},
		},
		{
			name:     "two paragraphs split on double newline",
			input:    "Para one\n\nPara two",
			contains: []string{"<p>Para one</p>", "<p>Para two</p>"},
			excludes: []string{},
		},
		{
			name:     "single newline becomes br",
			input:    "Line one\nLine two",
			contains: []string{"Line one<br>Line two"},
			excludes: []string{},
		},
		{
			name:     "HTML special chars are escaped",
			input:    "A <b>bold</b> & <em>italic</em> claim",
			contains: []string{"&lt;b&gt;", "&amp;", "&lt;em&gt;"},
			excludes: []string{"<b>", "<em>"},
		},
		{
			name:     "three-or-more newlines are a paragraph break",
			input:    "First\n\n\nSecond",
			contains: []string{"<p>First</p>", "<p>Second</p>"},
			excludes: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDescription(tt.input)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("formatDescription(%q) = %q: missing %q", tt.input, got, want)
				}
			}
			for _, unwanted := range tt.excludes {
				if strings.Contains(got, unwanted) {
					t.Errorf("formatDescription(%q) = %q: should not contain %q", tt.input, got, unwanted)
				}
			}
		})
	}
}

// --- Unit tests for synthesizeMediaContent ---

func makeExtensions(
	groupChildren map[string][]ext.Extension,
) map[string]map[string][]ext.Extension {
	return map[string]map[string][]ext.Extension{
		"media": {
			"group": {
				{
					Name:     "group",
					Children: groupChildren,
				},
			},
		},
	}
}

func TestSynthesizeMediaContent(t *testing.T) {
	ytExtensions := makeExtensions(map[string][]ext.Extension{
		"content": {
			{
				Attrs: map[string]string{
					"url":    "https://www.youtube.com/v/dQw4w9WgXcQ",
					"type":   "application/x-shockwave-flash",
					"width":  "640",
					"height": "390",
				},
			},
		},
		"thumbnail": {
			{
				Attrs: map[string]string{
					"url":    "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg",
					"width":  "480",
					"height": "360",
				},
			},
		},
		"description": {
			{Value: "Never gonna give you up\n\nRick Astley classic."},
		},
	})

	vimeoExtensions := makeExtensions(map[string][]ext.Extension{
		"content": {
			{
				Attrs: map[string]string{
					"url":  "https://vimeo.com/external/123456.mp4",
					"type": "video/mp4",
				},
			},
		},
		"description": {
			{Value: "A short film."},
		},
	})

	thumbWithPlayableExtensions := makeExtensions(map[string][]ext.Extension{
		"content": {
			{
				Attrs: map[string]string{
					"url":  "https://example.com/video.mp4",
					"type": "video/mp4",
				},
			},
		},
		"thumbnail": {
			{
				Attrs: map[string]string{
					"url": "https://example.com/thumb.jpg",
				},
			},
		},
	})

	thumbOnlyExtensions := makeExtensions(map[string][]ext.Extension{
		"thumbnail": {
			{Attrs: map[string]string{"url": "https://img.example.com/thumb.jpg"}},
		},
	})

	tests := []struct {
		name         string
		item         *gofeed.Item
		wantEmpty    bool // true if both content and description should be ""
		contentMust  []string
		contentMustN []string // must NOT contain
		wantDesc     string   // exact description value when set
	}{
		{
			name:      "content populated — no synthesis",
			item:      &gofeed.Item{Content: "<p>Already has content</p>", Extensions: thumbOnlyExtensions},
			wantEmpty: true,
		},
		{
			name:      "description populated — no synthesis",
			item:      &gofeed.Item{Description: "Already has description", Extensions: thumbOnlyExtensions},
			wantEmpty: true,
		},
		{
			name: "YouTube style — Flash content, thumbnail fallback",
			item: &gofeed.Item{
				Link:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
				Title:      "Rick Astley - Never Gonna Give You Up",
				Extensions: ytExtensions,
			},
			contentMust:  []string{"<a href=", "<img src=", "hqdefault.jpg", "Never gonna give you up"},
			contentMustN: []string{"<video", "<audio"},
			wantDesc:     "Never gonna give you up\n\nRick Astley classic.",
		},
		{
			name: "podcast style — audio enclosure",
			item: &gofeed.Item{
				Link:  "https://example.com/episode/1",
				Title: "Episode 1",
				Enclosures: []*gofeed.Enclosure{
					{URL: "https://example.com/ep1.mp3", Type: "audio/mpeg"},
				},
			},
			contentMust: []string{"<audio", "controls", "ep1.mp3"},
		},
		{
			name: "Vimeo style — video/mp4 media:content",
			item: &gofeed.Item{
				Link:       "https://vimeo.com/123456",
				Title:      "Short Film",
				Extensions: vimeoExtensions,
			},
			contentMust: []string{"<video", "controls", "123456.mp4", "A short film."},
			wantDesc:    "A short film.",
		},
		{
			name:      "no extensions no enclosures — no synthesis",
			item:      &gofeed.Item{Link: "https://example.com/", Title: "Title"},
			wantEmpty: true,
		},
		{
			name: "playable media:content wins over thumbnail",
			item: &gofeed.Item{
				Link:       "https://example.com/item",
				Title:      "Title",
				Extensions: thumbWithPlayableExtensions,
			},
			contentMust: []string{"<video"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, desc := synthesizeMediaContent(tt.item)

			if tt.wantEmpty {
				if content != "" || desc != "" {
					t.Errorf("expected empty synthesis, got content=%q desc=%q", content, desc)
				}
				return
			}

			if content == "" {
				t.Fatal("expected synthesized content, got empty string")
			}
			for _, want := range tt.contentMust {
				if !strings.Contains(content, want) {
					t.Errorf("content missing %q; got %q", want, content)
				}
			}
			for _, unwanted := range tt.contentMustN {
				if strings.Contains(content, unwanted) {
					t.Errorf("content must not contain %q; got %q", unwanted, content)
				}
			}
			if tt.wantDesc != "" && desc != tt.wantDesc {
				t.Errorf("description = %q, want %q", desc, tt.wantDesc)
			}
		})
	}
}

func FuzzSynthesizeMediaContent(f *testing.F) {
	f.Add(
		"https://www.youtube.com/watch?v=abc",
		"Some Video",
		"https://i.ytimg.com/vi/abc/hqdefault.jpg",
		"",
		"application/x-shockwave-flash",
		"A description with <script>alert(1)</script> embedded.",
	)
	f.Add(
		"https://example.com/podcast/ep1",
		"Episode 1",
		"",
		"https://example.com/ep1.mp3",
		"audio/mpeg",
		"",
	)
	f.Add(
		`javascript:alert(1)`,
		`"><script>alert(1)</script>`,
		`javascript:alert(1)`,
		`https://example.com/v.mp4" onerror="alert(1)`,
		"video/mp4",
		`<img src=x onerror=alert(1)>`,
	)

	f.Fuzz(func(t *testing.T, link, title, thumb, mediaURL, mimeType, descText string) {
		item := &gofeed.Item{
			Link:  link,
			Title: title,
			Extensions: map[string]map[string][]ext.Extension{
				"media": {
					"group": []ext.Extension{{
						Children: map[string][]ext.Extension{
							"thumbnail":   {{Attrs: map[string]string{"url": thumb}}},
							"content":     {{Attrs: map[string]string{"url": mediaURL, "type": mimeType}}},
							"description": {{Value: descText}},
						},
					}},
				},
			},
		}

		content, _ := synthesizeMediaContent(item)
		rendered := string(sanitize.HTML(content))

		doc, err := html.Parse(strings.NewReader(rendered))
		if err != nil {
			t.Fatalf("parsing sanitized output: %v\noutput: %q", err, rendered)
		}
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch strings.ToLower(n.Data) {
				case "script", "iframe", "object", "embed":
					t.Errorf("dangerous element <%s> survived: %q", n.Data, rendered)
				}
				for _, attr := range n.Attr {
					key := strings.ToLower(attr.Key)
					if strings.HasPrefix(key, "on") {
						t.Errorf("event handler %s=%q survived: %q", attr.Key, attr.Val, rendered)
					}
					if strings.HasPrefix(strings.ToLower(strings.TrimSpace(attr.Val)), "javascript:") {
						t.Errorf("javascript: URL in %s=%q survived: %q", attr.Key, attr.Val, rendered)
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
	})
}
