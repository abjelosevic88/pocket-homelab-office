// Package office knows ONLYOFFICE Document Server: the JSON web tokens it
// expects, which file types it opens and edits, blank documents to start
// from, and its conversion service.
package office

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Sign returns an HS256 JWT over claims (a map or struct) for the secret.
// Document Server checks every editor config and conversion request with it.
func Sign(secret string, claims any) (string, error) {
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(head + "." + payload))
	return head + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Verify checks an HS256 JWT's signature and expiry and returns its payload.
func Verify(secret, token string) (map[string]any, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}
	var head struct {
		Alg string `json:"alg"`
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &head) != nil || head.Alg != "HS256" {
		return nil, errors.New("token algorithm is not HS256")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("token signature does not verify")
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("malformed token payload")
	}
	var claims map[string]any
	if err := json.Unmarshal(pb, &claims); err != nil {
		return nil, errors.New("malformed token payload")
	}
	if exp, ok := claims["exp"].(float64); ok && time.Now().Unix() > int64(exp)+60 {
		return nil, errors.New("token expired")
	}
	return claims, nil
}
