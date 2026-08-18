package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/fogleman/gg"
	"golang.org/x/image/font/basicfont"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

var renderPreviewPNG = generatePreviewPNG

// parseHexColor converts a hex string like "#RRGGBB" or "RRGGBB" to color.RGBA.
func parseHexColor(s string) color.RGBA {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return color.RGBA{255, 255, 255, 255} // Fallback to white
	}
	var r, g, b uint8
	fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}

// handlePreviewImage generates a syntax-highlighted PNG image of the paste.
// Request:  GET /api/pastes/{id}/preview.png
// Response: 200 OK image/png
func handlePreviewImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Trim the .png suffix if it somehow leaked into the path var
	id = strings.TrimSuffix(id, ".png")

	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var imageBytes []byte
	var etag string
	var burnAfterRead bool
	notModified := false
	err := storage.UsePaste(id, func(paste models.CachedPaste) error {
		burnAfterRead = paste.BurnAfterRead
		lang := paste.Language
		contentBytes := []byte(paste.Content)
		etagInput := append([]byte(paste.ID+"\x00"+lang+"\x00"), contentBytes...)
		etag = fmt.Sprintf("\"%x\"", sha256.Sum256(etagInput))
		if r.Header.Get("If-None-Match") == etag {
			notModified = true
			return nil
		}
		generate := func() ([]byte, error) {
			if !acquireWorkSlot(r.Context(), previewWorkSlots) {
				return nil, context.DeadlineExceeded
			}
			defer releaseWorkSlot(previewWorkSlots)
			return renderPreviewPNG(lang, contentBytes)
		}
		var generateErr error
		if burnAfterRead {
			imageBytes, generateErr = generate()
		} else {
			imageBytes, generateErr = generatedPreviews.Load().getOrGenerate(r.Context(), etag, generate)
		}
		return generateErr
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, storage.ErrExpired) {
			respondStorageError(w, err, "Paste")
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "Preview service is busy", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Failed to generate preview", http.StatusInternalServerError)
		return
	}
	if burnAfterRead {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	}
	w.Header().Set("ETag", etag)
	if notModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(imageBytes)
}

func generatePreviewPNG(lang string, contentBytes []byte) ([]byte, error) {

	content := string(contentBytes)

	// Limit to first 15 lines
	lines := strings.Split(content, "\n")
	if len(lines) > 15 {
		lines = lines[:15]
		content = strings.Join(lines, "\n") + "\n..."
	}
	const maxPreviewRunes = 6_000
	contentRunes := []rune(content)
	if len(contentRunes) > maxPreviewRunes {
		content = string(contentRunes[:maxPreviewRunes]) + "..."
	}

	// Prepare Lexer and Style
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get("dracula") // dark theme
	if style == nil {
		style = styles.Fallback
	}

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return nil, fmt.Errorf("highlight preview: %w", err)
	}

	// Canvas dimensions
	const width = 1200
	const height = 630
	dc := gg.NewContext(width, height)

	// Background
	bgColor := parseHexColor(style.Get(chroma.Background).Background.String())
	if bgColor.A == 0 {
		bgColor = color.RGBA{30, 30, 30, 255} // dark fallback
	}
	dc.SetColor(bgColor)
	dc.Clear()

	// Setup font (using basicfont to avoid needing a .ttf file bundled)
	dc.SetFontFace(basicfont.Face7x13)
	// We'll scale up the basic font manually by moving coordinates further,
	// but basicfont is fixed size. For a real app, loading a TTF is better,
	// but basicfont works for a prototype without external assets.
	const scale = 2.5

	// Draw title bar
	dc.SetColor(color.RGBA{200, 200, 200, 255})

	// Poor man's scaled font rendering for basicfont:
	// Since we can't scale the fontface easily without TTF, we'll draw text normally
	// but it will be small. Let's just use regular drawing but maybe load a system font?
	// It's safer to use basic font to guarantee it runs everywhere without crashing.

	// A better approach is to manually draw it or accept it'll be small.
	// Actually, we can scale the context!
	dc.Scale(scale, scale)
	// Adjust coordinates for scale
	x := 20.0
	y := 20.0
	lineHeight := float64(basicfont.Face7x13.Height) * 1.5

	// Iterate tokens and draw
	for _, token := range iterator.Tokens() {
		entry := style.Get(token.Type)

		hex := entry.Colour.String()
		if hex == "" {
			hex = "#ffffff"
		}
		c := parseHexColor(hex)
		dc.SetColor(c)

		text := token.Value
		parts := strings.Split(text, "\n")

		for i, part := range parts {
			if i > 0 {
				x = 20.0
				y += lineHeight
			}
			if part != "" {
				dc.DrawString(part, x, y)
				// Basic font is fixed width
				x += float64(len(part)) * float64(basicfont.Face7x13.Width)
			}
		}
	}

	buf := new(bytes.Buffer)
	if err := png.Encode(buf, dc.Image()); err != nil {
		return nil, fmt.Errorf("encode preview: %w", err)
	}
	return buf.Bytes(), nil
}
