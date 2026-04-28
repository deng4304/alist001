package iso9660

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alist-org/alist/v3/internal/archive/tool"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/kdomanski/iso9660"
)

func getImage(ss *stream.SeekableStream) (*iso9660.Image, error) {
	reader, err := stream.NewReadAtSeeker(ss, 0)
	if err != nil {
		return nil, err
	}
	return iso9660.OpenImage(reader)
}

func getObj(img *iso9660.Image, path string) (*iso9660.File, error) {
	obj, err := img.RootDir()
	if err != nil {
		return nil, err
	}
	if path == "/" {
		return obj, nil
	}
	paths := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, p := range paths {
		if !obj.IsDir() {
			return nil, errs.ObjectNotFound
		}
		children, err := obj.GetChildren()
		if err != nil {
			return nil, err
		}
		exist := false
		for _, child := range children {
			if child.Name() == p {
				obj = child
				exist = true
				break
			}
		}
		if !exist {
			return nil, errs.ObjectNotFound
		}
	}
	return obj, nil
}

func toModelObj(file *iso9660.File) model.Obj {
	return &model.Object{
		Name:     file.Name(),
		Size:     file.Size(),
		Modified: file.ModTime(),
		IsFolder: file.IsDir(),
	}
}

func decompress(f *iso9660.File, path string, up model.UpdateProgress) error {
	return decompressEntry(f.Reader(), f.Size(), path, f.Name(), up)
}

func decompressEntry(reader io.Reader, size int64, path, entryName string, up model.UpdateProgress) error {
	dstPath, err := tool.SecureJoin(path, entryName)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dstPath), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = utils.CopyWithBuffer(file, &stream.ReaderUpdatingProgress{
		Reader: &stream.SimpleReaderWithSize{
			Reader: reader,
			Size:   size,
		},
		UpdateProgress: up,
	})
	return err
}

func decompressAll(children []*iso9660.File, path string) error {
	for _, child := range children {
		if child.IsDir() {
			nextChildren, err := child.GetChildren()
			if err != nil {
				return err
			}
			nextPath, err := tool.SecureJoin(path, child.Name())
			if err != nil {
				return err
			}
			if err = os.MkdirAll(nextPath, 0700); err != nil {
				return err
			}
			if err = decompressAll(nextChildren, nextPath); err != nil {
				return err
			}
		} else {
			if err := decompress(child, path, func(_ float64) {}); err != nil {
				return err
			}
		}
	}
	return nil
}
