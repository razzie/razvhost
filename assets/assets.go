package assets

import (
	"embed"
	"io/fs"
)

//go:embed *
var assets embed.FS

func Open(filename string) (fs.File, error) {
	return assets.Open(filename)
}
