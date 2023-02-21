package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"golang.org/x/crypto/scrypt"
)

const (
	nonceSize = 12
)

// deriveKey derives a key from the password and salt.
func deriveKey(password, salt []byte) ([]byte, error) {
	return scrypt.Key(password, salt, 32768, 8, 1, 32)
}

// KeyHash returns the hash of the key derived from the password and salt.
func KeyHash(password, salt []byte) (string, error) {
	keyData, err := deriveKey(password, salt)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(keyData)
	return hex.EncodeToString(hash[:]), nil
}

// Encrypt encrypts the input using the password and salt.
func Encrypt(input, password, salt []byte) ([]byte, error) {
	key, err := deriveKey(password, salt)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	ciphertext := aesgcm.Seal(nil, nonce, input, nil)

	encrypted := make([]byte, nonceSize+len(ciphertext))
	copy(encrypted, nonce)
	copy(encrypted[nonceSize:], ciphertext)

	return encrypted, nil
}

// DecryptString decrypts the encrypted string using the password and salt.
func DecryptString(encryptedString string, password, salt []byte) (string, error) {
	encrypted, err := hex.DecodeString(encryptedString)
	if err != nil {
		return "", err
	}
	decrypted, err := Decrypt(encrypted, password, salt)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}

// Decrypt decrypts the encrypted data using the password and salt.
func Decrypt(encrypted, password, salt []byte) ([]byte, error) {
	// Return empty slice if encrypted is empty
	if len(encrypted) == 0 {
		return []byte{}, nil
	}

	key, err := deriveKey(password, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := encrypted[:nonceSize]
	ciphertext := encrypted[nonceSize:]

	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
