package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
)

// ablage legt Geräteschlüssel und Sitzungen in einem Ordner ab, standardmäßig
// %APPDATA%\avlreminder – also außerhalb des Repos. Je Gerätename gibt es:
//
//	geraet-<name>.pem    privater Geräteschlüssel (PKCS #8)
//	sitzung-<name>.json  zuletzt erhaltene Sitzung
//
// Mehrere Namen simulieren mehrere Geräte desselben Kontos.
type ablage struct {
	ordner string
}

// gueltigerName verhindert Pfade wie "../x" als Gerätename.
var gueltigerName = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func pruefeName(name string) error {
	if !gueltigerName.MatchString(name) {
		return fmt.Errorf("Gerätename %q ungültig: nur a-z, 0-9 und -, höchstens 32 Zeichen", name)
	}
	return nil
}

// geraet ist ein simuliertes Gerät mit seinem Schlüssel. In der App liegt der
// Schlüssel im Android Keystore; hier in einer Datei.
type geraet struct {
	privat *ecdsa.PrivateKey
	der    []byte // öffentlicher Schlüssel, PKIX/DER – so geht er an den Server
}

func (g *geraet) id() string { return geraeteschluessel.GeraetID(g.der) }

func (g *geraet) signiere(nachricht []byte) ([]byte, error) {
	summe := sha256.Sum256(nachricht)
	return ecdsa.SignASN1(rand.Reader, g.privat, summe[:])
}

// geraet lädt den Schlüssel des Geräts oder legt ihn beim ersten Mal an.
func (a ablage) geraet(name string) (*geraet, error) {
	if err := pruefeName(name); err != nil {
		return nil, err
	}
	pfad := filepath.Join(a.ordner, "geraet-"+name+".pem")
	daten, err := os.ReadFile(pfad) //nolint:gosec // Pfad aus geprüftem Namen
	if errors.Is(err, os.ErrNotExist) {
		return a.neuesGeraet(pfad)
	}
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(daten)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%s: kein PEM-Schlüssel", pfad)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pfad, err)
	}
	privat, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: kein ECDSA-Schlüssel", pfad)
	}
	return zuGeraet(privat)
}

func (a ablage) neuesGeraet(pfad string) (*geraet, error) {
	privat, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(privat)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(a.ordner, 0o700); err != nil {
		return nil, err
	}
	// 0600: nur der eigene Benutzer darf lesen (unter Windows wirkt das kaum;
	// dort schützt das Benutzerprofil).
	if err := os.WriteFile(pfad, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), 0o600); err != nil {
		return nil, err
	}
	return zuGeraet(privat)
}

func zuGeraet(privat *ecdsa.PrivateKey) (*geraet, error) {
	der, err := x509.MarshalPKIXPublicKey(&privat.PublicKey)
	if err != nil {
		return nil, err
	}
	// Der Server nimmt nur P-256; ein anderer Schlüssel fiele erst dort auf.
	if _, err := geraeteschluessel.Lese(der); err != nil {
		return nil, err
	}
	return &geraet{privat: privat, der: der}, nil
}

// gespeicherteSitzung ist, was nach Anmelden oder Erneuern auf der Platte liegt.
type gespeicherteSitzung struct {
	GeraetID string    `json:"geraet_id"`
	Token    string    `json:"token"`
	LaeuftAb time.Time `json:"laeuft_ab"`
}

func (a ablage) sitzungsPfad(name string) string {
	return filepath.Join(a.ordner, "sitzung-"+name+".json")
}

func (a ablage) speichereSitzung(name string, s gespeicherteSitzung) error {
	if err := pruefeName(name); err != nil {
		return err
	}
	daten, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.ordner, 0o700); err != nil {
		return err
	}
	return os.WriteFile(a.sitzungsPfad(name), daten, 0o600)
}

func (a ablage) sitzung(name string) (gespeicherteSitzung, error) {
	var s gespeicherteSitzung
	if err := pruefeName(name); err != nil {
		return s, err
	}
	daten, err := os.ReadFile(a.sitzungsPfad(name))
	if errors.Is(err, os.ErrNotExist) {
		return s, fmt.Errorf("keine Sitzung für Gerät %q – zuerst anmelden", name)
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(daten, &s)
}
