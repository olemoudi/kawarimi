package vault

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"filippo.io/age"
	"github.com/olemoudi/kawarimi/internal/crypto"
)

const (
	HeaderFile    = "vault_header.json"
	HeaderVersion = 2
)

// SlotType identifies the type of credential a slot accepts.
type SlotType string

const (
	SlotTypeMnemonic SlotType = "mnemonic"
	SlotTypeOwner    SlotType = "owner"
	SlotTypeRecovery SlotType = "recovery"
)

// Slot represents a single key-wrapping slot in the vault header.
type Slot struct {
	ID                 int              `json:"id"`
	Type               SlotType         `json:"type"`
	KDF                string           `json:"kdf"`
	KDFParams          crypto.Argon2Params `json:"kdf_params"`
	Salt               []byte           `json:"salt"`
	EncryptedMasterKey []byte           `json:"encrypted_master_key"`
	Nonce              []byte           `json:"nonce"`
	// Owner slot fields
	DeviceID      string `json:"device_id,omitempty"`
	DeviceKeySalt []byte `json:"device_key_salt,omitempty"`
	// Recovery code encrypted by MK, stored only on recovery slot
	EncryptedRecoveryCode []byte `json:"encrypted_recovery_code,omitempty"`
	RecoveryCodeNonce     []byte `json:"recovery_code_nonce,omitempty"`
}

// Header is the vault key management structure stored as vault_header.json.
type Header struct {
	Version              int    `json:"version"`
	Slots                []Slot `json:"slots"`
	EncryptedAgeIdentity []byte `json:"encrypted_age_identity"`
	IdentityNonce        []byte `json:"identity_nonce"`
	AgeRecipient         string `json:"age_recipient"`
	HeaderHMAC           []byte `json:"header_hmac"`
}

// InitParams holds the inputs for creating a new vault header.
type InitParams struct {
	Password     string
	DeviceID     string
}

// InitResult holds the outputs from creating a new vault header.
type InitResult struct {
	Header       *Header
	MnemonicWords []string
	RecoveryCode []byte
	DeviceKey    []byte
	MasterKey    []byte // caller must zero after use
	AgeIdentity  string // caller must zero after use
}

// NewHeader creates a new vault header with all three slot types.
func NewHeader(params InitParams) (*InitResult, error) {
	// 1. Generate master key
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		return nil, fmt.Errorf("generating master key: %w", err)
	}

	// 2. Generate age X25519 identity
	ageIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generating age identity: %w", err)
	}
	identityStr := ageIdentity.String()
	recipientStr := ageIdentity.Recipient().String()

	// 3. Encrypt age identity with MK
	encIdentity, identityNonce, err := crypto.WrapKey(masterKey, []byte(identityStr))
	if err != nil {
		return nil, fmt.Errorf("encrypting age identity: %w", err)
	}

	// 4. Generate mnemonic
	mnemonicWords, mnemonicEntropy, err := crypto.GenerateMnemonic()
	if err != nil {
		return nil, fmt.Errorf("generating mnemonic: %w", err)
	}

	// 5. Generate device key
	deviceKey, err := crypto.GenerateDeviceKey()
	if err != nil {
		return nil, fmt.Errorf("generating device key: %w", err)
	}

	// 6. Generate recovery code (128 bits)
	recoveryCode := make([]byte, 16)
	if _, err := rand.Read(recoveryCode); err != nil {
		return nil, fmt.Errorf("generating recovery code: %w", err)
	}

	// 7. Create mnemonic slot (slot 0)
	mnemonicSlot, err := createMnemonicSlot(0, mnemonicEntropy, masterKey)
	if err != nil {
		return nil, fmt.Errorf("creating mnemonic slot: %w", err)
	}

	// 8. Create owner slot (slot 1)
	ownerSlot, err := createOwnerSlot(1, params.Password, deviceKey, params.DeviceID, masterKey)
	if err != nil {
		return nil, fmt.Errorf("creating owner slot: %w", err)
	}

	// 9. Create recovery slot (slot 2) with encrypted recovery code
	recoverySlot, err := createRecoverySlot(2, params.Password, recoveryCode, masterKey)
	if err != nil {
		return nil, fmt.Errorf("creating recovery slot: %w", err)
	}

	header := &Header{
		Version:              HeaderVersion,
		Slots:                []Slot{*mnemonicSlot, *ownerSlot, *recoverySlot},
		EncryptedAgeIdentity: encIdentity,
		IdentityNonce:        identityNonce,
		AgeRecipient:         recipientStr,
	}

	// 10. Compute HMAC
	if err := header.computeHMAC(masterKey); err != nil {
		return nil, fmt.Errorf("computing header HMAC: %w", err)
	}

	return &InitResult{
		Header:        header,
		MnemonicWords: mnemonicWords,
		RecoveryCode:  recoveryCode,
		DeviceKey:     deviceKey,
		MasterKey:     masterKey,
		AgeIdentity:   identityStr,
	}, nil
}

// OpenWithMnemonic opens the vault header using a mnemonic.
func (h *Header) OpenWithMnemonic(mnemonicWords []string) (masterKey []byte, ageIdentity string, err error) {
	entropy, err := crypto.DecodeMnemonic(mnemonicWords)
	if err != nil {
		return nil, "", fmt.Errorf("decoding mnemonic: %w", err)
	}
	defer crypto.ZeroBytes(entropy)

	for _, slot := range h.Slots {
		if slot.Type != SlotTypeMnemonic {
			continue
		}

		slotKey, err := crypto.DeriveKey(entropy, slot.Salt, slot.KDFParams)
		if err != nil {
			return nil, "", fmt.Errorf("deriving mnemonic slot key: %w", err)
		}
		defer crypto.ZeroBytes(slotKey)

		mk, err := crypto.UnwrapKey(slotKey, slot.EncryptedMasterKey, slot.Nonce)
		if err != nil {
			continue // try next mnemonic slot if multiple exist
		}

		if err := h.verifyHMAC(mk); err != nil {
			crypto.ZeroBytes(mk)
			return nil, "", fmt.Errorf("header integrity check failed: %w", err)
		}

		identity, err := h.decryptAgeIdentity(mk)
		if err != nil {
			crypto.ZeroBytes(mk)
			return nil, "", fmt.Errorf("decrypting age identity: %w", err)
		}

		return mk, identity, nil
	}

	return nil, "", fmt.Errorf("no mnemonic slot could decrypt the vault (wrong mnemonic?)")
}

// OpenWithOwner opens the vault header using password + device key.
func (h *Header) OpenWithOwner(password string, deviceKey []byte) (masterKey []byte, ageIdentity string, err error) {
	for _, slot := range h.Slots {
		if slot.Type != SlotTypeOwner {
			continue
		}

		pwDerived, err := crypto.DeriveKey([]byte(password), slot.Salt, slot.KDFParams)
		if err != nil {
			return nil, "", fmt.Errorf("deriving password key: %w", err)
		}

		slotKey, err := crypto.DeriveOwnerSlotKey(pwDerived, deviceKey, slot.DeviceKeySalt)
		crypto.ZeroBytes(pwDerived)
		if err != nil {
			return nil, "", fmt.Errorf("deriving owner slot key: %w", err)
		}

		mk, err := crypto.UnwrapKey(slotKey, slot.EncryptedMasterKey, slot.Nonce)
		crypto.ZeroBytes(slotKey)
		if err != nil {
			continue // try next owner slot (different device)
		}

		if err := h.verifyHMAC(mk); err != nil {
			crypto.ZeroBytes(mk)
			return nil, "", fmt.Errorf("header integrity check failed: %w", err)
		}

		identity, err := h.decryptAgeIdentity(mk)
		if err != nil {
			crypto.ZeroBytes(mk)
			return nil, "", fmt.Errorf("decrypting age identity: %w", err)
		}

		return mk, identity, nil
	}

	return nil, "", fmt.Errorf("no owner slot could decrypt the vault (wrong password or device key?)")
}

// OpenWithRecovery opens the vault header using password + recovery code.
func (h *Header) OpenWithRecovery(password string, recoveryCode []byte) (masterKey []byte, ageIdentity string, err error) {
	for _, slot := range h.Slots {
		if slot.Type != SlotTypeRecovery {
			continue
		}

		input := append([]byte(password), recoveryCode...)
		slotKey, err := crypto.DeriveKey(input, slot.Salt, slot.KDFParams)
		crypto.ZeroBytes(input)
		if err != nil {
			return nil, "", fmt.Errorf("deriving recovery slot key: %w", err)
		}

		mk, err := crypto.UnwrapKey(slotKey, slot.EncryptedMasterKey, slot.Nonce)
		crypto.ZeroBytes(slotKey)
		if err != nil {
			continue
		}

		if err := h.verifyHMAC(mk); err != nil {
			crypto.ZeroBytes(mk)
			return nil, "", fmt.Errorf("header integrity check failed: %w", err)
		}

		identity, err := h.decryptAgeIdentity(mk)
		if err != nil {
			crypto.ZeroBytes(mk)
			return nil, "", fmt.Errorf("decrypting age identity: %w", err)
		}

		return mk, identity, nil
	}

	return nil, "", fmt.Errorf("no recovery slot could decrypt the vault (wrong password or recovery code?)")
}

// GetEncryptedRecoveryCode returns the encrypted recovery code from the recovery slot.
// The caller must decrypt it with the master key.
func (h *Header) GetEncryptedRecoveryCode() (ciphertext, nonce []byte, err error) {
	for _, slot := range h.Slots {
		if slot.Type == SlotTypeRecovery && len(slot.EncryptedRecoveryCode) > 0 {
			return slot.EncryptedRecoveryCode, slot.RecoveryCodeNonce, nil
		}
	}
	return nil, nil, fmt.Errorf("no recovery slot with encrypted recovery code found")
}

// UpdateOwnerSlot re-wraps the master key with a new password + device key for a given device.
func (h *Header) UpdateOwnerSlot(deviceID, newPassword string, deviceKey, masterKey []byte) error {
	for i, slot := range h.Slots {
		if slot.Type == SlotTypeOwner && slot.DeviceID == deviceID {
			newSlot, err := createOwnerSlot(slot.ID, newPassword, deviceKey, deviceID, masterKey)
			if err != nil {
				return fmt.Errorf("creating updated owner slot: %w", err)
			}
			h.Slots[i] = *newSlot
			return h.computeHMAC(masterKey)
		}
	}
	return fmt.Errorf("no owner slot found for device %q", deviceID)
}

// UpdateRecoverySlot re-wraps the master key with a new password + recovery code.
func (h *Header) UpdateRecoverySlot(newPassword string, recoveryCode, masterKey []byte) error {
	for i, slot := range h.Slots {
		if slot.Type == SlotTypeRecovery {
			newSlot, err := createRecoverySlot(slot.ID, newPassword, recoveryCode, masterKey)
			if err != nil {
				return fmt.Errorf("creating updated recovery slot: %w", err)
			}
			h.Slots[i] = *newSlot
			return h.computeHMAC(masterKey)
		}
	}
	return fmt.Errorf("no recovery slot found")
}

// AddOwnerSlot adds a new owner slot for an additional device.
func (h *Header) AddOwnerSlot(password string, deviceKey []byte, deviceID string, masterKey []byte) error {
	nextID := 0
	for _, slot := range h.Slots {
		if slot.ID >= nextID {
			nextID = slot.ID + 1
		}
	}

	newSlot, err := createOwnerSlot(nextID, password, deviceKey, deviceID, masterKey)
	if err != nil {
		return fmt.Errorf("creating owner slot: %w", err)
	}

	h.Slots = append(h.Slots, *newSlot)
	return h.computeHMAC(masterKey)
}

// SaveHeader writes the header to disk.
func SaveHeader(vaultDir string, h *Header) error {
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling header: %w", err)
	}
	path := filepath.Join(vaultDir, HeaderFile)
	return os.WriteFile(path, data, 0600)
}

// LoadHeader reads the header from disk.
func LoadHeader(vaultDir string) (*Header, error) {
	path := filepath.Join(vaultDir, HeaderFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	var h Header
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("parsing header: %w", err)
	}

	if h.Version != HeaderVersion {
		return nil, fmt.Errorf("unsupported vault header version: %d (expected %d)", h.Version, HeaderVersion)
	}

	return &h, nil
}

// --- Internal helpers ---

func createMnemonicSlot(id int, mnemonicEntropy, masterKey []byte) (*Slot, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}

	params := crypto.MnemonicSlotParams()
	slotKey, err := crypto.DeriveKey(mnemonicEntropy, salt, params)
	if err != nil {
		return nil, fmt.Errorf("deriving key: %w", err)
	}
	defer crypto.ZeroBytes(slotKey)

	encMK, nonce, err := crypto.WrapKey(slotKey, masterKey)
	if err != nil {
		return nil, fmt.Errorf("wrapping master key: %w", err)
	}

	return &Slot{
		ID:                 id,
		Type:               SlotTypeMnemonic,
		KDF:                "argon2id",
		KDFParams:          params,
		Salt:               salt,
		EncryptedMasterKey: encMK,
		Nonce:              nonce,
	}, nil
}

func createOwnerSlot(id int, password string, deviceKey []byte, deviceID string, masterKey []byte) (*Slot, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}
	dkSalt := make([]byte, 32)
	if _, err := rand.Read(dkSalt); err != nil {
		return nil, fmt.Errorf("generating device key salt: %w", err)
	}

	params := crypto.OwnerSlotParams()
	pwDerived, err := crypto.DeriveKey([]byte(password), salt, params)
	if err != nil {
		return nil, fmt.Errorf("deriving password key: %w", err)
	}
	defer crypto.ZeroBytes(pwDerived)

	slotKey, err := crypto.DeriveOwnerSlotKey(pwDerived, deviceKey, dkSalt)
	if err != nil {
		return nil, fmt.Errorf("deriving owner slot key: %w", err)
	}
	defer crypto.ZeroBytes(slotKey)

	encMK, nonce, err := crypto.WrapKey(slotKey, masterKey)
	if err != nil {
		return nil, fmt.Errorf("wrapping master key: %w", err)
	}

	return &Slot{
		ID:                 id,
		Type:               SlotTypeOwner,
		KDF:                "argon2id",
		KDFParams:          params,
		Salt:               salt,
		EncryptedMasterKey: encMK,
		Nonce:              nonce,
		DeviceID:           deviceID,
		DeviceKeySalt:      dkSalt,
	}, nil
}

func createRecoverySlot(id int, password string, recoveryCode, masterKey []byte) (*Slot, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}

	params := crypto.OwnerSlotParams()
	input := append([]byte(password), recoveryCode...)
	slotKey, err := crypto.DeriveKey(input, salt, params)
	crypto.ZeroBytes(input)
	if err != nil {
		return nil, fmt.Errorf("deriving key: %w", err)
	}
	defer crypto.ZeroBytes(slotKey)

	encMK, nonce, err := crypto.WrapKey(slotKey, masterKey)
	if err != nil {
		return nil, fmt.Errorf("wrapping master key: %w", err)
	}

	// Encrypt the recovery code with MK for password-change flow
	encRecoveryCode, rcNonce, err := crypto.WrapKey(masterKey, recoveryCode)
	if err != nil {
		return nil, fmt.Errorf("encrypting recovery code: %w", err)
	}

	return &Slot{
		ID:                    id,
		Type:                  SlotTypeRecovery,
		KDF:                   "argon2id",
		KDFParams:             params,
		Salt:                  salt,
		EncryptedMasterKey:    encMK,
		Nonce:                 nonce,
		EncryptedRecoveryCode: encRecoveryCode,
		RecoveryCodeNonce:     rcNonce,
	}, nil
}

func (h *Header) decryptAgeIdentity(masterKey []byte) (string, error) {
	identityBytes, err := crypto.UnwrapKey(masterKey, h.EncryptedAgeIdentity, h.IdentityNonce)
	if err != nil {
		return "", fmt.Errorf("unwrapping age identity: %w", err)
	}
	return string(identityBytes), nil
}

// headerForHMAC returns a copy of the header with HeaderHMAC cleared for HMAC computation.
// This avoids mutating the original header, making the operation safe for concurrent use.
func (h *Header) headerForHMAC() ([]byte, error) {
	clone := *h
	clone.HeaderHMAC = nil
	// Deep copy slots to avoid aliasing
	clone.Slots = make([]Slot, len(h.Slots))
	copy(clone.Slots, h.Slots)
	return json.Marshal(&clone)
}

// computeHMAC computes the HMAC-SHA256 of the header (excluding the HMAC field itself).
func (h *Header) computeHMAC(masterKey []byte) error {
	data, err := h.headerForHMAC()
	if err != nil {
		return fmt.Errorf("marshaling header for HMAC: %w", err)
	}

	mac := hmac.New(sha256.New, masterKey)
	mac.Write(data)
	h.HeaderHMAC = mac.Sum(nil)
	return nil
}

// verifyHMAC verifies the header HMAC with the given master key.
func (h *Header) verifyHMAC(masterKey []byte) error {
	data, err := h.headerForHMAC()
	if err != nil {
		return fmt.Errorf("marshaling header for HMAC: %w", err)
	}

	mac := hmac.New(sha256.New, masterKey)
	mac.Write(data)
	computed := mac.Sum(nil)

	if !hmac.Equal(computed, h.HeaderHMAC) {
		return fmt.Errorf("HMAC verification failed")
	}
	return nil
}

// EncryptWithIdentity encrypts data using the vault's age X25519 recipient.
func EncryptWithIdentity(plaintext []byte, recipientStr string) ([]byte, error) {
	recipient, err := age.ParseX25519Recipient(recipientStr)
	if err != nil {
		return nil, fmt.Errorf("parsing age recipient: %w", err)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("initializing age encryption: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("writing encrypted data: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("finalizing encryption: %w", err)
	}

	return buf.Bytes(), nil
}

// DecryptWithIdentity decrypts data using an age X25519 identity string.
func DecryptWithIdentity(ciphertext []byte, identityStr string) ([]byte, error) {
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return nil, fmt.Errorf("parsing age identity: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading decrypted data: %w", err)
	}

	return plaintext, nil
}
