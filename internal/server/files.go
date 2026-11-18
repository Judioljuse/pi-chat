package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"pichat/internal/store"
)

const maxFile = 10 << 20

var inlineMime = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func rejectedMime(m string) bool {
	return strings.HasPrefix(m, "audio/") || strings.HasPrefix(m, "video/") || m == "application/zip" ||
		m == "application/x-gzip" || m == "application/x-rar-compressed" || m == "application/x-7z-compressed"
}

func (s *Server) blobPath(sum string) string {
	return filepath.Join(s.cfg.Data, "files", sum[:2], sum)
}

func (s *Server) upload(c *ctx) (any, error) {
	c.r.Body = http.MaxBytesReader(c.w, c.r.Body, maxFile+64<<10)
	mr, err := c.r.MultipartReader()
	if err != nil {
		return nil, badRequest("需要 multipart/form-data")
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return nil, badRequest("缺少 file 字段")
		}
		if err != nil {
			return nil, tooLargeOr(err)
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		if name == "" || name == "." || name == "/" {
			name = "file"
		}
		name = truncRunes(name, 200)
		tmp, err := os.CreateTemp(filepath.Join(s.cfg.Data, "files"), ".up-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp.Name())
		h := sha256.New()
		head := make([]byte, 512)
		n, _ := io.ReadFull(part, head)
		head = head[:n]
		size, err := io.Copy(io.MultiWriter(tmp, h), io.MultiReader(strings.NewReader(string(head)), io.LimitReader(part, maxFile+1-int64(n))))
		tmp.Close()
		if err != nil {
			return nil, tooLargeOr(err)
		}
		if size > maxFile {
			return nil, errf(413, "too_large", "单个文件最大 10 MB")
		}
		if size == 0 {
			return nil, badRequest("文件为空")
		}
		mt := http.DetectContentType(head)
		if base, _, err := mime.ParseMediaType(mt); err == nil {
			mt = base
		}
		if rejectedMime(mt) {
			return nil, badRequest("不支持压缩包和音视频文件（%s）", mt)
		}
		sum := hex.EncodeToString(h.Sum(nil))
		dst := s.blobPath(sum)
		s.blobMu.Lock()
		defer s.blobMu.Unlock()
		if _, err := os.Stat(dst); err != nil {
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return nil, err
			}
			if err := os.Rename(tmp.Name(), dst); err != nil {
				return nil, err
			}
		}
		id := store.NewID("f_")
		if _, err := s.db.W.Exec(`INSERT INTO files(id,uploader_id,name,mime,size,sha256,created_at) VALUES(?,?,?,?,?,?,?)`,
			id, c.a.ID, name, mt, size, sum, now()); err != nil {
			return nil, err
		}
		return obj{"file": fileJSON{ID: id, Name: name, Mime: mt, Size: size}}, nil
	}
}

func tooLargeOr(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return errf(413, "too_large", "单个文件最大 10 MB")
	}
	return badRequest("上传失败：%v", err)
}

func (s *Server) download(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	var gid, uploader, name, mt, sum string
	var size int64
	err := s.db.R.QueryRow(`SELECT COALESCE(group_id,''),uploader_id,name,mime,size,sha256 FROM files WHERE id=?`, id).Scan(&gid, &uploader, &name, &mt, &size, &sum)
	if err != nil {
		return nil, notFoundIf(err)
	}
	if gid == "" {
		if uploader != c.a.ID {
			return nil, errNotFound()
		}
	} else if _, err := s.role(s.db.R, c.a, gid); err != nil {
		return nil, errNotFound()
	}
	f, err := os.Open(s.blobPath(sum))
	if err != nil {
		return nil, errNotFound()
	}
	defer f.Close()
	disp := "attachment"
	if inlineMime[mt] {
		disp = "inline"
	}
	h := c.w.Header()
	h.Set("Content-Type", mt)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
	http.ServeContent(c.w, c.r, "", fileModTime(f), f)
	return nil, nil
}
