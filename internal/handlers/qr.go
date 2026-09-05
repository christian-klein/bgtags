package handlers

import (
	"log"
	"net/http"
	"strconv"

	qrcode "github.com/skip2/go-qrcode"
)

func (h *Handler) HandleQR(w http.ResponseWriter, r *http.Request) {
	content := r.URL.Query().Get("url")
	if content == "" {
		http.Error(w, "missing 'url' parameter", http.StatusBadRequest)
		return
	}

	size := 256
	if sizeParam := r.URL.Query().Get("size"); sizeParam != "" {
		if s, err := strconv.Atoi(sizeParam); err == nil && s >= 64 && s <= 1024 {
			size = s
		}
	}

	// Medium error recovery level (15%)
	png, err := qrcode.Encode(content, qrcode.Medium, size)
	if err != nil {
		log.Printf("QR code generation error: %v", err)
		http.Error(w, "failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(png)
}
