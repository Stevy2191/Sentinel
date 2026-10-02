package services

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	MaxMIBFileBytes   = 2 << 20
	MaxMIBUploadBytes = 20 << 20
	MaxMIBZipFiles    = 500
)

var (
	ErrMIBUploadTooLarge = errors.New("the upload is larger than 20 MB")
	ErrMIBFileTooLarge   = errors.New("a MIB file is larger than 2 MB")
	ErrMIBTooManyFiles   = errors.New("the zip holds more than 500 files")
)

// UploadFile is one uploaded file: a MIB, or a zip of them.
type UploadFile struct {
	FileName string
	Content  []byte
}

// ExpandUpload replaces each .zip with its files (read in memory; folders,
// dot files and __MACOSX entries dropped; base names only) and enforces the
// size and count limits.
func ExpandUpload(files []UploadFile) ([]UploadFile, error) {
	total := 0
	var out []UploadFile
	for _, f := range files {
		total += len(f.Content)
		if total > MaxMIBUploadBytes {
			return nil, ErrMIBUploadTooLarge
		}
		if !strings.EqualFold(path.Ext(f.FileName), ".zip") {
			if len(f.Content) > MaxMIBFileBytes {
				return nil, fmt.Errorf("%s: %w", f.FileName, ErrMIBFileTooLarge)
			}
			out = append(out, UploadFile{FileName: path.Base(f.FileName), Content: f.Content})
			continue
		}
		zr, err := zip.NewReader(bytes.NewReader(f.Content), int64(len(f.Content)))
		if err != nil {
			return nil, fmt.Errorf("%s: not a readable zip: %w", f.FileName, err)
		}
		if len(zr.File) > MaxMIBZipFiles {
			return nil, ErrMIBTooManyFiles
		}
		unpacked := 0
		for _, zf := range zr.File {
			base := path.Base(zf.Name)
			if zf.FileInfo().IsDir() || strings.HasPrefix(base, ".") || strings.HasPrefix(zf.Name, "__MACOSX/") {
				continue
			}
			if zf.UncompressedSize64 > MaxMIBFileBytes {
				return nil, fmt.Errorf("%s: %w", base, ErrMIBFileTooLarge)
			}
			rc, err := zf.Open()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", base, err)
			}
			b, err := io.ReadAll(io.LimitReader(rc, MaxMIBFileBytes+1))
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", base, err)
			}
			if len(b) > MaxMIBFileBytes {
				return nil, fmt.Errorf("%s: %w", base, ErrMIBFileTooLarge)
			}
			unpacked += len(b)
			if unpacked > MaxMIBUploadBytes {
				return nil, ErrMIBUploadTooLarge
			}
			out = append(out, UploadFile{FileName: base, Content: b})
		}
	}
	return out, nil
}
