package handler

import (
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/jszwec/s3fs"
	"github.com/razzie/razvhost/pkg/fileserver"
)

func newS3Handler(hostname, hostPath string, target url.URL) (http.Handler, error) {
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
	handler := fileserver.FileServer(http.FS(fsys))
	return handlePathCombinations(handler, hostname, hostPath, ""), nil
}
