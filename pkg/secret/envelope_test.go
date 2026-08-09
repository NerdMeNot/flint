package secret_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/NerdMeNot/flint/pkg/secret"
)

func generateKey(t *testing.T) []byte {
	t.Helper()
	key, err := secret.GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey() error: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("GenerateMasterKey() returned %d bytes, want 32", len(key))
	}
	return key
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := generateKey(t)

	tests := []struct {
		name       string
		plaintext  string
		keyVersion byte
	}{
		{"simple secret", "my-api-key-12345", 1},
		{"empty string", "", 0},
		{"unicode", "p@$$w0rd-日本語-🔑", 2},
		{"long secret", string(make([]byte, 10000)), 1},
		{"json secret", `{"token":"abc","refresh":"xyz"}`, 1},
		{"multiline", "line1\nline2\nline3\n", 1},
		{"binary-safe", string([]byte{0x00, 0x01, 0xff, 0xfe}), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob, err := secret.Encrypt([]byte(tt.plaintext), key, tt.keyVersion)
			if err != nil {
				t.Fatalf("Encrypt() error: %v", err)
			}

			// Blob should be significantly different from plaintext.
			if bytes.Contains(blob, []byte(tt.plaintext)) && tt.plaintext != "" {
				t.Error("blob contains plaintext — encryption may not be working")
			}

			plaintext, version, err := secret.Decrypt(blob, key)
			if err != nil {
				t.Fatalf("Decrypt() error: %v", err)
			}

			if string(plaintext) != tt.plaintext {
				t.Errorf("Decrypt() plaintext = %q, want %q", string(plaintext), tt.plaintext)
			}
			if version != tt.keyVersion {
				t.Errorf("Decrypt() version = %d, want %d", version, tt.keyVersion)
			}
		})
	}
}

func TestEncryptDecrypt_UniqueCiphertexts(t *testing.T) {
	key := generateKey(t)
	plaintext := []byte("same-secret")

	blob1, _ := secret.Encrypt(plaintext, key, 1)
	blob2, _ := secret.Encrypt(plaintext, key, 1)

	// Each encryption should produce a different blob (random DEK + random nonces).
	if bytes.Equal(blob1, blob2) {
		t.Error("two encryptions of the same plaintext produced identical blobs")
	}

	// But both should decrypt to the same value.
	p1, _, _ := secret.Decrypt(blob1, key)
	p2, _, _ := secret.Decrypt(blob2, key)

	if !bytes.Equal(p1, p2) {
		t.Error("decrypted values differ")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := generateKey(t)
	key2 := generateKey(t)

	blob, err := secret.Encrypt([]byte("secret-value"), key1, 1)
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	_, _, err = secret.Decrypt(blob, key2)
	if err == nil {
		t.Fatal("Decrypt() with wrong key should fail")
	}
}

func TestDecrypt_TamperedBlob(t *testing.T) {
	key := generateKey(t)

	blob, _ := secret.Encrypt([]byte("secret"), key, 1)

	// Flip a byte in the encrypted data section.
	tampered := make([]byte, len(blob))
	copy(tampered, blob)
	tampered[len(tampered)-1] ^= 0xff

	_, _, err := secret.Decrypt(tampered, key)
	if err == nil {
		t.Fatal("Decrypt() with tampered blob should fail")
	}
}

func TestDecrypt_TruncatedBlob(t *testing.T) {
	key := generateKey(t)

	tests := []struct {
		name string
		blob []byte
	}{
		{"empty", []byte{}},
		{"too short for header", []byte{0x01, 0x01}},
		{"header only", []byte{0x01, 0x01, 0x00, 0x10}},
		{"wrong version", []byte{0x02, 0x01, 0x00, 0x01, 0xff}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := secret.Decrypt(tt.blob, key)
			if err == nil {
				t.Fatal("expected error for malformed blob")
			}
		})
	}
}

func TestEncrypt_InvalidKeySize(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"too short", make([]byte, 16)},
		{"too long", make([]byte, 64)},
		{"empty", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := secret.Encrypt([]byte("secret"), tt.key, 1)
			if err == nil {
				t.Fatal("expected error for invalid key size")
			}
		})
	}
}

func TestReEncrypt(t *testing.T) {
	oldKey := generateKey(t)
	newKey := generateKey(t)

	original := []byte("my-production-secret")

	blob, err := secret.Encrypt(original, oldKey, 1)
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	// Re-encrypt with new key.
	newBlob, err := secret.ReEncrypt(blob, oldKey, newKey, 2)
	if err != nil {
		t.Fatalf("ReEncrypt() error: %v", err)
	}

	// Old key should no longer work.
	_, _, err = secret.Decrypt(newBlob, oldKey)
	if err == nil {
		t.Error("old key should not decrypt re-encrypted blob")
	}

	// New key should work.
	plaintext, version, err := secret.Decrypt(newBlob, newKey)
	if err != nil {
		t.Fatalf("Decrypt() with new key error: %v", err)
	}
	if string(plaintext) != string(original) {
		t.Errorf("plaintext = %q, want %q", string(plaintext), string(original))
	}
	if version != 2 {
		t.Errorf("key version = %d, want 2", version)
	}
}

func TestGenerateMasterKey_Uniqueness(t *testing.T) {
	keys := make([][]byte, 100)
	for i := range keys {
		var err error
		keys[i], err = secret.GenerateMasterKey()
		if err != nil {
			t.Fatalf("GenerateMasterKey() error: %v", err)
		}
	}

	// All keys should be unique.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if bytes.Equal(keys[i], keys[j]) {
				t.Fatalf("keys[%d] and keys[%d] are identical", i, j)
			}
		}
	}
}

func BenchmarkEncrypt(b *testing.B) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	plaintext := []byte("a]typical-secret-value-maybe-64-chars-long-1234567890abcdefghijklmn")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = secret.Encrypt(plaintext, key, 1)
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	plaintext := []byte("a-typical-secret-value-maybe-64-chars-long-1234567890abcdefghijklmn")
	blob, _ := secret.Encrypt(plaintext, key, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = secret.Decrypt(blob, key)
	}
}
