package crypto

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"

	"github.com/olemoudi/kawarimi/internal/atomicfile"
)

// DeviceKeyFile represents the encrypted device key stored on disk.
type DeviceKeyFile struct {
	Version      int          `json:"version"`
	KDF          string       `json:"kdf"`
	KDFParams    Argon2Params `json:"kdf_params"`
	Salt         []byte       `json:"salt"`
	Nonce        []byte       `json:"nonce"`
	EncryptedKey []byte       `json:"encrypted_key"`
}

// GenerateDeviceKey creates a new random 32-byte device key.
func GenerateDeviceKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating device key: %w", err)
	}
	return key, nil
}

// EncryptDeviceKey encrypts a device key with a password for storage at rest.
func EncryptDeviceKey(deviceKey []byte, password string) (*DeviceKeyFile, error) {
	return EncryptDeviceKeyWithParams(deviceKey, password, DeviceKeyParams())
}

// EncryptDeviceKeyWithParams encrypts a device key with a password using the given KDF params.
func EncryptDeviceKeyWithParams(deviceKey []byte, password string, params Argon2Params) (*DeviceKeyFile, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}
	wrappingKey, err := DeriveKey([]byte(password), salt, params)
	if err != nil {
		return nil, fmt.Errorf("deriving wrapping key: %w", err)
	}
	defer ZeroBytes(wrappingKey)

	ciphertext, nonce, err := WrapKey(wrappingKey, deviceKey)
	if err != nil {
		return nil, fmt.Errorf("encrypting device key: %w", err)
	}

	return &DeviceKeyFile{
		Version:      1,
		KDF:          "argon2id",
		KDFParams:    params,
		Salt:         salt,
		Nonce:        nonce,
		EncryptedKey: ciphertext,
	}, nil
}

// DecryptDeviceKey decrypts an encrypted device key file with a password.
func DecryptDeviceKey(dkf *DeviceKeyFile, password string) ([]byte, error) {
	if dkf.Version != 1 {
		return nil, fmt.Errorf("unsupported device key version: %d", dkf.Version)
	}

	wrappingKey, err := DeriveKey([]byte(password), dkf.Salt, dkf.KDFParams)
	if err != nil {
		return nil, fmt.Errorf("deriving wrapping key: %w", err)
	}
	defer ZeroBytes(wrappingKey)

	deviceKey, err := UnwrapKey(wrappingKey, dkf.EncryptedKey, dkf.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decrypting device key (wrong password?): %w", err)
	}

	return deviceKey, nil
}

// SaveDeviceKeyFile writes an encrypted device key to disk with 0600 permissions.
func SaveDeviceKeyFile(path string, dkf *DeviceKeyFile) error {
	data, err := json.MarshalIndent(dkf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling device key file: %w", err)
	}
	return atomicfile.WriteFile(path, data, 0600)
}

// LoadDeviceKeyFile reads an encrypted device key from disk.
func LoadDeviceKeyFile(path string) (*DeviceKeyFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading device key file: %w", err)
	}

	var dkf DeviceKeyFile
	if err := json.Unmarshal(data, &dkf); err != nil {
		return nil, fmt.Errorf("parsing device key file: %w", err)
	}

	return &dkf, nil
}
