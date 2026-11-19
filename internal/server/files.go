package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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
			return nil, s.storeErr(c, "create temp", err)
		}
		defer os.Remove(tmp.Name())
		h := sha256.New()
		head := make([]byte, 512)
		n, _ := io.ReadFull(part, head)
		head = head[:n]
		tw := &trackWriter{w: tmp}
		size, err := io.Copy(io.MultiWriter(tw, h), io.MultiReader(strings.NewReader(string(head)), io.LimitReader(part, maxFile+1-int64(n))))
		cerr := tmp.Close()
		if tw.err != nil {
			return nil, s.storeErr(c, "write temp", tw.err)
		}
		if err != nil {
			return nil, tooLargeOr(err)
		}
		if cerr != nil {
			return nil, s.storeErr(c, "close temp", cerr)
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
				return nil, s.storeErr(c, "mkdir", err)
			}
			if err := os.Rename(tmp.Name(), dst); err != nil {
				return nil, s.storeErr(c, "rename", err)
			}
		}
		id := store.NewID("f_")
		if _, err := s.db.W.Exec(`INSERT INTO files(id,uploader_id,name,mime,size,sha256,created_at) VALUES(?,?,?,?,?,?,?)`,
			id, c.a.ID, name, mt, size, sum, now()); err != nil {
			return nil, s.storeErr(c, "insert file row", err)
		}
		return obj{"file": fileJSON{ID: id, Name: name, Mime: mt, Size: size}}, nil
	}
}

// trackWriter records the first write error, so disk errors are not mistaken for client read errors.
type trackWriter struct {
	w   io.Writer
	err error
}

func (t *trackWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if err != nil && t.err == nil {
		t.err = err
	}
	return n, err
}

// storeErr logs a filesystem or database failure in full and returns a 500
// whose message names the cause without exposing server paths.
func (s *Server) storeErr(c *ctx, op string, err error) error {
	uid := ""
	if c != nil && c.a != nil {
		uid = c.a.ID
	}
	s.log.Error("保存附件失败", "op", op, "user", uid, "err", err)
	return errf(500, "storage", "保存附件失败：%s", storeReason(err))
}

// storeReason maps an error to a short Chinese reason without paths.
func storeReason(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.EACCES, syscall.EPERM, syscall.EROFS:
			return "数据目录没有写权限"
		case syscall.ENOSPC, syscall.EDQUOT:
			return "磁盘空间不足"
		}
		return errno.Error()
	}
	var coder interface{ Code() int }
	if errors.As(err, &coder) {
		// SQLite primary result codes: 8 READONLY, 13 FULL, 10 IOERR, 5 BUSY.
		switch coder.Code() & 0xff {
		case 8:
			return "数据库只读（检查数据目录权限）"
		case 13:
			return "磁盘空间不足"
		case 5:
			return "数据库忙，稍后重试"
		case 10:
			return "数据库读写出错"
		}
		return "数据库错误"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "数据目录没有写权限"
	}
	return "文件系统错误"
}

// probeFiles checks that <data>/files accepts create, write, fsync, rename and remove.
func probeFiles(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".up-probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write([]byte("pichat probe\n")); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	moved := name + ".mv"
	if err := os.Rename(name, moved); err != nil {
		return err
	}
	return os.Remove(moved)
}

func (s *Server) checkFilesDir() {
	if err := probeFiles(filepath.Join(s.cfg.Data, "files")); err != nil {
		s.log.Error("附件目录不可写，上传会失败：" + err.Error())
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
