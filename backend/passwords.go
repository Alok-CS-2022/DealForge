package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory      = 64 * 1024
	passwordIterations  = 3
	passwordParallelism = 4
	passwordSaltSize    = 16
	passwordDigestSize  = 32
)

var passwordHashPrefix = fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$", argon2.Version,
	passwordMemory, passwordIterations, passwordParallelism)

func hashPassword(password string) (string, error) {
	var salt [passwordSaltSize]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	digest := argon2.IDKey([]byte(password), salt[:], passwordIterations, passwordMemory, passwordParallelism, passwordDigestSize)
	return passwordHashPrefix + base64.RawStdEncoding.EncodeToString(salt[:]) + "$" +
		base64.RawStdEncoding.EncodeToString(digest), nil
}

func verifyPassword(password, encoded string) bool {
	// Accept the format and cost produced by registration. Never run costs supplied by storage.
	encoding := base64.RawStdEncoding.Strict()
	payload, hasPrefix := strings.CutPrefix(encoded, passwordHashPrefix)
	saltText, digestText, hasSeparator := strings.Cut(payload, "$")
	var salt [passwordSaltSize]byte
	var digest [passwordDigestSize]byte
	valid := false
	if hasPrefix && hasSeparator && len(saltText) == encoding.EncodedLen(len(salt)) &&
		len(digestText) == encoding.EncodedLen(len(digest)) {
		saltSize, saltErr := encoding.Decode(salt[:], []byte(saltText))
		digestSize, digestErr := encoding.Decode(digest[:], []byte(digestText))
		valid = saltErr == nil && digestErr == nil && saltSize == len(salt) && digestSize == len(digest)
	}

	// Missing or invalid hashes still incur a full derivation and can never authenticate.
	candidate := argon2.IDKey([]byte(password), salt[:], passwordIterations, passwordMemory, passwordParallelism, passwordDigestSize)
	matches := subtle.ConstantTimeCompare(candidate, digest[:]) == 1
	return valid && matches
}
