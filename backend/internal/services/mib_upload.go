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
	// budget is the decompressed-bytes-in-the-output ceiling for the whole
	// call (not per file and not per zip): a plain file's own bytes, or a
	// zip entry's decompressed bytes, add to one running total, so several
	// small, highly compressible zips cannot each stay under the limit while
	// together exceeding it.
	budget := 0
	var out []UploadFile
	for _, f := range files {
		// Cheap guard on the raw request: the sum of the uploaded files' own
		// sizes (a zip counts at its compressed size here) must also fit.
		total += len(f.Content)
		if total > MaxMIBUploadBytes {
			return nil, ErrMIBUploadTooLarge
		}
		if !strings.EqualFold(path.Ext(f.FileName), ".zip") {
			if len(f.Content) > MaxMIBFileBytes {
				return nil, fmt.Errorf("%s: %w", f.FileName, ErrMIBFileTooLarge)
			}
			budget += len(f.Content)
			if budget > MaxMIBUploadBytes {
				return nil, ErrMIBUploadTooLarge
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
			budget += len(b)
			if budget > MaxMIBUploadBytes {
				return nil, ErrMIBUploadTooLarge
			}
			out = append(out, UploadFile{FileName: base, Content: b})
		}
	}
	return out, nil
}
