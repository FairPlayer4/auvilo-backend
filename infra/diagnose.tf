# Diagnose-Log aus der App (docs/diagnose-entwurf.md).
#
# Freigeschaltete Konten senden gzip-komprimierte JSON-Zeilen ohne Inhalte; der
# Server legt jede Sendung als eigenes Objekt ab (diagnose/<konto_id>/…). Nach
# 14 Tagen löscht Google sie (E-D-5). Unverschlüsselt, damit Kevin und Claude
# sie lesen können (werkzeuge/diagnose-holen.sh).

variable "diagnose_konten" {
  description = "Konto-IDs, die Diagnosen senden dürfen (E-D-3). Steht in werte.auto.tfvars; leer = niemand."
  type        = list(string)
  default     = []

  validation {
    # Konto-IDs sind 128 Bit hexadezimal (speicher.neueKontoID). Ein Tippfehler
    # fiele sonst erst auf, wenn die App mit „nicht freigeschaltet“ abgelehnt wird.
    condition     = alltrue([for k in var.diagnose_konten : can(regex("^[0-9a-f]{32}$", k))])
    error_message = "Eine Konto-ID in diagnose_konten hat nicht das Format 32 Zeichen 0-9a-f."
  }
}

resource "google_storage_bucket" "diagnose" {
  name                        = "${var.projekt}-diagnose"
  location                    = var.region
  uniform_bucket_level_access = true       # Rechte nur am Bucket, keine pro Objekt
  public_access_prevention    = "enforced" # nie öffentlich, auch nicht aus Versehen
  force_destroy               = true       # Diagnosen sind nach 14 Tagen ohnehin weg

  # Keine Versionierung: Der Server legt nur neue Objekte an (DoesNotExist) und
  # überschreibt nie etwas.

  # Kein Soft Delete: Was nach 14 Tagen gelöscht wird, soll wirklich weg sein
  # (sonst 7 Tage länger, DSGVO-Zweckbindung).
  soft_delete_policy {
    retention_duration_seconds = 0
  }

  # KRITISCH: Alles nach 14 Tagen löschen (E-D-5). Google wendet die Regel
  # verzögert an (bis etwa einen Tag).
  lifecycle_rule {
    condition {
      age = 14
    }
    action {
      type = "Delete"
    }
  }

  depends_on = [google_project_service.apis]
}

# Der Server darf nur anlegen, nicht lesen, überschreiben oder löschen: Eine
# Lücke im Server kann so keine Diagnosen anderer Konten verraten. Lesen tut
# Kevin über seine Projektrolle.
resource "google_storage_bucket_iam_member" "server_diagnose" {
  bucket = google_storage_bucket.diagnose.name
  role   = "roles/storage.objectCreator"
  member = google_service_account.server.member
}

# Tageszähler (server/internal/kontingent): Firestore löscht die Zähler nach
# Ablauf von laeuft_ab (zwei Tage nach dem gezählten Tag).
resource "google_firestore_field" "kontingente_ttl" {
  database   = google_firestore_database.standard.name
  collection = "kontingente"
  field      = "laeuft_ab"

  ttl_config {}

  # Leer = kein Index; abgefragt wird danach nie.
  index_config {}
}
