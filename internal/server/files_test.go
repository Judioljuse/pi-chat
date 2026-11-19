package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStoreReason(t *testing.T) {
	wrap := func(e error) error {
		return fmt.Errorf("open /srv/data/files/x: %w", &os.PathError{Op: "open", Path: "/srv/data/files/x", Err: e})
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{wrap(syscall.EACCES), "数据目录没有写权限"},
		{wrap(syscall.EPERM), "数据目录没有写权限"},
		{wrap(syscall.EROFS), "数据目录没有写权限"},
		{wrap(syscall.ENOSPC), "磁盘空间不足"},
		{wrap(syscall.EDQUOT), "磁盘空间不足"},
		{wrap(syscall.EIO), syscall.EIO.Error()},
		{errors.New("boom /secret/path"), "文件系统错误"},
	} {
		if got := storeReason(c.err); got != c.want {
			t.Errorf("%v: got %q want %q", c.err, got, c.want)
		}
		if strings.Contains(storeReason(c.err), "/srv") || strings.Contains(storeReason(c.err), "/secret") {
			t.Errorf("reason leaks a path: %q", storeReason(c.err))
		}
	}
}

func uploadPNG(e *env, c *client) (int, map[string]any) {
	var buf bytes.Buffer
	buf.WriteString("--X\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\n\r\n")
	buf.Write([]byte("\x89PNG\r\n\x1a\n0000"))
	buf.WriteString("\r\n--X--\r\n")
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/v1/files", &buf)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=X")
	resp, err := c.hc.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestUploadReadOnlyFilesDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	e := newEnv(t)
	alice := e.human("alice")
	dir := filepath.Join(e.s.cfg.Data, "files")
	if err := probeFiles(dir); err != nil {
		t.Fatalf("probe on writable dir: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if err := probeFiles(dir); err == nil {
		t.Fatal("probe should fail on read-only dir")
	}
	st, body := uploadPNG(e, alice)
	er, _ := body["error"].(map[string]any)
	if st != 500 || er["message"] != "保存附件失败：数据目录没有写权限" {
		t.Fatalf("read-only upload: %d %v", st, body)
	}
	if strings.Contains(fmt.Sprint(er["message"]), e.s.cfg.Data) {
		t.Fatalf("message leaks path: %v", er)
	}
	os.Chmod(dir, 0o700)
	if st, body := uploadPNG(e, alice); st != 200 || body["file"] == nil {
		t.Fatalf("upload after chmod back: %d %v", st, body)
	}
}
