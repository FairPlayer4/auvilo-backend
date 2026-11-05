# Etappe B2b: Ablage der Datensicherungen (nur Chiffrate, docs/b2b-entwurf.md).
#
# Ein Objekt je Konto (sicherung/<konto_id>). Jede neue Sicherung ersetzt
# das Objekt; die vorige bleibt als ältere Fassung erhalten (Versionierung) und
# schützt so vor einer fehlerhaften App, die kaputte Daten hochlädt (E-B2b-4).

resource "google_storage_bucket" "datensicherung" {
  name                        = "${var.projekt}-datensicherung"
  location                    = var.region
  uniform_bucket_level_access = true       # Rechte nur am Bucket, keine pro Objekt
  public_access_prevention    = "enforced" # nie öffentlich, auch nicht aus Versehen
  force_destroy               = false      # vor echten Daten: Ein Bucket mit Sicherungen lässt sich nicht löschen

  versioning {
    enabled = true
  }

  # Kein "Soft Delete": Löschen soll wirklich löschen (DSGVO Art. 17,
  # Sicherung-Löschen im Client). Vor Fehlern der App schützen die älteren
  # Fassungen; ein Soft Delete hielte zusätzlich jede gelöschte Fassung
  # 7 Tage vor und kostete Speicher.
  soft_delete_policy {
    retention_duration_seconds = 0
  }

  # Ältere Fassungen nach 7 Tagen löschen (E-B2b-4). Gezählt wird ab dem
  # Zeitpunkt, zu dem eine Fassung von einer neueren abgelöst wurde.
  lifecycle_rule {
    condition {
      days_since_noncurrent_time = 7
    }
    action {
      type = "Delete"
    }
  }

  # Höchstens 200 ältere Fassungen: Die Regel trifft eine ältere Fassung, sobald
  # es mindestens 201 neuere gibt (die aktuelle und 200 ältere). Die App sichert
  # höchstens stündlich (7 × 24 = 168) plus manuell; die Grenze greift also erst,
  # wenn mehr gesichert wird als vorgesehen. Begrenzt den
  # Dauerzustand, wenn eine App in einer Schleife sichert – keine harte Grenze:
  # Google wendet die Regeln verzögert an (bis etwa einen Tag), bis dahin bleibt
  # jede Fassung liegen.
  lifecycle_rule {
    condition {
      num_newer_versions = 201
    }
    action {
      type = "Delete"
    }
  }

  depends_on = [google_project_service.apis]
}

# Von den Diensten darf nur der Server lesen, schreiben und löschen, und nur in
# diesem Bucket. (Projekt-Owner wie Kevin haben über die Projektrollen ohnehin
# Zugriff; sie sehen nur Chiffrate.)
# objectUser statt objectAdmin: keine Rechte an den Zugriffsregeln selbst.
resource "google_storage_bucket_iam_member" "server_datensicherung" {
  bucket = google_storage_bucket.datensicherung.name
  role   = "roles/storage.objectUser"
  member = google_service_account.server.member
}
