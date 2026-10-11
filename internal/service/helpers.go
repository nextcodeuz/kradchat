package service

import (
	"encoding/base64"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
)

func timeNow() time.Time { return time.Now() }

// qrDataURL otpauth URI ni base64 PNG data URL ga aylantiradi.
func qrDataURL(otpauth string) (string, error) {
	png, err := qrcode.Encode(otpauth, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func bcryptCompare(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
