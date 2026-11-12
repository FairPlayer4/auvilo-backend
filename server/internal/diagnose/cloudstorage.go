package diagnose

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// CloudStorage legt jede Sendung als eigenes Objekt in einem Bucket ab. Der
// Bucket löscht sie nach 14 Tagen von selbst (infra/diagnose.tf); er hat keine
// Versionierung, Löschen entfernt also wirklich.
type CloudStorage struct {
	bucket *storage.BucketHandle
}

var _ Ablage = (*CloudStorage)(nil)

// NeuesCloudStorage nutzt den Bucket mit diesem Namen. Den Client teilt sich der
// Server mit der Datensicherung (datensicherung.NeuerClient).
func NeuesCloudStorage(c *storage.Client, bucket string) *CloudStorage {
	return &CloudStorage{bucket: c.Bucket(bucket)}
}

func (c *CloudStorage) Ablegen(ctx context.Context, kontoID string, jetzt time.Time, nutzlast []byte) (string, error) {
	if kontoID == "" {
		return "", errors.New("Konto-ID fehlt")
	}
	name := objektname(kontoID, jetzt)
	// KRITISCH: nur anlegen, nie überschreiben. Das Server-Konto darf ohnehin nur
	// anlegen (objectCreator); die Bedingung macht es auch ohne diese Regel sicher.
	w := c.bucket.Object(name).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	// Kein ContentEncoding "gzip": Sonst entpackte Cloud Storage beim Holen von
	// selbst, und die Datei auf dem Rechner wäre nicht mehr das, was die App schickte.
	w.ContentType = "application/gzip"
	w.ChunkSize = 0 // in einer Anfrage hochladen, ohne zusätzlichen Puffer
	if _, err := w.Write(nutzlast); err != nil {
		_ = w.Close() // der erste Fehler zählt
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return name, nil
}

// Loeschen listet die Namen im Ordner des Kontos und löscht jedes Objekt. Das
// Auflisten liefert nur Namen und Metadaten, nicht den Inhalt.
func (c *CloudStorage) Loeschen(ctx context.Context, kontoID string) error {
	if kontoID == "" {
		return errors.New("Konto-ID fehlt")
	}
	it := c.bucket.Objects(ctx, &storage.Query{Prefix: ordner(kontoID)})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return nil
		}
		if err != nil {
			return err
		}
		// Schon weg (z. B. Ablauf nach 14 Tagen dazwischen): kein Fehler.
		err = c.bucket.Object(attrs.Name).Delete(ctx)
		if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
			return err
		}
	}
}
