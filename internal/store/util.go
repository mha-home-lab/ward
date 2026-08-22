package store

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

func sha8(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// SHA8 is the exported short-hash helper.
func SHA8(s string) string { return sha8(s) }

// nowISO returns current UTC time as a compact ISO-ish string.
func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// NowISO is the exported time helper.
func NowISO() string { return nowISO() }
