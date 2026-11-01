# Etappe B1: der Go-Server (server/) als Cloud-Run-Dienst.
#
# OpenTofu legt den Dienst an und verwaltet alles außer dem Image: Das setzt
# der Deploy aus GitHub Actions (.github/workflows/deploy.yml) bei jedem Merge
# nach main. Beim ersten Anlegen läuft deshalb ein Platzhalter-Image.

# --- Dienstkonto -------------------------------------------------------------

# Unter diesem Konto läuft der Server. Es darf nur Firestore benutzen und genau
# ein Secret (den Pfeffer) lesen.
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

  depends_on = [google_project_service.apis]
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
  name                = "server"
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false # Lernprojekt: muss vollständig löschbar sein

  template {
    service_account                  = google_service_account.server.email
    timeout                          = "30s"
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
    }
  }

  lifecycle {
    # ingress: Der Kill-Switch stellt ihn auf "nur intern"; `tofu apply` darf
    # das nicht wieder öffnen. image: setzt der Deploy. client*: setzt gcloud.
    ignore_changes = [
      ingress,
      template[0].containers[0].image,
      client,
      client_version,
    ]
  }

  depends_on = [
    google_project_iam_member.server_firestore,
    google_secret_manager_secret_iam_member.server_pfeffer,
  ]
}

# Öffentlich aufrufbar: Wer anfragen darf, prüft der Server selbst (Google-
# Anmeldung, Sitzung). Sperrt der Kill-Switch den Ingress, hilft das nicht mehr.
resource "google_cloud_run_v2_service_iam_member" "server_oeffentlich" {
  location = google_cloud_run_v2_service.server.location
  name     = google_cloud_run_v2_service.server.name
  role     = "roles/run.invoker"
  member   = "allUsers"
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
