package fileserver

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/razzie/razvhost/pkg/util"
)

// errors
var (
	ErrOutsideRoot     = fmt.Errorf("File points outside of the root directory")
	ErrSymlinkMaxDepth = fmt.Errorf("Symlink max depth exceeded")
)

// FileServer returns a http.Handler that serves files and directories under a root dir
func FileServer(fs http.FileSystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri := path.Clean(r.URL.Path)
		file, err := fs.Open(uri)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		defer file.Close()
		fi, err := file.Stat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if fi.IsDir() {
			handleDir(w, file, uri)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fi.Name()))
			io.Copy(w, file)
		}
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), file)
	})
}

// Directory implements http.FileSystem
type Directory string

func (root Directory) Open(relPath string) (http.File, error) {
	filename, err := root.resolve(relPath)
	if err != nil {
		return nil, err
	}
	file, err := http.Dir(string(root)).Open(filename)
	if err != nil {
		return nil, fmt.Errorf("Failed to open: %s", relPath)
	}
	return file, nil
}

func (root Directory) resolve(relPath string) (resolvedPath string, err error) {
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(string(root))
	if err != nil {
		return
	}
	var depth int
	filename := path.Join(absRoot, relPath)
	for {
		var fi os.FileInfo
		fi, err = os.Lstat(filename)
		if err != nil {
			return
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			if !strings.HasPrefix(filename, absRoot) {
				err = ErrOutsideRoot
				return
			}
			resolvedPath = filepath.ToSlash(strings.TrimPrefix(filename, absRoot))
			if len(resolvedPath) == 0 {
				resolvedPath = "."
			} else if resolvedPath[0] == '/' {
				resolvedPath = resolvedPath[1:]
			}
			return
		}
		filename, err = os.Readlink(filename)
		if err != nil {
			return
		}
		if depth++; depth > 16 {
			err = ErrSymlinkMaxDepth
			return
		}
	}
}

type fsEntry struct {
	Icon     template.HTML
	Name     string
	FullName string
	Size     string
	Modified string
	Created  string
}

func newFsEntry(fi os.FileInfo, prefix string) fsEntry {
	e := fsEntry{
		Icon:     "&#128196;",
		Name:     fi.Name(),
		FullName: path.Join(prefix, fi.Name()),
		Size:     util.ByteCountIEC(fi.Size()),
		Modified: fi.ModTime().Format("Mon, 02 Jan 2006 15:04:05 MST"),
		Created:  getCreationTime(fi).Format("Mon, 02 Jan 2006 15:04:05 MST"),
	}
	if fi.IsDir() {
		e.Icon = "&#128194;"
	}
	return e
}

func handleDir(w http.ResponseWriter, dir http.File, uri string) {
	files, err := dir.Readdir(-1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].IsDir() && !files[j].IsDir()
	})
	entries := make([]fsEntry, 0, len(files)+1)
	if uri != "." && uri != "/" {
		entries = append(entries, fsEntry{
			Icon:     "&#128194;",
			Name:     "..",
			FullName: path.Dir(uri),
		})
	}
	for _, fi := range files {
		entries = append(entries, newFsEntry(fi, uri))
	}
	fsTemplate.Execute(w, entries)
}

var fsTemplate = template.Must(template.New("FileServer").Parse(`
<style type="text/css" scoped>
a {
	color: black;
	text-decoration: underline;
	text-decoration-color: rgb(220, 53, 69);
	-webkit-text-decoration-color: rgb(220, 53, 69);
}
a:hover {
	color: dimgrey;
}
table {
	border-collapse: collapse;
	margin-bottom: 1rem;
	border-spacing: 0;
}
td {
	padding: 10px;
	border: 1px solid transparent;
}
tr:nth-child(odd) > td {
	background-color: #F0F0F0;
}
tr:first-child > td {
	font-weight: bold;
	border-bottom: 1px solid black;
	background-color: white;
}
tr:not(:first-child):hover > td {
	background-color: lightsteelblue;
}
</style>
<table>
	<tr>
		<td>Name</td>
		<td>Size</td>
		<td>Modified</td>
		<td>Created</td>
	</tr>
	{{range .}}
	<tr>
		<td>{{.Icon}} <a href="{{.FullName}}">{{.Name}}</a></td>
		<td>{{.Size}}</td>
		<td>{{.Modified}}</td>
		<td>{{.Created}}</td>
	</tr>
	{{else}}
	<tr>
		<td colspan="4">Empty</td>
	</tr>
	{{end}}
</table>
`))
