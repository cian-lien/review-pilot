package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

var errCredential = errors.New("INVALID_CREDENTIAL")

func credentialAEAD(key string) (cipher.AEAD, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 || base64.StdEncoding.EncodeToString(raw) != key {
		return nil, errCredential
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, errCredential
	}
	return cipher.NewGCM(block)
}
func SealToken(token, key string) (string, error) {
	aead, err := credentialAEAD(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", errCredential
	}
	combined := aead.Seal(nil, nonce, []byte(token), nil)
	size := len(combined) - 16
	encode := base64.RawURLEncoding.EncodeToString
	return encode(nonce) + "." + encode(combined[size:]) + "." + encode(combined[:size]), nil
}
func OpenToken(sealed, key string) (string, error) {
	aead, err := credentialAEAD(key)
	if err != nil {
		return "", err
	}
	parts := strings.Split(sealed, ".")
	if len(parts) != 3 {
		return "", errCredential
	}
	decoded := make([][]byte, 3)
	for i, part := range parts {
		decoded[i], err = base64.RawURLEncoding.DecodeString(part)
		if err != nil || base64.RawURLEncoding.EncodeToString(decoded[i]) != part {
			return "", errCredential
		}
	}
	if len(decoded[0]) != 12 || len(decoded[1]) != 16 {
		return "", errCredential
	}
	plaintext, err := aead.Open(nil, decoded[0], append(decoded[2], decoded[1]...), nil)
	if err != nil {
		return "", errCredential
	}
	return string(plaintext), nil
}
