package handler

import (
	"net/http"
	"os"

	"github.com/razzie/razvhost/pkg/fileserver"
)

func newFileServer(hostname, hostPath, dir string) http.Handler {
	var handler http.Handler
	if info, _ := os.Stat(dir); info != nil && !info.IsDir() { // not a dir
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, dir)
		})
	} else {
		handler = fileserver.FileServer(fileserver.Directory(dir))
	}
	return handlePathCombinations(handler, hostname, hostPath, "")
}
