package internal

import (
	"encoding/base64"

	qrcode "github.com/skip2/go-qrcode"
)

// qrDataURI renders text as a QR code PNG and returns it as a data: URI, so
// the image needs no separate request and no client-side JS at all.
//
// This replaces two earlier attempts at generating the QR code in the
// browser (a classic <script> tag, then an ES module import) — both failed
// in practice. Generating it here, once, at startup removes an entire class
// of "does the CDN/browser cooperate today" failure modes: this is plain Go
// that either works or fails loudly at build/boot time, not silently at
// some participant's phone.
func qrDataURI(text string) (string, error) {
	png, err := qrcode.Encode(text, qrcode.Medium, 512)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
