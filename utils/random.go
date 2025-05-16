package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const (
	defaultUsernameLength = 16
	defaultPasswordLength = 16
	usernameChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	passwordChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+"
)

func GenerateRandomString(length int, charSet string) (string, error) {
	bytes := make([]byte, length)
	for i := range bytes {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charSet))))
		if err != nil {
			return "", fmt.Errorf("failed to generate random number: %w", err)
		}
		bytes[i] = charSet[num.Int64()]
	}
	return string(bytes), nil
}

func GenerateRandomUsername() (string, error) {
	return GenerateRandomString(defaultUsernameLength, usernameChars)
}

func GenerateRandomSecurePassword() (string, error) {
	return GenerateRandomString(defaultPasswordLength, passwordChars)
}