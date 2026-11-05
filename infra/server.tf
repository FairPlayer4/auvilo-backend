# Etappe B1: der Go-Server (server/) als Cloud-Run-Dienst.
#
# OpenTofu legt den Dienst an und verwaltet alles außer dem Image: Das setzt
# der Deploy aus GitHub Actions (.github/workflows/deploy.yml) bei jedem Merge
# nach main. Beim ersten Anlegen läuft deshalb ein Platzhalter-Image.

# --- Dienstkonto -------------------------------------------------------------

# Unter diesem Konto läuft der Server. Es darf nur Firestore benutzen, genau
# ein Secret (den Pfeffer) lesen, im Bucket der Datensicherungen schreiben
# (datensicherung.tf) und Diagnosen anlegen (diagnose.tf).
resource "google_service_account" "server" {
  account_id   = "server"
  display_name = "Server (Cloud Run)"
  depends_on   = [google_project_service.apis]
}

# Firestore-Rechte gibt es nur für das ganze Projekt; im Projekt gibt es aber
# nur die eine Datenbank.
resource "google_project_iam_member" "server_firestore" {
  project = var.projekt
  role    = "roles/datastore.user"
  member  = google_service_account.server.member
}

# --- Pfeffer -----------------------------------------------------------------
# Mit dem Pfeffer bildet der Server aus der Google-Konto-ID (sub) einen HMAC.
# Wer die Datenbank liest, kann so nicht auf Google-Konten schließen.

resource "google_secret_manager_secret" "pfeffer" {
  secret_id = "pfeffer"

  # "user_managed" mit einer Region: Ohne diese Angabe repliziert Google das
  # Secret weltweit (Anforderung D1).
  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  # Schutz außerhalb von OpenTofu: Wird eine Version zerstört (Konsole,
  # gcloud), bleibt sie 30 Tage wiederherstellbar. Ohne Pfeffer wären alle
  # Konten unauffindbar.
  version_destroy_ttl = "2592000s"

  depends_on = [google_project_service.apis]

  # KRITISCH: Das Secret zu ersetzen hieße, auch den Pfeffer zu ersetzen.
  lifecycle {
    prevent_destroy = true
  }
}

# Der Wert entsteht nur während `tofu apply` (ephemeral) und wird write-only
# übergeben: Er steht weder im Plan noch im Zustand im Bucket.
ephemeral "random_password" "pfeffer" {
  length  = 64
  special = false # nur Buchstaben und Ziffern: 64 Zeichen ≈ 380 Bit
}

resource "google_secret_manager_secret_version" "pfeffer" {
  secret                 = google_secret_manager_secret.pfeffer.id
  secret_data_wo         = ephemeral.random_password.pfeffer.result
  secret_data_wo_version = 1

  # KRITISCH: Ein neuer Pfeffer macht alle bestehenden Konten unauffindbar
  # (jede Kennung ergäbe einen anderen HMAC). Darum nie ersetzen; ein Wechsel
  # bräuchte eine Migration. secret_data_wo_version erhöhen hieße: neuer Wert.
  lifecycle {
    prevent_destroy = true
  }
}

# Nur dieses eine Secret, nicht alle Secrets im Projekt.
resource "google_secret_manager_secret_iam_member" "server_pfeffer" {
  secret_id = google_secret_manager_secret.pfeffer.id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.server.member
}

# --- Firestore: abgelaufene Sitzungen löschen --------------------------------

# Firestore löscht Dokumente in "sitzungen" (meist innerhalb von 24 h), sobald
# laeuft_ab vorbei ist. Der Server prüft den Ablauf trotzdem selbst.
resource "google_firestore_field" "sitzungen_ttl" {
  database   = google_firestore_database.standard.name
  collection = "sitzungen"
  field      = "laeuft_ab"

  ttl_config {}

  # Leer = kein Index auf diesem Feld. Abgefragt wird danach nie, und Indizes
  # auf Zeitstempeln kosten Schreibvorgänge.
  index_config {}
}

# --- Dienst ------------------------------------------------------------------

resource "google_cloud_run_v2_service" "server" {
  name     = "server"
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"
  # Vor echten Daten (b2b-entwurf.md): Weder `tofu destroy` noch die Konsole
  # löschen den Dienst, solange dieser Schutz an ist.
  deletion_protection = true

  template {
    service_account = google_service_account.server.email
    timeout         = "30s"
    # 80 Anfragen gleichzeitig passen in 256 MiB: Normale Anfragen haben
    # höchstens 64 KiB, Datensicherungen (bis 8 MiB) lässt der Server nur vier
    # gleichzeitig zu (dienst.StandardGleichzeitigeSicherungen, ~95 MiB).
    max_instance_request_concurrency = 80

    # Kosten vor Verfügbarkeit: 0 Instanzen im Leerlauf (Kaltstart ca. 1 s),
    # höchstens 2 begrenzen die Kosten bei Missbrauch bis zum Kill-Switch.
    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    containers {
      # Platzhalter nur fürs erste Anlegen; danach setzt der Deploy das Image.
      image = var.server_image_start

      resources {
        limits = {
          cpu    = "1"
          memory = "256Mi"
        }
        cpu_idle          = true # CPU nur während Anfragen (günstigste Abrechnung)
        startup_cpu_boost = false
      }

      env {
        name  = "PROJEKT"
        value = var.projekt
      }
      env {
        name  = "GOOGLE_CLIENT_IDS"
        value = join(",", var.google_client_ids)
      }
      env {
        # Feste Version statt "latest": Ein versehentlich neuer Wert im Secret
        # Manager wirkt so erst, wenn er hier eingetragen wird.
        name  = "PFEFFER_SECRET"
        value = google_secret_manager_secret_version.pfeffer.name
      }
      env {
        name  = "SICHERUNG_BUCKET"
        value = google_storage_bucket.datensicherung.name
      }
      env {
        name  = "DIAGNOSE_BUCKET"
        value = google_storage_bucket.diagnose.name
      }
      env {
        # Konto-IDs sind Pseudonyme, keine Geheimnisse.
        name  = "DIAGNOSE_KONTEN"
        value = join(",", var.diagnose_konten)
      }
      env {
        # Weiche Grenze für Go: Ab hier räumt der Garbage Collector häufiger auf,
        # statt dass Cloud Run die Instanz bei 256 MiB beendet. Gut 25 % Luft für
        # das, was Go nicht zählt (Stacks des Laufzeitsystems, Puffer).
        name  = "GOMEMLIMIT"
        value = "192MiB"
      }
    }
  }

  lifecycle {
    # image: setzt der Deploy. client*: setzt gcloud. Den Ingress fasst der
    # Kill-Switch nicht mehr an (er sperrt über die Invoker-Bindung unten).
    ignore_changes = [
      template[0].containers[0].image,
      client,
      client_version,
    ]
  }

  depends_on = [
    google_project_iam_member.server_firestore,
    google_secret_manager_secret_iam_member.server_pfeffer,
    google_storage_bucket_iam_member.server_datensicherung,
    google_storage_bucket_iam_member.server_diagnose,
  ]
}

# Öffentlich aufrufbar: Wer anfragen darf, prüft der Server selbst (Google-
# Anmeldung, Sitzung). Der Kill-Switch sperrt, indem er diese Bindung entfernt;
# das Modul sorgt dafür, dass `tofu apply` sie dann nicht einfach neu anlegt.
module "server_oeffentlich" {
  source     = "./module/oeffentlich"
  name       = google_cloud_run_v2_service.server.name
  location   = google_cloud_run_v2_service.server.location
  oeffnen    = var.gesperrte_dienste_oeffnen
  depends_on = [google_cloud_run_v2_service.server]
}

# Dieselbe Bindung wie bisher, nur jetzt im Modul: nicht löschen und neu anlegen.
moved {
  from = google_cloud_run_v2_service_iam_member.server_oeffentlich
  to   = module.server_oeffentlich.google_cloud_run_v2_service_iam_member.oeffentlich
}

# --- Deploy aus GitHub Actions -----------------------------------------------

# Das Deploy-Konto darf nur diesen einen Dienst ändern (neues Image) …
resource "google_cloud_run_v2_service_iam_member" "deploy_server" {
  location = google_cloud_run_v2_service.server.location
  name     = google_cloud_run_v2_service.server.name
  role     = "roles/run.developer"
  member   = google_service_account.deploy.member
}

# … und dabei nur das Server-Konto als Laufzeitkonto einsetzen (actAs).
resource "google_service_account_iam_member" "deploy_act_as_server" {
  service_account_id = google_service_account.server.name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.deploy.member
}
