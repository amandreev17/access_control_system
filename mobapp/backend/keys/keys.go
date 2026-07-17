package keys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"log"
	"os"
	"path/filepath"
)

var (
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
)

const keySize = 2048

func Init() error {
	keyDir := filepath.Join(".", "keys")
	os.MkdirAll(keyDir, 0700)

	privPath := filepath.Join(keyDir, "private.pem")
	pubPath := filepath.Join(keyDir, "public.pem")

	// Try to load existing keys
	if _, err := os.Stat(privPath); err == nil {
		return loadKeys(privPath, pubPath)
	}

	// Generate new keys
	log.Println("Generating new RSA key pair...")
	privateKey, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return err
	}

	// Save private key
	privFile, err := os.Create(privPath)
	if err != nil {
		return err
	}
	defer privFile.Close()

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	if _, err := privFile.Write(privPEM); err != nil {
		return err
	}

	// Save public key
	pubFile, err := os.Create(pubPath)
	if err != nil {
		return err
	}
	defer pubFile.Close()

	pubASN1, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubASN1,
	})
	if _, err := pubFile.Write(pubPEM); err != nil {
		return err
	}

	PrivateKey = privateKey
	PublicKey = &privateKey.PublicKey
	log.Println("RSA key pair generated successfully")
	return nil
}

func loadKeys(privPath, pubPath string) error {
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return err
	}
	PrivateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return err
	}

	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return err
	}
	block, _ = pem.Decode(pubPEM)
	if block == nil {
		return err
	}
	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	PublicKey = pubInterface.(*rsa.PublicKey)

	log.Println("RSA keys loaded successfully")
	return nil
}

// SignData signs data with RSA private key and returns base64 encoded signature
func SignData(data string) (string, error) {
	hash := sha256.Sum256([]byte(data))
	signature, err := rsa.SignPKCS1v15(rand.Reader, PrivateKey, 0, hash[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// VerifySignature verifies the signature of data
func VerifySignature(data, signatureBase64 string) bool {
	hash := sha256.Sum256([]byte(data))
	sig, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false
	}
	err = rsa.VerifyPKCS1v15(PublicKey, 0, hash[:], sig)
	return err == nil
}

// GetPublicKeyPEM returns the public key in PEM format as a string
func GetPublicKeyPEM() string {
	pubASN1, err := x509.MarshalPKIXPublicKey(PublicKey)
	if err != nil {
		return ""
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubASN1,
	})
	return string(pubPEM)
}
