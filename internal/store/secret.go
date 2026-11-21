package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SecretFile is the instance secret under the data dir. It encrypts secrets stored in the
// database (search API key, external MCP server headers). Back it up with the database:
// without it those secrets can't be decrypted and must be entered again.
const SecretFile = "secret.key"

const secretVersion = 1

// LoadOrCreateSecret reads <dataDir>/secret.key (32 bytes), creating it with mode 0600 if missing.
func LoadOrCreateSecret(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, SecretFile)
	b, err := os.ReadFile(path)
	if err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("%s 应该是 32 字节，实际 %d 字节", path, len(b))
		}
		return b, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return LoadOrCreateSecret(dataDir) // created concurrently
	}
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	return key, f.Close()
}

// Seal encrypts plain with AES-256-GCM under key (fresh random nonce per value). aad binds the
// ciphertext to where it is stored (e.g. "mcp_servers:<id>"), so a value can't be moved to
// another row. Output: version byte | 12-byte nonce | ciphertext+tag.
func Seal(key []byte, plain, aad string) ([]byte, error) {
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 1+g.NonceSize(), 1+g.NonceSize()+len(plain)+g.Overhead())
	out[0] = secretVersion
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, err
	}
	return g.Seal(out, out[1:], []byte(plain), []byte(aad)), nil
}

// Unseal reverses Seal.
func Unseal(key, blob []byte, aad string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	if len(blob) < 1+g.NonceSize()+g.Overhead() || blob[0] != secretVersion {
		return "", errors.New("密文格式不对")
	}
	n := 1 + g.NonceSize()
	plain, err := g.Open(nil, blob[1:n], blob[n:], []byte(aad))
	if err != nil {
		return "", errors.New("无法解密（secret.key 换过了？）")
	}
	return string(plain), nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("实例密钥不可用（secret.key）")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}
