package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := decode(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The example of RFC 8291 section 5 / appendix A.
func TestEncryptRFC8291(t *testing.T) {
	asPriv := mustDecode(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw")
	as, err := ecdh.P256().NewPrivateKey(asPriv)
	if err != nil {
		t.Fatal(err)
	}
	sub := Subscription{
		Endpoint: "https://push.example.net/push/JzLQ3raZJfFBR0aqvOMsLrt54w4rJUsV",
		P256dh:   "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
		Auth:     "BTBZMqHH6r4Tts7J_aSIgg",
	}
	if err := sub.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := Encrypt([]byte("When I grow up, I want to be a watermelon"), sub, mustDecode(t, "DGv6ra1nlYgDCS1FRnbzlw"), as)
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if b64.EncodeToString(got) != want {
		t.Fatalf("body\n got %s\nwant %s", b64.EncodeToString(got), want)
	}
}

// decrypt is the user agent's side, for the round trip test.
func decrypt(t *testing.T, body []byte, ua *ecdh.PrivateKey, auth []byte) []byte {
	t.Helper()
	salt, idlen := body[:16], int(body[20])
	asPub, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := ua.ECDH(asPub)
	prkKey, _ := hkdf.Extract(sha256.New, secret, auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(ua.PublicKey().Bytes())+string(asPub.Bytes()), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	blk, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(blk)
	pt, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatal(err)
	}
	return pt[:len(pt)-1]
}

func TestSendRoundTrip(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(nil)
	auth := []byte("0123456789abcdef")
	key, _ := GenerateVAPID()
	v := VAPID{Key: key, Subject: "mailto:a@example.com"}
	enc, _ := EncodePrivate(key)
	if k2, err := ParseVAPID(enc); err != nil || !k2.Equal(key) {
		t.Fatalf("parse: %v", err)
	}
	var got []byte
	var hdr http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		hdr = r.Header
		if strings.HasSuffix(r.URL.Path, "/gone") {
			w.WriteHeader(410)
			return
		}
		w.WriteHeader(201)
	}))
	defer ts.Close()
	sub := Subscription{Endpoint: ts.URL + "/push/abc", P256dh: b64.EncodeToString(ua.PublicKey().Bytes()), Auth: b64.EncodeToString(auth)}
	res := v.Send(ts.Client(), sub, []byte(`{"title":"你好"}`), time.Hour, "high")
	if res.Err != nil || res.Status != 201 {
		t.Fatalf("send: %+v", res)
	}
	if string(decrypt(t, got, ua, auth)) != `{"title":"你好"}` {
		t.Fatal("round trip")
	}
	if hdr.Get("Content-Encoding") != "aes128gcm" || hdr.Get("TTL") != "3600" || hdr.Get("Urgency") != "high" {
		t.Fatalf("headers: %v", hdr)
	}
	a := hdr.Get("Authorization")
	if !strings.HasPrefix(a, "vapid t=") || !strings.HasSuffix(a, ", k="+v.PublicKey()) {
		t.Fatalf("auth: %s", a)
	}
	if !Verify(&key.PublicKey, strings.TrimSuffix(strings.TrimPrefix(a, "vapid t="), ", k="+v.PublicKey())) {
		t.Fatal("JWT signature")
	}
	sub.Endpoint = ts.URL + "/push/gone"
	if res := v.Send(ts.Client(), sub, []byte("x"), time.Hour, ""); !res.Gone {
		t.Fatalf("410 not gone: %+v", res)
	}
	if _, err := Encrypt(make([]byte, MaxPayload+1), sub, nil, nil); err == nil {
		t.Fatal("oversized payload accepted")
	}
	if (Subscription{P256dh: "AAAA", Auth: "BTBZMqHH6r4Tts7J_aSIgg"}).Validate() == nil {
		t.Fatal("bad key accepted")
	}
}
