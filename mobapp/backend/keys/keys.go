package keys

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"os"
	"path/filepath"
)

var (
	// ECDSA P-256 — основная подпись QR-кодов.
	// Подпись фиксированной длины 64 байта (r||s) вместо 256 байт у RSA-2048,
	// поэтому токен короче в разы, а QR-код получается заметно менее плотным
	// и легче считывается камерой.
	ECPrivateKey *ecdsa.PrivateKey
	ECPublicKey  *ecdsa.PublicKey

	// RSA-ключи оставлены только для проверки QR, подписанных прежней версией.
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
)

const (
	ecPrivFile  = "ec_private.pem"
	ecPubFile   = "ec_public.pem"
	rsaPrivFile = "private.pem"
	rsaPubFile  = "public.pem"
)

func Init() error {
	keyDir := filepath.Join(".", "keys")
	os.MkdirAll(keyDir, 0700)

	if err := initECDSA(keyDir); err != nil {
		return err
	}

	// RSA-ключи нужны только для проверки подписей прежнего формата
	rsaPriv := filepath.Join(keyDir, rsaPrivFile)
	if _, err := os.Stat(rsaPriv); err == nil {
		if err := loadRSAKeys(rsaPriv, filepath.Join(keyDir, rsaPubFile)); err != nil {
			log.Printf("Warning: RSA keys not loaded (legacy QR signatures only): %v", err)
		}
	}
	return nil
}

// initECDSA создаёт или загружает ключ ECDSA P-256 (короткая подпись — lighter QR).
func initECDSA(keyDir string) error {
	privPath := filepath.Join(keyDir, ecPrivFile)
	pubPath := filepath.Join(keyDir, ecPubFile)

	if _, err := os.Stat(privPath); err == nil {
		privPEM, err := os.ReadFile(privPath)
		if err != nil {
			return err
		}
		block, _ := pem.Decode(privPEM)
		if block == nil {
			return errors.New("invalid PEM block in ECDSA private key")
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return err
		}
		ECPrivateKey = key
		ECPublicKey = &key.PublicKey
		log.Println("ECDSA P-256 keys loaded successfully")
		return nil
	}

	log.Println("Generating new ECDSA P-256 key pair...")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	privDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return err
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(pubPath, pubPEM, 0644); err != nil {
		return err
	}

	ECPrivateKey = key
	ECPublicKey = &key.PublicKey
	log.Println("ECDSA P-256 key pair generated successfully")
	return nil
}

// loadRSAKeys загружает RSA-пару (только для проверки старых подписей)
func loadRSAKeys(privPath, pubPath string) error {
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return errors.New("invalid PEM block in RSA private key")
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
		return errors.New("invalid PEM block in RSA public key")
	}
	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	rsaPub, ok := pubInterface.(*rsa.PublicKey)
	if !ok {
		return errors.New("RSA public key has unexpected type")
	}
	PublicKey = rsaPub

	log.Println("RSA keys loaded (legacy QR verification)")
	return nil
}

// SignData подписывает данные и возвращает base64-подпись.
// ECDSA P-256 + SHA-256: подпись фиксированной длины 64 байта (r||s),
// это в 4 раза короче RSA-2048, поэтому QR-код получается менее плотным.
func SignData(data string) (string, error) {
	hash := sha256.Sum256([]byte(data))
	r, s, err := ecdsa.Sign(rand.Reader, ECPrivateKey, hash[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifySignature проверяет подпись токена.
// Сначала ECDSA (новый формат, подпись 64 байта), затем RSA — для QR,
// выпущенных прежней версией бэкенда.
func VerifySignature(data, signatureBase64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false
	}
	hash := sha256.Sum256([]byte(data))

	if len(sig) == 64 && ECPublicKey != nil {
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		return ecdsa.Verify(ECPublicKey, hash[:], r, s)
	}

	// Обратная совместимость: подпись прошлого формата (RSA-2048, 256 байт)
	if PublicKey == nil {
		return false
	}
	return rsa.VerifyPKCS1v15(PublicKey, 0, hash[:], sig) == nil
}

// GetPublicKeyPEM returns the ECDSA P-256 public key in PEM format as a string
func GetPublicKeyPEM() string {
	pubASN1, err := x509.MarshalPKIXPublicKey(ECPublicKey)
	if err != nil {
		return ""
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubASN1,
	})
	return string(pubPEM)
}
