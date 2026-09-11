// Package unitypkgbrowse reads .unitypackage files (tar.gz archives with
// a guid-based directory structure) and presents their contents as a
// reconstructed file tree with real asset paths.
package unitypkgbrowse

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"mime"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tgdrive/teldrive/internal/zipbrowse"
)

// assetRecord collects the pieces of a single asset entry inside the
// unitypackage tar.  Each asset lives under a GUID folder with files
// named "pathname", "asset", and optionally "asset.meta" / "preview.png".
type assetRecord struct {
	pathname string
	size     int64
	data     []byte // only populated for extract
}

// parsePackage reads the full tar.gz and returns a map[guid]assetRecord.
func parsePackage(data []byte) (map[string]*assetRecord, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	records := make(map[string]*assetRecord)
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		parts := strings.SplitN(strings.TrimPrefix(hdr.Name, "./"), "/", 2)
		if len(parts) < 2 {
			continue
		}
		guid := parts[0]
		leaf := parts[1]

		rec, ok := records[guid]
		if !ok {
			rec = &assetRecord{}
			records[guid] = rec
		}

		switch leaf {
		case "pathname":
			raw, _ := io.ReadAll(io.LimitReader(tr, 4096))
			rec.pathname = strings.TrimSpace(string(raw))
		case "asset":
			rec.size = hdr.Size
		}
	}

	return records, nil
}

// ListEntries returns a directory listing for innerPath inside the
// unitypackage.  The result reuses zipbrowse types for UI compatibility.
func ListEntries(data []byte, innerPath string) (*zipbrowse.ListResponse, error) {
	records, err := parsePackage(data)
	if err != nil {
		return nil, err
	}

	innerPath = strings.Trim(innerPath, "/")
	if innerPath == "" {
		innerPath = "."
	}

	type dirInfo struct {
		childCount int
	}
	dirs := map[string]*dirInfo{}
	var entries []zipbrowse.Entry

	for _, rec := range records {
		if rec.pathname == "" {
			continue
		}
		p := rec.pathname

		// Register all parent dirs.
		d := path.Dir(p)
		for d != "." && d != "/" {
			if _, ok := dirs[d]; !ok {
				dirs[d] = &dirInfo{}
			}
			d = path.Dir(d)
		}

		// Check if this entry belongs under innerPath.
		var rel string
		if innerPath == "." {
			rel = p
		} else if strings.HasPrefix(p, innerPath+"/") {
			rel = strings.TrimPrefix(p, innerPath+"/")
		} else {
			continue
		}

		// If rel contains a slash, it's a deeper entry; only the first
		// segment (a directory) should appear at this level.
		if idx := strings.Index(rel, "/"); idx >= 0 {
			dirName := rel[:idx]
			dirPath := innerPath + "/" + dirName
			if innerPath == "." {
				dirPath = dirName
			}
			if _, seen := dirs[dirPath]; !seen {
				dirs[dirPath] = &dirInfo{}
			}
			dirs[dirPath].childCount++
			continue
		}

		ext := strings.ToLower(filepath.Ext(p))
		mt := mime.TypeByExtension(ext)
		if mt == "" && ext != "" {
			mt = "application/octet-stream"
		}

		entries = append(entries, zipbrowse.Entry{
			Name:     path.Base(p),
			Path:     p,
			Size:     rec.size,
			IsDir:    false,
			MimeType: mt,
		})
	}

	// Add directory entries for this level.
	for dp, di := range dirs {
		var parent string
		if innerPath == "." {
			parent = "."
		} else {
			parent = innerPath
		}
		dirParent := path.Dir(dp)
		if dirParent == "." || dirParent == "/" {
			dirParent = "."
		}
		if dirParent != parent {
			continue
		}

		entries = append(entries, zipbrowse.Entry{
			Name:       path.Base(dp),
			Path:       dp,
			IsDir:      true,
			ChildCount: di.childCount,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	zp := innerPath
	if zp == "." {
		zp = "/"
	}
	return &zipbrowse.ListResponse{
		Entries: entries,
		Total:   len(entries),
		ZipPath: zp,
	}, nil
}

// ExtractFile writes the raw asset bytes for the asset whose pathname
// matches innerPath.
func ExtractFile(w io.Writer, data []byte, innerPath string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()

	// First pass: map guids to pathnames.
	guidPath := map[string]string{}
	pathGuid := map[string]string{}
	tr1 := tar.NewReader(gz)
	for {
		hdr, err := tr1.Next()
		if err != nil {
			break
		}
		parts := strings.SplitN(strings.TrimPrefix(hdr.Name, "./"), "/", 2)
		if len(parts) == 2 && parts[1] == "pathname" {
			raw, _ := io.ReadAll(io.LimitReader(tr1, 4096))
			p := strings.TrimSpace(string(raw))
			guidPath[parts[0]] = p
			pathGuid[p] = parts[0]
		}
	}

	targetGuid, ok := pathGuid[innerPath]
	if !ok {
		return io.ErrUnexpectedEOF
	}

	// Second pass: find and stream the asset.
	gz2, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz2.Close()
	tr2 := tar.NewReader(gz2)
	for {
		hdr, err := tr2.Next()
		if err != nil {
			return err
		}
		parts := strings.SplitN(strings.TrimPrefix(hdr.Name, "./"), "/", 2)
		if len(parts) == 2 && parts[0] == targetGuid && parts[1] == "asset" {
			_, err := io.Copy(w, tr2)
			return err
		}
	}
}
