package razvhost

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/jszwec/s3fs"
	"github.com/yookoala/gofast"

	"github.com/hpcloud/tail"
)

//go:embed assets/*
var assets embed.FS

type HandlerFactory struct {
	phpClientFactory gofast.ClientFactory
}

// NewHandlerFactory returns a new HandlerFactory
func NewHandlerFactory(phpaddr *url.URL) *HandlerFactory {
	hf := &HandlerFactory{}
	if phpaddr != nil {
		hf.setupPHP(phpaddr)
	}
	return hf
}

func (hf *HandlerFactory) Handler(hostname string, target url.URL) (handler http.Handler, err error) {
	hostname, hostPath := splitHostnameAndPath(hostname)
	switch target.Scheme {
	case "file":
		handler = hf.newFileServer(hostname, hostPath, target.Host+target.Path)
	case "http", "https":
		handler = hf.newProxyHandler(hostname, hostPath, target)
	case "redirect":
		handler = hf.newRedirectHandler(hostname, hostPath, target)
	case "s3":
		handler, err = hf.newS3Handler(hostname, hostPath, target)
	case "php":
		handler, err = hf.newPHPHandler(hostname, hostPath, target.Host+target.Path)
	case "go-wasm":
		handler = hf.newGoWasmHandler(hostname, hostPath, target.Host+target.Path)
	case "tail":
		handler = hf.newTailHandler(hostname, hostPath, target.Host+target.Path, io.SeekStart)
	case "tail-new":
		handler = hf.newTailHandler(hostname, hostPath, target.Host+target.Path, io.SeekEnd)
	default:
		err = fmt.Errorf("unknown target URL scheme: %s", target.Scheme)
	}
	return
}

func (hf *HandlerFactory) newFileServer(hostname, hostPath, dir string) http.Handler {
	var handler http.Handler
	if info, _ := os.Stat(dir); info != nil && !info.IsDir() { // not a dir
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, dir)
		})
	} else {
		handler = FileServer(Directory(dir))
	}
	return handlePathCombinations(handler, hostname, hostPath, "")
}

func (hf *HandlerFactory) newProxyHandler(hostname, hostPath string, target url.URL) http.Handler {
	handler := httputil.NewSingleHostReverseProxy(&target)
	return handlePathCombinations(handler, hostname, hostPath, target.Path)
}

func (hf *HandlerFactory) newRedirectHandler(hostname, hostPath string, target url.URL) http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetQuery := target.RawQuery
		redirURL := *r.URL
		redirURL.Host = target.Host
		redirURL.Path, redirURL.RawPath = joinURLPath(&target, &redirURL)
		if targetQuery == "" || redirURL.RawQuery == "" {
			redirURL.RawQuery = targetQuery + redirURL.RawQuery
		} else {
			redirURL.RawQuery = targetQuery + "&" + redirURL.RawQuery
		}
		http.Redirect(w, r, redirURL.String(), http.StatusSeeOther)
	})
	return handlePathCombinations(handler, "", hostPath, "")
}

func (hf *HandlerFactory) newS3Handler(hostname, hostPath string, target url.URL) (http.Handler, error) {
	conf := aws.NewConfig()
	if secret, hasSecret := target.User.Password(); hasSecret {
		id := target.User.Username()
		conf = conf.WithCredentials(credentials.NewStaticCredentials(id, secret, ""))
	} else {
		conf = conf.WithCredentials(credentials.AnonymousCredentials)
	}
	bucket := ""
	if strings.Contains(target.Host, ".") {
		bucketAndEndpoint := strings.SplitN(target.Host, ".", 2)
		bucket = bucketAndEndpoint[0]
		conf = conf.WithEndpoint(bucketAndEndpoint[1])
	} else {
		bucket = target.Host
	}
	if target.Query().Has("region") {
		conf = conf.WithRegion(target.Query().Get("region"))
	}
	sess, err := session.NewSession(conf)
	if err != nil {
		return nil, err
	}
	prefix := target.Path
	if len(prefix) <= 1 {
		prefix = "."
	}
	var fsys fs.FS = s3fs.New(s3.New(sess), bucket)
	fsys, err = fs.Sub(fsys, prefix)
	if err != nil {
		return nil, err
	}
	handler := FileServer(http.FS(fsys))
	return handlePathCombinations(handler, hostname, hostPath, ""), nil
}

func (hf *HandlerFactory) setupPHP(cgiaddr *url.URL) {
	network := cgiaddr.Scheme
	address := cgiaddr.Host
	if len(address) == 0 {
		address = cgiaddr.Path
	}
	connFactory := gofast.SimpleConnFactory(network, address)
	hf.phpClientFactory = gofast.SimpleClientFactory(connFactory)
}

func (hf *HandlerFactory) newPHPHandler(hostname, hostPath, endpoint string) (http.Handler, error) {
	if hf.phpClientFactory == nil {
		return nil, fmt.Errorf("PHP not configured")
	}
	fi, err := os.Stat(endpoint)
	if err != nil {
		return nil, err
	}
	endpoint, err = filepath.Abs(endpoint)
	if err != nil {
		return nil, err
	}
	var targetPath string
	var sessHandler gofast.SessionHandler
	if fi.IsDir() {
		sessHandler = gofast.NewPHPFS(endpoint)(gofast.BasicSession)
		targetPath = endpoint
	} else {
		sessHandler = gofast.NewFileEndpoint(endpoint)(gofast.BasicSession)
		targetPath = filepath.Dir(endpoint)
	}
	handler := gofast.NewHandler(sessHandler, hf.phpClientFactory)
	return handlePathCombinations(handler, hostname, hostPath, targetPath), nil
}

func (hf *HandlerFactory) newGoWasmHandler(hostname, hostPath, wasmFile string) http.Handler {
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

func (hf *HandlerFactory) newTailHandler(hostname, hostPath, tailFile string, whence int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := tail.Config{
			Follow:   true,
			Logger:   tail.DiscardingLogger,
			Location: &tail.SeekInfo{Offset: 0, Whence: whence},
		}
		t, err := tail.TailFile(tailFile, cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		w.Header().Add("content-type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		f, _ := w.(http.Flusher)
		for {
			select {
			case <-r.Context().Done():
				return
			case line, ok := <-t.Lines:
				if !ok {
					return
				}
				io.WriteString(w, line.Text+"\n")
				if line.Err != nil {
					io.WriteString(w, line.Err.Error())
					return
				}
				if f != nil {
					f.Flush()
				}
			}
		}
	})
}

func handlePathCombinations(handler http.Handler, hostname, hostPath, targetPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, hostPath)
		r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, hostPath)
		if !strings.HasPrefix(r.URL.Path, "/") {
			r.URL.Path = "/" + r.URL.Path
			r.URL.RawPath = "/" + r.URL.RawPath
		}
		ww := NewPathPrefixHTMLResponseWriter(hostname, hostPath, targetPath, w)
		defer ww.Close()
		handler.ServeHTTP(ww, r)
	})
}

func splitHostnameAndPath(hostname string) (string, string) {
	i := strings.Index(hostname, "/")
	if i == -1 {
		return hostname, ""
	}
	return hostname[:i], hostname[i:]
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}

	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")

	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	}
	return a.Path + b.Path, apath + bpath
}
