package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	argonMemoryKiB      uint32 = 65536
	argonIterations     uint32 = 3
	argonParallelism    uint8  = 2
	argonSaltBytes             = 16
	argonHashBytes             = 32
	maxArgonMemoryKiB   uint32 = 131072
	maxArgonIterations  uint32 = 6
	maxArgonParallelism uint8  = 4
	defaultBcryptCost          = 12
)

var bcryptPattern = regexp.MustCompile(`^\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}$`)
var passwordVerificationSlots = make(chan struct{}, 2)

type argonHash struct {
	memory, iterations uint32
	parallelism        uint8
	salt, digest       []byte
}

func GeneratePasswordHash(password []byte, algorithm string) (string, error) {
	if len(password) == 0 {
		return "", errors.New("password must not be empty")
	}
	switch algorithm {
	case "argon2id", "":
		salt := make([]byte, argonSaltBytes)
		if _, err := rand.Read(salt); err != nil {
			return "", err
		}
		passwordVerificationSlots <- struct{}{}
		digest := argon2.IDKey(password, salt, argonIterations, argonMemoryKiB, argonParallelism, argonHashBytes)
		<-passwordVerificationSlots
		return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemoryKiB, argonIterations, argonParallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(digest)), nil
	case "bcrypt":
		hash, err := bcrypt.GenerateFromPassword(password, defaultBcryptCost)
		return string(hash), err
	default:
		return "", errors.New("algorithm must be argon2id or bcrypt")
	}
}

func ValidatePasswordHash(encoded string) error {
	if bcryptPattern.MatchString(encoded) {
		cost, err := bcrypt.Cost([]byte(normalizeBcrypt(encoded)))
		if err != nil || cost < bcrypt.MinCost || cost > 14 {
			return errors.New("bcrypt hash has invalid or excessive cost")
		}
		return nil
	}
	if _, err := parseArgon(encoded); err != nil {
		return err
	}
	return nil
}

func VerifyPassword(encoded string, password []byte) bool {
	if err := ValidatePasswordHash(encoded); err != nil {
		return false
	}
	if bcryptPattern.MatchString(encoded) {
		return bcrypt.CompareHashAndPassword([]byte(normalizeBcrypt(encoded)), password) == nil
	}
	parsed, err := parseArgon(encoded)
	if err != nil {
		return false
	}
	passwordVerificationSlots <- struct{}{}
	actual := argon2.IDKey(password, parsed.salt, parsed.iterations, parsed.memory, parsed.parallelism, uint32(len(parsed.digest)))
	<-passwordVerificationSlots
	return subtle.ConstantTimeCompare(actual, parsed.digest) == 1
}

func normalizeBcrypt(hash string) string {
	if strings.HasPrefix(hash, "$2y$") {
		return "$2a$" + hash[4:]
	}
	return hash
}

func parseArgon(encoded string) (*argonHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return nil, errors.New("password hash must be bcrypt or complete Argon2id PHC")
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return nil, errors.New("Argon2id parameters are invalid")
	}
	values := map[string]uint64{}
	for _, item := range params {
		kv := strings.SplitN(item, "=", 2)
		if len(kv) != 2 || (kv[0] != "m" && kv[0] != "t" && kv[0] != "p") {
			return nil, errors.New("Argon2id parameters are invalid")
		}
		if _, exists := values[kv[0]]; exists {
			return nil, errors.New("Argon2id parameters are duplicated")
		}
		v, err := strconv.ParseUint(kv[1], 10, 32)
		if err != nil {
			return nil, errors.New("Argon2id parameters are invalid")
		}
		values[kv[0]] = v
	}
	m, t, p := values["m"], values["t"], values["p"]
	if m < 8192 || m > uint64(maxArgonMemoryKiB) || t < 1 || t > uint64(maxArgonIterations) || p < 1 || p > uint64(maxArgonParallelism) || m < 8*p {
		return nil, errors.New("Argon2id parameters exceed allowed resource bounds")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < argonSaltBytes || len(salt) > 64 {
		return nil, errors.New("Argon2id salt is invalid")
	}
	digest, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(digest) < argonHashBytes || len(digest) > 64 {
		return nil, errors.New("Argon2id digest is invalid")
	}
	return &argonHash{uint32(m), uint32(t), uint8(p), salt, digest}, nil
}
