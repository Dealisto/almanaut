package domain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters. SHA1/6-digit/30s is the near-universal default that every
// authenticator app (Google Authenticator, Aegis, 1Password, …) supports.
const (
	totpDigits = 6
	totpPeriod = 30 // seconds
)

var totpEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// UserTOTP is a user's stored second-factor state.
type UserTOTP struct {
	Secret  string
	Enabled bool // false = enrollment started but not yet confirmed
}

// GenerateTOTPSecret returns a fresh 160-bit base32 secret (RFC 4226 §4 minimum).
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return totpEnc.EncodeToString(b), nil
}

// TOTPCode computes the RFC 6238 code for secret at time t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := totpEnc.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("invalid TOTP secret")
	}
	return hotp(key, uint64(t.Unix())/totpPeriod), nil
}

// hotp is the RFC 4226 HMAC-based one-time password truncated to totpDigits.
func hotp(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	h := hmac.New(sha1.New, key)
	h.Write(buf[:])
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := uint32(sum[offset]&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%0*d", totpDigits, code%pow10(totpDigits))
}

func pow10(n int) uint32 {
	p := uint32(1)
	for range n {
		p *= 10
	}
	return p
}

// VerifyTOTP reports whether code is valid for secret at now, allowing ±1 step
// of clock skew. The comparison is constant-time.
func VerifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	for _, skew := range []int64{0, -1, 1} {
		want, err := TOTPCode(secret, now.Add(time.Duration(skew*totpPeriod)*time.Second))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TOTPURI builds the otpauth:// provisioning URI encoded into the enrollment QR.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprintf("%d", totpDigits))
	v.Set("period", fmt.Sprintf("%d", totpPeriod))
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// GenerateRecoveryCodes returns n fresh single-use recovery codes in the form
// "xxxxx-xxxxx" (lowercase base32). Callers store only their hashes.
func GenerateRecoveryCodes(n int) ([]string, error) {
	codes := make([]string, 0, n)
	for range n {
		b := make([]byte, 8) // 64 bits → 13 base32 chars, take 10
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s := strings.ToLower(totpEnc.EncodeToString(b))[:10]
		codes = append(codes, s[:5]+"-"+s[5:])
	}
	return codes, nil
}

// NormalizeRecoveryCode strips formatting so "ABCDE-FGHIJ", "abcde fghij", and
// "abcdefghij" all match one stored hash.
func NormalizeRecoveryCode(s string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
}
