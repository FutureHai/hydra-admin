package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashes(t *testing.T) {
	argon, err := GeneratePasswordHash([]byte("example-password"), "argon2id")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(argon, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected parameters: %s", argon)
	}
	parsed, err := parseArgon(argon)
	if err != nil || len(parsed.salt) != 16 || len(parsed.digest) != 32 {
		t.Fatalf("invalid PHC: %v", err)
	}
	if !VerifyPassword(argon, []byte("example-password")) || VerifyPassword(argon, []byte("wrong")) {
		t.Fatal("Argon2id verification failed")
	}
	for _, bad := range []string{
		strings.Replace(argon, "m=65536", "m=999999999", 1),
		strings.Replace(argon, "t=3", "t=999", 1),
		strings.Replace(argon, "p=2", "p=9", 1),
		strings.Replace(argon, "v=19", "v=18", 1),
		"$argon2id$v=19$m=65536,t=3,p=2$short$short",
	} {
		if ValidatePasswordHash(bad) == nil || VerifyPassword(bad, []byte("example-password")) {
			t.Fatalf("accepted unsafe hash: %s", bad)
		}
	}
	bcryptHash, err := GeneratePasswordHash([]byte("example-password"), "bcrypt")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(bcryptHash, []byte("example-password")) || VerifyPassword(bcryptHash, []byte("wrong")) {
		t.Fatal("bcrypt verification failed")
	}
	if !VerifyPassword("$2y$"+bcryptHash[4:], []byte("example-password")) {
		t.Fatal("bcrypt 2y compatibility failed")
	}
}
