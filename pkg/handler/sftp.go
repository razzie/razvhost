package handler

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"

	"github.com/pkg/sftp"
	"github.com/razzie/razvhost/pkg/fileserver"
	"golang.org/x/crypto/ssh"
)

func newSftpHandler(hostname, hostPath string, target url.URL) (http.Handler, error) {
	handler := fileserver.FileServer(newSftpFS(target))
	return handlePathCombinations(handler, hostname, hostPath, ""), nil
}

type sftpFS struct {
	hostname string
	config   *ssh.ClientConfig
	dir      string
}

func newSftpFS(target url.URL) http.FileSystem {
	config := &ssh.ClientConfig{
		User:            "anonymous",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	if password, hasPassword := target.User.Password(); hasPassword {
		config.User = target.User.Username()
		config.Auth = []ssh.AuthMethod{
			ssh.Password(password),
		}
	}
	dir := target.Path
	if len(dir) <= 1 {
		dir = "."
	}
	return &sftpFS{
		hostname: target.Host,
		config:   config,
		dir:      dir,
	}
}

func (fs *sftpFS) getClient() (*sftp.Client, error) {
	sshClient, err := ssh.Dial("tcp", fs.hostname, fs.config)
	if err != nil {
		return nil, err
	}
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, err
	}
	return client, nil
}

func (fs *sftpFS) Open(name string) (http.File, error) {
	client, err := fs.getClient()
	if err != nil {
		return nil, err
	}
	name = path.Join(fs.dir, name)
	fi, err := client.Stat(name)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		files, err := client.ReadDir(name)
		if err != nil {
			return nil, err
		}
		return &sftpDir{
			fi:    fi,
			files: files,
		}, nil
	}
	file, err := client.Open(name)
	if err != nil {
		return nil, err
	}
	return &sftpFile{
		client: client,
		file:   file,
	}, nil
}

type sftpFile struct {
	client *sftp.Client
	file   *sftp.File
}

func (f *sftpFile) Stat() (fs.FileInfo, error) {
	return f.file.Stat()
}

func (f *sftpFile) Read(buf []byte) (int, error) {
	return f.file.Read(buf)
}

func (f *sftpFile) Seek(offset int64, whence int) (int64, error) {
	return f.file.Seek(offset, whence)
}

func (f *sftpFile) Close() error {
	f.client.Close()
	return f.file.Close()
}

func (f *sftpFile) Readdir(int) ([]fs.FileInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

type sftpDir struct {
	fi    os.FileInfo
	files []os.FileInfo
}

func (d *sftpDir) Stat() (fs.FileInfo, error) {
	return d.fi, nil
}

func (d *sftpDir) Read(buf []byte) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (d *sftpDir) Seek(offset int64, whence int) (int64, error) {
	return 0, fmt.Errorf("not implemented")
}

func (d *sftpDir) Close() error {
	return nil
}

func (d *sftpDir) Readdir(int) ([]fs.FileInfo, error) {
	return d.files, nil
}
