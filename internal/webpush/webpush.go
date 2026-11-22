// Package webpush sends Web Push messages: payload encryption per RFC 8291 (aes128gcm,
// RFC 8188) and VAPID authentication per RFC 8292. Standard library only.
package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MaxPayload is the largest plaintext that fits one 4096-byte record (RFC 8291 section 4).
const MaxPayload = 3993

const recordSize = 4096

var b64 = base64.RawURLEncoding

// decode accepts base64url or standard base64, padded or not (browsers and libraries vary).
func decode(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	if b, err := b64.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// Subscription is what PushManager.subscribe() returns in the browser.
type Subscription struct {
	Endpoint string
	P256dh   string // the user agent's public key, base64url (65 bytes uncompressed)
	Auth     string // the auth secret, base64url (16 bytes)
}

// Validate checks the keys decode to the sizes RFC 8291 requires.
func (s Subscription) Validate() error {
	pub, err := decode(s.P256dh)
	if err != nil || len(pub) != 65 || pub[0] != 4 {
		return errors.New("p256dh 不是 65 字节的 P-256 公钥")
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return errors.New("p256dh 不是有效的 P-256 公钥")
	}
	if a, err := decode(s.Auth); err != nil || len(a) != 16 {
		return errors.New("auth 不是 16 字节")
	}
	return nil
}

// Encrypt encrypts plaintext for sub (RFC 8291). salt and asKey are random when nil; tests pass
// fixed values to compare against the RFC's example.
func Encrypt(plaintext []byte, sub Subscription, salt []byte, asKey *ecdh.PrivateKey) ([]byte, error) {
	if len(plaintext) > MaxPayload {
		return nil, fmt.Errorf("payload 太大（%d 字节，最多 %d）", len(plaintext), MaxPayload)
	}
	uaPubBytes, err := decode(sub.P256dh)
	if err != nil {
		return nil, err
	}
	uaPub, err := ecdh.P256().NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, err
	}
	auth, err := decode(sub.Auth)
	if err != nil || len(auth) != 16 {
		return nil, errors.New("auth 无效")
	}
	if salt == nil {
		salt = make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
	}
	if asKey == nil {
		if asKey, err = ecdh.P256().GenerateKey(rand.Reader); err != nil {
			return nil, err
		}
	}
	asPub := asKey.PublicKey().Bytes()
	secret, err := asKey.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	prkKey, err := hkdf.Extract(sha256.New, secret, auth)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPubBytes) + string(asPub)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	var hdr bytes.Buffer
	hdr.Write(salt)
	binary.Write(&hdr, binary.BigEndian, uint32(recordSize))
	hdr.WriteByte(byte(len(asPub)))
	hdr.Write(asPub)
	// One record: plaintext then the last-record delimiter 0x02, no padding.
	padded := append(append([]byte{}, plaintext...), 2)
	return gcm.Seal(hdr.Bytes(), nonce, padded, nil), nil
}

// VAPID is the application server's signing key (RFC 8292).
type VAPID struct {
	Key     *ecdsa.PrivateKey
	Subject string // "mailto:..." or an https URL
}

// GenerateVAPID makes a new P-256 key.
func GenerateVAPID() (*ecdsa.PrivateKey, error) { return ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }

// ParseVAPID rebuilds a key from its 32-byte private scalar (base64url).
func ParseVAPID(priv string) (*ecdsa.PrivateKey, error) {
	d, err := decode(priv)
	if err != nil || len(d) != 32 {
		return nil, errors.New("VAPID 私钥无效")
	}
	return ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
}

// EncodePrivate is the inverse of ParseVAPID.
func EncodePrivate(k *ecdsa.PrivateKey) (string, error) {
	b, err := k.Bytes()
	if err != nil {
		return "", err
	}
	return b64.EncodeToString(b), nil
}

// PublicKey is the applicationServerKey for PushManager.subscribe(): base64url, 65 bytes.
func (v VAPID) PublicKey() string {
	b, err := v.Key.PublicKey.Bytes()
	if err != nil {
		return ""
	}
	return b64.EncodeToString(b)
}

// header returns the Authorization value for endpoint ("vapid t=<JWT>, k=<public key>").
func (v VAPID) header(endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("endpoint 无效")
	}
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(12 * time.Hour).Unix(), "sub": v.Subject})
	signing := head + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, v.Key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + v.PublicKey(), nil
}

// Verify checks a JWT made by header (used by tests).
func Verify(pub *ecdsa.PublicKey, jwt string) bool {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return false
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		return false
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	return ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}

// Result of one delivery. Gone means the subscription no longer exists (404/410): delete it.
type Result struct {
	Status int
	Gone   bool
	Err    error
}

// Send encrypts payload for sub and posts it to the push service.
func (v VAPID) Send(c *http.Client, sub Subscription, payload []byte, ttl time.Duration, urgency string) Result {
	body, err := Encrypt(payload, sub, nil, nil)
	if err != nil {
		return Result{Err: err}
	}
	auth, err := v.header(sub.Endpoint, time.Now())
	if err != nil {
		return Result{Err: err}
	}
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{Err: err}
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	if urgency != "" {
		req.Header.Set("Urgency", urgency)
	}
	req.Header.Set("Authorization", auth)
	resp, err := c.Do(req)
	if err != nil {
		return Result{Err: err}
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	res := Result{Status: resp.StatusCode, Gone: resp.StatusCode == 404 || resp.StatusCode == 410}
	if resp.StatusCode/100 != 2 {
		res.Err = fmt.Errorf("推送服务返回 %d：%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return res
}
