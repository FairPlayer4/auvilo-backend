package dienst

import (
	"bytes"
	"context"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

func (ts *testServer) backupSpeichern(token string, chiffrat []byte, erwartet int64) (*v1.BackupSpeichernResponse, error) {
	res, err := ts.backup.BackupSpeichern(context.Background(), mitSitzung(&v1.BackupSpeichernRequest{Chiffrat: chiffrat, ErwarteteVersion: erwartet}, token))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (ts *testServer) backupHolen(token string) (*v1.BackupHolenResponse, error) {
	res, err := ts.backup.BackupHolen(context.Background(), mitSitzung(&v1.BackupHolenRequest{}, token))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// Ablauf aus docs/b2-entwurf.md: Das erste Gerät legt das Backup an, das
// zweite Gerät desselben Kontos holt es.
func TestZweitesGeraetHoltDasBackupDesErsten(t *testing.T) {
	ts := neuerTestServer(t)
	handy, laptop := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	handySitzung := ts.anmelden(t, "google-kevin", handy).Sitzung.Token

	gespeichert, err := ts.backupSpeichern(handySitzung, []byte("chiffrat-1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if gespeichert.Version != 1 {
		t.Errorf("Version = %d, erwartet 1", gespeichert.Version)
	}

	laptopSitzung := ts.anmelden(t, "google-kevin", laptop).Sitzung.Token
	geholt, err := ts.backupHolen(laptopSitzung)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(geholt.Chiffrat, []byte("chiffrat-1")) || geholt.Version != 1 {
		t.Errorf("geholt = %q Version %d", geholt.Chiffrat, geholt.Version)
	}

	// Der Laptop ersetzt es mit der gesehenen Version.
	if v, err := ts.backupSpeichern(laptopSitzung, []byte("chiffrat-2"), 1); err != nil || v.Version != 2 {
		t.Errorf("Ersetzen: Version %v, Fehler %v", v, err)
	}
}

func TestBackupSpeichernPrueftDieAnfrage(t *testing.T) {
	ts := neuerTestServer(t)
	token := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token

	t.Run("nimmt ein Backup von genau 16 KiB an", func(t *testing.T) {
		if _, err := ts.backupSpeichern(token, make([]byte, MaxBackup), 0); err != nil {
			t.Fatal(err)
		}
	})

	faelle := []struct {
		name     string
		chiffrat []byte
		erwartet int64
	}{
		{"lehnt leeres Backup ab", nil, 1},
		{"lehnt Backup über 16 KiB ab", make([]byte, MaxBackup+1), 1},
		{"lehnt negative Version ab", []byte("chiffrat"), -1},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			_, err := ts.backupSpeichern(token, fall.chiffrat, fall.erwartet)
			erwarteCode(t, err, connect.CodeInvalidArgument)
		})
	}

	// KRITISCH: Ein Gerät mit veraltetem Stand überschreibt nichts.
	t.Run("lehnt veraltete Version ab und behält das gespeicherte Backup", func(t *testing.T) {
		_, err := ts.backupSpeichern(token, []byte("veraltet"), 0)
		erwarteCode(t, err, connect.CodeFailedPrecondition)
		geholt, err := ts.backupHolen(token)
		if err != nil {
			t.Fatal(err)
		}
		if len(geholt.Chiffrat) != MaxBackup || geholt.Version != 1 {
			t.Errorf("Backup verändert: %d Bytes, Version %d", len(geholt.Chiffrat), geholt.Version)
		}
	})
}

func TestBackupHolenOhneBackupMeldetNichtGefunden(t *testing.T) {
	ts := neuerTestServer(t)
	token := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token
	_, err := ts.backupHolen(token)
	erwarteCode(t, err, connect.CodeNotFound)
}

// KRITISCH: Jedes Konto sieht und schreibt nur sein eigenes Backup.
func TestBackupGehoertNurZumEigenenKonto(t *testing.T) {
	ts := neuerTestServer(t)
	kevin := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token
	andere := ts.anmelden(t, "google-andere", testhilfe.NeuesGeraet(t)).Sitzung.Token
	if _, err := ts.backupSpeichern(kevin, []byte("kevins-backup"), 0); err != nil {
		t.Fatal(err)
	}

	t.Run("anderes Konto bekommt Kevins Backup nicht", func(t *testing.T) {
		_, err := ts.backupHolen(andere)
		erwarteCode(t, err, connect.CodeNotFound)
	})
	t.Run("eigenes Backup des anderen Kontos lässt Kevins unberührt", func(t *testing.T) {
		if _, err := ts.backupSpeichern(andere, []byte("anderes-backup"), 0); err != nil {
			t.Fatal(err)
		}
		geholt, err := ts.backupHolen(kevin)
		if err != nil {
			t.Fatal(err)
		}
		if string(geholt.Chiffrat) != "kevins-backup" || geholt.Version != 1 {
			t.Errorf("Kevins Backup = %q Version %d", geholt.Chiffrat, geholt.Version)
		}
	})
}

func TestBackupVerlangtGueltigeSitzung(t *testing.T) {
	ts := neuerTestServer(t)
	handy, laptop := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	handySitzung := ts.anmelden(t, "google-kevin", handy).Sitzung.Token
	laptopSitzung := ts.anmelden(t, "google-kevin", laptop).Sitzung.Token
	if _, err := ts.backupSpeichern(laptopSitzung, []byte("chiffrat"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.geraete.GeraetSperren(context.Background(), mitSitzung(&v1.GeraetSperrenRequest{GeraetId: handy.ID()}, laptopSitzung)); err != nil {
		t.Fatal(err)
	}

	faelle := []struct {
		name  string
		token string
	}{
		{"ohne Sitzung", ""},
		{"mit erfundener Sitzung", "erfunden"},
		{"mit Sitzung eines gesperrten Geräts", handySitzung},
	}
	for _, fall := range faelle {
		t.Run("Speichern lehnt ab "+fall.name, func(t *testing.T) {
			_, err := ts.backupSpeichern(fall.token, []byte("chiffrat"), 1)
			erwarteCode(t, err, connect.CodeUnauthenticated)
		})
		t.Run("Holen lehnt ab "+fall.name, func(t *testing.T) {
			_, err := ts.backupHolen(fall.token)
			erwarteCode(t, err, connect.CodeUnauthenticated)
		})
	}
}
