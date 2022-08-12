package handler

import (
	"io"
	"net/http"
	"path"

	"github.com/razzie/razvhost/assets"
)

func newGoWasmHandler(hostname, hostPath, wasmFile string) http.Handler {
	cleanHostPath := path.Clean(hostPath)
	wasmJs := path.Join(hostPath, "go-wasm.js")
	wasmMain := path.Join(hostPath, "main.wasm")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cleanHostPath:
			http.Redirect(w, r, cleanHostPath+"/", http.StatusSeeOther)
		case cleanHostPath + "/":
			file, _ := assets.Open("assets/go-wasm.html")
			defer file.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.Copy(w, file)
		case wasmJs:
			file, _ := assets.Open("assets/go-wasm.js")
			defer file.Close()
			w.Header().Set("Content-Type", "text/javascript")
			io.Copy(w, file)
		case wasmMain:
			http.ServeFile(w, r, wasmFile)
		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
	})
}
