package razvhost

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

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
	{{end}}
</table>
`))

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
		Size:     byteCountIEC(fi.Size()),
		Modified: fi.ModTime().Format("Mon, 02 Jan 2006 15:04:05 MST"),
		Created:  getCreationTime(fi).Format("Mon, 02 Jan 2006 15:04:05 MST"),
	}
	if fi.IsDir() {
		e.Icon = "&#128194;"
	}
	return e
}

type fileServer struct {
	fs     http.FileSystem
	prefix string
}

func (fs *fileServer) handleDir(w http.ResponseWriter, dir http.File, uri string) {
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
	if uri != "." {
		entries = append(entries, fsEntry{
			Name:     "..",
			FullName: path.Join(fs.prefix, path.Dir(uri)),
		})
	}
	prefix := path.Join(fs.prefix, uri)
	for _, fi := range files {
		entries = append(entries, newFsEntry(fi, prefix))
	}
	fsTemplate.Execute(w, entries)
}

func (fs *fileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	uri := path.Clean(r.URL.Path)
	file, err := fs.fs.Open(uri)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if fi.IsDir() {
		fs.handleDir(w, file, uri)
		return
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), file)
}

// FileServer returns a http.Handler that serves files and directories under a root
func FileServer(fs http.FileSystem, prefix string) http.Handler {
	return &fileServer{
		fs:     fs,
		prefix: prefix,
	}
}

// ErrOutsideRoot ...
var ErrOutsideRoot = fmt.Errorf("File points outside of the root directory")

// Directory ...
type Directory string

// Open ...
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
	}
}

func byteCountIEC(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(b)/float64(div), "KMGTPE"[exp])
}
