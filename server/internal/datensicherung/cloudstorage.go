package datensicherung

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

// CloudStorage legt jede Sicherung als ein Objekt "sicherung/<konto>" in einem
// Bucket ab. Der Bucket hat Versionierung: Überschreiben macht die alte Fassung
// zu einer "nicht aktuellen" Version, die der Lebenszyklus des Buckets nach
// einiger Zeit löscht (infra/).
//
// Die Version ist die Generation des Objekts. Cloud Storage prüft die erwartete
// Generation beim Schreiben selbst (ifGenerationMatch); zwei Geräte können sich
// so nicht gegenseitig überschreiben, ohne dass eins es merkt.
type CloudStorage struct {
	bucket *storage.BucketHandle
}

var _ Ablage = (*CloudStorage)(nil)

// NeuerClient verbindet sich mit Cloud Storage (im Betrieb mit dem Dienstkonto
// von Cloud Run; mit STORAGE_EMULATOR_HOST mit dem Nachbau fake-gcs-server).
// Server und Tests nutzen beide diesen Client: Lesen über die JSON-Schnittstelle
// statt über XML, weil der Nachbau Namen mit "/" nur dort richtig findet. Für
// Cloud Storage selbst sind beide Wege gleichwertig.
func NeuerClient(ctx context.Context) (*storage.Client, error) {
	return storage.NewClient(ctx, storage.WithJSONReads())
}

// NeuesCloudStorage nutzt den Bucket mit diesem Namen.
func NeuesCloudStorage(c *storage.Client, bucket string) *CloudStorage {
	return &CloudStorage{bucket: c.Bucket(bucket)}
}

// objektname: Die Konto-ID ist ein Pseudonym (128 Bit Zufall), kein Name und keine E-Mail.
func objektname(kontoID string) string { return "sicherung/" + kontoID }

func (c *CloudStorage) Speichern(ctx context.Context, kontoID string, chiffrat []byte, erwartet int64) (int64, error) {
	if kontoID == "" {
		return 0, errors.New("Konto-ID fehlt")
	}
	// KRITISCH: Bedingung für Cloud Storage. 0 heißt: nur anlegen, wenn es noch
	// kein Objekt gibt; sonst nur ersetzen, wenn die Generation noch stimmt.
	bedingung := storage.Conditions{GenerationMatch: erwartet}
	if erwartet == 0 {
		bedingung = storage.Conditions{DoesNotExist: true}
	}
	w := c.bucket.Object(objektname(kontoID)).If(bedingung).NewWriter(ctx)
	w.ContentType = "application/octet-stream"
	// ChunkSize 0: in einer Anfrage hochladen, ohne die Sicherung noch einmal
	// zwischenzuspeichern (sonst bis zu 16 MiB Puffer je Upload).
	w.ChunkSize = 0
	if _, err := w.Write(chiffrat); err != nil {
		_ = w.Close() // der erste Fehler zählt
		return 0, uebersetze(err)
	}
	if err := w.Close(); err != nil {
		return 0, uebersetze(err)
	}
	return w.Attrs().Generation, nil
}

func (c *CloudStorage) Holen(ctx context.Context, kontoID string, version int64) (Fassung, error) {
	// KRITISCH: Eine negative Generation hieße für die Bibliothek "keine Angabe"
	// und lieferte die neueste Fassung statt eines Fehlers.
	if version < 0 {
		return Fassung{}, ErrNichtGefunden
	}
	// Ohne Angabe einer Generation liest Cloud Storage die aktuelle Fassung; der
	// Reader bleibt bei genau dieser, auch wenn währenddessen jemand speichert.
	// Eine ältere Fassung gibt es nur unter dem Objekt dieses Kontos; die
	// Generation eines anderen Kontos findet Cloud Storage hier nicht.
	obj := c.bucket.Object(objektname(kontoID))
	if version > 0 {
		obj = obj.Generation(version)
	}
	r, err := obj.NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return Fassung{}, ErrNichtGefunden
	}
	if err != nil {
		return Fassung{}, err
	}
	defer func() { _ = r.Close() }() // nur gelesen; Fehler beim Schließen ist egal
	// Höchstens ein Byte mehr als erlaubt lesen: Ein zu großes Objekt (das es
	// nicht geben darf) soll den Speicher des Servers nicht füllen.
	chiffrat, err := io.ReadAll(io.LimitReader(r, MaxChiffrat+1))
	if err != nil {
		return Fassung{}, err
	}
	if len(chiffrat) > MaxChiffrat {
		return Fassung{}, fmt.Errorf("gespeicherte Sicherung größer als %d Bytes", MaxChiffrat)
	}
	return Fassung{Chiffrat: chiffrat, Version: r.Attrs.Generation}, nil
}

// Fassungen listet alle Generationen des Objekts, auch die älteren.
func (c *CloudStorage) Fassungen(ctx context.Context, kontoID string) ([]FassungsInfo, error) {
	name := objektname(kontoID)
	liste := []FassungsInfo{}
	it := c.bucket.Objects(ctx, &storage.Query{Prefix: name, Versions: true})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		// KRITISCH: Prefix passt auch auf längere Namen ("…/konto-10" zu
		// "…/konto-1"); deren Fassungen gehören einem anderen Konto.
		if attrs.Name != name {
			continue
		}
		// Created ist bei jeder Generation der Zeitpunkt ihres Speicherns.
		liste = append(liste, FassungsInfo{Version: attrs.Generation, Gespeichert: attrs.Created, Bytes: attrs.Size})
	}
	slices.SortFunc(liste, func(a, b FassungsInfo) int { return cmp.Compare(b.Version, a.Version) })
	return liste, nil
}

// Loeschen entfernt alle Generationen des Objekts, auch die älteren Fassungen
// (DSGVO Art. 17: Löschen heißt wirklich löschen). Ein normales Löschen würde in
// einem Bucket mit Versionierung nur die aktuelle Fassung zur älteren machen.
func (c *CloudStorage) Loeschen(ctx context.Context, kontoID string) error {
	if kontoID == "" {
		return errors.New("Konto-ID fehlt")
	}
	name := objektname(kontoID)
	it := c.bucket.Objects(ctx, &storage.Query{Prefix: name, Versions: true})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return nil
		}
		if err != nil {
			return err
		}
		// Prefix passt auch auf längere Namen ("…/konto-10" zu "…/konto-1"). Gelöscht
		// wird immer unter name, nie unter attrs.Name; die Abfrage spart nur
		// unnötige Aufrufe.
		if attrs.Name != name {
			continue
		}
		err = c.bucket.Object(name).Generation(attrs.Generation).Delete(ctx)
		if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
			return err
		}
	}
}

// uebersetze macht aus "Bedingung nicht erfüllt" (HTTP 412) ErrVersionVeraltet.
func uebersetze(err error) error {
	var g *googleapi.Error
	if errors.As(err, &g) && g.Code == http.StatusPreconditionFailed {
		return ErrVersionVeraltet
	}
	return err
}
