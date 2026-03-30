package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Envelope encryption: each secret gets a random Data Encryption Key (DEK).
// The DEK encrypts the plaintext via AES-256-GCM. The DEK itself is then
// encrypted with the master key (also AES-256-GCM). The output blob contains
// both the encrypted DEK and the encrypted plaintext.
//
// Why envelope encryption:
//   - Rotating the master key only requires re-encrypting DEKs, not every secret
//   - Each secret has a unique DEK, limiting blast radius of any single key compromise
//   - Standard pattern used by AWS KMS, GCP KMS, Vault transit, etc.
//
// Blob format (binary):
//   [1 byte]  version (0x01)
//   [1 byte]  master key version (for future key rotation)
//   [2 bytes] encrypted DEK length (big-endian uint16)
//   [N bytes] encrypted DEK (AES-256-GCM: 12-byte nonce + ciphertext + 16-byte tag)
//   [rest]    encrypted plaintext (AES-256-GCM: 12-byte nonce + ciphertext + 16-byte tag)

const (
	blobVersion    = 0x01
	dekSize        = 32 // AES-256
	gcmNonceSize   = 12
	gcmOverhead    = gcmNonceSize + 16 // nonce + tag
	headerSize     = 4                  // version + key version + 2-byte dek length
)

// Encrypt encrypts plaintext using envelope encryption with the given master key.
// The masterKey must be exactly 32 bytes (AES-256).
// masterKeyVersion is stored in the blob header for key rotation tracking.
func Encrypt(plaintext []byte, masterKey []byte, masterKeyVersion byte) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("secret: master key must be exactly 32 bytes")
	}

	// Generate a random DEK.
	dek := make([]byte, dekSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("secret: failed to generate DEK: %w", err)
	}

	// Encrypt the DEK with the master key.
	encryptedDEK, err := aesGCMEncrypt(masterKey, dek)
	if err != nil {
		return nil, fmt.Errorf("secret: failed to encrypt DEK: %w", err)
	}

	// Encrypt the plaintext with the DEK.
	encryptedData, err := aesGCMEncrypt(dek, plaintext)
	if err != nil {
		return nil, fmt.Errorf("secret: failed to encrypt plaintext: %w", err)
	}

	// Zero the DEK from memory.
	for i := range dek {
		dek[i] = 0
	}

	// Assemble the blob.
	blob := make([]byte, headerSize+len(encryptedDEK)+len(encryptedData))
	blob[0] = blobVersion
	blob[1] = masterKeyVersion
	binary.BigEndian.PutUint16(blob[2:4], uint16(len(encryptedDEK)))
	copy(blob[headerSize:], encryptedDEK)
	copy(blob[headerSize+len(encryptedDEK):], encryptedData)

	return blob, nil
}

// Decrypt decrypts a blob produced by Encrypt.
// Returns the plaintext and the master key version that was used to encrypt.
func Decrypt(blob []byte, masterKey []byte) (plaintext []byte, masterKeyVersion byte, err error) {
	if len(masterKey) != 32 {
		return nil, 0, errors.New("secret: master key must be exactly 32 bytes")
	}

	if len(blob) < headerSize {
		return nil, 0, errors.New("secret: blob too short")
	}

	version := blob[0]
	if version != blobVersion {
		return nil, 0, fmt.Errorf("secret: unsupported blob version %d", version)
	}

	masterKeyVersion = blob[1]
	dekLen := int(binary.BigEndian.Uint16(blob[2:4]))

	if len(blob) < headerSize+dekLen {
		return nil, 0, errors.New("secret: blob truncated (DEK section)")
	}

	encryptedDEK := blob[headerSize : headerSize+dekLen]
	encryptedData := blob[headerSize+dekLen:]

	if len(encryptedData) == 0 {
		return nil, 0, errors.New("secret: blob truncated (data section)")
	}

	// Decrypt the DEK.
	dek, err := aesGCMDecrypt(masterKey, encryptedDEK)
	if err != nil {
		return nil, 0, fmt.Errorf("secret: failed to decrypt DEK (wrong master key?): %w", err)
	}
	defer func() {
		for i := range dek {
			dek[i] = 0
		}
	}()

	// Decrypt the plaintext.
	plaintext, err = aesGCMDecrypt(dek, encryptedData)
	if err != nil {
		return nil, 0, fmt.Errorf("secret: failed to decrypt data: %w", err)
	}

	return plaintext, masterKeyVersion, nil
}

// ReEncrypt decrypts a blob with the old master key and re-encrypts it with
// the new master key. Used during master key rotation.
func ReEncrypt(blob []byte, oldMasterKey, newMasterKey []byte, newKeyVersion byte) ([]byte, error) {
	plaintext, _, err := Decrypt(blob, oldMasterKey)
	if err != nil {
		return nil, fmt.Errorf("secret: re-encrypt failed during decrypt: %w", err)
	}
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()

	return Encrypt(plaintext, newMasterKey, newKeyVersion)
}

// GenerateMasterKey generates a cryptographically random 32-byte master key.
func GenerateMasterKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("secret: failed to generate master key: %w", err)
	}
	return key, nil
}

// aesGCMEncrypt encrypts data with AES-256-GCM. Returns nonce + ciphertext + tag.
func aesGCMEncrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Seal appends ciphertext+tag after nonce.
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// aesGCMDecrypt decrypts data produced by aesGCMEncrypt.
func aesGCMDecrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, data, nil)
}
